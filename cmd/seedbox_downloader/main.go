package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/cleanup"
	"github.com/italolelis/seedbox_downloader/internal/config"
	"github.com/italolelis/seedbox_downloader/internal/dc/deluge"
	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage/sqlite"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Config error", "err", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("seedbox downloader starting...", "log_level", cfg.LogLevel)

	if err := run(ctx, cfg, logger); err != nil {
		slog.Error("Fatal error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	database, err := sqlite.InitDB()
	if err != nil {
		slog.Error("DB error", "err", err)

		return err
	}
	defer database.Close()

	writeRepo := sqlite.NewDownloadWriteRepository(database)
	readRepo := sqlite.NewDownloadReadRepository(database)

	dlClient := deluge.NewClient(cfg.DelugeBaseURL, cfg.DelugeAPIURLPath, cfg.DelugeUsername, cfg.DelugePassword, true)
	if err := dlClient.Authenticate(ctx); err != nil {
		slog.Error("Deluge authentication error", "err", err)

		return err
	}

	downloader := downloader.NewDownloader(
		writeRepo,
		readRepo,
		cfg.TargetDir,
		dlClient,
		cfg.TargetLabel,
	)

	ctx = logctx.WithLogger(ctx, logger)

	logger.Info("waiting for downloads...",
		"target_label", cfg.TargetLabel,
		"target_dir", cfg.TargetDir,
		"update_interval", cfg.UpdateInterval.String(),
		"retention", cfg.KeepDownloadedFor.String(),
	)

	ticker := time.NewTicker(cfg.UpdateInterval)
	defer ticker.Stop()

	// Start independent cleanup goroutine
	cleanupInterval := cfg.CleanupInterval
	cleanupTicker := time.NewTicker(cleanupInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("cleanup goroutine shutting down.")

				return
			case <-cleanupTicker.C:
				log := logctx.LoggerFromContext(ctx)

				tracked, err := readRepo.GetDownloads()
				if err != nil {
					log.Error("Failed to get tracked downloads for cleanup", "err", err)

					continue
				}

				if err := cleanup.DeleteExpiredFiles(ctx, tracked, cfg.TargetDir, cfg.KeepDownloadedFor); err != nil {
					log.Error("Failed to delete expired tracked files", "err", err)
				}
			}
		}
	}()

	iteration := 0

	for {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, shutting down main loop.")

			return ctx.Err()
		case <-ticker.C:
			iteration++
			log := logctx.LoggerFromContext(ctx)
			log.Info("Tick: polling Deluge for tagged torrents", "iteration", iteration, "interval", cfg.UpdateInterval.String())

			start := time.Now()

			if err := downloader.DownloadTaggedTorrents(ctx); err != nil {
				log.Error("Error downloading tagged torrents", "err", err, "iteration", iteration)
			} else {
				log.Info("DownloadTaggedTorrents completed", "duration_ms", time.Since(start).Milliseconds(), "iteration", iteration)
			}
		}
	}
}
