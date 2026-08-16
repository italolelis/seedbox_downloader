# Telemetry Implementation

This document describes the comprehensive telemetry implementation for the Seedbox Downloader, which provides RED (Rate, Errors, Duration) and USE (Utilization, Saturation, Errors) metrics using OpenTelemetry and Prometheus.

## Overview

The telemetry system provides:

- **HTTP Metrics**: Automatic HTTP instrumentation using `otelhttp` following OpenTelemetry semantic conventions
- **USE Metrics**: System resource utilization, saturation, and errors
- **Business Metrics**: Application-specific metrics for downloads, transfers, and client operations
- **OTLP Export**: Metrics pushed to an OpenTelemetry collector, with resource attributes attached

> **Tracing is not currently wired.** The instrumentation and spans described below all
> exist, but no tracer provider is installed, so spans are created against a no-op
> tracer and never leave the process. Metrics work; traces do not. Tracked separately.

## Configuration

Telemetry is configured via environment variables:

```bash
# Enable/disable telemetry (default: false -- telemetry is opt-in)
TELEMETRY_ENABLED=true

# OTLP gRPC collector address (default: 0.0.0.0:4317).
# Required when telemetry is enabled -- startup fails if it is empty.
TELEMETRY_OTEL_ADDRESS=otel-collector:4317

# Send OTLP over plaintext gRPC rather than TLS (default: true).
# The OTLP exporter defaults to TLS, so against an ordinary plaintext collector
# this must stay true or the export fails with:
#   tls: first record does not look like a TLS handshake
# Set false when the collector is reached over a network you do not trust.
TELEMETRY_OTEL_INSECURE=true

# Service name for telemetry (default: seedbox_downloader)
TELEMETRY_SERVICE_NAME=seedbox_downloader

# Service version for telemetry (default: 1.0.0)
TELEMETRY_SERVICE_VERSION=1.0.0
```

Telemetry is **opt-in**: with `TELEMETRY_ENABLED` unset or false, no exporter is
constructed and no connection is attempted. Enabling it without an address is a
startup error rather than a silent no-op.

## Metrics Exposed

### HTTP Metrics (Automatic via otelhttp)

The following metrics are automatically collected by `otelhttp` middleware following OpenTelemetry semantic conventions:

- `http.server.request.duration` - HTTP server request duration histogram (seconds)
- `http.server.request.body.size` - HTTP server request body size histogram (bytes)
- `http.server.response.body.size` - HTTP server response body size histogram (bytes)

**Attributes included:**
- `http.request.method` - HTTP method (GET, POST, etc.)
- `http.response.status_code` - HTTP status code
- `server.address` - Server address
- `server.port` - Server port
- `url.scheme` - URL scheme (http/https)

### USE Metrics (System Resources)

Following OpenTelemetry semantic conventions:

- `process.cpu.utilization` - Process CPU utilization (0-1 scale)
- `process.memory.usage` - Process memory usage in bytes
- `process.runtime.go.goroutines` - Number of active goroutines
- `system.filesystem.usage` - Filesystem usage in bytes
- `process.uptime` - Process uptime in seconds

### Business Metrics

Application-specific metrics following OpenTelemetry naming conventions:

#### Downloads
- `downloads.total` - Total number of downloads by status
- `downloads.active` - Number of active downloads
- `downloads.duration` - Download duration histogram (seconds)

#### Transfers
- `transfers.total` - Total number of transfers by operation and status
- `transfers.active` - Number of active transfers

#### Download Client Operations
- `client.operations.total` - Total client operations by client type, operation, and status
- `client.errors.total` - Total client errors by client type and operation

#### Database Operations
- `db.operations.total` - Total database operations by operation and status
- `db.operations.duration` - Database operation duration histogram (seconds)

#### System Health
- `system.errors.total` - Total system errors by component and error type

**Note**: All business metrics are automatically associated with the service through OpenTelemetry resource attributes (`service.name`, `service.version`).

## Instrumentation

### HTTP Middleware (otelhttp)

All HTTP requests are automatically instrumented using the standard `otelhttp` package:

- **Automatic Metrics**: Request duration, body sizes following OpenTelemetry semantic conventions
- **Distributed Tracing**: Automatic span creation with proper HTTP attributes
- **Context Propagation**: Trace context propagated across service boundaries
- **Standards Compliance**: Follows OpenTelemetry HTTP semantic conventions

**Implementation**:
```go
// Server middleware
middleware := telemetry.NewHTTPMiddleware(serviceName)
router.Use(middleware)

// Individual handler wrapping
handler := telemetry.NewHTTPHandler(myHandler, "operation_name")
```

### Database Operations

Database operations are instrumented via the `InstrumentedDownloadRepository`:
- Operation timing
- Success/failure tracking
- Distributed tracing

### Download Client Operations

Download client operations are instrumented via `InstrumentedDownloadClient` and `InstrumentedTransferClient`:
- Authentication tracking
- Transfer operations
- File operations
- Error tracking

### Custom Instrumentation

You can add custom instrumentation using the telemetry package:

```go
// Instrument a generic operation
err := telemetry.InstrumentOperation(ctx, "operation_name", "component", func(ctx context.Context) error {
    // Your operation here
    return nil
})

// Instrument a database operation
err := telemetry.InstrumentDBOperation(ctx, "select_users", func(ctx context.Context) error {
    // Database operation here
    return nil
})

// Instrument a client operation
err := telemetry.InstrumentClientOperation(ctx, "deluge", "get_torrents", func(ctx context.Context) error {
    // Client operation here
    return nil
})
```

## How Metrics Leave the Process

**OTLP is the only export path.** The application opens no metrics endpoint of its
own -- there is nothing to scrape on the application itself. Metrics are pushed
over OTLP/gRPC to `TELEMETRY_OTEL_ADDRESS` on a periodic interval.

If no collector is listening, export failures are logged at `WARN` and the
application carries on. An unreachable collector is an operational condition, not
a startup failure.

## Integration with Monitoring Stack

Prometheus cannot scrape the application directly. Run a collector that receives
OTLP and re-exposes the metrics in Prometheus format, then scrape the collector:

```
application --OTLP/gRPC--> collector --Prometheus /metrics--> Prometheus --> Grafana
```

The bundled stack does exactly this. See `monitoring/otel-collector.yml` for the
collector configuration and `monitoring/prometheus.yml` for the scrape job, which
targets `otel-collector:8889` rather than the application.

```yaml
scrape_configs:
  - job_name: 'seedbox-downloader'
    static_configs:
      - targets: ['otel-collector:8889']
    scrape_interval: 15s
    metrics_path: /metrics
```

### Grafana Dashboard

Key metrics to monitor:

1. **Request Rate**: `rate(http_server_request_duration_sum[5m])`
2. **Error Rate**: `rate(http_server_request_duration_count{http_response_status_code=~"5.."}[5m])`
3. **Request Duration**: `histogram_quantile(0.95, rate(http_server_request_duration_bucket[5m]))`
4. **Active Downloads**: `downloads_active`
5. **System Resources**: `process_memory_usage`, `process_runtime_go_goroutines`

**Note**: Metric names in Prometheus may have underscores instead of dots due to Prometheus naming conventions.

### Alerting Rules

Example Prometheus alerting rules:

```yaml
groups:
  - name: seedbox-downloader
    rules:
      - alert: HighErrorRate
        expr: rate(http_server_request_duration_count{http_response_status_code=~"5.."}[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High HTTP error rate detected"
          
      - alert: HighMemoryUsage
        expr: process_memory_usage > 1000000000  # 1GB
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High memory usage detected"
          
      - alert: DownloadClientErrors
        expr: rate(client_errors_total[5m]) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Download client errors detected"
          
      - alert: HighRequestDuration
        expr: histogram_quantile(0.95, rate(http_server_request_duration_bucket[5m])) > 2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High request duration detected (95th percentile > 2s)"
```

## Distributed Tracing

The application uses OpenTelemetry for distributed tracing with automatic instrumentation:

### Automatic Tracing (via otelhttp)
- **HTTP requests**: Automatic span creation with HTTP semantic conventions
- **Context propagation**: Trace context automatically propagated across service boundaries
- **Attributes**: Standard HTTP attributes (method, status code, URL, etc.)

### Manual Tracing
- **Database operations**: Custom spans for database queries
- **Download client operations**: Spans for external service calls
- **File downloads**: Operation-specific spans

### Trace Attributes
Following OpenTelemetry semantic conventions:
- `http.request.method`, `http.response.status_code`
- `service.name`, `service.version` (from resource attributes)
- `db.operation`, `db.statement` (for database operations)
- Custom business attributes for downloads and transfers

## Performance Impact

The telemetry implementation is designed to have minimal performance impact:

### HTTP Instrumentation
- **otelhttp**: Optimized for production use with minimal overhead
- **Automatic sampling**: Configurable trace sampling to control overhead
- **Efficient metrics**: Histogram buckets optimized for HTTP request patterns

### System Metrics
- **Collection interval**: System metrics collected every 15 seconds
- **Memory efficient**: Metrics stored in memory with bounded cardinality
- **Non-blocking**: Metrics collection doesn't block application threads

### Resource Usage
- **Memory**: Bounded metric cardinality prevents memory leaks
- **CPU**: Minimal CPU overhead from instrumentation
- **Network**: Metrics exposed via efficient HTTP endpoint

### Cardinality Management
- **Bounded Attributes**: All metric attributes use bounded value sets to prevent cardinality explosion
- **High-Cardinality Data**: Transfer IDs, names, and error messages are excluded from metric attributes
- **Safe Attributes**: Only operation types, status values, and client types are used as metric dimensions

## Troubleshooting

### Metrics Not Appearing

1. Check if telemetry is enabled: `TELEMETRY_ENABLED=true` (it is **off** by default)
2. Check the application logs for `telemetry export failed` warnings -- that means the
   collector is unreachable at `TELEMETRY_OTEL_ADDRESS`
3. If the warning mentions `tls: first record does not look like a TLS handshake`, the
   collector is plaintext and `TELEMETRY_OTEL_INSECURE` has been set to false
4. Verify the collector is re-exposing metrics: `curl http://localhost:8889/metrics`
5. Check Prometheus is scraping the collector: http://localhost:9090/targets

### Verifying the pipeline end to end

With the bundled stack running, this pushes real metrics through the real export path
and fails if the collector does not accept them:

```sh
docker compose -f docker-compose.telemetry.yml up -d otel-collector prometheus
TELEMETRY_LIVE_TEST=1 go test ./internal/telemetry/ -run TestLiveExport -v
```

### High Memory Usage

If you notice high memory usage from metrics:
1. Check for cardinality explosion in custom metrics
2. Verify that high-cardinality data (IDs, names, paths) are not used as metric attributes
3. Adjust system metrics collection interval
4. Consider implementing metric retention policies

### Cardinality Issues

Signs of high cardinality problems:
- Exponential growth in memory usage
- Slow Prometheus queries
- Large metrics payload sizes

**Prevention**:
- Never use unique identifiers (transfer IDs, file names) as metric attributes
- Limit attribute values to bounded sets (status: success/error, client: deluge/putio)
- Use high-cardinality data in logs and traces, not metrics

### Missing Business Metrics

Ensure that:
1. Operations are going through instrumented wrappers
2. Telemetry instance is properly passed to components
3. Context is properly propagated through the call chain

## OpenTelemetry Architecture

### Resource Attributes

The application uses OpenTelemetry resource attributes to identify the service:

```go
resource.New(ctx,
    resource.WithAttributes(
        semconv.ServiceNameKey.String("seedbox_downloader"),
        semconv.ServiceVersionKey.String(version),
    ),
)
```

This ensures all metrics and traces are properly attributed to the service without hardcoding service names in metric names.

### Component Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   HTTP Client   │───▶│  otelhttp        │───▶│   Application   │
│                 │    │  Middleware      │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │                        │
                                ▼                        ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Prometheus    │◀───│  OTel Prometheus │◀───│  Instrumented   │
│    Server       │    │    Exporter      │    │   Components    │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ▼
                       ┌──────────────────┐
                       │  Metrics Server  │
                       │ (otelhttp wrapped)│
                       └──────────────────┘
```

### Key Components

1. **otelhttp**: Provides automatic HTTP instrumentation following OpenTelemetry semantic conventions
2. **Resource Attributes**: Service identification without hardcoded prefixes
3. **Prometheus Exporter**: Converts OpenTelemetry metrics to Prometheus format
4. **Instrumented Metrics Endpoint**: Even the `/metrics` endpoint is instrumented for complete observability

The telemetry system follows OpenTelemetry best practices and provides comprehensive observability for monitoring, alerting, and debugging.
