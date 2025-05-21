package downloader

import (
	"context"
	"path/filepath"

	"github.com/italolelis/seedbox_downloader/internal/dc"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
)

type Downloader struct {
	repo        storage.DownloadWriteRepository
	readRepo    storage.DownloadReadRepository
	targetDir   string
	dlClient    dc.DownloadClient
	targetLabel string

	instanceID string // unique for this process
}

type Torrent = dc.TorrentInfo

func NewDownloader(wr storage.DownloadWriteRepository, rr storage.DownloadReadRepository, dir string, c dc.DownloadClient, lbl string) *Downloader {
	return &Downloader{
		repo:        wr,
		readRepo:    rr,
		targetDir:   dir,
		dlClient:    c,
		targetLabel: lbl,
		instanceID:  GenerateInstanceID(),
	}
}

func (d *Downloader) DownloadTaggedTorrents(ctx context.Context) error {
	logger := logctx.LoggerFromContext(ctx)

	torrents, err := d.dlClient.GetTaggedTorrents(ctx, d.targetLabel)
	if err != nil {
		logger.Error("failed to fetch torrents from download client", "err", err)

		return err
	}

	// Group by torrent ID
	torrentByID := make(map[string]Torrent)
	for _, torrent := range torrents {
		if _, exists := torrentByID[torrent.ID]; !exists {
			torrentByID[torrent.ID] = torrent
		}
	}

	for id, torrent := range torrentByID {
		// Ensure the torrent is tracked in the DB before claiming
		err := d.repo.TrackDownload(id, torrent.FileName)
		if err != nil {
			logger.Error("failed to track torrent in DB", "download_id", id, "err", err)
		}

		// Try to claim the download atomically, but only if not already downloading or downloaded
		claimed, err := d.repo.ClaimDownload(id, d.instanceID)
		if err != nil {
			logger.Error("failed to claim download", "download_id", id, "err", err)

			continue
		}

		if !claimed {
			logger.Debug("torrent already claimed, downloading, or not pending", "download_id", id)

			continue
		}

		logger.Info("downloading new torrent", "download_id", id)

		targetPath := filepath.Join(d.targetDir, torrent.FileName)
		if err := d.dlClient.DownloadFile(ctx, torrent, targetPath); err != nil {
			logger.Error("failed to download file", "download_id", id, "err", err)
			_ = d.repo.UpdateDownloadStatus(id, "failed")

			continue
		}

		_ = d.repo.UpdateDownloadStatus(id, "downloaded")
	}

	logger.Debug("downloadTaggedTorrents completed")

	return nil
}
