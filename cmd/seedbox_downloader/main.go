package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/cleanup"
	"github.com/italolelis/seedbox_downloader/internal/config"
	"github.com/italolelis/seedbox_downloader/internal/dc/deluge"
	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/notifier"
	"github.com/italolelis/seedbox_downloader/internal/storage/sqlite"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("seedbox downloader starting...", "log_level", cfg.LogLevel)

	if err := run(logctx.WithLogger(ctx, logger), cfg); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	logger := logctx.LoggerFromContext(ctx)

	database, err := sqlite.InitDB(cfg.DBPath)
	if err != nil {
		logger.Error("DB error", "err", err)

		return err
	}
	defer database.Close()

	dr := sqlite.NewDownloadRepository(database)

	dlClient := deluge.NewClient(cfg.DelugeBaseURL, cfg.DelugeAPIURLPath, cfg.DelugeCompletedDir, cfg.DelugeUsername, cfg.DelugePassword, true)
	if err := dlClient.Authenticate(ctx); err != nil {
		return fmt.Errorf("Deluge authentication error: %w", err)
	}

	downloader := downloader.NewDownloader(
		dr,
		cfg.TargetDir,
		cfg.TargetLabel,
		dlClient,
	)

	setupNotificationForDownloader(ctx, downloader, cfg)

	logger.Info("waiting for downloads...",
		"target_label", cfg.TargetLabel,
		"target_dir", cfg.TargetDir,
		"update_interval", cfg.UpdateInterval.String(),
		"retention", cfg.KeepDownloadedFor.String(),
	)

	// Start independent cleanup goroutine
	cleanupTicker := time.NewTicker(cfg.CleanupInterval)
	defer cleanupTicker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("cleanup goroutine shutting down.")

				return
			case <-cleanupTicker.C:
				tracked, err := dr.GetDownloads()
				if err != nil {
					logger.Error("failed to get tracked downloads for cleanup", "err", err)

					continue
				}

				if err := cleanup.DeleteExpiredFiles(ctx, tracked, cfg.TargetDir, cfg.KeepDownloadedFor); err != nil {
					logger.Error("failed to delete expired tracked files", "err", err)
				}
			}
		}
	}()

	ticker := time.NewTicker(cfg.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, shutting down main loop.")

			return ctx.Err()
		case <-ticker.C:
			if err := downloader.DownloadTaggedTorrents(ctx); err != nil {
				logger.Error("error downloading tagged torrents", "err", err)
			}
		}
	}
}

func setupNotificationForDownloader(ctx context.Context, downloader *downloader.Downloader, cfg *config.Config) {
	logger := logctx.LoggerFromContext(ctx)

	var notif notifier.Notifier
	if cfg.DiscordWebhookURL != "" {
		notif = &notifier.DiscordNotifier{WebhookURL: cfg.DiscordWebhookURL}
	}

	go func() {
		for event := range downloader.OnFileDownloadError {
			logger.Error("file download file", "err", event.Path)

			if notifyErr := notif.Notify(
				"❌ Download failed for torrent: " + event.Path,
			); notifyErr != nil {
				logger.Error("failed to send notification", "err", notifyErr)
			}
		}
	}()

	go func() {
		for event := range downloader.OnTorrentDownloadFinished {
			logger.Info("torrent download finished", "torrent_id", event.ID, "torrent_name", event.Name)

			if notifyErr := notif.Notify(
				"✅ Download finished for torrent: " + event.Name + " (" + event.ID + ")",
			); notifyErr != nil {
				logger.Error("failed to send notification", "download_id", event.ID, "err", notifyErr)
			}
		}
	}()
}
