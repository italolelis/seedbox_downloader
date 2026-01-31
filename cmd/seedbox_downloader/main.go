package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
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
	"github.com/italolelis/seedbox_downloader/internal/telemetry"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/kelseyhightower/envconfig"
)

var version = "develop"

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

	TargetLabel       string         `envconfig:"TARGET_LABEL"`
	DownloadDir       string         `envconfig:"DOWNLOAD_DIR" required:"true"`
	KeepDownloadedFor time.Duration  `envconfig:"KEEP_DOWNLOADED_FOR" default:"24h"`
	PollingInterval   time.Duration  `envconfig:"POLLING_INTERVAL" default:"10m"`
	CleanupInterval   time.Duration  `envconfig:"CLEANUP_INTERVAL" default:"10m"`
	LogLevel          *slog.LevelVar `envconfig:"LOG_LEVEL" default:"INFO"`
	DiscordWebhookURL string         `envconfig:"DISCORD_WEBHOOK_URL"`
	DBPath            string         `envconfig:"DB_PATH" default:"downloads.db"`
	DBMaxOpenConns    int            `envconfig:"DB_MAX_OPEN_CONNS" default:"25"`
	DBMaxIdleConns    int            `envconfig:"DB_MAX_IDLE_CONNS" default:"5"`
	MaxParallel       int            `envconfig:"MAX_PARALLEL" default:"5"`

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

	Telemetry struct {
		Enabled     bool   `split_words:"true" default:"true"`
		OTELAddress string `split_words:"true" default:"0.0.0.0:4317"`
		ServiceName string `split_words:"true" default:"seedbox_downloader"`
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
	cfg, logger, err := initializeConfig()
	if err != nil {
		return err
	}

	ctx = logctx.WithLogger(ctx, logger)
	logger = logger.WithGroup("main")
	logger.Info("starting...", "log_level", cfg.LogLevel, "version", version)

	tel, err := initializeTelemetry(ctx, cfg)
	if err != nil {
		return err
	}
	defer tel.Shutdown(ctx)

	services, err := initializeServices(ctx, cfg, tel)
	if err != nil {
		return err
	}
	defer services.Close()

	servers, err := startServers(ctx, cfg, tel)
	if err != nil {
		return err
	}

	logger.Info("waiting for downloads...",
		"target_label", cfg.TargetLabel,
		"download_dir", cfg.DownloadDir,
		"polling_interval", cfg.PollingInterval.String(),
	)

	return runMainLoop(ctx, cfg, servers)
}

type services struct {
	downloader           *downloader.Downloader
	transferOrchestrator *transfer.TransferOrchestrator
}

func (s *services) Close() {
	s.downloader.Close()
	s.transferOrchestrator.Close()
}

type servers struct {
	api     *http.Server
	metrics *http.Server
	errors  chan error
}

func initializeConfig() (*config, *slog.Logger, error) {
	var cfg config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, nil, fmt.Errorf("failed to load the env vars: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	slog.SetDefault(logger)

	return &cfg, logger, nil
}

func initializeTelemetry(ctx context.Context, cfg *config) (*telemetry.Telemetry, error) {
	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName:    cfg.Telemetry.ServiceName,
		ServiceVersion: version,
		OTELAddress:    cfg.Telemetry.OTELAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	return tel, nil
}

func initializeServices(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*services, error) {
	database, err := sqlite.InitDB(ctx, cfg.DBPath, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the database: %w", err)
	}

	dr := sqlite.NewInstrumentedDownloadRepository(database, tel)

	dc, err := buildDownloadClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build download client: %w", err)
	}

	instrumentedDC := transfer.NewInstrumentedDownloadClient(dc, tel, cfg.DownloadClient)
	if err := instrumentedDC.Authenticate(ctx); err != nil {
		return nil, fmt.Errorf("failed to authenticate with the download client: %w", err)
	}

	arrServices := []*arr.Client{
		arr.NewClient(cfg.Sonarr.APIKey, cfg.Sonarr.BaseURL),
		arr.NewClient(cfg.Radarr.APIKey, cfg.Radarr.BaseURL),
	}

	instrumentedTC := transfer.NewInstrumentedTransferClient(dc.(transfer.TransferClient), tel, cfg.DownloadClient)

	downloader := downloader.NewDownloader(
		cfg.DownloadDir,
		cfg.MaxParallel,
		instrumentedDC,
		instrumentedTC,
		arrServices,
	)

	setupNotificationForDownloader(ctx, dr, downloader, cfg)

	transferOrchestrator := transfer.NewTransferOrchestrator(dr, instrumentedDC, cfg.TargetLabel, cfg.PollingInterval)
	transferOrchestrator.ProduceTransfers(ctx)
	downloader.WatchDownloads(ctx, transferOrchestrator.OnDownloadQueued)

	return &services{
		downloader:           downloader,
		transferOrchestrator: transferOrchestrator,
	}, nil
}

func startServers(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*servers, error) {
	logger := logctx.LoggerFromContext(ctx)

	serverErrors := make(chan error, 1)

	server, err := setupServer(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to setup server: %w", err)
	}

	go func() {
		logger.Info("initializing API support", "host", cfg.Web.BindAddress)
		serverErrors <- server.ListenAndServe()
	}()

	return &servers{
		api:    server,
		errors: serverErrors,
	}, nil
}

func runMainLoop(ctx context.Context, cfg *config, servers *servers) error {
	logger := logctx.LoggerFromContext(ctx)

	for {
		select {
		case err := <-servers.errors:
			return fmt.Errorf("server error: %w", err)
		case <-ctx.Done():
			logger.Info("start shutdown")

			shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Web.ShutdownTimeout)
			defer cancel()

			if servers.metrics != nil {
				if err := servers.metrics.Shutdown(shutdownCtx); err != nil {
					logger.Error("failed to gracefully shutdown metrics server", "err", err)
				}
			}

			if err := servers.api.Shutdown(shutdownCtx); err != nil {
				logger.Error("failed to gracefully shutdown the server", "err", err)

				if err = servers.api.Close(); err != nil {
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
		defer func() {
			if r := recover(); r != nil {
				logger.Error("notification loop panic",
					"operation", "notification_loop",
					"panic", r,
					"stack", string(debug.Stack()))

				// Restart with clean state if context not cancelled
				if ctx.Err() == nil {
					logger.Info("restarting notification loop after panic",
						"operation", "notification_loop")
					time.Sleep(time.Second) // Brief backoff before restart
					setupNotificationForDownloader(ctx, repo, downloader, cfg)
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				logger.Info("notification loop shutdown",
					"operation", "notification_loop",
					"reason", "context_cancelled")

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
func setupServer(ctx context.Context, cfg *config) (*http.Server, error) {
	r := chi.NewRouter()
	r.Use(telemetry.NewHTTPMiddleware(cfg.Telemetry.ServiceName))

	var tHandler *rest.TransmissionHandler

	// Get the original client for the transmission handler
	originalClient, err := buildDownloadClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build download client for handler: %w", err)
	}

	if putioClient, ok := originalClient.(*putio.Client); ok {
		tHandler = rest.NewTransmissionHandler(cfg.Transmission.Username, cfg.Transmission.Password, putioClient, cfg.TargetLabel, cfg.PutioBaseDir)
		r.Mount("/", tHandler.Routes())
	} else {
		return nil, fmt.Errorf("download client is not a putio client: %s", cfg.DownloadClient)
	}

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
