package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

		downloadedFiles, err := d.DownloadTorrent(ctx, torrent)
		if err != nil {
			logger.Error("failed to download torrent", "download_id", torrentID, "err", err)

			return err
		}

		if downloadedFiles > 0 {
			logger.Info("downloads completed", "download_id", torrentID, "torrent_name", torrent.Name)

			d.OnTorrentDownloadFinished <- torrent
		}
	}

	return nil
}

func (d *Downloader) DownloadTorrent(ctx context.Context, torrent *dc.Torrent) (int, error) {
	var downloadedFiles int

	logger := logctx.LoggerFromContext(ctx)

	if len(torrent.Files) == 0 {
		return 0, fmt.Errorf("no files to download")
	}

	for _, file := range torrent.Files {
		targetPath := filepath.Join(d.targetDir, file.Path)

		if err := d.DownloadFile(ctx, torrent.ID, file, targetPath); err != nil {
			logger.Error("failed to download file", "download_id", torrent.ID, "file_path", file.Path, "err", err)

			return 0, err
		}

		downloadedFiles++
	}

	return downloadedFiles, nil
}

func (d *Downloader) DownloadFile(ctx context.Context, torrentID string, file *dc.File, targetPath string) error {
	logger := logctx.LoggerFromContext(ctx)

	hash := sha256.Sum256([]byte(targetPath))
	downloadID := hex.EncodeToString(hash[:])

	// Try to claim the download atomically, but only if not already downloading or downloaded
	claimed, err := d.repo.ClaimDownload(downloadID, torrentID, targetPath, d.instanceID)
	if err != nil {
		if err == storage.ErrDownloaded {
			return fmt.Errorf("file already downloaded: %w", err)
		}

		return fmt.Errorf("error claiming download: %w", err)
	}

	if !claimed {
		return fmt.Errorf("download already claimed")
	}

	logger.Info("downloading new files", "download_id", downloadID)

	err = d.dlClient.DownloadFile(ctx, file, targetPath)
	if err != nil {
		if err := d.repo.UpdateDownloadStatus(downloadID, "failed"); err != nil {
			return fmt.Errorf("failed to update download status: %w", err)
		}

		d.OnFileDownloadError <- file

		return fmt.Errorf("failed to download file: %w", err)
	}

	if err := d.repo.UpdateDownloadStatus(downloadID, "downloaded"); err != nil {
		return fmt.Errorf("failed to update download status: %w", err)
	}

	return nil
}
