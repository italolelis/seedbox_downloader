package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/dustin/go-humanize"
	"github.com/italolelis/seedbox_downloader/internal/dc/putio"
	"github.com/italolelis/seedbox_downloader/internal/downloader/progress"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/svc/arr"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"golang.org/x/sync/errgroup"
)

// MissingTransferEvent carries a missing transfer and its classification.
type MissingTransferEvent struct {
	Transfer    *transfer.Transfer
	MissingType string // "files_missing" or "transfer_removed"
}

const (
	dirPerm = 0755
)

// ErrSizeMismatch reports that a file was written whose byte count does not match
// the size the seedbox reported for it. Distinct from a network failure: the copy
// completed without error, and the shortfall is the only evidence anything is
// wrong -- so a transfer must never be considered downloaded on the strength of a
// clean copy alone.
var ErrSizeMismatch = errors.New("downloaded file size does not match the size reported by the seedbox")

type Downloader struct {
	downloadDir string
	dc          transfer.DownloadClient
	tc          transfer.TransferClient
	arrServices []*arr.Client
	maxParallel int

	// Event channels. These are deliberately never closed: several goroutines
	// send on them, so no single goroutine can correctly own closing them.
	// Context cancellation stops the producers and the channels are collected.
	OnTransferDownloadError    chan *transfer.Transfer
	OnTransferDownloadFinished chan *transfer.Transfer
	OnTransferImported         chan *transfer.Transfer
	OnTransferMissing          chan MissingTransferEvent
	// OnTransferCleanedUp fires once a watched transfer is gone from the seedbox,
	// whether this process removed it or something else did first.
	OnTransferCleanedUp chan *transfer.Transfer
}

func NewDownloader(
	downloadDir string,
	maxParallel int,
	dc transfer.DownloadClient,
	tc transfer.TransferClient,
	arrServices []*arr.Client,
) *Downloader {
	return &Downloader{
		downloadDir:                downloadDir,
		dc:                         dc,
		maxParallel:                maxParallel,
		tc:                         tc,
		arrServices:                arrServices,
		OnTransferDownloadError:    make(chan *transfer.Transfer),
		OnTransferDownloadFinished: make(chan *transfer.Transfer),
		OnTransferImported:         make(chan *transfer.Transfer),
		OnTransferMissing:          make(chan MissingTransferEvent),
		OnTransferCleanedUp:        make(chan *transfer.Transfer),
	}
}

// WatchDownloads watches for transfers and downloads them.
func (d *Downloader) WatchDownloads(ctx context.Context, incomingTransfers <-chan *transfer.Transfer) {
	logger := logctx.LoggerFromContext(ctx)

	logger.InfoContext(ctx, "watching downloads")

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.InfoContext(ctx, "shutting down downloader")

				return
			case transfer := <-incomingTransfers:
				logger.DebugContext(ctx, "downloading transfer", "transfer_id", transfer.ID, "transfer_name", transfer.Name)

				downloadedFiles, err := d.DownloadTransfer(ctx, transfer)
				if err != nil {
					if errors.Is(err, putio.ErrTransferNotFound) {
						logger.WarnContext(ctx, "transfer removed from Put.io", "transfer_id", transfer.ID, "transfer_name", transfer.Name)
						d.OnTransferMissing <- MissingTransferEvent{Transfer: transfer, MissingType: "transfer_removed"}

						continue
					}

					if errors.Is(err, putio.ErrTransferFilesNotFound) {
						// Warn log already emitted inside DownloadTransfer for the specific file
						d.OnTransferMissing <- MissingTransferEvent{Transfer: transfer, MissingType: "files_missing"}

						continue
					}

					logger.ErrorContext(ctx, "failed to download transfer", "download_id", transfer.ID, "err", err)

					d.OnTransferDownloadError <- transfer

					continue
				}

				if downloadedFiles > 0 {
					logger.InfoContext(ctx, "downloads completed", "download_id", transfer.ID, "transfer_name", transfer.Name)

					d.OnTransferDownloadFinished <- transfer
				}
			}
		}
	}()
}

// DownloadTransfer downloads a transfer and returns the number of files downloaded.
func (d *Downloader) DownloadTransfer(ctx context.Context, transfer *transfer.Transfer) (int, error) {
	var downloadedFiles int32

	wg, ctx := errgroup.WithContext(ctx)

	if len(transfer.Files) == 0 {
		return 0, fmt.Errorf("transfer %s has no files (transfer removed from Put.io): %w", transfer.Name, putio.ErrTransferNotFound)
	}

	logger := logctx.LoggerFromContext(ctx)

	logger.InfoContext(ctx, "starting download",
		"transfer_id", transfer.ID,
		"transfer_name", transfer.Name,
		"file_count", len(transfer.Files))

	sem := make(chan struct{}, d.maxParallel)

	for i := range transfer.Files {
		file := transfer.Files[i]
		sem <- struct{}{}

		wg.Go(func() error {
			defer func() { <-sem }() // release the slot

			targetPath := filepath.Join(d.downloadDir, transfer.LocalSubdir(), file.Path)
			if err := d.DownloadFile(ctx, transfer.ID, file, targetPath); err != nil {
				return d.classifyFileError(ctx, logger, transfer, file, err)
			}

			atomic.AddInt32(&downloadedFiles, 1)

			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return 0, fmt.Errorf("failed to download files: %w", err)
	}

	return int(downloadedFiles), nil
}

func (d *Downloader) DownloadFile(ctx context.Context, transferID string, file *transfer.File, targetPath string) error {
	logger := logctx.LoggerFromContext(ctx).With("transfer_id", transferID)

	fileReader, err := d.dc.GrabFile(ctx, file)
	if err != nil {
		return fmt.Errorf("failed to grab file: %w", err)
	}

	defer fileReader.Close()

	if err := d.ensureTargetDir(ctx, targetPath, logger); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}

	// The error is returned, not published. A per-file event channel used to be
	// sent to here with nothing receiving from it, which blocked this goroutine
	// forever -- taking the errgroup, the whole transfer, and every subsequent
	// transfer down with it. The error group already carries this up to the
	// transfer level, which is the only level anything acts on.
	written, err := d.writeFile(ctx, out, fileReader, file.Path, targetPath, file.Size)
	if err != nil {
		out.Close()

		return fmt.Errorf("failed to download file: %w", err)
	}

	// Closed explicitly rather than deferred, because its error matters: bytes are
	// not durable until close returns, so a failed final flush -- the classic full
	// disk -- is a failed download, not a successful one.
	if err := out.Close(); err != nil {
		logger.ErrorContext(ctx, "failed to close downloaded file",
			"target", targetPath, "written_bytes", written, "err", err)

		return fmt.Errorf("failed to close target file %s: %w", targetPath, err)
	}

	// The seedbox told us how big this file is, so a mismatch is decisive: it
	// catches truncation, a body that ended early, and content that was never the
	// file at all. Without it, any of those is reported as a successful download.
	if file.Size > 0 && written != file.Size {
		logger.ErrorContext(ctx, "downloaded file is not the size the seedbox reported",
			"target", targetPath,
			"expected_bytes", file.Size,
			"written_bytes", written,
			"missing_bytes", file.Size-written)

		// This specific file is known-bad, so it does not stay on disk under a
		// media file's name. Other files in the same transfer are left alone.
		if rmErr := os.Remove(targetPath); rmErr != nil {
			logger.WarnContext(ctx, "failed to remove short file", "target", targetPath, "err", rmErr)
		}

		return fmt.Errorf("%w: %s expected %d bytes, wrote %d", ErrSizeMismatch, file.Path, file.Size, written)
	}

	logger.DebugContext(ctx, "file downloaded", "target", targetPath, "written_bytes", written)

	return nil
}

func (d *Downloader) WatchForImported(ctx context.Context, t *transfer.Transfer, pollingInterval time.Duration) {
	logger := logctx.LoggerFromContext(ctx)

	logger.InfoContext(ctx, "watching for imported transfers", "transfer_id", t.ID, "polling_interval", pollingInterval)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorContext(ctx, "watch imported panic",
					"operation", "watch_imported",
					"transfer_id", t.ID,
					"panic", r,
					"stack", string(debug.Stack()))
			}
		}()

		ticker := time.NewTicker(pollingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.InfoContext(ctx, "watch imported shutdown",
					"operation", "watch_imported",
					"transfer_id", t.ID,
					"reason", "context_cancelled")

				return
			case <-ticker.C:
				imported, err := d.checkForImported(ctx, t)
				if err != nil {
					logger.ErrorContext(ctx, "failed to check for imported transfer", "transfer_id", t.ID, "err", err)

					continue
				}

				if imported {
					logger.InfoContext(ctx, "transfer imported, stopping watch",
						"operation", "watch_imported",
						"transfer_id", t.ID,
						"reason", "transfer_imported")
					publish(ctx, d.OnTransferImported, t)

					return
				}
			}
		}
	}()
}

// CleanupTransfer removes the Put.io transfer and its file data with exponential backoff retry.
// If the transfer is not found on Put.io (already deleted), this is treated as success.
// A failure after retries is returned so the caller can leave the transfer marked as
// still needing cleanup, rather than forgetting it with its files still on Put.io.
func (d *Downloader) CleanupTransfer(ctx context.Context, t *transfer.Transfer) error {
	logger := logctx.LoggerFromContext(ctx)

	_, err := backoff.Retry[struct{}](ctx, func() (struct{}, error) {
		if err := d.tc.RemoveTransfers(ctx, []string{t.HashString()}, true); err != nil {
			if strings.Contains(err.Error(), "transfer not found") {
				logger.InfoContext(ctx, "Put.io transfer already removed, treating as success",
					"transfer_id", t.ID, "transfer_name", t.Name)

				return struct{}{}, nil
			}

			return struct{}{}, err
		}

		return struct{}{}, nil
	}, backoff.WithMaxTries(3))

	if err != nil {
		logger.ErrorContext(ctx, "failed to clean up Put.io transfer after retries",
			"transfer_id", t.ID, "err", err)

		return fmt.Errorf("failed to clean up transfer %s: %w", t.ID, err)
	}

	logger.InfoContext(ctx, "Put.io transfer and files cleaned up",
		"transfer_id", t.ID, "transfer_name", t.Name)

	return nil
}

// SeedPolicy says when an imported transfer has seeded enough to be removed from
// the seedbox: whichever target is reached first. A zero target is disabled.
type SeedPolicy struct {
	// Ratio is the upload ratio to reach.
	Ratio float64
	// Time is how long to seed for. Most private trackers accept this in place
	// of a ratio, and it is the only way an unpopular torrent ever finishes.
	Time time.Duration
}

// Enabled reports whether imported transfers are kept seeding at all, rather
// than removed as soon as they are imported.
func (p SeedPolicy) Enabled() bool {
	return p.Ratio > 0 || p.Time > 0
}

// satisfiedBy reports whether a transfer seeding like this can be removed, and
// why, for the log.
func (p SeedPolicy) satisfiedBy(s transfer.SeedingState) (string, bool) {
	if p.Ratio > 0 && s.Ratio >= p.Ratio {
		return "seed ratio reached", true
	}

	if p.Time > 0 && time.Duration(s.SecondsSeeding)*time.Second >= p.Time {
		return "seed time reached", true
	}

	// Once the seedbox stops seeding, neither number grows again: a target not
	// reached by now never will be, and keeping the transfer does nothing for the
	// tracker. The final ratio is in the log line this reason goes into.
	if s.StoppedSeeding {
		return "seedbox stopped seeding", true
	}

	return "", false
}

// WatchForSeeding watches until the Put.io transfer satisfies the seed policy, then cleans it up.
func (d *Downloader) WatchForSeeding(ctx context.Context, t *transfer.Transfer, pollingInterval time.Duration, policy SeedPolicy) {
	logger := logctx.LoggerFromContext(ctx).With("operation", "watch_seeding", "transfer_id", t.ID)

	logger.InfoContext(ctx, "watching for seeding transfers",
		"polling_interval", pollingInterval, "seed_ratio", policy.Ratio, "seed_time", policy.Time)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorContext(ctx, "watch seeding panic",
					"panic", r,
					"stack", string(debug.Stack()))
			}
		}()

		ticker := time.NewTicker(pollingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.InfoContext(ctx, "watch seeding shutdown", "reason", "context_cancelled")

				return
			case <-ticker.C:
				if d.checkSeeding(ctx, logger, t, policy) {
					return
				}
			}
		}
	}()
}

// checkSeeding makes one seeding check and reports whether the watch is over.
func (d *Downloader) checkSeeding(ctx context.Context, logger *slog.Logger, t *transfer.Transfer, policy SeedPolicy) bool {
	infoer, ok := d.dc.(transfer.TransferInfoer)
	if !ok {
		logger.ErrorContext(ctx, "download client does not support transfer info, cannot watch seeding")

		return true
	}

	state, found, err := infoer.GetTransferInfo(ctx, t.ID)
	if err != nil {
		logger.ErrorContext(ctx, "failed to get transfer info, retrying next tick", "err", err)

		return false
	}

	if !found {
		logger.InfoContext(ctx, "transfer no longer exists on Put.io, cleanup already done")

		publish(ctx, d.OnTransferCleanedUp, t)

		return true
	}

	logger = logger.With(
		"upload_ratio", state.Ratio,
		"target_ratio", policy.Ratio,
		"seconds_seeding", state.SecondsSeeding,
		"target_seed_time", policy.Time)

	reason, done := policy.satisfiedBy(state)
	if !done {
		logger.DebugContext(ctx, "seed policy not yet satisfied")

		return false
	}

	logger.InfoContext(ctx, reason+", cleaning up transfer")

	// A failed cleanup keeps the watch alive: the policy is still satisfied on
	// the next tick, so it is simply tried again.
	if err := d.CleanupTransfer(ctx, t); err != nil {
		return false
	}

	publish(ctx, d.OnTransferCleanedUp, t)

	return true
}

// publish sends v on ch unless ctx is cancelled first. Shutdown stops the
// receiver, so a plain send from a watch that fires during shutdown would block
// its goroutine forever.
func publish[T any](ctx context.Context, ch chan<- T, v T) {
	select {
	case ch <- v:
	case <-ctx.Done():
	}
}

// checkForImported reports whether an *arr application has imported this transfer,
// and removes the local copy once one has.
//
// One query per *arr application per transfer. It used to be one per file per
// application -- a ten-file season pack with Sonarr and Radarr configured meant
// twenty full history walks every tick.
func (d *Downloader) checkForImported(ctx context.Context, t *transfer.Transfer) (bool, error) {
	logger := logctx.LoggerFromContext(ctx)
	logger.DebugContext(ctx, "checking if transfer has been imported", "transfer_id", t.ID, "transfer_name", t.Name)

	for _, arrService := range d.arrServices {
		imported, err := arrService.CheckImported(ctx, t.HashString())
		if err != nil {
			return false, fmt.Errorf("failed to check if transfer has been imported: %w", err)
		}

		if !imported {
			continue
		}

		logger.InfoContext(ctx, "transfer has been imported", "transfer_id", t.ID, "transfer_name", t.Name)

		// Cleanup failure is logged, not returned. Reporting "not imported" because
		// a delete failed would leave this transfer being polled forever and block
		// the seeding and remote-cleanup steps that follow, none of which the failed
		// delete has anything to do with.
		d.removeLocalOutput(ctx, logger, t)

		return true, nil
	}

	return false, nil
}

// classifyFileError turns a per-file failure into the error the transfer level
// acts on. Two conditions are distinguished from a plain failure: content already
// on disk, and a transfer whose data has been deleted from the seedbox -- the
// latter is reported as missing rather than retried forever.
func (d *Downloader) classifyFileError(
	ctx context.Context, logger *slog.Logger, t *transfer.Transfer, file *transfer.File, err error,
) error {
	if errors.Is(err, storage.ErrDownloaded) {
		logger.DebugContext(ctx, "file already downloaded", "download_id", t.ID, "file_path", file.Path)

		return err
	}

	if errors.Is(err, putio.ErrTransferFilesNotFound) {
		logger.WarnContext(ctx, "transfer files missing from Put.io",
			"transfer_id", t.ID, "transfer_name", t.Name, "file_path", file.Path)
		d.removeLocalOutput(ctx, logger, t)

		return fmt.Errorf("file %s missing from Put.io: %w", file.Path, putio.ErrTransferFilesNotFound)
	}

	logger.ErrorContext(ctx, "failed to download file",
		"download_id", t.ID, "file_path", file.Path, "err", err)

	return err
}

// removeLocalOutput deletes what was written for a transfer, addressed by its
// category subdirectory plus the name derived from its file paths -- the same two
// components the RPC advertises, so this can only ever delete what was written. Addressing it by transfer name removed the wrong
// path -- or nothing at all -- whenever the two disagreed, which is the same
// divergence that stopped imports working.
func (d *Downloader) removeLocalOutput(ctx context.Context, logger *slog.Logger, t *transfer.Transfer) {
	name, derived := t.LocalName()
	if !derived {
		logger.WarnContext(ctx, "cannot locate local output to remove, leaving it in place",
			"transfer_id", t.ID, "transfer_name", t.Name)

		return
	}

	if err := os.RemoveAll(filepath.Join(d.downloadDir, t.LocalSubdir(), name)); err != nil {
		logger.WarnContext(ctx, "failed to remove partial transfer output",
			"transfer_id", t.ID, "local_name", name, "err", err)
	}
}

func (d *Downloader) ensureTargetDir(ctx context.Context, targetPath string, logger *slog.Logger) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		logger.ErrorContext(ctx, "failed to create target directory", "dir", dir, "err", err)

		return fmt.Errorf("failed to create target directory: %w", err)
	}

	return nil
}

// writeFile copies reader into out, reporting how many bytes were written so the
// caller can check that against the size the seedbox promised.
func (d *Downloader) writeFile(ctx context.Context, out *os.File, reader io.Reader, url, targetPath string, totalBytes int64) (int64, error) {
	logger := logctx.LoggerFromContext(ctx)

	logger.DebugContext(ctx, "downloading file", "file_path", targetPath, "file_size", humanize.Bytes(uint64(totalBytes)))

	progressInterval := int64(100 * 1024 * 1024) // 100MB
	progressCb := func(written int64, total int64) {
		if total > 0 {
			logger.DebugContext(ctx, "download progress",
				"url", url,
				"downloaded", humanize.Bytes(uint64(written)),
				"total", humanize.Bytes(uint64(total)),
				"percent", humanize.FtoaWithDigits(float64(written)*100/float64(total), 2))
		} else {
			logger.DebugContext(ctx, "download progress", "url", url, "downloaded", humanize.Bytes(uint64(written)))
		}
	}
	pr := progress.NewReader(reader, totalBytes, progressInterval, progressCb)

	written, err := io.Copy(out, pr)
	if err != nil {
		return written, fmt.Errorf("failed to copy file: %w", err)
	}

	return written, nil
}
