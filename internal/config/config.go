package config

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config struct for environment variables.
type Config struct {
	DownloadClient string `envconfig:"DOWNLOAD_CLIENT" default:"deluge"`

	DelugeBaseURL      string `envconfig:"DELUGE_BASE_URL"`
	DelugeAPIURLPath   string `envconfig:"DELUGE_API_URL_PATH"`
	DelugeUsername     string `envconfig:"DELUGE_USERNAME"`
	DelugePassword     string `envconfig:"DELUGE_PASSWORD"`
	DelugeCompletedDir string `envconfig:"DELUGE_COMPLETED_DIR"`

	PutioBaseURL string `envconfig:"PUTIO_BASE_URL"`
	PutioToken   string `envconfig:"PUTIO_TOKEN"`

	TargetLabel       string        `envconfig:"TARGET_LABEL"`
	TargetDir         string        `envconfig:"TARGET_DIR" required:"true"`
	KeepDownloadedFor time.Duration `envconfig:"KEEP_DOWNLOADED_FOR" default:"24h"`
	UpdateInterval    time.Duration `envconfig:"UPDATE_INTERVAL" default:"10m"`
	CleanupInterval   time.Duration `envconfig:"CLEANUP_INTERVAL" default:"10m"`
	LogLevel          string        `envconfig:"LOG_LEVEL" default:"INFO"`
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
}

// LoadConfig reads environment variables and populates the Config struct.
func LoadConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("error processing env: %w", err)
	}

	return &cfg, nil
}

func (c *Config) SlogLevel() slog.Level {
	switch strings.ToUpper(c.LogLevel) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
