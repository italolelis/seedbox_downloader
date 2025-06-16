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
	"github.com/italolelis/seedbox_downloader/internal/dc/deluge"
	"github.com/italolelis/seedbox_downloader/internal/dc/putio"
	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/http/rest"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/notifier"
	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/storage/sqlite"
	"github.com/italolelis/seedbox_downloader/internal/svc/arr"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/kelseyhightower/envconfig"
)

var version = "develop"

const goRoutineCount = 100

// Config struct for environment variables.
type config struct {
	DownloadClient string `envconfig:"DOWNLOAD_CLIENT" default:"deluge"`

	DelugeBaseURL      string `envconfig:"DELUGE_BASE_URL"`
	DelugeAPIURLPath   string `envconfig:"DELUGE_API_URL_PATH"`
	DelugeUsername     string `envconfig:"DELUGE_USERNAME"`
	DelugePassword     string `envconfig:"DELUGE_PASSWORD"`
	DelugeCompletedDir string `envconfig:"DELUGE_COMPLETED_DIR"`

	PutioToken   string `envconfig:"PUTIO_TOKEN"`
	PutioBaseDir string `envconfig:"PUTIO_BASE_DIR"`

	TargetLabel       string        `envconfig:"TARGET_LABEL"`
	DownloadDir       string        `envconfig:"DOWNLOAD_DIR" required:"true"`
	KeepDownloadedFor time.Duration `envconfig:"KEEP_DOWNLOADED_FOR" default:"24h"`
	PollingInterval   time.Duration `envconfig:"POLLING_INTERVAL" default:"10m"`
	CleanupInterval   time.Duration `envconfig:"CLEANUP_INTERVAL" default:"10m"`
	LogLevel          slog.Level    `envconfig:"LOG_LEVEL" default:"INFO"`
	DiscordWebhookURL string        `envconfig:"DISCORD_WEBHOOK_URL"`
	DBPath            string        `envconfig:"DB_PATH" default:"downloads.db"`
	MaxParallel       int           `envconfig:"MAX_PARALLEL" default:"5"`

	Transmission struct {
		Username string `split_words:"true"`
		Password string `split_words:"true"`
	}

	Web struct {
		BindAddress     string        `split_words:"true" default:"0.0.0.0:9091"`
		ReadTimeout     time.Duration `split_words:"true" default:"30s"`
		WriteTimeout    time.Duration `split_words:"true" default:"30s"`
		IdleTimeout     time.Duration `split_words:"true" default:"5s"`
		ShutdownTimeout time.Duration `split_words:"true" default:"30s"`
	}

	Sonarr arrConfig `envconfig:"SONARR"`
	Radarr arrConfig `envconfig:"RADARR"`
}

type arrConfig struct {
	APIKey  string `envconfig:"API_KEY"`
	BaseURL string `envconfig:"BASE_URL"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "fatal error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// =========================================================================
	// Configuration
	var cfg config

	if err := envconfig.Process("", &cfg); err != nil {
		return fmt.Errorf("failed to load the env vars: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx = logctx.WithLogger(ctx, logger)

	logger = logger.WithGroup("main")
	logger.Info("starting...", "log_level", cfg.LogLevel, "version", version)

	// =========================================================================
	// Start Database
	database, err := sqlite.InitDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize the database: %w", err)
	}
	defer database.Close()

	dr := sqlite.NewDownloadRepository(database)

	// =========================================================================
	// Start Download Client
	dc, err := buildDownloadClient(&cfg)
	if err != nil {
		return fmt.Errorf("failed to build download client: %w", err)
	}

	if err := dc.Authenticate(ctx); err != nil {
		return fmt.Errorf("failed to authenticate with the download client: %w", err)
	}

	// =========================================================================
	// Start Downloader
	arrServices := []*arr.Client{
		arr.NewClient(cfg.Sonarr.APIKey, cfg.Sonarr.BaseURL),
		arr.NewClient(cfg.Radarr.APIKey, cfg.Radarr.BaseURL),
	}

	downloader := downloader.NewDownloader(
		cfg.DownloadDir,
		cfg.MaxParallel,
		dc,
		dc.(transfer.TransferClient),
		arrServices,
	)
	defer downloader.Close()

	// =========================================================================
	// Start Notification
	setupNotificationForDownloader(ctx, dr, downloader, &cfg)

	// =========================================================================
	// Start Transfer Orchestrator
	transferOrchestrator := transfer.NewTransferOrchestrator(dr, dc, cfg.TargetLabel, cfg.PollingInterval)
	defer transferOrchestrator.Close()

	transferOrchestrator.ProduceTransfers(ctx)

	downloader.WatchDownloads(ctx, transferOrchestrator.OnDownloadQueued)

	// =========================================================================
	// Start API Service

	// Make a channel to listen for errors coming from the listener. Use a
	// buffered channel so the goroutine can exit if we don't collect this error.
	serverErrors := make(chan error, 1)

	server, err := setupServer(ctx, dc, &cfg)
	if err != nil {
		return fmt.Errorf("failed to setup server: %w", err)
	}

	go func() {
		logger.Info("initializing API support", "host", cfg.Web.BindAddress)
		serverErrors <- server.ListenAndServe()
	}()

	logger.Info("waiting for downloads...",
		"target_label", cfg.TargetLabel,
		"download_dir", cfg.DownloadDir,
		"polling_interval", cfg.PollingInterval.String(),
	)

	// =========================================================================
	// Start Main Loop
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
					return fmt.Errorf("failed to stop server gracefully: %w", err)
				}
			}

			return ctx.Err()
		}
	}
}

func setupNotificationForDownloader(
	ctx context.Context,
	repo storage.DownloadRepository,
	downloader *downloader.Downloader,
	cfg *config,
) {
	logger := logctx.LoggerFromContext(ctx).WithGroup("notification")

	var notif notifier.Notifier
	if cfg.DiscordWebhookURL != "" {
		notif = &notifier.DiscordNotifier{WebhookURL: cfg.DiscordWebhookURL}
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("shutting down notification for downloader")

				return
			case t := <-downloader.OnTransferDownloadError:
				err := repo.UpdateTransferStatus(t.ID, "failed")
				if err != nil {
					logger.Error("failed to update transfer status", "transfer_id", t.ID, "err", err)

					continue
				}

				logger.Warn("transfer download error", "transfer_id", t.ID, "transfer_name", t.Name)

				if notifyErr := notif.Notify(
					"❌ Download failed for transfer: " + t.Name + " (" + t.ID + ")",
				); notifyErr != nil {
					logger.Error("failed to send notification", "err", notifyErr)
				}
			case t := <-downloader.OnTransferDownloadFinished:
				err := repo.UpdateTransferStatus(t.ID, "downloaded")
				if err != nil {
					logger.Error("failed to update transfer status", "transfer_id", t.ID, "err", err)

					continue
				}

				downloader.WatchForImported(ctx, t, cfg.PollingInterval)

				logger.Info("transfer download finished", "transfer_id", t.ID, "transfer_name", t.Name)

				if notifyErr := notif.Notify(
					"✅ Download finished for transfer: " + t.Name + " (" + t.ID + ")",
				); notifyErr != nil {
					logger.Error("failed to send notification", "err", notifyErr)
				}
			case t := <-downloader.OnTransferImported:
				downloader.WatchForSeeding(ctx, t, cfg.PollingInterval)

				if notifyErr := notif.Notify(
					"📪 Transfer imported: " + t.Name + " (" + t.ID + ")",
				); notifyErr != nil {
					logger.Error("failed to send notification", "err", notifyErr)
				}
			}
		}
	}()
}

// This is an abstract factory for the download client.
func buildDownloadClient(cfg *config) (transfer.DownloadClient, error) {
	switch cfg.DownloadClient {
	case "deluge":
		return deluge.NewClient(cfg.DelugeBaseURL, cfg.DelugeAPIURLPath, cfg.DelugeCompletedDir, cfg.DelugeUsername, cfg.DelugePassword, true), nil
	case "putio":
		return putio.NewClient(cfg.PutioToken, true), nil
	}

	return nil, fmt.Errorf("invalid download client: %s", cfg.DownloadClient)
}

// setupServer prepares the handlers and services to create the http rest server.
func setupServer(ctx context.Context, dc transfer.DownloadClient, cfg *config) (*http.Server, error) {
	var tHandler *rest.TransmissionHandler

	if dc, ok := dc.(*putio.Client); ok {
		tHandler = rest.NewTransmissionHandler(cfg.Transmission.Username, cfg.Transmission.Password, dc, cfg.TargetLabel, cfg.PutioBaseDir)
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
