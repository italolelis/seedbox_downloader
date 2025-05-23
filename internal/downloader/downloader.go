package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/italolelis/seedbox_downloader/internal/dc"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
)

type Downloader struct {
	repo        storage.DownloadRepository
	targetDir   string
	targetLabel string
	instanceID  string // unique for this process
	dlClient    dc.DownloadClient

	OnFileDownloadError       chan *dc.File
	OnTorrentDownloadFinished chan *dc.Torrent
}

func NewDownloader(
	dr storage.DownloadRepository,
	dir string,
	lbl string,
	c dc.DownloadClient,
) *Downloader {
	return &Downloader{
		repo:        dr,
		targetDir:   dir,
		dlClient:    c,
		targetLabel: lbl,
		instanceID:  GenerateInstanceID(),

		OnFileDownloadError:       make(chan *dc.File),
		OnTorrentDownloadFinished: make(chan *dc.Torrent),
	}
}

func (d *Downloader) DownloadTaggedTorrents(ctx context.Context) error {
	logger := logctx.LoggerFromContext(ctx)

	torrents, err := d.dlClient.GetTaggedTorrents(ctx, d.targetLabel)
	if err != nil {
		logger.Error("failed to fetch torrents from download client", "err", err)

		return err
	}

	for _, torrent := range torrents {
		torrentID := torrent.ID

		var downloadedFiles int

		for _, file := range torrent.Files {
			targetPath := filepath.Join(d.targetDir, file.Path)
			hash := sha256.Sum256([]byte(targetPath))
			downloadID := hex.EncodeToString(hash[:])

			// Try to claim the download atomically, but only if not already downloading or downloaded
			claimed, err := d.repo.ClaimDownload(downloadID, torrentID, targetPath, d.instanceID)
			if err != nil {
				if err == storage.ErrDownloaded {
					logger.Debug("files already downloaded", "download_id", torrentID, "file_path", targetPath)
				} else {
					logger.Error("error claiming download", "download_id", torrentID, "err", err)
				}

				continue
			}

			if !claimed {
				logger.Debug("download already claimed or downloaded", "download_id", downloadID)

				continue
			}

			logger.Info("downloading new files", "download_id", downloadID)

			err = d.dlClient.DownloadFile(ctx, file, targetPath)
			if err != nil {
				logger.Error("failed to download file", "download_id", downloadID, "err", err)

				if err := d.repo.UpdateDownloadStatus(downloadID, "failed"); err != nil {
					logger.Error("failed to update download status", "download_id", downloadID, "err", err)
				}

				d.OnFileDownloadError <- file

				continue
			}

			if err := d.repo.UpdateDownloadStatus(downloadID, "downloaded"); err != nil {
				logger.Error("failed to update download status", "download_id", downloadID, "err", err)
			}

			downloadedFiles++
		}

		if downloadedFiles > 0 {
			logger.Info("downloads completed", "download_id", torrentID, "torrent_name", torrent.Name)

			d.OnTorrentDownloadFinished <- torrent
		}
	}

	return nil
}
