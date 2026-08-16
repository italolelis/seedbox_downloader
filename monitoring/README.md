# Monitoring Setup

This directory contains the complete monitoring stack for the Seedbox Downloader application.

## 🚀 Quick Start

1. **Start the monitoring stack:**
   ```bash
   docker-compose -f docker-compose.telemetry.yml up -d
   ```

2. **Access the services:**
   - **Grafana Dashboard**: http://localhost:3000 (admin/admin)
   - **Prometheus**: http://localhost:9090
   - **Collector Metrics** (scraped by Prometheus): http://localhost:8889/metrics
   - **Main Application**: http://localhost:9091

## 📊 Dashboard Overview

The Grafana dashboard provides comprehensive monitoring across four key areas:

### 🔴 RED Metrics (Request-focused)
- **HTTP Request Rate**: Requests per second by method and path
- **HTTP Error Rate**: Percentage of 4xx/5xx responses
- **HTTP Response Time**: 95th percentile latency

### 💼 Business Metrics
- **Downloads by Status**: Pie chart showing success/failure distribution
- **Active Downloads**: Current number of downloads in progress
- **Active Transfers**: Current number of transfers being processed
- **Download Duration**: Time taken for downloads to complete
- **Client Operations**: Rate of operations per download client
- **Client Errors**: Error rate by client and operation type

### 📈 USE Metrics (Resource-focused)
- **Memory Usage**: Application memory consumption over time
- **Goroutine Count**: Number of active goroutines
- **System Uptime**: How long the application has been running
- **System Error Rate**: Application errors by component

### 🗄️ Database Performance
- **Database Operations Rate**: Operations per second by type and status
- **Database Operation Duration**: Query performance metrics

## 🎨 Dashboard Features

- **Auto-refresh**: Updates every 30 seconds
- **Time range**: Default 1-hour view with customizable ranges
- **Color-coded thresholds**: Green/Yellow/Red indicators for health
- **Interactive legends**: Click to show/hide specific metrics
- **Responsive design**: Works on desktop and mobile
- **Dark theme**: Easy on the eyes for monitoring

## 📋 Key Metrics Explained

### RED Metrics
- **Rate**: `rate(http_requests_total[5m])` - Requests per second
- **Errors**: `rate(http_requests_total{status=~"4..|5.."}[5m]) / rate(http_requests_total[5m])` - Error percentage
- **Duration**: `histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))` - 95th percentile latency

### Business Metrics
- **Active Downloads**: `downloads_active` - Current downloads
- **Download Success Rate**: `downloads_total{status="success"}` vs `downloads_total{status="error"}`
- **Client Performance**: `rate(client_operations_total[5m])` by client type

### USE Metrics
- **Utilization**: `memory_usage_bytes` - Memory consumption
- **Saturation**: `goroutine_count` - Goroutine pressure
- **Errors**: `rate(system_errors_total[5m])` - System error rate

## 🚨 Alerting

The dashboard includes visual thresholds:
- **Green**: Normal operation
- **Yellow**: Warning levels (requires attention)
- **Red**: Critical levels (requires immediate action)

For production, configure Prometheus alerting rules in `alert_rules.yml`.

## 🔧 Customization

### Adding New Panels
1. Edit `dashboards/seedbox-downloader.json`
2. Add new panel configuration
3. Restart Grafana or wait for auto-reload

### Modifying Thresholds
Update the `thresholds` section in panel configurations:
```json
"thresholds": {
  "mode": "absolute",
  "steps": [
    {"color": "green", "value": null},
    {"color": "yellow", "value": 50},
    {"color": "red", "value": 100}
  ]
}
```

### Custom Queries
All panels use PromQL queries. Examples:
- `rate(metric_name[5m])` - Rate over 5 minutes
- `histogram_quantile(0.95, rate(metric_bucket[5m]))` - 95th percentile
- `increase(metric_total[1h])` - Total increase over 1 hour

## 🐳 Docker Configuration

The monitoring stack includes:
- **Prometheus**: Metrics collection and storage
- **Grafana**: Visualization and dashboards
- **Application**: Seedbox downloader with telemetry

All services are connected via Docker network for seamless communication.

## 📁 File Structure

```
monitoring/
├── README.md                          # This file
├── prometheus.yml                     # Prometheus configuration
├── alert_rules.yml                    # Alerting rules
└── grafana/
    ├── dashboards/
    │   ├── dashboard.yml              # Dashboard provisioning
    │   └── seedbox-downloader.json    # Main dashboard
    └── datasources/
        └── prometheus.yml             # Prometheus datasource
```

## 🔍 Troubleshooting

### Dashboard Not Loading
1. Check Grafana logs: `docker-compose logs grafana`
2. Verify Prometheus connectivity: http://localhost:9090/targets
3. Ensure the collector is exposing metrics: http://localhost:8889/metrics

### Missing Metrics
1. Verify application is running with telemetry enabled
2. Check Prometheus scraping: http://localhost:9090/targets
3. Confirm metric names in Prometheus: http://localhost:9090/graph

### Performance Issues
1. Reduce dashboard refresh rate
2. Limit time range for queries
3. Use recording rules for complex queries

## 📚 Resources

- [Grafana Documentation](https://grafana.com/docs/)
- [Prometheus Query Language](https://prometheus.io/docs/prometheus/latest/querying/)
- [RED Method](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/)
- [USE Method](http://www.brendangregg.com/usemethod.html)
