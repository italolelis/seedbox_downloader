package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Telemetry holds all telemetry instruments and providers.
//
// Metrics leave the process over OTLP and nothing else. There is no Prometheus
// scrape endpoint: point a collector at OTELAddress and scrape the collector.
type Telemetry struct {
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
	tracer         trace.Tracer
	meter          metric.Meter

	// RED Metrics are handled by otelhttp, and process resources (memory,
	// goroutines, GC) by the Go runtime instrumentation started in
	// initializeMetrics. Neither needs an instrument here.

	// Business Metrics
	downloadsTotal        metric.Int64Counter
	downloadsActive       metric.Int64UpDownCounter
	downloadDuration      metric.Float64Histogram
	transfersTotal        metric.Int64Counter
	clientOperationsTotal metric.Int64Counter
	clientErrors          metric.Int64Counter
	dbOperationsTotal     metric.Int64Counter
	dbOperationDuration   metric.Float64Histogram
	torrentTypeCounter    metric.Int64Counter
}

// ErrMissingOTELAddress is returned when telemetry is enabled but no collector
// address was configured. Enabling telemetry with nowhere to send it is a
// misconfiguration, not a request to silently discard metrics.
var ErrMissingOTELAddress = errors.New("telemetry is enabled but no OTEL address is configured")

// Config holds telemetry configuration.
type Config struct {
	// Enabled turns telemetry export on or off. When false, no exporter is
	// constructed and no connection to OTELAddress is attempted.
	Enabled        bool
	ServiceName    string
	ServiceVersion string
	OTELAddress    string
	// Insecure sends OTLP over plaintext gRPC rather than TLS. The OTLP exporter
	// defaults to TLS, so talking to an ordinary local collector without this
	// fails with "first record does not look like a TLS handshake" -- which is not
	// an obvious diagnosis. Set false when the collector is reached over a network
	// you do not trust.
	Insecure bool
}

// New creates a new telemetry instance.
//
// When cfg.Enabled is false, no-op providers are installed: the instruments are
// still created so callers need no nil handling, but nothing is exported and no
// connection is opened. When cfg.Enabled is true and cfg.OTELAddress is empty,
// New returns ErrMissingOTELAddress rather than silently discarding metrics.
func New(ctx context.Context, cfg Config) (*Telemetry, error) {
	// Create resource with service attributes
	extraResources, _ := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.OTelScopeName(cfg.ServiceName),
		),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithContainer(),
		resource.WithHost(),
	)

	res, err := resource.Merge(
		resource.Default(),
		extraResources,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	var (
		meterProvider  metric.MeterProvider
		tracerProvider trace.TracerProvider
	)

	switch {
	case !cfg.Enabled:
		slog.InfoContext(ctx, "telemetry disabled - metrics and traces will not be exported")

		meterProvider = noop.NewMeterProvider()
		tracerProvider = tracenoop.NewTracerProvider()
	case cfg.OTELAddress == "":
		return nil, ErrMissingOTELAddress
	default:
		// Report export failures at WARN. An unreachable collector means metrics
		// are silently not arriving, which is not routine -- but it also must not
		// take the process down, so it stays a log line rather than an error.
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			slog.WarnContext(ctx, "telemetry export failed",
				"component", "telemetry",
				"otel_address", cfg.OTELAddress,
				"err", err)
		}))

		// Create OTLP exporter
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTELAddress)}
		if cfg.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}

		exporter, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
		}

		// Create meter provider with resource
		meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
			sdkmetric.WithResource(res),
		)

		tracerProvider, err = newTracerProvider(ctx, cfg, res)
		if err != nil {
			return nil, err
		}
	}

	// Set global providers. The spans created by otelhttp, the instrumented
	// clients and the repository all come from the global tracer provider, which
	// is a no-op until one is installed -- so without this they were created and
	// thrown away, and the trace ids in the logs were always zero.
	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	// Create tracer and meter
	tracer := otel.Tracer(cfg.ServiceName)
	meter := otel.Meter(cfg.ServiceName)

	t := &Telemetry{
		meterProvider:  meterProvider,
		tracerProvider: tracerProvider,
		tracer:         tracer,
		meter:          meter,
	}

	// Initialize all metrics
	if err := t.initializeMetrics(cfg.Enabled); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return t, nil
}

// RecordDownload records download metrics.
func (t *Telemetry) RecordDownload(ctx context.Context, status string, duration time.Duration) {
	if t.downloadsTotal != nil {
		t.downloadsTotal.Add(ctx, 1,
			metric.WithAttributes(attribute.String("status", status)),
		)
	}

	if t.downloadDuration != nil {
		t.downloadDuration.Record(ctx, duration.Seconds(),
			metric.WithAttributes(attribute.String("status", status)),
		)
	}
}

// IncrementActiveDownloads increments active downloads counter.
func (t *Telemetry) IncrementActiveDownloads(ctx context.Context) {
	if t.downloadsActive != nil {
		t.downloadsActive.Add(ctx, 1)
	}
}

// DecrementActiveDownloads decrements active downloads counter.
func (t *Telemetry) DecrementActiveDownloads(ctx context.Context) {
	if t.downloadsActive != nil {
		t.downloadsActive.Add(ctx, -1)
	}
}

// RecordTransfer records transfer metrics.
func (t *Telemetry) RecordTransfer(ctx context.Context, operation, status string) {
	if t.transfersTotal != nil {
		t.transfersTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}
}

// RecordClientOperation records download client operation metrics.
func (t *Telemetry) RecordClientOperation(ctx context.Context, client, operation, status string) {
	if t.clientOperationsTotal != nil {
		t.clientOperationsTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("client", client),
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}

	if status == "error" && t.clientErrors != nil {
		t.clientErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("client", client),
				attribute.String("operation", operation),
			),
		)
	}
}

// RecordDBOperation records database operation metrics.
func (t *Telemetry) RecordDBOperation(ctx context.Context, operation, status string, duration time.Duration) {
	if t.dbOperationsTotal != nil {
		t.dbOperationsTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}

	if t.dbOperationDuration != nil {
		t.dbOperationDuration.Record(ctx, duration.Seconds(),
			metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}
}

// RecordTorrentType records a torrent add operation by type.
func (t *Telemetry) RecordTorrentType(ctx context.Context, torrentType string) {
	if t.torrentTypeCounter != nil {
		t.torrentTypeCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("torrent_type", torrentType),
			),
		)
	}
}

// Shutdown gracefully shuts down the telemetry system.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error

	if mp, ok := t.meterProvider.(*sdkmetric.MeterProvider); ok {
		errs = append(errs, mp.Shutdown(ctx))
	}

	if tp, ok := t.tracerProvider.(*sdktrace.TracerProvider); ok {
		errs = append(errs, tp.Shutdown(ctx))
	}

	return errors.Join(errs...)
}

// newTracerProvider exports spans over OTLP to the same collector as metrics,
// batched so that recording a span never waits on the network.
func newTracerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTELAddress)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	), nil
}

// initializeMetrics creates all metric instruments. The instruments are created
// even when telemetry is disabled -- they resolve to no-ops, which keeps callers
// free of nil checks. Runtime collection is skipped when disabled, since polling
// memstats to feed a no-op provider is pure waste.
func (t *Telemetry) initializeMetrics(enabled bool) error {
	// HTTP metrics are handled by otelhttp automatically
	if err := t.initializeBusinessMetrics(); err != nil {
		return err
	}

	if !enabled {
		return nil
	}

	return runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
}

func (t *Telemetry) initializeBusinessMetrics() error {
	var err error

	t.downloadsTotal, err = t.meter.Int64Counter(
		"downloads.total",
		metric.WithDescription("Total number of downloads"),
		metric.WithUnit("{download}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create downloads.total counter: %w", err)
	}

	t.downloadsActive, err = t.meter.Int64UpDownCounter(
		"downloads.active",
		metric.WithDescription("Number of active downloads"),
		metric.WithUnit("{download}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create downloads.active counter: %w", err)
	}

	t.downloadDuration, err = t.meter.Float64Histogram(
		"downloads.duration",
		metric.WithDescription("Download duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create downloads.duration histogram: %w", err)
	}

	t.transfersTotal, err = t.meter.Int64Counter(
		"transfers.total",
		metric.WithDescription("Total number of transfers"),
		metric.WithUnit("{transfer}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create transfers.total counter: %w", err)
	}

	t.clientOperationsTotal, err = t.meter.Int64Counter(
		"client.operations.total",
		metric.WithDescription("Total number of download client operations"),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create client.operations.total counter: %w", err)
	}

	t.clientErrors, err = t.meter.Int64Counter(
		"client.errors.total",
		metric.WithDescription("Total number of download client errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create client.errors.total counter: %w", err)
	}

	t.dbOperationsTotal, err = t.meter.Int64Counter(
		"db.operations.total",
		metric.WithDescription("Total number of database operations"),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create db.operations.total counter: %w", err)
	}

	t.dbOperationDuration, err = t.meter.Float64Histogram(
		"db.operations.duration",
		metric.WithDescription("Database operation duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create db.operations.duration histogram: %w", err)
	}

	t.torrentTypeCounter, err = t.meter.Int64Counter(
		"torrents.type.total",
		metric.WithDescription("Total torrents added by type (magnet vs metainfo)"),
		metric.WithUnit("{torrent}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create torrents.type.total counter: %w", err)
	}

	return nil
}
