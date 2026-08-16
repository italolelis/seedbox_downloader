package telemetry

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// closedAddr returns an address with nothing listening on it.
func closedAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := l.Addr().String()
	require.NoError(t, l.Close())

	return addr
}

func TestNew_Disabled_InstallsNoopAndOpensNoConnection(t *testing.T) {
	// A listener that records whether anything ever dialled it. Telemetry is
	// disabled, so nothing should.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	dialed := make(chan struct{}, 1)

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			select {
			case dialed <- struct{}{}:
			default:
			}

			conn.Close()
		}
	}()

	tel, err := New(context.Background(), Config{
		Enabled:        false,
		ServiceName:    "test",
		ServiceVersion: "test",
		OTELAddress:    listener.Addr().String(),
	})
	require.NoError(t, err)
	require.NotNil(t, tel)

	assert.IsType(t, noop.NewMeterProvider(), tel.meterProvider,
		"disabled telemetry must install a no-op meter provider")

	// The OTLP gRPC exporter connects lazily, so give it a window in which it
	// would have dialled had one been constructed at all.
	select {
	case <-dialed:
		t.Fatal("disabled telemetry dialled the collector address")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNew_EnabledWithoutAddress_IsAConfigError(t *testing.T) {
	tel, err := New(context.Background(), Config{
		Enabled:        true,
		ServiceName:    "test",
		ServiceVersion: "test",
		OTELAddress:    "",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingOTELAddress),
		"enabling telemetry with no address must be a configuration error, not a silent no-op")
	assert.Nil(t, tel)
}

func TestNew_EnabledWithAddress_Succeeds(t *testing.T) {
	// A port with nothing on it, chosen by the OS and then released. Deliberately
	// not the standard 4317: if a real collector happens to be running locally,
	// this test would export a second resource to it and its Prometheus exporter
	// would then log duplicate-label errors for every metric family.
	tel, err := New(context.Background(), Config{
		Enabled:        true,
		ServiceName:    "test",
		ServiceVersion: "test",
		OTELAddress:    closedAddr(t),
		Insecure:       true,
	})

	// No collector is listening. Construction must still succeed, because an
	// unreachable collector is an operational condition, not a startup failure --
	// that is the whole distinction this ticket draws against a missing address.
	require.NoError(t, err)
	require.NotNil(t, tel)

	// Shutdown flushes, so against a dead collector it fails after the export
	// timeout. That is the SDK's behaviour and not what this test asserts; a
	// short context keeps the test from waiting it out.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = tel.Shutdown(shutdownCtx)
}

func TestDisabledTelemetry_RecordingAndShutdownAreSafe(t *testing.T) {
	tel, err := New(context.Background(), Config{
		Enabled:        false,
		ServiceName:    "test",
		ServiceVersion: "test",
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Every recording path must be safe against a no-op provider -- callers are
	// deliberately not made nil-aware.
	assert.NotPanics(t, func() {
		tel.RecordDownload(ctx, "success", time.Second)
		tel.IncrementActiveDownloads(ctx)
		tel.DecrementActiveDownloads(ctx)
		tel.RecordTransfer(ctx, "add", "success")
		tel.IncrementActiveTransfers(ctx)
		tel.DecrementActiveTransfers(ctx)
		tel.RecordClientOperation(ctx, "putio", "list", "success")
		tel.RecordClientOperation(ctx, "putio", "list", "error")
		tel.RecordDBOperation(ctx, "claim", "success", time.Second)
		tel.RecordSystemError(ctx, "downloader", "io")
		tel.RecordTorrentType(ctx, "metainfo")
	})

	assert.NoError(t, tel.Shutdown(ctx))
}
