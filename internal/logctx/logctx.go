package logctx

import (
	"context"
	"log/slog"
)

type contextKey string

const loggerKey contextKey = "logger"

// WithLogger returns a new context with the provided slog.Logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext retrieves the slog.Logger from the context, or returns slog.Default() if not found.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
