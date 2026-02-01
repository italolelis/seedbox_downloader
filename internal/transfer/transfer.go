package transfer

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
)

type DownloadClient interface {
	Authenticate(ctx context.Context) error
	GetTaggedTorrents(ctx context.Context, label string) ([]*Transfer, error)
	GrabFile(ctx context.Context, file *File) (io.ReadCloser, error)
}

type TransferClient interface {
	AddTransfer(ctx context.Context, url string, downloadDir string) (*Transfer, error)
	AddTransferByBytes(ctx context.Context, torrentBytes []byte, filename string, downloadDir string) (*Transfer, error)
	RemoveTransfers(ctx context.Context, transferIDs []string, deleteLocalData bool) error
}

type Transfer struct {
	ID                 string
	Label              string
	Name               string
	SavePath           string
	Progress           float64
	Downloaded         int64
	ErrorMessage       string
	EstimatedTime      int64
	PeersConnected     int64
	PeersGettingFromUs int64
	PeersSendingToUs   int64
	SecondsSeeding     int64
	Size               int64
	Source             string
	Status             string
	Files              []*File
}

type File struct {
	ID   int64
	Path string
	Size int64
}

func (t *Transfer) IsSeeding() bool {
	return t.Status == "seeding" || t.Status == "seedingwait"
}

func (t *Transfer) IsDownloadable() bool {
	return len(t.Files) > 0
}

func (t *Transfer) IsAvailable() bool {
	status := strings.ToLower(t.Status)

	return status == "completed" || status == "seeding" || status == "seedingwait" || status == "finished"
}

type TransferOrchestrator struct {
	repo            storage.DownloadRepository
	dc              DownloadClient
	label           string
	pollingInterval time.Duration

	OnDownloadQueued   chan *Transfer
	OnTransferImported chan *Transfer
}

func NewTransferOrchestrator(repo storage.DownloadRepository, dc DownloadClient, label string, pollingInterval time.Duration) *TransferOrchestrator {
	return &TransferOrchestrator{
		repo:            repo,
		dc:              dc,
		label:           label,
		pollingInterval: pollingInterval,

		OnDownloadQueued:   make(chan *Transfer),
		OnTransferImported: make(chan *Transfer),
	}
}

func (o *TransferOrchestrator) Close() {
	close(o.OnDownloadQueued)
	close(o.OnTransferImported)
}

func (o *TransferOrchestrator) ProduceTransfers(ctx context.Context) {
	logger := logctx.LoggerFromContext(ctx)

	logger.Info("checking unfinished transfers", "label", o.label)

	go func() {
		// Panic recovery (deferred last, executes first during unwind)
		defer func() {
			if r := recover(); r != nil {
				logger.Error("transfer orchestrator panic",
					"operation", "produce_transfers",
					"panic", r,
					"stack", string(debug.Stack()))

				// Restart with clean state if context not cancelled
				if ctx.Err() == nil {
					logger.Info("restarting transfer orchestrator after panic",
						"operation", "produce_transfers")
					time.Sleep(time.Second) // Brief backoff before restart
					o.ProduceTransfers(ctx)
				}
			}
		}()

		// Ticker with cleanup (deferred second, executes second during unwind)
		ticker := time.NewTicker(o.pollingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("transfer orchestrator shutdown",
					"operation", "produce_transfers",
					"reason", "context_cancelled")
				return
			case <-ticker.C:
				if err := o.watchTransfers(ctx); err != nil {
					logger.Error("failed to watch transfers", "err", err)
				}
			}
		}
	}()
}

func (o *TransferOrchestrator) watchTransfers(ctx context.Context) error {
	logger := logctx.LoggerFromContext(ctx)

	logger.Info("watching transfers", "label", o.label)

	transfers, err := o.dc.GetTaggedTorrents(ctx, o.label)
	if err != nil {
		return fmt.Errorf("failed to get tagged torrents: %w", err)
	}

	logger.Info("active transfers", "transfer_count", len(transfers))

	for _, transfer := range transfers {
		transferLogger := logger.With("transfer_id", transfer.ID, "status", transfer.Status)

		if !transfer.IsAvailable() || !transfer.IsDownloadable() {
			transferLogger.Debug("skipping transfer because it's not available or not downloadable")

			continue
		}

		claimed, err := o.repo.ClaimTransfer(transfer.ID)
		if err != nil {
			if err == storage.ErrDownloaded {
				transferLogger.Debug("skipping transfer because it's already downloaded")

				continue
			}

			return fmt.Errorf("failed to claim transfer: %w", err)
		}

		if !claimed {
			transferLogger.Debug("skipping transfer because it's already claimed")

			continue
		}

		transferLogger.Info("transfer ready for download")

		o.OnDownloadQueued <- transfer
	}

	return nil
}
