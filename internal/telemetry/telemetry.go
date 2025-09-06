package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry holds all telemetry instruments and providers.
type Telemetry struct {
	meterProvider metric.MeterProvider
	tracer        trace.Tracer
	meter         metric.Meter
	exporter      *prometheus.Exporter

	// RED Metrics are now handled by otelhttp automatically

	// USE Metrics (Utilization, Saturation, Errors)
	cpuUsage       metric.Float64Gauge
	memoryUsage    metric.Int64Gauge
	goroutineCount metric.Int64Gauge
	diskUsage      metric.Int64Gauge

	// Business Metrics
	downloadsTotal        metric.Int64Counter
	downloadsActive       metric.Int64UpDownCounter
	downloadDuration      metric.Float64Histogram
	transfersTotal        metric.Int64Counter
	transfersActive       metric.Int64UpDownCounter
	clientOperationsTotal metric.Int64Counter
	clientErrors          metric.Int64Counter
	dbOperationsTotal     metric.Int64Counter
	dbOperationDuration   metric.Float64Histogram

	// System health
	systemErrors metric.Int64Counter
	systemUptime metric.Float64Gauge
}

// Config holds telemetry configuration.
type Config struct {
	Enabled        bool
	ServiceName    string
	ServiceVersion string
}

// New creates a new telemetry instance.
func New(ctx context.Context, cfg Config) (*Telemetry, error) {
	if !cfg.Enabled {
		return &Telemetry{}, nil
	}

	// Create resource with service attributes
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create Prometheus exporter
	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// Create meter provider with resource
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)

	// Set global meter provider
	otel.SetMeterProvider(meterProvider)

	// Create tracer and meter
	tracer := otel.Tracer(cfg.ServiceName)
	meter := otel.Meter(cfg.ServiceName)

	t := &Telemetry{
		meterProvider: meterProvider,
		tracer:        tracer,
		meter:         meter,
		exporter:      exporter,
	}

	// Initialize all metrics
	if err := t.initializeMetrics(); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	// Start system metrics collection
	go t.collectSystemMetrics(ctx)

	return t, nil
}

// Tracer returns the OpenTelemetry tracer.
func (t *Telemetry) Tracer() trace.Tracer {
	return t.tracer
}

// Meter returns the OpenTelemetry meter.
func (t *Telemetry) Meter() metric.Meter {
	return t.meter
}

// HTTP metrics are now automatically handled by otelhttp middleware

// RecordDownload records download metrics.
func (t *Telemetry) RecordDownload(status string, duration time.Duration) {
	if t.downloadsTotal != nil {
		t.downloadsTotal.Add(context.Background(), 1,
			metric.WithAttributes(attribute.String("status", status)),
		)
	}

	if t.downloadDuration != nil {
		t.downloadDuration.Record(context.Background(), duration.Seconds(),
			metric.WithAttributes(attribute.String("status", status)),
		)
	}
}

// IncrementActiveDownloads increments active downloads counter.
func (t *Telemetry) IncrementActiveDownloads() {
	if t.downloadsActive != nil {
		t.downloadsActive.Add(context.Background(), 1)
	}
}

// DecrementActiveDownloads decrements active downloads counter.
func (t *Telemetry) DecrementActiveDownloads() {
	if t.downloadsActive != nil {
		t.downloadsActive.Add(context.Background(), -1)
	}
}

// RecordTransfer records transfer metrics.
func (t *Telemetry) RecordTransfer(operation, status string) {
	if t.transfersTotal != nil {
		t.transfersTotal.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}
}

// IncrementActiveTransfers increments active transfers counter.
func (t *Telemetry) IncrementActiveTransfers() {
	if t.transfersActive != nil {
		t.transfersActive.Add(context.Background(), 1)
	}
}

// DecrementActiveTransfers decrements active transfers counter.
func (t *Telemetry) DecrementActiveTransfers() {
	if t.transfersActive != nil {
		t.transfersActive.Add(context.Background(), -1)
	}
}

// RecordClientOperation records download client operation metrics.
func (t *Telemetry) RecordClientOperation(client, operation, status string) {
	if t.clientOperationsTotal != nil {
		t.clientOperationsTotal.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("client", client),
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}

	if status == "error" && t.clientErrors != nil {
		t.clientErrors.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("client", client),
				attribute.String("operation", operation),
			),
		)
	}
}

// RecordDBOperation records database operation metrics.
func (t *Telemetry) RecordDBOperation(operation, status string, duration time.Duration) {
	if t.dbOperationsTotal != nil {
		t.dbOperationsTotal.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}

	if t.dbOperationDuration != nil {
		t.dbOperationDuration.Record(context.Background(), duration.Seconds(),
			metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("status", status),
			),
		)
	}
}

// RecordSystemError records system error metrics.
func (t *Telemetry) RecordSystemError(component, errorType string) {
	if t.systemErrors != nil {
		t.systemErrors.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("component", component),
				attribute.String("error_type", errorType),
			),
		)
	}
}

// Handler returns the HTTP handler for metrics endpoint with OpenTelemetry instrumentation.
func (t *Telemetry) Handler() http.Handler {
	if t.exporter == nil {
		return http.NotFoundHandler()
	}

	// Wrap the Prometheus handler with OpenTelemetry instrumentation
	prometheusHandler := promhttp.Handler()

	return otelhttp.NewHandler(prometheusHandler, "metrics")
}

// Shutdown gracefully shuts down the telemetry system.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if mp, ok := t.meterProvider.(*sdkmetric.MeterProvider); ok {
		return mp.Shutdown(ctx)
	}

	return nil
}

// initializeMetrics creates all metric instruments.
func (t *Telemetry) initializeMetrics() error {
	// HTTP metrics are handled by otelhttp automatically
	if err := t.initializeUSEMetrics(); err != nil {
		return err
	}

	if err := t.initializeBusinessMetrics(); err != nil {
		return err
	}

	return t.initializeSystemMetrics()
}

func (t *Telemetry) initializeUSEMetrics() error {
	var err error

	t.cpuUsage, err = t.meter.Float64Gauge(
		"process.cpu.utilization",
		metric.WithDescription("Process CPU utilization"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create process.cpu.utilization gauge: %w", err)
	}

	t.memoryUsage, err = t.meter.Int64Gauge(
		"process.memory.usage",
		metric.WithDescription("Process memory usage"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return fmt.Errorf("failed to create process.memory.usage gauge: %w", err)
	}

	t.goroutineCount, err = t.meter.Int64Gauge(
		"process.runtime.go.goroutines",
		metric.WithDescription("Number of goroutines"),
		metric.WithUnit("{goroutine}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create process.runtime.go.goroutines gauge: %w", err)
	}

	t.diskUsage, err = t.meter.Int64Gauge(
		"system.filesystem.usage",
		metric.WithDescription("Filesystem usage"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return fmt.Errorf("failed to create system.filesystem.usage gauge: %w", err)
	}

	return nil
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

	t.transfersActive, err = t.meter.Int64UpDownCounter(
		"transfers.active",
		metric.WithDescription("Number of active transfers"),
		metric.WithUnit("{transfer}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create transfers.active counter: %w", err)
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

	return nil
}

func (t *Telemetry) initializeSystemMetrics() error {
	var err error

	t.systemErrors, err = t.meter.Int64Counter(
		"system.errors.total",
		metric.WithDescription("Total number of system errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create system.errors.total counter: %w", err)
	}

	t.systemUptime, err = t.meter.Float64Gauge(
		"process.uptime",
		metric.WithDescription("Process uptime"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create process.uptime gauge: %w", err)
	}

	return nil
}

// collectSystemMetrics collects system-level metrics periodically.
func (t *Telemetry) collectSystemMetrics(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.updateSystemMetrics(ctx, startTime)
		}
	}
}

// updateSystemMetrics updates system-level metrics.
func (t *Telemetry) updateSystemMetrics(ctx context.Context, startTime time.Time) {
	var m runtime.MemStats

	runtime.ReadMemStats(&m)

	// Memory usage
	if t.memoryUsage != nil {
		t.memoryUsage.Record(ctx, int64(m.Alloc))
	}

	// Goroutine count
	if t.goroutineCount != nil {
		t.goroutineCount.Record(ctx, int64(runtime.NumGoroutine()))
	}

	// System uptime
	if t.systemUptime != nil {
		uptime := time.Since(startTime).Seconds()
		t.systemUptime.Record(ctx, uptime)
	}
}
