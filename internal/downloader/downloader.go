package downloader

import (
	"context"
	"path/filepath"

	"github.com/italolelis/seedbox_downloader/internal/download_client"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
)

type Downloader struct {
	repo        storage.DownloadWriteRepository
	readRepo    storage.DownloadReadRepository
	targetDir   string
	dlClient    download_client.DownloadClient
	targetLabel string

	instanceID string // unique for this process
}

type Torrent = download_client.TorrentInfo

func NewDownloader(writeRepo storage.DownloadWriteRepository, readRepo storage.DownloadReadRepository, targetDir string, dlClient download_client.DownloadClient, targetLabel string) *Downloader {
	return &Downloader{
		repo:        writeRepo,
		readRepo:    readRepo,
		targetDir:   targetDir,
		dlClient:    dlClient,
		targetLabel: targetLabel,
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
			logger.Error("failed to track torrent in DB", "torrent_id", id, "err", err)
		}

		// Try to claim the download atomically, but only if not already downloading or downloaded
		claimed, err := d.repo.ClaimDownload(id, d.instanceID)
		if err != nil {
			logger.Error("failed to claim download", "torrent_id", id, "err", err)
			continue
		}
		if !claimed {
			logger.Debug("torrent already claimed, downloading, or not pending", "torrent_id", id)
			continue
		}
		logger.Info("downloading new torrent", "torrent_id", id)
		targetPath := filepath.Join(d.targetDir, torrent.FileName)
		if err := d.dlClient.DownloadFile(ctx, torrent, targetPath); err != nil {
			logger.Error("failed to download torrent", "torrent_id", id, "err", err)
			_ = d.repo.UpdateDownloadStatus(id, "failed")
			continue
		}
		_ = d.repo.UpdateDownloadStatus(id, "downloaded")
	}

	logger.Debug("downloadTaggedTorrents completed")
	return nil
}
