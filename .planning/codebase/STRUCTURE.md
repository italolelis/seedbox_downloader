# Codebase Structure

**Analysis Date:** 2026-01-31

## Directory Layout

```
seedbox_downloader/
├── cmd/
│   └── seedbox_downloader/
│       └── main.go                    # Application entry point
├── internal/
│   ├── dc/                            # Download client implementations
│   │   ├── deluge/
│   │   │   ├── client.go
│   │   │   └── client_test.go
│   │   └── putio/
│   │       └── client.go
│   ├── downloader/
│   │   ├── downloader.go              # File parallel download engine
│   │   ├── progress/
│   │   │   └── progressreader.go
│   │   └── instanceid.go
│   ├── http/
│   │   └── rest/
│   │       └── transmission.go        # Transmission RPC API handler
│   ├── logctx/
│   │   └── logctx.go                  # Context-based logging
│   ├── notifier/
│   │   └── discord.go                 # Discord webhook notifications
│   ├── storage/
│   │   ├── storage.go                 # Repository interface
│   │   ├── instanceid.go
│   │   └── sqlite/
│   │       ├── init.go
│   │       ├── download_repository.go # SQLite implementation
│   │       └── instrumented_repository.go
│   ├── svc/
│   │   └── arr/
│   │       └── arr.go                 # Sonarr/Radarr API client
│   ├── telemetry/
│   │   ├── telemetry.go               # Metrics & traces setup
│   │   ├── instrumentation.go         # Instrumentation helpers
│   │   └── middleware.go              # HTTP middleware for tracing
│   └── transfer/
│       ├── transfer.go                # Transfer model & orchestrator
│       └── instrumented_client.go     # Telemetry decorators
├── monitoring/
│   └── grafana/
│       └── dashboards/
│           └── seedbox-downloader.json
├── docker-compose.telemetry.yml       # OTEL collector setup
├── Dockerfile
├── go.mod
├── go.sum
├── README.md
├── TELEMETRY.md
└── AGENTS.md
```

## Directory Purposes

**cmd/seedbox_downloader/:**
- Purpose: Application entry point and composition root
- Contains: Main function, configuration structs, service initialization, dependency injection
- Key files: `main.go` (374 lines) - handles startup orchestration and graceful shutdown

**internal/dc/ (Download Clients):**
- Purpose: Pluggable implementations of download client abstraction
- Contains: Provider-specific API interactions (HTTP RPC, oauth2, file streaming)
- Key files:
  - `deluge/client.go`: Deluge JSON-RPC over HTTP, session cookie management, TLS handling
  - `putio/client.go`: Put.io official SDK wrapper, OAuth2, recursive file traversal

**internal/downloader/:**
- Purpose: Parallel file download engine and import monitoring
- Contains: Concurrent file download with semaphore pooling, progress tracking, import detection
- Key files:
  - `downloader.go` (318 lines): Download orchestration, event channels, seeding monitoring
  - `progress/progressreader.go`: Progress callback wrapper around io.Reader
  - `instanceid.go`: Instance identification for distributed locking

**internal/http/rest/:**
- Purpose: REST API for external transfer requests (Transmission protocol compatibility)
- Contains: Transmission RPC handler, protocol marshaling, auth middleware
- Key files: `transmission.go` (356 lines) - implements session-get, torrent-get, torrent-add, torrent-remove methods

**internal/logctx/:**
- Purpose: Context-based logger propagation
- Contains: Context key type, WithLogger/LoggerFromContext helpers
- Key files: `logctx.go` (25 lines) - minimal helper for slog integration with context

**internal/notifier/:**
- Purpose: Event-driven notifications to external systems
- Contains: Interface for notifiers, Discord webhook implementation
- Key files: `discord.go` (38 lines) - JSON POST to webhook URL

**internal/storage/:**
- Purpose: Persistent state management and atomic claiming
- Contains: Repository interface, SQLite implementation with instance locking
- Key files:
  - `storage.go` (23 lines): DownloadRepository interface definition
  - `sqlite/download_repository.go` (85 lines): SQLite implementation with atomic upsert/claim
  - `sqlite/instrumented_repository.go` (69 lines): Telemetry wrapper

**internal/svc/arr/:**
- Purpose: Integration with *arr applications (Sonarr, Radarr)
- Contains: API client for import status checks
- Key files: `arr.go` (91 lines) - history pagination, downloadFolderImported event detection

**internal/telemetry/:**
- Purpose: Observability infrastructure setup and recording
- Contains: OpenTelemetry meter/tracer initialization, metric definitions, instrumentation helpers
- Key files:
  - `telemetry.go` (388 lines): OTEL resource setup, metric instruments, RED+USE patterns
  - `instrumentation.go` (183 lines): Instrument* helper functions with span/metric recording
  - `middleware.go`: HTTP middleware for request tracing

**internal/transfer/:**
- Purpose: Transfer domain model and producer-consumer orchestration
- Contains: Transfer abstraction, polling orchestrator, instrumented client wrappers
- Key files:
  - `transfer.go` (189 lines): Transfer struct, TransferOrchestrator, polling logic
  - `instrumented_client.go` (125 lines): Client telemetry decorators

## Key File Locations

**Entry Points:**
- `cmd/seedbox_downloader/main.go`: Application bootstrap and run() function
- `internal/http/rest/transmission.go`: HTTP API routes and handlers

**Configuration:**
- `cmd/seedbox_downloader/main.go`: config struct and initializeConfig()
- `docker-compose.telemetry.yml`: OTEL collector service definition

**Core Logic:**
- `internal/transfer/transfer.go`: TransferOrchestrator with watchTransfers()
- `internal/downloader/downloader.go`: Downloader with parallel download logic
- `internal/storage/sqlite/download_repository.go`: Atomic claim logic with upsert-on-conflict

**Testing:**
- `internal/dc/deluge/client_test.go`: Deluge client tests
- Test files follow `*_test.go` pattern in same package

## Naming Conventions

**Files:**
- Implementation files: `<component>.go` (e.g., `client.go`, `downloader.go`)
- Wrapper/instrumented files: `instrumented_<component>.go`
- Initialization: `init.go` (for package setup)
- Test files: `<name>_test.go`

**Directories:**
- Functional groupings: `internal/<feature>/` (lowercase, plural for grouped components)
- Example: `internal/dc/` for "download clients", `internal/svc/arr/` for "services - arr"

**Packages:**
- One file per interface in some cases (`storage.go`, `transfer.go`)
- Implementation grouped with interface in others (`client.go` contains Deluge, Put.io, etc)
- Instrumented wrapper separate file when wrapping existing interface

**Functions/Types:**
- Exported: PascalCase (e.g., `NewClient`, `Downloader`, `DownloadRepository`)
- Unexported: camelCase (e.g., `getTaggedTorrentsRaw`, `ensureTargetDir`)
- Interface names: Verb-based (e.g., `DownloadClient`, `Notifier`) or Noun-based (e.g., `DownloadRepository`)

## Where to Add New Code

**New Feature (e.g., new notifier type):**
- Primary code: `internal/notifier/<type>.go` (implement Notifier interface)
- Tests: `internal/notifier/<type>_test.go`
- Integration: Register in `setupNotificationForDownloader()` at `cmd/seedbox_downloader/main.go:266`

**New Download Client Provider:**
- Implementation: `internal/dc/<provider>/client.go` (implement DownloadClient, TransferClient)
- Tests: `internal/dc/<provider>/client_test.go`
- Factory: Add case to `buildDownloadClient()` at `cmd/seedbox_downloader/main.go:332`
- Configuration: Add env vars to `config` struct at `cmd/seedbox_downloader/main.go:30`

**New *arr Service Integration:**
- Add client: `internal/svc/arr/<service>.go` or extend `internal/svc/arr/arr.go`
- Integration: Initialize in `initializeServices()` at `cmd/seedbox_downloader/main.go:186`
- Usage: Pass to `Downloader` for import checking

**Utilities/Helpers:**
- Shared across components: `internal/<component>/` as private functions
- Logging utilities: Add to `internal/logctx/logctx.go`
- Telemetry utilities: Add to `internal/telemetry/instrumentation.go`

**HTTP Endpoints:**
- New REST routes: Add method to `TransmissionHandler` in `internal/http/rest/transmission.go`
- New handlers: Add case to `HandleRPC()` switch statement
- Middleware: Extend `basicAuthMiddleware()` or add new middleware in Routes()

**Metrics/Observability:**
- New metrics: Define in `telemetry.go` Telemetry struct (line 23-51)
- Initialize: Add to `initializeBusinessMetrics()` or `initializeUSEMetrics()`
- Record: Add method like `RecordDownload()` or `RecordClientOperation()`
- Use: Call from instrumented client/operation wrappers

**Database Schema Changes:**
- Location: `internal/storage/sqlite/init.go`
- Repository changes: Update `DownloadRepository` interface in `internal/storage/storage.go`
- Implementation: Update SQLite methods in `internal/storage/sqlite/download_repository.go`
- Tests: Create schema fixtures in test files

## Special Directories

**monitoring/:**
- Purpose: Observability dashboards and configuration
- Generated: Grafana dashboard JSON exported from UI
- Committed: Yes, tracked in git for reproducibility

**.planning/:**
- Purpose: GSD (Goal-Structured Development) planning documents
- Generated: No (manual creation)
- Committed: Yes, used by other GSD tools for planning

**cmd/:**
- Purpose: Executable entry points
- Location: Separate from internal logic per Go conventions
- Committed: Yes

---

*Structure analysis: 2026-01-31*
