# External Integrations

**Analysis Date:** 2026-01-31

## APIs & External Services

**Download Clients:**
- Deluge - Download manager with JSON-RPC API
  - SDK/Client: Built-in via `net/http` (JSON-RPC over HTTP)
  - Auth: Username/password authentication
  - Location: `internal/dc/deluge/client.go`
  - Operations: Get torrent status, retrieve files, manage downloads
  - TLS: Supports insecure skip verification

- Put.io - Cloud download service
  - SDK/Client: github.com/putdotio/go-putio v1.7.2
  - Auth: OAuth2 token-based (via golang.org/x/oauth2)
  - Location: `internal/dc/putio/client.go`
  - Operations: List transfers, fetch files, manage downloads
  - API: RESTful HTTP API with OAuth2 protection

**PVR/Media Management:**
- Sonarr - TV series management and automation
  - Protocol: HTTP REST API (v3)
  - Auth: API key header (X-Api-Key)
  - Location: `internal/svc/arr/arr.go`
  - Operations: Check if content imported via history API
  - Endpoint pattern: `{BASE_URL}/api/v3/history`
  - Configuration: Environment variables `SONARR_BASE_URL`, `SONARR_API_KEY`

- Radarr - Movie management and automation
  - Protocol: HTTP REST API (v3)
  - Auth: API key header (X-Api-Key)
  - Location: `internal/svc/arr/arr.go`
  - Operations: Check if content imported via history API
  - Endpoint pattern: `{BASE_URL}/api/v3/history`
  - Configuration: Environment variables `RADARR_BASE_URL`, `RADARR_API_KEY`

## Data Storage

**Databases:**
- SQLite (local file)
  - Driver: github.com/mattn/go-sqlite3 v1.14.14
  - Location: File specified by `DB_PATH` environment variable (default: `downloads.db`)
  - Client: `database/sql` standard library
  - Schema: Single table `downloads` with columns:
    - `transfer_id` (TEXT UNIQUE) - Unique identifier for transfer
    - `downloaded_at` (DATETIME) - Timestamp of download
    - `status` (TEXT) - Status: 'pending', 'downloaded', 'failed'
    - `locked_by` (TEXT) - Lock mechanism for distributed processing
  - Location: `internal/storage/sqlite/init.go`, `internal/storage/sqlite/download_repository.go`
  - Instrumentation: Wrapped with telemetry metrics in `internal/storage/sqlite/instrumented_repository.go`

**File Storage:**
- Local filesystem only
  - Download directory: Environment variable `DOWNLOAD_DIR` (required)
  - Configuration per client:
    - Deluge: `DELUGE_COMPLETED_DIR` for completed torrent location
    - Put.io: `PUTIO_BASE_DIR` for base directory in Put.io

## Authentication & Identity

**Auth Providers:**
- Custom OAuth2 (Put.io)
  - Implementation: golang.org/x/oauth2 v0.30.0
  - Token source: Static token from environment variable `PUTIO_TOKEN`
  - Location: `internal/dc/putio/client.go` line 27-30

- API Key authentication (Sonarr/Radarr)
  - Implementation: HTTP header `X-Api-Key`
  - Tokens stored in: `SONARR_API_KEY`, `RADARR_API_KEY`
  - Location: `internal/svc/arr/arr.go` line 55

- Basic authentication (Deluge, Transmission)
  - Implementation: Username/password credentials
  - Stored in environment variables
  - Deluge: `DELUGE_USERNAME`, `DELUGE_PASSWORD`
  - Transmission: `TRANSMISSION_USERNAME`, `TRANSMISSION_PASSWORD`

## Monitoring & Observability

**Error Tracking:**
- Custom error tracking via OpenTelemetry
  - Tracks client errors by operation and client type
  - Metric: `client.errors.total` (counter)
  - System errors tracked: `system.errors.total` (counter)
  - Location: `internal/telemetry/telemetry.go`

**Logs:**
- Structured logging via Go standard library `log/slog`
  - Handler: JSON output to stdout
  - Location: `cmd/seedbox_downloader/main.go` lines 148
  - Context propagation: Via `internal/logctx/logctx.go`
  - Logging configuration: Environment variable `LOG_LEVEL` (default: INFO)

**Metrics & Tracing:**
- OpenTelemetry (go.opentelemetry.io/otel v1.38.0)
  - Traces: Distributed tracing with context propagation
  - Metrics: USE metrics (utilization, saturation, errors) and business metrics
  - Export: OTLP gRPC to external collector
  - Exporter: `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`
  - Prometheus: Alternative export via `go.opentelemetry.io/otel/exporters/prometheus`
  - Configuration: `TELEMETRY_OTEL_ADDRESS` (default: "0.0.0.0:4317")
  - Location: `internal/telemetry/telemetry.go`

**Prometheus Integration:**
- Metrics endpoint available for scraping (optional Prometheus exporter)
- Integration: docker-compose.telemetry.yml includes Prometheus + Grafana stack
- Grafana dashboards: `monitoring/grafana/dashboards/seedbox-downloader.json`

## CI/CD & Deployment

**Hosting:**
- Docker container deployment (primary)
- Base image: `gcr.io/distroless/cc:nonroot` (production)
- Builder image: `golang:1.23` (build stage)
- Entrypoint: `/app/seedbox_downloader`

**Container Registry:**
- Configured for Docker build via `Dockerfile`
- Multi-stage build: Golang builder → distroless runtime

**Orchestration:**
- Docker Compose support
  - Primary: `docker-compose.telemetry.yml` (with Prometheus and Grafana)
  - Networks: Monitoring network for all services

## Environment Configuration

**Required env vars (must be set):**
- `DOWNLOAD_DIR` - Destination for downloads

**Critical env vars (with defaults):**
- `DOWNLOAD_CLIENT` (default: "deluge")
- `LOG_LEVEL` (default: "INFO")
- `WEB_BIND_ADDRESS` (default: "0.0.0.0:9091")
- `TELEMETRY_OTEL_ADDRESS` (default: "0.0.0.0:4317")

**Secrets location:**
- Environment variables (all secrets passed via environment)
- No .env file support documented
- Supported by `kelseyhightower/envconfig` v1.4.0
- For deployment: Use container orchestration secrets (Kubernetes Secrets, Docker Secrets, etc.) or CI/CD secret management

## Webhooks & Callbacks

**Incoming:**
- Transmission Protocol Handler: `/` (PUT/POST endpoints)
  - Location: `internal/http/rest/transmission.go`
  - Handler: `TransmissionHandler` for Put.io integration
  - Authentication: Basic auth (`TRANSMISSION_USERNAME`, `TRANSMISSION_PASSWORD`)
  - Usage: Receive webhook callbacks from Sonarr/Radarr via Transmission protocol

**Outgoing:**
- Discord Webhooks
  - Optional: Environment variable `DISCORD_WEBHOOK_URL`
  - Content: Download status notifications
  - Events:
    - Download started: Queued transfers
    - Download finished: Success notification with emoji
    - Download failed: Error notification with emoji
    - Transfer imported: Confirmation notification
  - Implementation: `internal/notifier/discord.go`
  - Payload format: Simple JSON `{"content": "message"}`
  - Error handling: Non-blocking (logged but doesn't fail main process)

---

*Integration audit: 2026-01-31*
