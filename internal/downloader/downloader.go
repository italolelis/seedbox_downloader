package downloader

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/italolelis/seedbox_downloader/internal/dc/putio"
	"github.com/italolelis/seedbox_downloader/internal/downloader/progress"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/svc/arr"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"golang.org/x/sync/errgroup"
)

const (
	dirPerm = 0755
)

type Downloader struct {
	downloadDir string
	dc          transfer.DownloadClient
	tc          transfer.TransferClient
	arrServices []*arr.Client
	maxParallel int

	OnFileDownloadError        chan *transfer.File
	OnTransferDownloadError    chan *transfer.Transfer
	OnTransferDownloadFinished chan *transfer.Transfer
	OnTransferImported         chan *transfer.Transfer
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
		OnFileDownloadError:        make(chan *transfer.File),
		OnTransferDownloadError:    make(chan *transfer.Transfer),
		OnTransferDownloadFinished: make(chan *transfer.Transfer),
		OnTransferImported:         make(chan *transfer.Transfer),
	}
}

func (d *Downloader) Close() {
	close(d.OnFileDownloadError)
	close(d.OnTransferDownloadError)
	close(d.OnTransferDownloadFinished)
	close(d.OnTransferImported)
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
		return 0, fmt.Errorf("no files to download")
	}

	logger := logctx.LoggerFromContext(ctx)

	sem := make(chan struct{}, d.maxParallel)

	for i := range transfer.Files {
		file := transfer.Files[i]
		sem <- struct{}{}

		wg.Go(func() error {
			defer func() { <-sem }() // release the slot

			targetPath := filepath.Join(d.downloadDir, file.Path)
			if err := d.DownloadFile(ctx, transfer.ID, file, targetPath); err != nil {
				if err == storage.ErrDownloaded {
					logger.DebugContext(ctx, "file already downloaded", "download_id", transfer.ID, "file_path", file.Path)

					return err
				}

				logger.ErrorContext(ctx, "failed to download file", "download_id", transfer.ID, "file_path", file.Path, "err", err)

				return err
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

	defer out.Close()

	if err := d.writeFile(ctx, out, fileReader, file.Path, targetPath, file.Size); err != nil {
		d.OnFileDownloadError <- file

		return fmt.Errorf("failed to download file: %w", err)
	}

	logger.InfoContext(ctx, "downloaded and saved file", "target", targetPath)

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
					d.OnTransferImported <- t
					return
				}
			}
		}
	}()
}

func (d *Downloader) WatchForSeeding(ctx context.Context, t *transfer.Transfer, pollingInterval time.Duration) {
	logger := logctx.LoggerFromContext(ctx)

	logger.InfoContext(ctx, "watching for seeding transfers", "transfer_id", t.ID, "polling_interval", pollingInterval)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorContext(ctx, "watch seeding panic",
					"operation", "watch_seeding",
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
				logger.InfoContext(ctx, "watch seeding shutdown",
					"operation", "watch_seeding",
					"transfer_id", t.ID,
					"reason", "context_cancelled")
				return
			case <-ticker.C:
				if !t.IsSeeding() {
					logger.InfoContext(ctx, "transfer stopped seeding",
						"operation", "watch_seeding",
						"transfer_id", t.ID,
						"reason", "seeding_complete")

					hash := sha1.Sum([]byte(t.ID))

					if dc, ok := d.dc.(*putio.Client); ok {
						if err := dc.RemoveTransfers(ctx, []string{hex.EncodeToString(hash[:])}, true); err != nil {
							logger.ErrorContext(ctx, "failed to remove transfer", "transfer_id", t.ID, "err", err)
						}
					}

					return
				}
			}
		}
	}()
}

func (d *Downloader) checkForImported(ctx context.Context, transfer *transfer.Transfer) (bool, error) {
	logger := logctx.LoggerFromContext(ctx)
	logger.DebugContext(ctx, "checking if transfer has been imported", "transfer_id", transfer.ID, "transfer_name", transfer.Name)

	for _, file := range transfer.Files {
		for _, arrService := range d.arrServices {
			filePath := filepath.Join(d.downloadDir, file.Path)

			imported, err := arrService.CheckImported(ctx, filePath)
			if err != nil {
				return false, fmt.Errorf("failed to check if transfer has been imported: %w", err)
			}

			if imported {
				logger.InfoContext(ctx, "transfer has been imported", "transfer_id", transfer.ID, "transfer_name", transfer.Name)

				if err := os.RemoveAll(filePath); err != nil {
					return false, fmt.Errorf("failed to remove file: %w", err)
				}

				logger.InfoContext(ctx, "transfer removed", "transfer_id", transfer.ID, "transfer_name", transfer.Name)

				return true, nil
			}
		}
	}

	return false, nil
}

func (d *Downloader) ensureTargetDir(ctx context.Context, targetPath string, logger *slog.Logger) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		logger.ErrorContext(ctx, "failed to create target directory", "dir", dir, "err", err)

		return fmt.Errorf("failed to create target directory: %w", err)
	}

	return nil
}

func (d *Downloader) writeFile(ctx context.Context, out *os.File, reader io.Reader, url, targetPath string, totalBytes int64) error {
	logger := logctx.LoggerFromContext(ctx)

	logger.InfoContext(ctx, "downloading file", "file_path", targetPath, "file_size", humanize.Bytes(uint64(totalBytes)))

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

	if _, err := io.Copy(out, pr); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}
