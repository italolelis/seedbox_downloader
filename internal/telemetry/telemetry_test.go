package telemetry

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
		tel.RecordClientOperation(ctx, "putio", "list", "success")
		tel.RecordClientOperation(ctx, "putio", "list", "error")
		tel.RecordDBOperation(ctx, "claim", "success", time.Second)
		tel.RecordTorrentType(ctx, "metainfo")
	})

	assert.NoError(t, tel.Shutdown(ctx))
}

// Spans were created all along -- by otelhttp, the instrumented clients and the
// repository -- but against the default no-op tracer, so none was ever
// recorded and none left the process. Enabled telemetry has to record them.
func TestNew_Enabled_RecordsSpans(t *testing.T) {
	tel, err := New(context.Background(), Config{
		Enabled:        true,
		ServiceName:    "test",
		ServiceVersion: "test",
		OTELAddress:    closedAddr(t),
		Insecure:       true,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_ = tel.Shutdown(ctx)
	})

	_, span := otel.Tracer("test").Start(context.Background(), "operation")
	defer span.End()

	assert.True(t, span.IsRecording(), "an enabled tracer must record spans")
	assert.True(t, span.SpanContext().IsValid(), "and give them real trace and span ids, which the logs carry")

	// Outgoing requests carry the trace onward only if a propagator is installed;
	// the default one injects nothing.
	assert.Contains(t, otel.GetTextMapPropagator().Fields(), "traceparent")
}

func TestNew_Disabled_RecordsNoSpans(t *testing.T) {
	tel, err := New(context.Background(), Config{Enabled: false, ServiceName: "test"})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(context.Background(), "operation")
	defer span.End()

	assert.False(t, span.IsRecording())
	assert.NoError(t, tel.Shutdown(context.Background()))
}

// spanRecorder points a Telemetry's tracer at an in-memory recorder, so a test
// can see exactly which spans an instrumented call produced.
func spanRecorder(t *testing.T) (*Telemetry, *tracetest.SpanRecorder) {
	t.Helper()

	tel, err := New(context.Background(), Config{Enabled: false, ServiceName: "test"})
	require.NoError(t, err)

	recorder := tracetest.NewSpanRecorder()
	tel.tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	return tel, recorder
}

func spanNames(recorder *tracetest.SpanRecorder) []string {
	var names []string

	for _, s := range recorder.Ended() {
		names = append(names, s.Name())
	}

	return names
}

// Each instrumented call is one operation, so it is one span. Client calls and
// downloads used to open a second span of the same name inside the first, which
// doubled every one of them in a trace view.
func TestInstrumentedCalls_ProduceOneSpanEach(t *testing.T) {
	tel, recorder := spanRecorder(t)
	ctx := context.Background()

	require.NoError(t, tel.InstrumentClientOperation(ctx, "putio", "grab_file", func(context.Context) error { return nil }))
	require.NoError(t, tel.InstrumentDownload(ctx, "1", "name", func(context.Context) error { return nil }))

	assert.Equal(t, []string{"client_grab_file", "download"}, spanNames(recorder))
}

// The ledger is called without a request context, so a span of its own would
// be the root of a one-span trace -- and the claim heartbeat alone would start
// one every renewal. A database call is traced only as part of a trace already
// under way; its metrics are recorded either way.
func TestInstrumentDBOperation_TracedOnlyWithinATrace(t *testing.T) {
	tel, recorder := spanRecorder(t)

	require.NoError(t, tel.InstrumentDBOperation(context.Background(), "renew_claims", func(context.Context) error { return nil }))
	assert.Empty(t, spanNames(recorder), "an untraced database call must not start a trace")

	ctx, parent := tel.tracer.Start(context.Background(), "download")
	require.NoError(t, tel.InstrumentDBOperation(ctx, "claim_transfer", func(context.Context) error { return nil }))
	parent.End()

	assert.Equal(t, []string{"db_claim_transfer", "download"}, spanNames(recorder))
}
