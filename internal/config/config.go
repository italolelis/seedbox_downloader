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
	DelugeBaseURL      string        `envconfig:"DELUGE_BASE_URL" required:"true"`
	DelugeAPIURLPath   string        `envconfig:"DELUGE_API_URL_PATH" required:"true"`
	DelugeUsername     string        `envconfig:"DELUGE_USERNAME" required:"true"`
	DelugePassword     string        `envconfig:"DELUGE_PASSWORD" required:"true"`
	DelugeCompletedDir string        `envconfig:"DELUGE_COMPLETED_DIR" required:"true"`
	TargetLabel        string        `envconfig:"TARGET_LABEL" required:"true"`
	TargetDir          string        `envconfig:"TARGET_DIR" required:"true"`
	KeepDownloadedFor  time.Duration `envconfig:"KEEP_DOWNLOADED_FOR" default:"24h"`
	UpdateInterval     time.Duration `envconfig:"UPDATE_INTERVAL" default:"10m"`
	CleanupInterval    time.Duration `envconfig:"CLEANUP_INTERVAL" default:"10m"`
	LogLevel           string        `envconfig:"LOG_LEVEL" default:"INFO"`
	DiscordWebhookURL  string        `envconfig:"DISCORD_WEBHOOK_URL"`
	DBPath             string        `envconfig:"DB_PATH" default:"downloads.db"`
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
