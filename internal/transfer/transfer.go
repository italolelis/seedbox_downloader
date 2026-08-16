package transfer

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
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
	ID    string
	Label string
	Name  string
	// RemoteFolder is where the transfer lives on the seedbox -- a Put.io folder
	// or a Deluge save path. It is meaningless to an *arr app and must never be
	// advertised as a local path.
	RemoteFolder       string
	Progress           float64
	Downloaded         int64
	DownloadSpeed      int64
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

// LocalName returns the name of the single filesystem entry -- a file for a
// single-file transfer, a folder for a multi-file one -- that holds this
// transfer's content directly beneath the local root.
//
// Joined onto the local root it yields both the path written to disk and the
// path advertised over RPC. Deriving both from this one function is what stops
// them diverging: the local root itself is configuration, so the name is the
// only part that has to be worked out.
//
// It is derived from file paths, never from Name. The seedbox may store content
// under a different name than the transfer carries -- notably when it appends a
// collision suffix because the name was already taken on the account -- and the
// transfer name cannot carry a suffix that was assigned after it.
//
// The returned bool reports whether the name was derived. It is false when the
// file list is empty (an in-progress transfer, whose files are not yet known) or
// when the paths share no common root; in both cases Name is returned as a
// fallback and the caller should log that it was used.
func (t *Transfer) LocalName() (string, bool) {
	if len(t.Files) == 0 {
		return t.Name, false
	}

	// A single file with no separator sits directly beneath the local root, so it
	// is its own entry -- this is what Transmission does with a single-file
	// torrent, and no enclosing folder is invented for it.
	if len(t.Files) == 1 {
		if p := cleanRelPath(t.Files[0].Path); p != "" && !strings.Contains(p, "/") {
			return p, true
		}
	}

	// Otherwise every file must live beneath one shared leading segment, which is
	// the folder the seedbox stored the transfer under.
	root := leadingSegment(t.Files[0].Path)
	if root == "" {
		return t.Name, false
	}

	for _, f := range t.Files[1:] {
		if leadingSegment(f.Path) != root {
			return t.Name, false
		}
	}

	return root, true
}

// cleanRelPath normalises a file path to slash-separated and strips any leading
// separator, so segment comparisons don't depend on the host's path style.
func cleanRelPath(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(p), "/")
}

// leadingSegment returns the first path segment of p, or "" when p has no
// separator -- meaning it is a bare name rather than something inside a folder.
func leadingSegment(p string) string {
	p = cleanRelPath(p)

	i := strings.Index(p, "/")
	if i <= 0 {
		return ""
	}

	return p[:i]
}

type TransferOrchestrator struct {
	repo            storage.DownloadRepository
	dc              DownloadClient
	label           string
	pollingInterval time.Duration

	// OnDownloadQueued is deliberately never closed: context cancellation stops
	// the producer, and closing a channel from a goroutine that also sends on it
	// is how shutdown panics happen.
	OnDownloadQueued chan *Transfer
}

func NewTransferOrchestrator(repo storage.DownloadRepository, dc DownloadClient, label string, pollingInterval time.Duration) *TransferOrchestrator {
	return &TransferOrchestrator{
		repo:            repo,
		dc:              dc,
		label:           label,
		pollingInterval: pollingInterval,

		OnDownloadQueued: make(chan *Transfer),
	}
}

func (o *TransferOrchestrator) ProduceTransfers(ctx context.Context) {
	logger := logctx.LoggerFromContext(ctx)

	logger.InfoContext(ctx, "checking unfinished transfers", "label", o.label)

	go func() {
		// Panic recovery (deferred last, executes first during unwind)
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorContext(ctx, "transfer orchestrator panic",
					"operation", "produce_transfers",
					"panic", r,
					"stack", string(debug.Stack()))

				// Restart with clean state if context not cancelled
				if ctx.Err() == nil {
					logger.InfoContext(ctx, "restarting transfer orchestrator after panic",
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
				logger.InfoContext(ctx, "transfer orchestrator shutdown",
					"operation", "produce_transfers",
					"reason", "context_cancelled")

				return
			case <-ticker.C:
				if err := o.watchTransfers(ctx); err != nil {
					logger.ErrorContext(ctx, "failed to watch transfers", "err", err)
				}
			}
		}
	}()
}

func (o *TransferOrchestrator) watchTransfers(ctx context.Context) error {
	logger := logctx.LoggerFromContext(ctx)

	logger.DebugContext(ctx, "polling for transfers", "label", o.label)

	transfers, err := o.dc.GetTaggedTorrents(ctx, o.label)
	if err != nil {
		return fmt.Errorf("failed to get tagged torrents: %w", err)
	}

	if len(transfers) > 0 {
		logger.InfoContext(ctx, "transfers found", "count", len(transfers))
	} else {
		logger.DebugContext(ctx, "no transfers found")
	}

	for _, transfer := range transfers {
		transferLogger := logger.With("transfer_id", transfer.ID, "status", transfer.Status)

		if !transfer.IsAvailable() || !transfer.IsDownloadable() {
			transferLogger.DebugContext(ctx, "skipping transfer because it's not available or not downloadable")

			continue
		}

		claimed, err := o.repo.ClaimTransfer(transfer.ID)
		if err != nil {
			if err == storage.ErrDownloaded {
				transferLogger.DebugContext(ctx, "skipping transfer because it's already downloaded")

				continue
			}

			return fmt.Errorf("failed to claim transfer: %w", err)
		}

		if !claimed {
			transferLogger.DebugContext(ctx, "skipping transfer because it's already claimed")

			continue
		}

		transferLogger.InfoContext(ctx, "transfer ready for download")

		o.OnDownloadQueued <- transfer
	}

	return nil
}
