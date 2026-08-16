package test

import (
	"io"
	"log/slog"
)

// testLogger discards output. The downloader pulls its logger from the context,
// so one has to be present; what it writes is not under test.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}
