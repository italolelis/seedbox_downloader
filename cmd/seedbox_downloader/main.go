package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi"
	"github.com/italolelis/seedbox_downloader/internal/cleanup"
	"github.com/italolelis/seedbox_downloader/internal/config"
	"github.com/italolelis/seedbox_downloader/internal/dc"
	"github.com/italolelis/seedbox_downloader/internal/dc/deluge"
	"github.com/italolelis/seedbox_downloader/internal/dc/putio"
	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/http/rest"
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

	// =========================================================================
	// Start Database
	database, err := sqlite.InitDB(cfg.DBPath)
	if err != nil {
		logger.Error("DB error", "err", err)

		return err
	}
	defer database.Close()

	dr := sqlite.NewDownloadRepository(database)

	// =========================================================================
	// Start Download Client
	dc, err := buildDownloadClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to build download client: %w", err)
	}

	if err := dc.Authenticate(ctx); err != nil {
		return fmt.Errorf("authentication error: %w", err)
	}

	// =========================================================================
	// Start Downloader
	downloader := downloader.NewDownloader(
		dr,
		cfg.TargetDir,
		cfg.TargetLabel,
		dc,
		cfg.MaxParallel,
	)

	// =========================================================================
	// Start Notification
	setupNotificationForDownloader(ctx, downloader, cfg)

	// =========================================================================
	// Start API Service

	// Make a channel to listen for errors coming from the listener. Use a
	// buffered channel so the goroutine can exit if we don't collect this error.
	serverErrors := make(chan error, 1)

	server, err := setupServer(ctx, dc, cfg)
	if err != nil {
		return fmt.Errorf("failed to setup server: %w", err)
	}

	go func() {
		logger.Info("Initializing API support", "host", cfg.Web.BindAddress)
		serverErrors <- server.ListenAndServe()
	}()

	logger.Info("waiting for downloads...",
		"target_label", cfg.TargetLabel,
		"target_dir", cfg.TargetDir,
		"update_interval", cfg.UpdateInterval.String(),
		"retention", cfg.KeepDownloadedFor.String(),
	)

	// =========================================================================
	// Start Cleanup
	setupCleanup(ctx, dr, cfg)

	// =========================================================================
	// Start Main Loop
	ticker := time.NewTicker(cfg.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-serverErrors:
			return fmt.Errorf("server error: %w", err)
		case <-ctx.Done():
			logger.Info("start shutdown")

			// Give outstanding requests a deadline for completion.
			ctx, cancel := context.WithTimeout(ctx, cfg.Web.ShutdownTimeout)
			defer cancel()

			if err := server.Shutdown(ctx); err != nil {
				logger.Error("failed to gracefully shutdown the server", "err", err)

				if err = server.Close(); err != nil {
					return fmt.Errorf("could not stop server gracefully: %w", err)
				}
			}

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

// This is an abstract factory for the download client.
func buildDownloadClient(cfg *config.Config) (dc.DownloadClient, error) {
	switch cfg.DownloadClient {
	case "deluge":
		return deluge.NewClient(cfg.DelugeBaseURL, cfg.DelugeAPIURLPath, cfg.DelugeCompletedDir, cfg.DelugeUsername, cfg.DelugePassword, true), nil
	case "putio":
		return putio.NewClient(cfg.PutioToken, true), nil
	}

	return nil, fmt.Errorf("invalid download client: %s", cfg.DownloadClient)
}

// setupServer prepares the handlers and services to create the http rest server.
func setupServer(ctx context.Context, dc dc.DownloadClient, cfg *config.Config) (*http.Server, error) {
	var tHandler *rest.TransmissionHandler

	if dc, ok := dc.(*putio.Client); ok {
		tHandler = rest.NewTransmissionHandler(cfg.Transmission.Username, cfg.Transmission.Password, dc, cfg.TargetLabel, cfg.TargetDir)
	} else {
		return nil, fmt.Errorf("download client is not a putio client: %s", cfg.DownloadClient)
	}

	r := chi.NewRouter()
	r.Mount("/", tHandler.Routes())

	return &http.Server{
		Addr:         cfg.Web.BindAddress,
		ReadTimeout:  cfg.Web.ReadTimeout,
		WriteTimeout: cfg.Web.WriteTimeout,
		IdleTimeout:  cfg.Web.IdleTimeout,
		Handler:      r,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}, nil
}

func setupCleanup(ctx context.Context, dr *sqlite.DownloadRepository, cfg *config.Config) {
	logger := logctx.LoggerFromContext(ctx)

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
}
