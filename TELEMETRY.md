# Telemetry Implementation

This document describes the comprehensive telemetry implementation for the Seedbox Downloader, which provides RED (Rate, Errors, Duration) and USE (Utilization, Saturation, Errors) metrics using OpenTelemetry and Prometheus.

## Overview

The telemetry system provides:

- **RED Metrics**: Request rate, error rate, and request duration for HTTP endpoints
- **USE Metrics**: System resource utilization, saturation, and errors
- **Business Metrics**: Application-specific metrics for downloads, transfers, and client operations
- **Distributed Tracing**: Request tracing across components
- **Prometheus Integration**: Metrics exposed in Prometheus format

## Configuration

Telemetry is configured via environment variables:

```bash
# Enable/disable telemetry (default: true)
TELEMETRY_ENABLED=true

# Metrics server address (default: 0.0.0.0:2112)
TELEMETRY_METRICS_ADDRESS=0.0.0.0:2112

# Metrics endpoint path (default: /metrics)
TELEMETRY_METRICS_PATH=/metrics

# Service name for telemetry (default: seedbox_downloader)
TELEMETRY_SERVICE_NAME=seedbox_downloader

# Service version for telemetry (default: 1.0.0)
TELEMETRY_SERVICE_VERSION=1.0.0
```

The metrics server runs on a separate address from the main API server, using chi router for consistency with the main application architecture.

## Metrics Exposed

### RED Metrics (HTTP)

- `http_requests_total` - Total number of HTTP requests by method, path, and status
- `http_request_duration_seconds` - HTTP request duration histogram
- `http_requests_in_flight` - Number of HTTP requests currently being processed

### USE Metrics (System Resources)

- `cpu_usage_percent` - CPU usage percentage
- `memory_usage_bytes` - Memory usage in bytes
- `goroutine_count` - Number of active goroutines
- `disk_usage_bytes` - Disk usage in bytes
- `system_uptime_seconds` - System uptime in seconds

### Business Metrics

#### Downloads
- `downloads_total` - Total number of downloads by status
- `downloads_active` - Number of active downloads
- `download_duration_seconds` - Download duration histogram

#### Transfers
- `transfers_total` - Total number of transfers by operation and status
- `transfers_active` - Number of active transfers

#### Download Client Operations
- `client_operations_total` - Total client operations by client type, operation, and status
- `client_errors_total` - Total client errors by client type and operation

#### Database Operations
- `db_operations_total` - Total database operations by operation and status
- `db_operation_duration_seconds` - Database operation duration histogram

#### System Health
- `system_errors_total` - Total system errors by component and error type

## Instrumentation

### HTTP Middleware

All HTTP requests are automatically instrumented with:
- Request counting
- Duration measurement
- Error tracking
- In-flight request tracking
- Distributed tracing

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

## Metrics Endpoint

Metrics are exposed on a separate HTTP server to avoid interfering with the main application:

- **URL**: `http://localhost:2112/metrics` (default)
- **Format**: Prometheus exposition format
- **Content-Type**: `text/plain`

## Integration with Monitoring Stack

### Prometheus Configuration

Add the following job to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'seedbox-downloader'
    static_configs:
      - targets: ['localhost:2112']
    scrape_interval: 15s
    metrics_path: /metrics
```

### Grafana Dashboard

Key metrics to monitor:

1. **Request Rate**: `rate(http_requests_total[5m])`
2. **Error Rate**: `rate(http_requests_total{status=~"4..|5.."}[5m])`
3. **Request Duration**: `histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))`
4. **Active Downloads**: `downloads_active`
5. **System Resources**: `memory_usage_bytes`, `goroutine_count`

### Alerting Rules

Example Prometheus alerting rules:

```yaml
groups:
  - name: seedbox-downloader
    rules:
      - alert: HighErrorRate
        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High error rate detected"
          
      - alert: HighMemoryUsage
        expr: memory_usage_bytes > 1000000000  # 1GB
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
```

## Distributed Tracing

The application creates traces for:
- HTTP requests
- Database operations
- Download client operations
- File downloads

Traces include:
- Operation timing
- Error information
- Request/response attributes
- Component identification

## Performance Impact

The telemetry implementation is designed to have minimal performance impact:
- Metrics collection is non-blocking
- System metrics are collected every 15 seconds
- Tracing uses sampling to reduce overhead
- Metrics are stored in memory and exposed via HTTP

## Troubleshooting

### Metrics Not Appearing

1. Check if telemetry is enabled: `TELEMETRY_ENABLED=true`
2. Verify metrics endpoint is accessible: `curl http://localhost:2112/metrics`
3. Check application logs for telemetry initialization errors

### High Memory Usage

If you notice high memory usage from metrics:
1. Reduce metric cardinality by limiting label values
2. Adjust system metrics collection interval
3. Consider implementing metric retention policies

### Missing Business Metrics

Ensure that:
1. Operations are going through instrumented wrappers
2. Telemetry instance is properly passed to components
3. Context is properly propagated through the call chain

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   HTTP Client   │───▶│  HTTP Middleware │───▶│   Application   │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │                        │
                                ▼                        ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Prometheus    │◀───│   Telemetry      │◀───│  Instrumented   │
│    Server       │    │    Package       │    │   Components    │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │
                                ▼
                       ┌──────────────────┐
                       │  Metrics Server  │
                       │  (Chi Router)    │
                       └──────────────────┘
```

The telemetry system is fully integrated into the application lifecycle and provides comprehensive observability for monitoring, alerting, and debugging. Both the main API server and metrics server use chi router for consistency and standardization.
