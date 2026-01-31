# Architecture

**Analysis Date:** 2026-01-31

## Pattern Overview

**Overall:** Distributed, event-driven pipeline architecture with pluggable download clients

**Key Characteristics:**
- Two-stage processing: transfers are discovered and orchestrated, then downloaded in parallel
- Client-agnostic design via interface-based abstractions (Deluge/Put.io plugins)
- Observability-first with OpenTelemetry instrumentation throughout
- Graceful shutdown and error handling with context propagation
- Event channels for loose coupling between components

## Layers

**Transport Layer:**
- Purpose: Handles HTTP communication with external download clients (Deluge, Put.io) and media services (Sonarr, Radarr)
- Location: `internal/dc/`, `internal/svc/arr/`, `internal/http/rest/`
- Contains: HTTP clients, RPC protocol implementations, authentication
- Depends on: Standard library net/http, oauth2, external APIs
- Used by: Transfer orchestration, download operations, REST API endpoints

**Orchestration Layer:**
- Purpose: Polls external sources for transfers, manages transfer-to-download transitions, implements retry logic
- Location: `internal/transfer/transfer.go`
- Contains: `TransferOrchestrator` that watches labeled transfers and emits download events
- Depends on: Download client, storage repository
- Used by: Main application loop, download service

**Download Layer:**
- Purpose: Parallelizes file downloads from transfer content, monitors import status, manages cleanup
- Location: `internal/downloader/downloader.go`
- Contains: `Downloader` with parallel file handling (up to `maxParallel` concurrent operations)
- Depends on: Download client, transfer client, *arr services, filesystem
- Used by: Main application loop, notification system

**Storage Layer:**
- Purpose: Persists download state, enforces atomic claims to prevent duplicate processing
- Location: `internal/storage/`, `internal/storage/sqlite/`
- Contains: Repository interface with SQLite implementation, instance locking
- Depends on: SQLite database
- Used by: Orchestrator, repository access

**Telemetry Layer:**
- Purpose: Cross-cutting observability for metrics, spans, and errors
- Location: `internal/telemetry/`
- Contains: Metric recorders (RED + USE patterns), span instrumentation, client/DB operation wrappers
- Depends on: OpenTelemetry OTEL
- Used by: Wrapped implementations of all clients and repositories

**Support Layers:**
- Logging: `internal/logctx/` - context-based logger propagation using Go's slog
- Notifications: `internal/notifier/` - event-driven Discord webhooks for transfer state changes
- Configuration: `cmd/seedbox_downloader/main.go` - environment-based config via envconfig

## Data Flow

**Transfer Discovery and Download Pipeline:**

1. Application starts with config initialization, telemetry setup, and service instantiation
2. `TransferOrchestrator.ProduceTransfers()` spawns goroutine with ticker polling at `pollingInterval`
3. Ticker calls `watchTransfers()` → queries download client via `GetTaggedTorrents(label)`
4. For each available transfer, attempts atomic `repo.ClaimTransfer()` with instance lock
5. Successfully claimed transfers emitted on `OnDownloadQueued` channel
6. `Downloader.WatchDownloads()` goroutine receives transfers and calls `DownloadTransfer()`
7. Each file in transfer downloaded in parallel (semaphore limits to `maxParallel`)
8. File download: `GrabFile()` streams content, writes to `downloadDir`, tracks progress
9. On completion, `OnTransferDownloadFinished` event triggers import monitoring
10. `WatchForImported()` polls *arr API until files appear in history
11. Once imported, files deleted locally and `OnTransferImported` event fired
12. `WatchForSeeding()` monitors transfer seed status; on completion, removes transfer from seedbox

**State Transitions:**

```
Pending → Downloading (atomically claimed) → Downloaded → Imported → Removed
             ↓
         Failed (updates status, emits error event)
```

**State Management:**

- **Orchestrator:** Maintains polling loop, channels for producer-consumer pattern
- **Downloader:** Maintains active transfer state via goroutines, channels for events
- **Repository:** SQLite with atomic upsert/claim to prevent race conditions across instances
- **Context:** Propagated through all operations, carries logger and telemetry spans

## Key Abstractions

**DownloadClient Interface:**
- Purpose: Abstracts different torrent/seedbox providers (Deluge, Put.io)
- Location: `internal/transfer/transfer.go` (lines 14-18)
- Implementations: `internal/dc/deluge/client.go`, `internal/dc/putio/client.go`
- Pattern: Factory-based selection in `buildDownloadClient()` at `cmd/seedbox_downloader/main.go:332`

**TransferClient Interface:**
- Purpose: Abstracts transfer lifecycle operations (add, remove) separate from discovery
- Location: `internal/transfer/transfer.go` (lines 20-23)
- Implementations: Embedded in Deluge/Put.io clients via type assertions
- Pattern: Operations that modify remote state separate from read-only discovery

**Transfer Domain Model:**
- Purpose: Unified representation of torrent/transfer across client implementations
- Location: `internal/transfer/transfer.go` (lines 25-62)
- Contains: ID, name, label, files array, progress, status, peer/seeding info
- Methods: `IsSeeding()`, `IsDownloadable()`, `IsAvailable()` encapsulate status logic

**DownloadRepository Interface:**
- Purpose: Abstracts persistent state tracking and atomic claiming
- Location: `internal/storage/storage.go` (lines 18-22)
- Implementation: `internal/storage/sqlite/download_repository.go` with instance-aware locking
- Pattern: Upsert-on-conflict with status guard prevents concurrent downloads

**InstrumentedClient Wrappers:**
- Purpose: Decorator pattern for adding telemetry without modifying implementations
- Location: `internal/transfer/instrumented_client.go`
- Wraps: `DownloadClient`, `TransferClient`
- Pattern: Each method delegates to underlying client within instrumented context

## Entry Points

**Main Entry Point:**
- Location: `cmd/seedbox_downloader/main.go:80-88`
- Triggers: Process startup
- Responsibilities: Error handling, graceful shutdown via context cancellation

**Run Function:**
- Location: `cmd/seedbox_downloader/main.go:90-124`
- Sequence: Config → Telemetry → Services → Servers → Main loop
- Responsibilities: Orchestrates initialization, coordinates graceful shutdown

**HTTP REST Entry Points:**
- Location: `internal/http/rest/transmission.go:118-126`
- Routes: `/transmission/rpc` (POST/GET)
- Purpose: Transmission-compatible API for externally-triggered transfers (via webhooks)
- Responsibilities: Validate auth, marshal Transmission protocol, delegate to Put.io client

**Event Channels:**
- `TransferOrchestrator.OnDownloadQueued`: Signals ready-to-download transfers
- `Downloader.OnTransferDownloadFinished`: Signals successful download completion
- `Downloader.OnTransferDownloadError`: Signals download failure
- `Downloader.OnTransferImported`: Signals import completion in *arr

## Error Handling

**Strategy:** Error propagation with structured logging, telemetry recording, and user notification

**Patterns:**

- **Context Cancellation:** All long-running operations select on `ctx.Done()` for graceful shutdown (`transfer.go:129`, `downloader.go:78`)
- **Retry on Failure:** No built-in retry; failures recorded in DB and notification system, operator-triggered retry
- **Error Logging:** Structured slog with context key-values (transfer_id, file_path, err)
- **Error Events:** Critical operations emit error channel events (`OnTransferDownloadError`, `OnFileDownloadError`)
- **Telemetry Recording:** All errors recorded as `status="error"` in metrics with cardinality-safe attributes
- **Database Constraints:** SQLite unique constraint on `transfer_id` prevents duplicate records; status guards prevent re-claiming failed transfers

**Error Recovery:**

- Failed downloads: Status set to "failed" in DB, notification sent, can be retried when status resets
- Import check failures: Monitoring continues with exponential polling
- Client auth failures: Logged and returned to caller; fatal during startup
- Network errors: Logged, no retry (background polling will catch again)

## Cross-Cutting Concerns

**Logging:**
- Mechanism: Context-based propagation via `logctx.WithLogger()` and `logctx.LoggerFromContext()`
- Format: JSON with slog.NewJSONHandler
- Level: Configurable via `LOG_LEVEL` env var, defaults to INFO
- Pattern: All functions receive context, extract logger via `logctx.LoggerFromContext(ctx)`
- Key attributes: transfer_id, file_path, status, duration, err

**Validation:**
- Configuration: Environment variables parsed and validated via envconfig (required fields)
- File paths: Created with os.MkdirAll before file operations
- Transfer state: Status guards in SQL prevent invalid transitions
- HTTP requests: Basic auth middleware validates Transmission API credentials

**Authentication:**
- Download clients: Authenticate on startup, store session cookies/tokens
- *arr services: X-Api-Key header required for imports check
- Transmission REST API: HTTP Basic Auth middleware validates credentials
- Put.io: OAuth2 token-based via external client library

**Observability:**
- Traces: OpenTelemetry spans created for operations, cardinality-safe attributes only
- Metrics: RED (Request rate, Errors, Duration) + USE (Utilization, Saturation, Errors) patterns
- Context propagation: OTEL TextMapPropagator injected into HTTP headers for distributed tracing
- Cardinality safeguards: High-cardinality data (transfer names, file paths) only in logs/spans, not metrics

---

*Architecture analysis: 2026-01-31*
