package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry holds all telemetry instruments and providers.
type Telemetry struct {
	meterProvider metric.MeterProvider
	tracer        trace.Tracer
	meter         metric.Meter
	exporter      *prometheus.Exporter

	// RED Metrics (Rate, Errors, Duration)
	httpRequestsTotal    metric.Int64Counter
	httpRequestDuration  metric.Float64Histogram
	httpRequestsInFlight metric.Int64UpDownCounter

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

	// Create Prometheus exporter
	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// Create meter provider
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
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

// RecordHTTPRequest records HTTP request metrics.
func (t *Telemetry) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	if t.httpRequestsTotal != nil {
		t.httpRequestsTotal.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("method", method),
				attribute.String("path", path),
				attribute.String("status", status),
			),
		)
	}

	if t.httpRequestDuration != nil {
		t.httpRequestDuration.Record(context.Background(), duration.Seconds(),
			metric.WithAttributes(
				attribute.String("method", method),
				attribute.String("path", path),
				attribute.String("status", status),
			),
		)
	}
}

// IncrementHTTPInFlight increments in-flight HTTP requests.
func (t *Telemetry) IncrementHTTPInFlight() {
	if t.httpRequestsInFlight != nil {
		t.httpRequestsInFlight.Add(context.Background(), 1)
	}
}

// DecrementHTTPInFlight decrements in-flight HTTP requests.
func (t *Telemetry) DecrementHTTPInFlight() {
	if t.httpRequestsInFlight != nil {
		t.httpRequestsInFlight.Add(context.Background(), -1)
	}
}

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

// Handler returns the HTTP handler for metrics endpoint.
func (t *Telemetry) Handler() http.Handler {
	if t.exporter == nil {
		return http.NotFoundHandler()
	}

	// Return the standard Prometheus HTTP handler
	return promhttp.Handler()
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
	if err := t.initializeREDMetrics(); err != nil {
		return err
	}

	if err := t.initializeUSEMetrics(); err != nil {
		return err
	}

	if err := t.initializeBusinessMetrics(); err != nil {
		return err
	}

	return t.initializeSystemMetrics()
}

func (t *Telemetry) initializeREDMetrics() error {
	var err error

	t.httpRequestsTotal, err = t.meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create http_requests_total counter: %w", err)
	}

	t.httpRequestDuration, err = t.meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("HTTP request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create http_request_duration histogram: %w", err)
	}

	t.httpRequestsInFlight, err = t.meter.Int64UpDownCounter(
		"http_requests_in_flight",
		metric.WithDescription("Number of HTTP requests currently being processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create http_requests_in_flight counter: %w", err)
	}

	return nil
}

func (t *Telemetry) initializeUSEMetrics() error {
	var err error

	t.cpuUsage, err = t.meter.Float64Gauge(
		"cpu_usage_percent",
		metric.WithDescription("CPU usage percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return fmt.Errorf("failed to create cpu_usage gauge: %w", err)
	}

	t.memoryUsage, err = t.meter.Int64Gauge(
		"memory_usage_bytes",
		metric.WithDescription("Memory usage in bytes"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return fmt.Errorf("failed to create memory_usage gauge: %w", err)
	}

	t.goroutineCount, err = t.meter.Int64Gauge(
		"goroutine_count",
		metric.WithDescription("Number of goroutines"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create goroutine_count gauge: %w", err)
	}

	t.diskUsage, err = t.meter.Int64Gauge(
		"disk_usage_bytes",
		metric.WithDescription("Disk usage in bytes"),
		metric.WithUnit("bytes"),
	)
	if err != nil {
		return fmt.Errorf("failed to create disk_usage gauge: %w", err)
	}

	return nil
}

func (t *Telemetry) initializeBusinessMetrics() error {
	var err error

	t.downloadsTotal, err = t.meter.Int64Counter(
		"downloads_total",
		metric.WithDescription("Total number of downloads"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create downloads_total counter: %w", err)
	}

	t.downloadsActive, err = t.meter.Int64UpDownCounter(
		"downloads_active",
		metric.WithDescription("Number of active downloads"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create downloads_active counter: %w", err)
	}

	t.downloadDuration, err = t.meter.Float64Histogram(
		"download_duration_seconds",
		metric.WithDescription("Download duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create download_duration histogram: %w", err)
	}

	t.transfersTotal, err = t.meter.Int64Counter(
		"transfers_total",
		metric.WithDescription("Total number of transfers"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create transfers_total counter: %w", err)
	}

	t.transfersActive, err = t.meter.Int64UpDownCounter(
		"transfers_active",
		metric.WithDescription("Number of active transfers"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create transfers_active counter: %w", err)
	}

	t.clientOperationsTotal, err = t.meter.Int64Counter(
		"client_operations_total",
		metric.WithDescription("Total number of download client operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create client_operations_total counter: %w", err)
	}

	t.clientErrors, err = t.meter.Int64Counter(
		"client_errors_total",
		metric.WithDescription("Total number of download client errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create client_errors counter: %w", err)
	}

	t.dbOperationsTotal, err = t.meter.Int64Counter(
		"db_operations_total",
		metric.WithDescription("Total number of database operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create db_operations_total counter: %w", err)
	}

	t.dbOperationDuration, err = t.meter.Float64Histogram(
		"db_operation_duration_seconds",
		metric.WithDescription("Database operation duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create db_operation_duration histogram: %w", err)
	}

	return nil
}

func (t *Telemetry) initializeSystemMetrics() error {
	var err error

	t.systemErrors, err = t.meter.Int64Counter(
		"system_errors_total",
		metric.WithDescription("Total number of system errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create system_errors counter: %w", err)
	}

	t.systemUptime, err = t.meter.Float64Gauge(
		"system_uptime_seconds",
		metric.WithDescription("System uptime in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create system_uptime gauge: %w", err)
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
			t.updateSystemMetrics(startTime)
		}
	}
}

// updateSystemMetrics updates system-level metrics.
func (t *Telemetry) updateSystemMetrics(startTime time.Time) {
	var m runtime.MemStats

	runtime.ReadMemStats(&m)

	// Memory usage
	if t.memoryUsage != nil {
		t.memoryUsage.Record(context.Background(), int64(m.Alloc))
	}

	// Goroutine count
	if t.goroutineCount != nil {
		t.goroutineCount.Record(context.Background(), int64(runtime.NumGoroutine()))
	}

	// System uptime
	if t.systemUptime != nil {
		uptime := time.Since(startTime).Seconds()
		t.systemUptime.Record(context.Background(), uptime)
	}
}
