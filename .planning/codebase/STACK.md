# Technology Stack

**Analysis Date:** 2026-01-31

## Languages

**Primary:**
- Go 1.23.0 (with toolchain go1.23.8) - All source code, build system

**Secondary:**
- JSON - Configuration and data serialization
- YAML - Docker Compose configuration

## Runtime

**Environment:**
- Go runtime 1.23.8
- Linux (distroless container image)

**Build:**
- Docker - Multi-stage build (golang:1.23 builder → distroless/cc:nonroot)
- CGO enabled (required for SQLite support)

## Frameworks

**Core:**
- Chi Router v5.2.1 - HTTP routing and middleware
- Go standard library `net/http` - HTTP server

**Testing:**
- Testify v1.11.1 - Assertion library and mocking

**Build/Dev:**
- golangci-lint - Code linting (config: `.golangci.yml`)
- envconfig v1.4.0 - Environment variable parsing

## Key Dependencies

**Critical:**
- go.opentelemetry.io/* v1.38.0 - Observability framework (metrics, tracing, logging)
  - `go.opentelemetry.io/otel` - Core telemetry
  - `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` - HTTP instrumentation
  - `go.opentelemetry.io/contrib/instrumentation/runtime` - Runtime metrics
  - `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` - OTLP metric export
  - `go.opentelemetry.io/otel/exporters/prometheus` - Prometheus exporter

**Data Storage:**
- github.com/mattn/go-sqlite3 v1.14.14 - SQLite database driver (CGO required)

**External Integrations:**
- github.com/putdotio/go-putio v1.7.2 - Put.io API client for download transfers
- golang.org/x/oauth2 v0.30.0 - OAuth2 authentication (used with Put.io)

**Utilities:**
- github.com/dustin/go-humanize v1.0.1 - Human-readable formatting
- github.com/kelseyhightower/envconfig v1.4.0 - Environment configuration
- golang.org/x/sync v0.16.0 - Synchronization primitives (sync.ErrGroup, etc.)

**Telemetry Support:**
- github.com/prometheus/client_golang v1.20.5 - Prometheus metrics (indirect dependency)
- github.com/samber/lo v1.51.0 - Utility functions (indirect)
- github.com/samber/slog-common v0.19.0 - Structured logging utilities
- github.com/samber/slog-multi v1.5.0 - Multi-handler logging
- go.opentelemetry.io/auto/sdk v1.1.0 - Automatic instrumentation
- go.opentelemetry.io/contrib/bridges/otelslog v0.13.0 - slog bridge for OTel

## Configuration

**Environment Variables:**

Application:
- `DOWNLOAD_CLIENT` (default: "deluge") - Choose between "deluge" or "putio"
- `DOWNLOAD_DIR` (required) - Local directory for downloads
- `TARGET_LABEL` - Label/tag for filtering transfers
- `KEEP_DOWNLOADED_FOR` (default: "24h") - Retention period for downloaded files
- `POLLING_INTERVAL` (default: "10m") - Check interval for new transfers
- `CLEANUP_INTERVAL` (default: "10m") - Cleanup task interval
- `LOG_LEVEL` (default: "INFO") - Logging level
- `MAX_PARALLEL` (default: "5") - Concurrent downloads
- `DB_PATH` (default: "downloads.db") - SQLite database path
- `DISCORD_WEBHOOK_URL` - Webhook for notifications (optional)

Deluge Client:
- `DELUGE_BASE_URL` - Deluge server URL
- `DELUGE_API_URL_PATH` - API endpoint path
- `DELUGE_USERNAME` - Deluge username
- `DELUGE_PASSWORD` - Deluge password
- `DELUGE_COMPLETED_DIR` - Directory for completed downloads in Deluge

Put.io Client:
- `PUTIO_TOKEN` - Put.io API token
- `PUTIO_BASE_DIR` - Base directory in Put.io

*Arr Services:
- `SONARR_API_KEY` - Sonarr API key
- `SONARR_BASE_URL` - Sonarr base URL
- `RADARR_API_KEY` - Radarr API key
- `RADARR_BASE_URL` - Radarr base URL

Transmission/Legacy:
- `TRANSMISSION_USERNAME` - Transmission username
- `TRANSMISSION_PASSWORD` - Transmission password

Web Server:
- `WEB_BIND_ADDRESS` (default: "0.0.0.0:9091") - Server bind address
- `WEB_READ_TIMEOUT` (default: "30s") - HTTP read timeout
- `WEB_WRITE_TIMEOUT` (default: "30s") - HTTP write timeout
- `WEB_IDLE_TIMEOUT` (default: "5s") - HTTP idle timeout
- `WEB_SHUTDOWN_TIMEOUT` (default: "30s") - Graceful shutdown timeout

Telemetry:
- `TELEMETRY_ENABLED` (default: "true") - Enable telemetry collection
- `TELEMETRY_OTEL_ADDRESS` (default: "0.0.0.0:4317") - OTLP collector gRPC address
- `TELEMETRY_SERVICE_NAME` (default: "seedbox_downloader") - Service name for telemetry

**Build Configuration:**
- `Dockerfile` - Multi-stage build with distroless base
- `docker-compose.telemetry.yml` - Stack with Prometheus and Grafana
- `.golangci.yml` - Linting configuration

## Platform Requirements

**Development:**
- Go 1.23.0 or later
- CGO enabled for SQLite
- Standard Unix build tools (GCC/Clang for CGO)

**Production:**
- Docker runtime (recommended via distroless image)
- OTLP collector endpoint (optional, for telemetry export)
- 100-200MB RAM (typical for small workloads)
- Disk space for SQLite database and downloads

**Deployment Targets:**
- Docker/OCI containers (primary)
- Linux/Unix systems
- Cloud platforms supporting Linux containers (Kubernetes, Docker Swarm, etc.)

**Networking:**
- Port 9091 for HTTP API (main application)
- Port 2112 for Prometheus metrics (optional)
- Port 4317 for OTLP gRPC (optional, for telemetry)

---

*Stack analysis: 2026-01-31*
