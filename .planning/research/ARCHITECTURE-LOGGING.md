# Architecture Research: Structured Logging in Event-Driven Go Application

**Domain:** Logging improvements for existing event-driven Go application
**Researched:** 2026-02-01
**Confidence:** HIGH

## Current Architecture Analysis

### Existing Components

```
┌─────────────────────────────────────────────────────────────────┐
│                       HTTP Layer                                 │
│  ┌──────────────────────────┐  ┌─────────────────────────────┐  │
│  │ TransmissionHandler      │  │ HTTP Middleware             │  │
│  │ - torrent-add            │  │ - Basic Auth                │  │
│  │ - torrent-get            │  │ - OpenTelemetry tracing     │  │
│  │ - torrent-remove         │  │                             │  │
│  └────────┬─────────────────┘  └─────────────────────────────┘  │
├───────────┴──────────────────────────────────────────────────────┤
│                    Event Pipeline                                │
│  ┌──────────────────┐    ┌──────────────┐    ┌───────────────┐  │
│  │ Transfer         │───▶│  Downloader  │───▶│ Import Monitor│  │
│  │ Orchestrator     │    │              │    │               │  │
│  │ (polling loop)   │    │ (parallel DL)│    │ (polling loop)│  │
│  └──────────────────┘    └──────────────┘    └───────┬───────┘  │
│                                                       │           │
│  ┌────────────────────────────────────────────────────┘           │
│  │                                                                │
│  ▼                                                                │
│  ┌──────────────────┐    ┌─────────────────────────────────┐    │
│  │ Cleanup Monitor  │    │  Discord Notifier               │    │
│  │ (polling loop)   │    │  (event consumer)               │    │
│  └──────────────────┘    └─────────────────────────────────┘    │
├──────────────────────────────────────────────────────────────────┤
│                    External Integrations                         │
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │ Download Clients │  │ Transfer Clients │  │ ARR Services  │  │
│  │ - Deluge/Put.io  │  │ - Instrumented   │  │ - Sonarr      │  │
│  │ - Instrumented   │  │ - Telemetry      │  │ - Radarr      │  │
│  └──────────────────┘  └──────────────────┘  └───────────────┘  │
├──────────────────────────────────────────────────────────────────┤
│                    Infrastructure                                │
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │ SQLite DB        │  │ OpenTelemetry    │  │ Log Context   │  │
│  │ - Instrumented   │  │ - Traces/Metrics │  │ - logctx pkg  │  │
│  └──────────────────┘  └──────────────────┘  └───────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### Current Logging State

**Strengths:**
- Uses standard `log/slog` with JSON output
- Context-aware logging via `logctx` package (logger stored in context)
- OpenTelemetry tracing infrastructure already in place
- Structured logging with consistent attributes (transfer_id, operation, etc.)

**Gaps:**
- No correlation between logs and OpenTelemetry traces (trace_id/span_id missing)
- Inconsistent log levels across components (mix of Info/Debug/Error usage)
- HTTP requests not logged (no middleware)
- No component-level logger scoping (all loggers use .WithGroup())
- Startup sequence not clearly logged
- Pipeline flow hard to trace across goroutines

## Recommended Logging Architecture

### Pattern 1: OpenTelemetry Bridge Integration

**What:** Integrate `otelslog` to automatically inject trace/span IDs into all log records.

**Why:** The existing application already uses OpenTelemetry for tracing. Adding the otelslog bridge creates automatic correlation between logs and traces with zero code changes to existing logging calls.

**How it works:**
1. OpenTelemetry stores active span in `context.Context`
2. `otelslog.Handler` intercepts slog records
3. Handler extracts trace context from the context passed to `InfoContext()`
4. Handler automatically adds `trace_id` and `span_id` attributes to log record
5. Logs can be correlated with distributed traces in observability backend

**Trade-offs:**
- **Pro:** Zero changes to existing `LoggerFromContext(ctx)` pattern
- **Pro:** Automatic trace correlation across entire pipeline
- **Pro:** Standard OpenTelemetry integration (official bridge)
- **Con:** Requires using `*Context()` logging methods (already doing this)
- **Con:** Small performance overhead for context extraction (negligible)

**Implementation:**
```go
// In main.go initializeConfig()
import "go.opentelemetry.io/contrib/bridges/otelslog"

func initializeConfig() (*config, *slog.Logger, error) {
    var cfg config
    if err := envconfig.Process("", &cfg); err != nil {
        return nil, nil, fmt.Errorf("failed to load env vars: %w", err)
    }

    // Wrap JSONHandler with otelslog bridge
    baseHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: cfg.LogLevel,
    })
    logger := slog.New(otelslog.NewHandler("seedbox_downloader", otelslog.WithHandler(baseHandler)))

    slog.SetDefault(logger)

    return &cfg, logger, nil
}
```

**Current compatibility:** ✅ Works with existing `logctx.LoggerFromContext(ctx)` pattern without changes.

### Pattern 2: HTTP Request Logging Middleware

**What:** Add structured HTTP request/response logging using `go-chi/httplog` middleware.

**Why:** Currently, HTTP requests to the Transmission API endpoint are not logged (only Debug logs inside handlers). This makes troubleshooting webhook issues difficult and provides no visibility into API usage patterns.

**Trade-offs:**
- **Pro:** Zero dependencies (uses standard slog)
- **Pro:** Built for Chi router (already in use)
- **Pro:** Automatic request ID generation and propagation
- **Pro:** Configurable verbosity (by status code)
- **Con:** Adds ~50-100μs per request (negligible)

**Implementation:**
```go
// In main.go setupServer()
import "github.com/go-chi/httplog/v2"

func setupServer(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*http.Server, error) {
    // Create httplog logger with options
    httpLogger := httplog.NewLogger("seedbox_downloader", httplog.Options{
        LogLevel:        slog.LevelInfo,
        Concise:         true,
        RequestHeaders:  false,
        ResponseHeaders: false,
        MessageFieldName: "message",
        LevelFieldName:   "level",
        TimeFieldName:    "time",
        Tags: map[string]string{
            "component": "http",
        },
        QuietDownRoutes: []string{
            "/health",    // If added
            "/metrics",   // If added
        },
        QuietDownPeriod: 5 * time.Minute,
    })

    r := chi.NewRouter()
    r.Use(httplog.RequestLogger(httpLogger))  // Add before other middleware
    r.Use(telemetry.NewHTTPMiddleware(cfg.Telemetry.ServiceName))

    // ... rest of setup
}
```

**Log output includes:**
- `http_method`, `http_path`, `http_status`, `http_latency_ms`
- `remote_addr`, `user_agent`
- Request ID for correlation
- Automatic level assignment (5xx=error, 4xx=warn, 2xx=info)

### Pattern 3: Component-Scoped Loggers

**What:** Create component-scoped loggers with permanent attributes at initialization time.

**Why:** Currently, loggers use `.WithGroup("component_name")` which groups attributes under a nested key. Component-scoped loggers use `.With()` to add a `component` attribute at the root level, making filtering easier in log aggregation systems.

**Trade-offs:**
- **Pro:** Flat attribute structure (easier to query)
- **Pro:** Component name appears on every log line (clear ownership)
- **Pro:** Can add component-specific permanent attributes
- **Con:** Requires refactoring logger initialization in each component

**Current pattern:**
```go
// In main.go
logger := logctx.LoggerFromContext(ctx).WithGroup("main")
logger.Info("starting...")
// Output: {"level":"info","main":{"message":"starting..."}}
```

**Recommended pattern:**
```go
// In main.go
logger := logctx.LoggerFromContext(ctx).With(slog.String("component", "main"))
logger.Info("starting...")
// Output: {"level":"info","component":"main","message":"starting..."}
```

**Implementation strategy:**
1. Create component loggers at initialization
2. Add component-specific attributes (instance_id, client_type, etc.)
3. Store in context or pass to component constructors

```go
// Example: TransferOrchestrator
func NewTransferOrchestrator(
    ctx context.Context,
    repo storage.DownloadRepository,
    dc DownloadClient,
    label string,
    pollingInterval time.Duration,
) *TransferOrchestrator {
    logger := logctx.LoggerFromContext(ctx).With(
        slog.String("component", "transfer_orchestrator"),
        slog.String("label", label),
        slog.Duration("polling_interval", pollingInterval),
    )

    return &TransferOrchestrator{
        logger: logger,
        // ... rest of fields
    }
}
```

### Pattern 4: Lifecycle Event Logging

**What:** Log application lifecycle events at consistent levels with structured attributes.

**Why:** Startup sequence is partially logged, but lacks consistency. Operators need to understand initialization order, readiness state, and shutdown sequence.

**Log levels:**
- **INFO:** Normal lifecycle events (startup, shutdown, ready states)
- **DEBUG:** Detailed initialization steps (config loaded, connection established)
- **WARN:** Non-fatal initialization issues (telemetry disabled, optional features unavailable)
- **ERROR:** Fatal initialization failures (database unavailable, authentication failed)

**Lifecycle stages:**
```
1. Bootstrap    - Config loading, logger initialization
2. Initialize   - Dependencies (database, telemetry, clients)
3. Start        - Servers, goroutines, polling loops
4. Ready        - All components operational
5. Shutdown     - Graceful cleanup
```

**Implementation:**
```go
// In main.go run()
func run(ctx context.Context) error {
    // BOOTSTRAP
    cfg, logger, err := initializeConfig()
    if err != nil {
        return err
    }

    ctx = logctx.WithLogger(ctx, logger)
    logger = logger.With(slog.String("component", "main"))

    logger.Info("bootstrap complete",
        slog.String("version", version),
        slog.String("log_level", cfg.LogLevel.String()),
    )

    // INITIALIZE
    logger.Info("initializing telemetry", slog.Bool("enabled", cfg.Telemetry.Enabled))
    tel, err := initializeTelemetry(ctx, cfg)
    if err != nil {
        return err
    }
    defer tel.Shutdown(ctx)
    logger.Info("telemetry initialized")

    logger.Info("initializing services")
    services, err := initializeServices(ctx, cfg, tel)
    if err != nil {
        return err
    }
    defer services.Close()
    logger.Info("services initialized")

    // START
    logger.Info("starting servers", slog.String("bind_address", cfg.Web.BindAddress))
    servers, err := startServers(ctx, cfg, tel)
    if err != nil {
        return err
    }

    // READY
    logger.Info("application ready",
        slog.String("target_label", cfg.TargetLabel),
        slog.String("download_dir", cfg.DownloadDir),
        slog.Duration("polling_interval", cfg.PollingInterval),
    )

    return runMainLoop(ctx, cfg, servers)
}
```

### Pattern 5: Pipeline Flow Tracing

**What:** Add structured attributes that enable tracing a torrent through the entire pipeline.

**Why:** Currently, logs use `transfer_id` inconsistently. Need to trace: Webhook → Orchestrator → Downloader → Import Monitor → Cleanup.

**Required attributes:**
- `transfer_id` - Unique identifier for the transfer (always present)
- `transfer_name` - Human-readable name for filtering
- `operation` - Pipeline stage (discover, claim, download, import, cleanup)
- `component` - Which component emitted the log
- `trace_id`, `span_id` - OpenTelemetry correlation (automatic with otelslog)

**Implementation:**
```go
// Create transfer-scoped logger when transfer enters pipeline
func (o *TransferOrchestrator) watchTransfers(ctx context.Context) error {
    logger := logctx.LoggerFromContext(ctx)

    transfers, err := o.dc.GetTaggedTorrents(ctx, o.label)
    if err != nil {
        return fmt.Errorf("failed to get tagged torrents: %w", err)
    }

    logger.Info("discovered transfers",
        slog.Int("count", len(transfers)),
        slog.String("operation", "discover"),
    )

    for _, transfer := range transfers {
        // Create transfer-scoped logger
        transferLogger := logger.With(
            slog.String("transfer_id", transfer.ID),
            slog.String("transfer_name", transfer.Name),
            slog.String("status", transfer.Status),
        )

        if !transfer.IsAvailable() || !transfer.IsDownloadable() {
            transferLogger.Debug("skipping transfer",
                slog.String("operation", "filter"),
                slog.String("reason", "not_available_or_downloadable"),
            )
            continue
        }

        claimed, err := o.repo.ClaimTransfer(transfer.ID)
        if err != nil {
            if err == storage.ErrDownloaded {
                transferLogger.Debug("skipping transfer",
                    slog.String("operation", "claim"),
                    slog.String("reason", "already_downloaded"),
                )
                continue
            }
            return fmt.Errorf("failed to claim transfer: %w", err)
        }

        if !claimed {
            transferLogger.Debug("skipping transfer",
                slog.String("operation", "claim"),
                slog.String("reason", "already_claimed"),
            )
            continue
        }

        transferLogger.Info("transfer claimed",
            slog.String("operation", "claim"),
        )

        o.OnDownloadQueued <- transfer
    }

    return nil
}
```

**Query examples:**
- All logs for transfer: `transfer_id="abc123"`
- Transfer pipeline flow: `transfer_id="abc123" | sort by time`
- Transfers stuck in download: `operation="download" AND level="error"`
- Full trace with distributed context: `trace_id="xyz789"`

### Pattern 6: Goroutine Lifecycle Logging

**What:** Log goroutine start/stop events with consistent attributes.

**Why:** Application runs 5+ long-running goroutines. Need visibility into which are running, when they start/stop, and why they exit.

**Attributes:**
- `operation` - Goroutine purpose (produce_transfers, watch_downloads, watch_imported, etc.)
- `reason` - Exit reason (context_cancelled, error, panic, completion)
- `component` - Which component owns the goroutine

**Implementation:**
```go
// Current pattern in transfer.go
func (o *TransferOrchestrator) ProduceTransfers(ctx context.Context) {
    logger := logctx.LoggerFromContext(ctx)

    logger.Info("starting transfer orchestrator",
        slog.String("operation", "produce_transfers"),
    )

    go func() {
        defer func() {
            if r := recover(); r != nil {
                logger.Error("goroutine panic",
                    slog.String("operation", "produce_transfers"),
                    slog.Any("panic", r),
                    slog.String("stack", string(debug.Stack())),
                )

                if ctx.Err() == nil {
                    logger.Info("restarting after panic",
                        slog.String("operation", "produce_transfers"),
                    )
                    time.Sleep(time.Second)
                    o.ProduceTransfers(ctx)
                }
            }
        }()

        ticker := time.NewTicker(o.pollingInterval)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                logger.Info("goroutine shutdown",
                    slog.String("operation", "produce_transfers"),
                    slog.String("reason", "context_cancelled"),
                )
                return
            case <-ticker.C:
                if err := o.watchTransfers(ctx); err != nil {
                    logger.Error("watch error",
                        slog.String("operation", "produce_transfers"),
                        slog.Any("error", err),
                    )
                }
            }
        }
    }()
}
```

**Already implemented well:** ✅ Existing code has good patterns for panic recovery and shutdown logging.

## Data Flow: Logging Through Pipeline

### Request Flow (Webhook → Pipeline)

```
[Sonarr/Radarr Webhook]
    ↓ (HTTP POST /transmission/rpc)
[HTTP Middleware] ────────────────────▶ Log: http_method, http_path, remote_addr
    ↓
[TransmissionHandler.HandleRPC] ──────▶ Log: method="torrent-add", torrent_type="magnet|metainfo"
    ↓
[Put.io Client.AddTransfer] ──────────▶ Log: component="putio", operation="add_transfer"
    ↓ (returns Transfer with ID)
[Response] ───────────────────────────▶ Log: http_status=200, http_latency_ms=X
```

**Trace correlation:** All logs in this request have same `trace_id` (from HTTP middleware span).

### Pipeline Flow (Orchestrator → Cleanup)

```
[TransferOrchestrator polling loop]
    ↓ (GetTaggedTorrents)
[Discover] ──────────────────────────▶ Log: operation="discover", count=N
    ↓ (for each transfer)
[Filter] ────────────────────────────▶ Log: transfer_id, operation="filter", reason="not_available"
    ↓ (if available)
[Claim] ─────────────────────────────▶ Log: transfer_id, operation="claim", status="claimed|skipped"
    ↓ (send to channel)
[Downloader.WatchDownloads]
    ↓ (receive from channel)
[Download] ──────────────────────────▶ Log: transfer_id, operation="download", downloaded_files=N
    ↓ (for each file)
[File Download] ─────────────────────▶ Log: transfer_id, file_path, file_size, downloaded_bytes
    ↓ (on completion)
[Import Monitor] ────────────────────▶ Log: transfer_id, operation="watch_imported", polling_interval
    ↓ (polling loop)
[Check Imported] ────────────────────▶ Log: transfer_id, operation="check_import", imported=true|false
    ↓ (when imported)
[Cleanup] ───────────────────────────▶ Log: transfer_id, operation="cleanup", reason="imported"
```

**Trace correlation:** Each pipeline stage creates child spans, all logs include same root `trace_id`.

### Error Flow

```
[Any Component]
    ↓ (error occurs)
[Error Log] ─────────────────────────▶ Log: level="error", component, operation, error
    ↓ (if unrecoverable)
[Channel Send] ──────────────────────▶ OnTransferDownloadError channel
    ↓
[Notification Loop] ─────────────────▶ Log: transfer_id, operation="notify", status="error"
    ↓
[Discord Notifier] ──────────────────▶ Log: component="discord", operation="send_webhook"
```

## Integration Points

### OpenTelemetry Bridge (NEW)

**Pattern:** Wrap slog handler with otelslog bridge
**Configuration:**
```go
import "go.opentelemetry.io/contrib/bridges/otelslog"

baseHandler := slog.NewJSONHandler(os.Stdout, opts)
bridgeHandler := otelslog.NewHandler("service-name", otelslog.WithHandler(baseHandler))
logger := slog.New(bridgeHandler)
```

**Benefits:**
- Automatic trace_id/span_id injection
- Zero changes to existing logging calls
- Works with existing OpenTelemetry setup
- Standard OpenTelemetry integration

**Integration points:**
- ✅ Existing HTTP middleware creates spans
- ✅ Existing instrumented clients create spans
- ✅ Existing logctx package stores logger in context
- ✅ All components use `LoggerFromContext(ctx)`

### HTTP Request Logging Middleware (NEW)

**Pattern:** Add go-chi/httplog middleware before existing middleware
**Configuration:**
```go
r.Use(httplog.RequestLogger(httpLogger, opts))
r.Use(telemetry.NewHTTPMiddleware(serviceName))  // Existing
```

**Benefits:**
- Request/response logging
- Automatic request ID generation
- Level by status code (5xx=error, 4xx=warn)
- Zero dependencies (uses stdlib slog)

**Integration points:**
- ✅ Works with existing Chi router
- ✅ Runs before existing telemetry middleware
- ✅ Request ID propagates through context
- ✅ Compatible with otelslog bridge

### Existing logctx Package (PRESERVE)

**Current pattern:** Store/retrieve logger from context
```go
// Store
ctx = logctx.WithLogger(ctx, logger)

// Retrieve
logger := logctx.LoggerFromContext(ctx)
```

**Status:** ✅ Keep as-is, works perfectly with new patterns

**Enhancement:** Add instance ID to context logger
```go
// In main.go after creating logger
instanceID := downloader.GenerateInstanceID()
logger = logger.With(slog.String("instance_id", instanceID))
ctx = logctx.WithLogger(ctx, logger)
```

### Existing OpenTelemetry Instrumentation (PRESERVE)

**Current instrumentation:**
- ✅ HTTP middleware: Creates spans for all HTTP requests
- ✅ Instrumented clients: Wraps DownloadClient/TransferClient with telemetry
- ✅ Database operations: Instrumented repository with telemetry
- ✅ Metrics: Business metrics for downloads, transfers, operations

**Enhancement:** Logs will automatically correlate with existing traces via otelslog bridge.

## Component Responsibilities (With Logging)

| Component | Current Logging | Recommended Enhancement |
|-----------|----------------|-------------------------|
| **main.go** | Uses `.WithGroup("main")` | Change to component-scoped logger, add lifecycle logging |
| **TransferOrchestrator** | Minimal logging, no operation attribute | Add operation attributes (discover, claim), transfer-scoped loggers |
| **Downloader** | Good file download logging | Add operation attributes (download, verify), enhance error context |
| **ImportMonitor** | Basic polling logs | Add operation attributes (check_import, verify), reduce log noise |
| **TransmissionHandler** | Debug-only logs | Add structured request/response logging via middleware |
| **DownloadClients** | No direct logging (instrumented) | Keep as-is (telemetry handles it) |
| **Notifier** | Basic success/error logs | Add component attribute, structured error details |
| **Database** | No logging (instrumented) | Keep as-is (telemetry handles it) |

## Implementation Order

Based on existing architecture and dependencies, implement in this order:

### Phase 1: Foundation (Low Risk)
1. **Add otelslog bridge** - Wraps existing logger, zero code changes
2. **Add HTTP logging middleware** - One-line change in setupServer()
3. **Refactor to component-scoped loggers** - Change .WithGroup() to .With()

**Why first:** These changes wrap existing patterns without modifying component logic.

### Phase 2: Lifecycle (Low Risk)
4. **Add lifecycle event logging** - Structured logging in main.go run()
5. **Standardize goroutine lifecycle logs** - Already partially implemented

**Why second:** Changes isolated to main.go and goroutine start/stop (already have patterns).

### Phase 3: Pipeline (Medium Risk)
6. **Add pipeline operation attributes** - Add operation= attribute throughout
7. **Create transfer-scoped loggers** - Add .With(transfer_id, transfer_name) when entering pipeline
8. **Enhance error context** - Add structured attributes to error logs

**Why third:** Touches component logic but doesn't change behavior, just adds attributes.

### Phase 4: Cleanup (Low Risk)
9. **Remove noisy Debug logs** - Convert to Info/Debug based on importance
10. **Standardize log messages** - Consistent verbs (starting, started, completed, failed)
11. **Add log level guidelines** - Document when to use each level

**Why last:** Refinement pass after new patterns established.

## Anti-Patterns to Avoid

### Anti-Pattern 1: Logging Without Context

**What people do:** Use `slog.Info()` instead of `InfoContext(ctx, ...)`.

**Why it's wrong:**
- Logs missing trace_id/span_id (can't correlate with traces)
- Logs missing request-scoped attributes
- Can't propagate logger state through call stack

**Do this instead:** Always use `InfoContext(ctx, ...)` with context from function parameter.

```go
// ❌ Bad
func (d *Downloader) DownloadFile(ctx context.Context, file *transfer.File) {
    slog.Info("downloading file", "file_path", file.Path)
}

// ✅ Good
func (d *Downloader) DownloadFile(ctx context.Context, file *transfer.File) {
    logger := logctx.LoggerFromContext(ctx)
    logger.InfoContext(ctx, "downloading file", slog.String("file_path", file.Path))
}
```

### Anti-Pattern 2: Inconsistent Attribute Names

**What people do:** Use different keys for the same concept (download_id vs transfer_id, err vs error).

**Why it's wrong:**
- Breaks log aggregation queries
- Confuses operators (is download_id the same as transfer_id?)
- Makes filtering difficult

**Do this instead:** Standardize attribute names across codebase.

**Standard names:**
- `transfer_id` - Unique identifier for transfer (not download_id)
- `error` - Error message (not err)
- `operation` - Pipeline stage (discover, claim, download, etc.)
- `component` - Component name (transfer_orchestrator, downloader, etc.)
- `reason` - Why something happened (context_cancelled, not_available, etc.)

### Anti-Pattern 3: Logging Secrets

**What people do:** Log full config objects, API tokens, or credentials.

**Why it's wrong:**
- Security vulnerability (secrets in log aggregation)
- Compliance violation (PII, credentials)

**Do this instead:** Redact secrets, log only non-sensitive fields.

```go
// ❌ Bad
logger.Info("config loaded", slog.Any("config", cfg))

// ✅ Good
logger.Info("config loaded",
    slog.String("download_dir", cfg.DownloadDir),
    slog.String("log_level", cfg.LogLevel.String()),
    slog.Bool("telemetry_enabled", cfg.Telemetry.Enabled),
    // Omit: PutioToken, DelugePassword, Transmission credentials
)
```

### Anti-Pattern 4: High-Frequency Debug Logging in Hot Paths

**What people do:** Log every iteration of a tight loop at Info/Debug level.

**Why it's wrong:**
- Performance impact (I/O is expensive)
- Log volume explosion (GBs per hour)
- Hides important events (signal-to-noise ratio)

**Do this instead:** Log summary metrics, sample high-frequency events, or use structured counters.

```go
// ❌ Bad (logs N times for N files)
for _, file := range files {
    logger.Debug("downloading file", "file_path", file.Path)
    // ... download ...
}

// ✅ Good (logs once per transfer)
logger.Info("downloading transfer",
    slog.String("transfer_id", transfer.ID),
    slog.Int("file_count", len(files)),
)
for _, file := range files {
    // ... download ...
    // Only log errors or completion
}
logger.Info("transfer downloaded",
    slog.String("transfer_id", transfer.ID),
    slog.Int("files_downloaded", downloadedCount),
)
```

### Anti-Pattern 5: Using .WithGroup() for Components

**What people do:** Use `.WithGroup("component")` to namespace logs.

**Why it's wrong:**
- Creates nested JSON structure (harder to query)
- Doesn't work well with log aggregation filters
- Standard convention is flat attributes

**Do this instead:** Use `.With(slog.String("component", "name"))` for flat structure.

```go
// ❌ Bad (nested structure)
logger := logger.WithGroup("downloader")
logger.Info("starting")
// Output: {"level":"info","downloader":{"message":"starting"}}

// ✅ Good (flat structure)
logger := logger.With(slog.String("component", "downloader"))
logger.Info("starting")
// Output: {"level":"info","component":"downloader","message":"starting"}
```

## Sources

### OpenTelemetry Integration
- [How to Set Up Structured Logging in Go with OpenTelemetry](https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view) - 2026 guide
- [OpenTelemetry Slog [otelslog]: Golang Bridge Setup & Examples](https://uptrace.dev/guides/opentelemetry-slog) - Official bridge documentation
- [OpenTelemetry Logging Specification](https://opentelemetry.io/docs/specs/otel/logs/) - Standard specification
- [Go instrumentation sample | Cloud Trace](https://cloud.google.com/trace/docs/setup/go-ot) - Google Cloud patterns

### Context Propagation
- [slog-context library](https://github.com/veqryn/slog-context) - Context-aware logging patterns
- [Contextual Logging in Go with Slog](https://betterstack.com/community/guides/logging/golang-contextual-logging/) - Best practices guide
- [Logging in Go with Slog: The Ultimate Guide](https://betterstack.com/community/guides/logging/logging-in-go/) - Comprehensive guide
- [log/slog package documentation](https://pkg.go.dev/log/slog) - Official Go documentation

### HTTP Middleware
- [go-chi/httplog](https://github.com/go-chi/httplog) - Chi-compatible HTTP logging middleware
- [A Guide To Writing Logging Middleware in Go](https://blog.questionable.services/article/guide-logging-middleware-go/) - Middleware patterns
- [slog-http package](https://pkg.go.dev/github.com/samber/slog-http) - Alternative HTTP logging middleware

### Architecture Patterns
- [Event-Driven Architecture with Go](https://dev.to/joseowino/event-driven-architecture-with-go-22k5) - Event-driven patterns
- [Take Your Distributed System to the Next Level with Event-Driven Logging](https://solace.com/blog/event-driven-logging-architecture/) - Logging architecture patterns
- [Architecting a Go Backend with Event-Driven Design](https://medium.com/@steffankharmaaiarvi/architecting-a-go-backend-with-event-driven-design-and-the-outbox-pattern-3928bf315e0a) - Clean architecture patterns

### Structured Logging
- [Structured Logging with slog](https://go.dev/blog/slog) - Official Go blog
- [Building a Logger Wrapper in Go](https://medium.com/@ansujain/building-a-logger-wrapper-in-go-with-support-for-multiple-logging-libraries-48092b826bee) - Wrapper patterns
- [How to collect, standardize, and centralize Golang logs](https://www.datadoghq.com/blog/go-logging/) - Production patterns

### Application Lifecycle
- [go-lifecycle library](https://github.com/effxhq/go-lifecycle) - State-based lifecycle management
- [How to shutdown a Go application gracefully](https://josemyduarte.github.io/2023-04-24-golang-lifecycle/) - Shutdown patterns
- [The Art of Logging in Go](https://naiknotebook.medium.com/the-art-of-logging-in-go-golang-best-practices-and-implementation-e64a27494ee5) - Best practices
- [Effective Strategies and Best Practices for Go Logging](https://www.dataset.com/blog/effective-strategies-and-best-practices-for-go-logging/) - Production strategies

---
*Architecture research for: Seedbox Downloader logging improvements*
*Researched: 2026-02-01*
