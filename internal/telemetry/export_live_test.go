package telemetry

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLiveExport pushes metrics through the real telemetry package to a real OTLP
// collector, so the monitoring stack is verified rather than assumed.
//
// Opt-in via TELEMETRY_LIVE_TEST=1. Gating on an explicit flag rather than merely
// on a reachable port matters: a developer with a collector running would otherwise
// have test metrics pushed into their real pipeline by an ordinary `go test ./...`.
//
//	docker compose -f docker-compose.telemetry.yml up -d otel-collector prometheus
//	TELEMETRY_LIVE_TEST=1 go test ./internal/telemetry/ -run TestLiveExport -v
func TestLiveExport(t *testing.T) {
	if os.Getenv("TELEMETRY_LIVE_TEST") != "1" {
		t.Skip("set TELEMETRY_LIVE_TEST=1 with a collector running to exercise the real export path")
	}

	addr := os.Getenv("TELEMETRY_OTEL_ADDRESS")
	if addr == "" {
		addr = "127.0.0.1:4317"
	}

	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("TELEMETRY_LIVE_TEST is set but no collector is reachable at %s: %v", addr, err)
	}

	conn.Close()

	tel, err := New(context.Background(), Config{
		Enabled:        true,
		ServiceName:    "seedbox_downloader",
		ServiceVersion: "live-export-test",
		OTELAddress:    addr,
		Insecure:       true,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// One of each family, so the collector's output shows business, client and DB
	// metrics arriving -- not just that the connection opened.
	tel.RecordDownload(ctx, "success", 3*time.Second)
	tel.IncrementActiveDownloads(ctx)
	tel.RecordTransfer(ctx, "add", "success")
	tel.RecordClientOperation(ctx, "putio", "get_tagged_torrents", "success")
	tel.RecordDBOperation(ctx, "claim_transfer", "success", 12*time.Millisecond)
	tel.RecordTorrentType(ctx, "metainfo")
	tel.RecordSystemError(ctx, "downloader", "size_mismatch")

	// Shutdown flushes, so a successful return means the collector accepted the
	// export -- this is the assertion, not the recording above.
	shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	require.NoError(t, tel.Shutdown(shutdownCtx),
		"the collector did not accept the metric export")
}
