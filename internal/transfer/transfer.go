package transfer

import (
	"context"
	"fmt"
	"io"
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

	// We only need to check if something has been imported. Just by looking at the filesystem we
	// can't determine if a transfer has been imported and removed or hasn't been downloaded.
	// This avoids downloading a tranfer that has already been imported. In case there is a download,
	// but it wasn't (completely) imported, we will attempt a (partial) download. Files that have
	// been completed downloading will be skipped.
	// transfers, err := o.dc.GetTaggedTorrents(ctx, o.label)
	// if err != nil {
	// 	return fmt.Errorf("failed to get tagged torrents: %w", err)
	// }

	// for _, transfer := range transfers {
	// 	if !transfer.IsDownloadable() {
	// 		logger.Debug("skipping transfer because it's not a downloadable transfer", "transfer_id", transfer.ID, "status", transfer.Status)

	// 		continue
	// 	}

	// 	// we will check if the transfer has been imported by radarr or sonarr
	// 	if o.repo.IsImported(ctx, transfer.ID) {
	// 		logger.Debug("transfer not imported yet", "transfer_id", transfer.ID, "status", transfer.Status)

	// 		o.OnTransferImported <- transfer

	// 		continue
	// 	}
	// }
	// logger.Info("done checking for unfinished transfers. Starting to monitor transfers.", "count", len(transfers))

	ticker := time.NewTicker(o.pollingInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("shutting down transfer orchestrator")

				ticker.Stop()

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
		if !transfer.IsAvailable() || !transfer.IsDownloadable() {
			logger.Debug("skipping transfer because it's not available or not downloadable", "transfer_id", transfer.ID, "status", transfer.Status)

			continue
		}

		claimed, err := o.repo.ClaimTransfer(transfer.ID)
		if err != nil {
			return fmt.Errorf("failed to claim transfer: %w", err)
		}

		if !claimed {
			logger.Debug("transfer already claimed", "transfer_id", transfer.ID)

			continue
		}

		logger.Info("transfer ready for download", "transfer_id", transfer.ID)

		o.OnDownloadQueued <- transfer
	}

	return nil
}
