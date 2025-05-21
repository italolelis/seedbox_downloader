package cleanup

import (
	"os"
	"path/filepath"
	"time"

	"context"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
)

// DeleteExpiredFiles deletes files older than keepDuration based on tracked records.
func DeleteExpiredFiles(ctx context.Context, dr []storage.DownloadRecord, dir string, keepDuration time.Duration) error {
	logger := logctx.LoggerFromContext(ctx)
	now := time.Now()

	for _, rec := range dr {
		filePath := filepath.Join(dir, rec.FilePath)

		info, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // already deleted
			}

			logger.Error("Failed to stat file", "file", filePath, "err", err)

			return err
		}
		// Parse DB time
		downloadedAt, err := time.Parse(time.RFC3339, rec.DownloadedAt)
		if err != nil {
			// fallback: use file mod time
			logger.Warn("Failed to parse download time, using file mod time", "file", filePath, "err", err)

			downloadedAt = info.ModTime()
		}

		if now.Sub(downloadedAt) > keepDuration {
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				logger.Error("Failed to delete expired file", "file", filePath, "err", err)

				return err
			}

			logger.Info("Deleted expired file", "file", filePath)
		}
	}

	return nil
}
