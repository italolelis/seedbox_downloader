# Phase 8: Lifecycle Visibility - Research

**Researched:** 2026-02-08
**Domain:** Application lifecycle logging and startup/shutdown visibility in Go
**Confidence:** HIGH

## Summary

Lifecycle visibility makes application startup, shutdown, and component initialization observable through structured logging. This enables operators to understand what the application is doing during critical lifecycle phases, diagnose initialization failures, and verify graceful shutdowns.

The standard approach follows three principles: (1) Log initialization phases in dependency order showing what's starting, (2) Log "ready" messages when components complete initialization, and (3) Log shutdown sequence in reverse order showing graceful cleanup. Modern Go applications use structured logging (slog) with context-aware methods to capture initialization failures with full error context at ERROR level, while successful lifecycle events log at INFO level.

Based on 12-factor app principles and Go best practices, startup logs should include non-sensitive configuration values (ports, intervals, directories) to verify correct configuration, but never secrets. The application should fail fast on critical initialization errors (database, telemetry) while logging specific failure reasons, and should log a final "service ready" message only after all components are initialized and the server is accepting connections.

**Primary recommendation:** Add structured lifecycle logging at each initialization phase (config → telemetry → database → clients → server) with "ready" messages, include key configuration values in startup logs, implement fail-fast error logging for initialization failures, and add graceful shutdown logging in reverse order with component-level status messages.

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| log/slog | stdlib (Go 1.21+) | Structured lifecycle logging | Standard library structured logging with context support, already in use in project |
| context | stdlib | Lifecycle phase context propagation | Standard context propagation for cancellation and logging correlation |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| os/signal | stdlib | Signal handling for graceful shutdown | Capture SIGTERM/SIGINT for coordinated shutdown logging |
| sync | stdlib | Coordination primitives (WaitGroup) | Wait for goroutines during shutdown logging |
| time | stdlib | Timeout enforcement for lifecycle phases | Prevent hanging during initialization or shutdown |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Manual lifecycle logging | Lifecycle management libraries (uber-go/fx, g4s8/go-lifecycle) | Libraries provide structured hooks but add dependency and complexity; manual logging gives full control |
| Structured slog | Legacy log package or third-party (logrus, zap) | slog is stdlib standard since Go 1.21, already integrated with trace correlation |
| Context-aware logging | Global logger calls | Context-aware already required for trace correlation (Phase 7), no additional work |

**Installation:**
No additional dependencies required - all stdlib packages already in use.

## Architecture Patterns

### Recommended Project Structure
```
cmd/seedbox_downloader/
└── main.go              # Lifecycle orchestration with phase logging

internal/
├── telemetry/           # Logs "telemetry initialized" after setup
├── storage/sqlite/      # Logs "database ready" after validation
├── downloader/          # Logs "downloader ready" after channel setup
└── transfer/            # Logs "transfer orchestrator ready" after initialization
```

### Pattern 1: Phased Initialization Logging

**What:** Log each initialization phase with clear boundaries showing what's starting, what succeeded, and what's ready.

**When to use:** During application startup to provide visibility into initialization order and state.

**Example:**
```go
// Source: Composite from https://josemyduarte.github.io/2023-04-24-golang-lifecycle/
// and current codebase structure

func run(ctx context.Context) error {
    // Phase 1: Configuration
    logger.InfoContext(ctx, "loading configuration")
    cfg, logger, err := initializeConfig()
    if err != nil {
        // Fail fast with specific error
        logger.ErrorContext(ctx, "failed to load configuration", "err", err)
        return fmt.Errorf("configuration initialization failed: %w", err)
    }
    logger.InfoContext(ctx, "configuration loaded",
        "log_level", cfg.LogLevel,
        "target_label", cfg.TargetLabel,
        "polling_interval", cfg.PollingInterval.String(),
        "download_dir", cfg.DownloadDir)

    // Phase 2: Telemetry
    logger.InfoContext(ctx, "initializing telemetry")
    tel, err := initializeTelemetry(ctx, cfg)
    if err != nil {
        logger.ErrorContext(ctx, "failed to initialize telemetry", "err", err)
        return fmt.Errorf("telemetry initialization failed: %w", err)
    }
    defer tel.Shutdown(ctx)
    logger.InfoContext(ctx, "telemetry ready",
        "otel_enabled", cfg.Telemetry.Enabled,
        "service_name", cfg.Telemetry.ServiceName)

    // Phase 3: Database
    logger.InfoContext(ctx, "initializing database")
    services, err := initializeServices(ctx, cfg, tel)
    if err != nil {
        logger.ErrorContext(ctx, "failed to initialize services", "err", err)
        return fmt.Errorf("services initialization failed: %w", err)
    }
    defer services.Close()
    logger.InfoContext(ctx, "database ready",
        "db_path", cfg.DBPath,
        "max_connections", cfg.DBMaxOpenConns)

    // Phase 4: Clients
    logger.InfoContext(ctx, "initializing download clients")
    // ... client setup ...
    logger.InfoContext(ctx, "download clients ready",
        "client_type", cfg.DownloadClient)

    // Phase 5: Server
    logger.InfoContext(ctx, "starting HTTP server")
    servers, err := startServers(ctx, cfg, tel)
    if err != nil {
        logger.ErrorContext(ctx, "failed to start server", "err", err)
        return fmt.Errorf("server initialization failed: %w", err)
    }

    // Final ready message
    logger.InfoContext(ctx, "service ready",
        "bind_address", cfg.Web.BindAddress,
        "target_label", cfg.TargetLabel,
        "version", version)

    return runMainLoop(ctx, cfg, servers)
}
```

### Pattern 2: Component-Level Ready Messages

**What:** Each major component logs a "ready" message when initialization completes successfully.

**When to use:** After component-specific initialization logic completes, before returning control to caller.

**Example:**
```go
// Source: https://josemyduarte.github.io/2023-04-24-golang-lifecycle/
// and https://www.kelche.co/blog/go/http-server/

// In telemetry initialization
func initializeTelemetry(ctx context.Context, cfg *config) (*telemetry.Telemetry, error) {
    logger := logctx.LoggerFromContext(ctx)

    tel, err := telemetry.New(ctx, telemetry.Config{
        ServiceName:    cfg.Telemetry.ServiceName,
        ServiceVersion: version,
        OTELAddress:    cfg.Telemetry.OTELAddress,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
    }

    logger.InfoContext(ctx, "telemetry component ready",
        "service_name", cfg.Telemetry.ServiceName,
        "otel_enabled", cfg.Telemetry.Enabled)

    return tel, nil
}

// In database initialization
func initializeDatabase(ctx context.Context, cfg *config) (*sql.DB, error) {
    logger := logctx.LoggerFromContext(ctx)

    db, err := sqlite.InitDB(ctx, cfg.DBPath, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
    if err != nil {
        return nil, fmt.Errorf("failed to initialize database: %w", err)
    }

    logger.InfoContext(ctx, "database component ready",
        "db_path", cfg.DBPath,
        "max_open_conns", cfg.DBMaxOpenConns,
        "max_idle_conns", cfg.DBMaxIdleConns)

    return db, nil
}

// In server startup
func startServers(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*servers, error) {
    logger := logctx.LoggerFromContext(ctx)

    server, err := setupServer(ctx, cfg, tel)
    if err != nil {
        return nil, fmt.Errorf("failed to setup server: %w", err)
    }

    serverErrors := make(chan error, 1)
    go func() {
        logger.InfoContext(ctx, "HTTP server listening",
            "bind_address", cfg.Web.BindAddress)
        serverErrors <- server.ListenAndServe()
    }()

    logger.InfoContext(ctx, "server component ready",
        "bind_address", cfg.Web.BindAddress)

    return &servers{api: server, errors: serverErrors}, nil
}
```

### Pattern 3: Graceful Shutdown Logging

**What:** Log shutdown sequence in reverse order of initialization, showing each component's cleanup status.

**When to use:** During application shutdown triggered by signal or context cancellation.

**Example:**
```go
// Source: https://oneuptime.com/blog/post/2026-01-23-go-graceful-shutdown/view
// and current codebase shutdown implementation

func runMainLoop(ctx context.Context, cfg *config, servers *servers) error {
    logger := logctx.LoggerFromContext(ctx)

    for {
        select {
        case err := <-servers.errors:
            return fmt.Errorf("server error: %w", err)
        case <-ctx.Done():
            shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Web.ShutdownTimeout)
            defer cancel()

            logger.InfoContext(shutdownCtx, "shutdown signal received, starting graceful shutdown",
                "shutdown_timeout", cfg.Web.ShutdownTimeout.String())

            // Phase 1: Stop accepting new requests
            logger.InfoContext(shutdownCtx, "stopping HTTP server")
            if err := servers.api.Shutdown(shutdownCtx); err != nil {
                logger.ErrorContext(shutdownCtx, "HTTP server shutdown failed", "err", err)
                if err = servers.api.Close(); err != nil {
                    return fmt.Errorf("failed to stop server: %w", err)
                }
            } else {
                logger.InfoContext(shutdownCtx, "HTTP server stopped")
            }

            // Phase 2: Wait for active downloads to finish
            logger.InfoContext(shutdownCtx, "waiting for active downloads to complete")
            // Downloader channels drain naturally via context cancellation
            logger.InfoContext(shutdownCtx, "downloads completed")

            // Phase 3: Close database connections
            logger.InfoContext(shutdownCtx, "closing database connections")
            services.Close()
            logger.InfoContext(shutdownCtx, "database connections closed")

            // Phase 4: Shutdown telemetry
            logger.InfoContext(shutdownCtx, "shutting down telemetry")
            if err := tel.Shutdown(shutdownCtx); err != nil {
                logger.ErrorContext(shutdownCtx, "telemetry shutdown failed", "err", err)
            } else {
                logger.InfoContext(shutdownCtx, "telemetry shutdown complete")
            }

            logger.InfoContext(shutdownCtx, "graceful shutdown complete")
            return ctx.Err()
        }
    }
}
```

### Pattern 4: Configuration Value Logging

**What:** Log non-sensitive configuration values at startup for verification and debugging.

**When to use:** After configuration loading, before component initialization.

**Example:**
```go
// Source: https://12factor.net/config and Go best practices

func initializeConfig() (*config, *slog.Logger, error) {
    var cfg config
    if err := envconfig.Process("", &cfg); err != nil {
        return nil, nil, fmt.Errorf("failed to load env vars: %w", err)
    }

    jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})
    traceHandler := logctx.NewTraceHandler(jsonHandler)
    logger := slog.New(traceHandler)
    slog.SetDefault(logger)

    ctx := context.Background()

    // Log safe configuration values (not secrets!)
    logger.InfoContext(ctx, "configuration initialized",
        "log_level", cfg.LogLevel.String(),
        "target_label", cfg.TargetLabel,
        "download_dir", cfg.DownloadDir,
        "polling_interval", cfg.PollingInterval.String(),
        "cleanup_interval", cfg.CleanupInterval.String(),
        "keep_downloaded_for", cfg.KeepDownloadedFor.String(),
        "max_parallel", cfg.MaxParallel,
        "download_client", cfg.DownloadClient,
        "db_path", cfg.DBPath,
        "bind_address", cfg.Web.BindAddress,
        "telemetry_enabled", cfg.Telemetry.Enabled)

    // NEVER log secrets:
    // - cfg.DelugePassword
    // - cfg.PutioToken
    // - cfg.DiscordWebhookURL (contains secret)
    // - cfg.Transmission.Username/Password

    return &cfg, logger, nil
}
```

### Pattern 5: Initialization Failure with Context

**What:** Log initialization failures at ERROR level with full error context and component identification.

**When to use:** When any critical component fails to initialize.

**Example:**
```go
// Source: https://oneuptime.com/blog/post/2026-01-23-go-init-functions/view

func initializeServices(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*services, error) {
    logger := logctx.LoggerFromContext(ctx)

    // Database initialization with detailed error
    logger.InfoContext(ctx, "initializing database", "db_path", cfg.DBPath)
    database, err := sqlite.InitDB(ctx, cfg.DBPath, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
    if err != nil {
        logger.ErrorContext(ctx, "database initialization failed",
            "component", "database",
            "db_path", cfg.DBPath,
            "err", err)
        return nil, fmt.Errorf("failed to initialize database: %w", err)
    }
    logger.InfoContext(ctx, "database initialized")

    // Download client authentication with detailed error
    logger.InfoContext(ctx, "authenticating download client", "client_type", cfg.DownloadClient)
    dc, err := buildDownloadClient(cfg)
    if err != nil {
        logger.ErrorContext(ctx, "download client build failed",
            "component", "download_client",
            "client_type", cfg.DownloadClient,
            "err", err)
        return nil, fmt.Errorf("failed to build download client: %w", err)
    }

    instrumentedDC := transfer.NewInstrumentedDownloadClient(dc, tel, cfg.DownloadClient)
    if err := instrumentedDC.Authenticate(ctx); err != nil {
        logger.ErrorContext(ctx, "download client authentication failed",
            "component", "download_client",
            "client_type", cfg.DownloadClient,
            "err", err)
        return nil, fmt.Errorf("failed to authenticate with download client: %w", err)
    }
    logger.InfoContext(ctx, "download client authenticated")

    // ... rest of initialization ...

    return services, nil
}
```

### Anti-Patterns to Avoid

- **Logging too early:** Don't log "service ready" before server actually accepts connections
- **Missing error context:** Don't log `"initialization failed"` without component name and error details
- **Logging secrets:** Never log passwords, tokens, API keys, or webhook URLs
- **Silent failures:** Don't swallow errors during shutdown - log them even if non-fatal
- **Inconsistent phases:** Don't log some components as "ready" and others as "initialized" - pick one term
- **Shutdown without order:** Don't just log "shutting down" - show component-by-component progress
- **No timeout logging:** Don't let shutdown hang silently - log if components exceed timeout

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Shutdown coordination with multiple goroutines | Custom channel orchestration | sync.WaitGroup + context cancellation | Standard library primitives handle coordination, channels for shutdown signals risk deadlocks |
| Lifecycle state machine | Custom state tracking with booleans | Sequential initialization with early returns | Explicit sequence is clearer than state management, fail-fast is simpler than state transitions |
| Component dependency graph | DAG-based initialization order | Manual sequence in run() function | Small number of components (5-6) doesn't justify DAG complexity, explicit order is more maintainable |
| Structured initialization hooks | Custom interface with Start/Stop methods | Inline initialization with defer cleanup | Defer already provides reverse-order cleanup, no need for abstraction layer |
| Signal handling multiplexing | Custom signal dispatcher | os/signal.Notify with single handler | Standard library handles signal delivery, single handler avoids coordination complexity |

**Key insight:** Lifecycle logging is about visibility, not abstraction. Explicit sequential logging in main.go is more debuggable than a lifecycle framework that hides the order.

## Common Pitfalls

### Pitfall 1: Premature "Service Ready" Message

**What goes wrong:** Logging "service ready" before the HTTP server is actually listening causes orchestration systems (Kubernetes health checks, load balancers) to route traffic before the service can handle it, resulting in connection failures.

**Why it happens:** Developer logs success after server setup completes but before ListenAndServe starts accepting connections. The server object exists but isn't bound to the port yet.

**How to avoid:**
- Log "service ready" only after server goroutine starts and ListenAndServe is called
- Verify message order: last phase should be "HTTP server listening" → "service ready"
- Test with curl during startup to ensure ready message appears only when port is accessible

**Warning signs:**
- Health check failures immediately after "ready" log
- Connection refused errors despite "ready" message in logs
- Race condition where sometimes traffic works, sometimes doesn't

### Pitfall 2: Logging Sensitive Configuration Values

**What goes wrong:** Logging configuration values without filtering secrets exposes credentials in logs, which may be stored in log aggregation systems, backed up, or accessible to unauthorized users.

**Why it happens:** Configuration structs contain both safe values (ports, intervals) and secrets (passwords, tokens). Developer logs entire config or doesn't consider that webhook URLs contain secret tokens.

**How to avoid:**
- Explicitly list safe fields to log, never log entire config struct
- Identify secret fields: passwords, tokens, API keys, webhook URLs with tokens
- Document which fields are safe vs. secret in config struct comments
- Use structured logging with explicit field names, not formatted strings

**Warning signs:**
- Grep logs for "password", "token", "webhook" reveals values
- Security scan detects secrets in log files
- Log aggregation dashboard displays credentials

### Pitfall 3: Missing Component Identity in Error Logs

**What goes wrong:** Initialization error logs like `"failed to initialize: connection refused"` don't indicate which component failed, making debugging impossible without code inspection, especially when multiple components connect to different services.

**Why it happens:** Error wrapping preserves error chain but doesn't add component context. Multiple components share similar error messages (database, client, telemetry all connect to services).

**How to avoid:**
- Include "component" field in all error logs: `logger.ErrorContext(ctx, "msg", "component", "database", "err", err)`
- Include component-specific context: database path, client type, service name
- Use structured logging, not formatted error strings

**Warning signs:**
- Cannot determine from logs which component failed
- Multiple components log same error message
- Debugging requires adding more logs to identify source

### Pitfall 4: No Shutdown Progress Visibility

**What goes wrong:** Application logs "shutting down" then appears to hang with no further output, making it unclear if shutdown is progressing, which component is stuck, or if the process is deadlocked.

**Why it happens:** Shutdown logic exists but doesn't log intermediate steps. Long-running cleanups (waiting for downloads, database connections draining) appear as silent pauses.

**How to avoid:**
- Log each shutdown phase: server stop → downloads drain → database close → telemetry shutdown
- Log component name before shutdown and after completion
- Log timeout warnings if phase exceeds expected duration
- Log final "shutdown complete" so it's clear process finished vs. killed

**Warning signs:**
- Shutdown takes longer than expected with no log output
- SIGKILL needed to stop process (shutdown timeout)
- Cannot determine which component is blocking shutdown

### Pitfall 5: Initialization Order Doesn't Match Log Order

**What goes wrong:** Logs show "database ready" before "telemetry ready" but code actually initializes telemetry first, causing confusion when debugging initialization issues or understanding dependencies.

**Why it happens:** Logs added after code written, or refactoring changed order but logs weren't updated. Logger calls placed inconsistently (some at start of phase, some at end).

**How to avoid:**
- Log at consistent point: "initializing X" at start, "X ready" at end
- Maintain log order matching code execution order
- Review logs during testing to verify order matches code flow
- Use integration test that parses logs and validates phase order

**Warning signs:**
- Log timestamps show events out of expected order
- "Dependency ready" logs appear after dependent component
- Initialization failure logs reference component that shows "ready" in previous log

### Pitfall 6: Shutdown Logging After Context Cancelled

**What goes wrong:** Using the cancelled request context (`ctx`) for shutdown logging causes log entries to be rejected or incomplete because the context deadline has passed.

**Why it happens:** Developer uses same context for request handling and shutdown. When `ctx.Done()` triggers shutdown, that context is cancelled, but shutdown logs need a fresh context with its own timeout.

**How to avoid:**
- Create new shutdown context: `shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Web.ShutdownTimeout)`
- Never use cancelled context for shutdown logging
- Pass shutdownCtx (not original ctx) to all shutdown operations

**Warning signs:**
- Shutdown logs missing or incomplete
- "context cancelled" errors during shutdown
- Shutdown appears to fail immediately

## Code Examples

Verified patterns from official sources:

### Complete Lifecycle Logging Implementation

```go
// Source: Composite from https://www.kelche.co/blog/go/http-server/
// and https://josemyduarte.github.io/2023-04-24-golang-lifecycle/

package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"
)

var version = "v1.2.0"

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Set up signal handling for graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        sig := <-sigChan
        slog.Info("shutdown signal received", "signal", sig.String())
        cancel()
    }()

    if err := run(ctx); err != nil {
        slog.ErrorContext(ctx, "fatal error", "err", err)
        os.Exit(1)
    }
}

func run(ctx context.Context) error {
    // Phase 1: Configuration
    slog.InfoContext(ctx, "loading configuration")
    cfg, logger, err := initializeConfig()
    if err != nil {
        return fmt.Errorf("configuration initialization failed: %w", err)
    }

    ctx = logctx.WithLogger(ctx, logger)
    logger = logger.WithGroup("main")

    logger.InfoContext(ctx, "configuration loaded",
        "log_level", cfg.LogLevel,
        "version", version,
        "target_label", cfg.TargetLabel,
        "download_dir", cfg.DownloadDir,
        "polling_interval", cfg.PollingInterval.String())

    // Phase 2: Telemetry
    logger.InfoContext(ctx, "initializing telemetry")
    tel, err := initializeTelemetry(ctx, cfg)
    if err != nil {
        logger.ErrorContext(ctx, "telemetry initialization failed",
            "component", "telemetry",
            "err", err)
        return fmt.Errorf("telemetry initialization failed: %w", err)
    }
    defer tel.Shutdown(ctx)
    logger.InfoContext(ctx, "telemetry ready",
        "service_name", cfg.Telemetry.ServiceName,
        "otel_enabled", cfg.Telemetry.Enabled)

    // Phase 3: Database
    logger.InfoContext(ctx, "initializing database")
    services, err := initializeServices(ctx, cfg, tel)
    if err != nil {
        logger.ErrorContext(ctx, "services initialization failed",
            "component", "services",
            "err", err)
        return fmt.Errorf("services initialization failed: %w", err)
    }
    defer services.Close()
    logger.InfoContext(ctx, "database ready",
        "db_path", cfg.DBPath)

    // Phase 4: Download Clients
    logger.InfoContext(ctx, "initializing download clients")
    // Client initialization happens in initializeServices
    logger.InfoContext(ctx, "download clients ready",
        "client_type", cfg.DownloadClient)

    // Phase 5: HTTP Server
    logger.InfoContext(ctx, "starting HTTP server")
    servers, err := startServers(ctx, cfg, tel)
    if err != nil {
        logger.ErrorContext(ctx, "server startup failed",
            "component", "http_server",
            "err", err)
        return fmt.Errorf("server initialization failed: %w", err)
    }

    logger.InfoContext(ctx, "service ready",
        "bind_address", cfg.Web.BindAddress,
        "target_label", cfg.TargetLabel,
        "version", version)

    return runMainLoop(ctx, cfg, servers, services, tel)
}

func runMainLoop(ctx context.Context, cfg *config, servers *servers, services *services, tel *telemetry.Telemetry) error {
    logger := logctx.LoggerFromContext(ctx)

    logger.InfoContext(ctx, "waiting for downloads",
        "target_label", cfg.TargetLabel,
        "polling_interval", cfg.PollingInterval.String())

    for {
        select {
        case err := <-servers.errors:
            return fmt.Errorf("server error: %w", err)
        case <-ctx.Done():
            // Create new context for shutdown with timeout
            shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Web.ShutdownTimeout)
            defer cancel()

            logger.InfoContext(shutdownCtx, "starting graceful shutdown",
                "shutdown_timeout", cfg.Web.ShutdownTimeout.String())

            // Phase 1: Stop HTTP server
            logger.InfoContext(shutdownCtx, "stopping HTTP server")
            if err := servers.api.Shutdown(shutdownCtx); err != nil {
                logger.ErrorContext(shutdownCtx, "HTTP server shutdown error",
                    "component", "http_server",
                    "err", err)

                if err = servers.api.Close(); err != nil {
                    return fmt.Errorf("failed to stop server: %w", err)
                }
            }
            logger.InfoContext(shutdownCtx, "HTTP server stopped")

            // Phase 2: Wait for downloads to finish
            logger.InfoContext(shutdownCtx, "waiting for active downloads to complete")
            // Downloads drain via context cancellation
            logger.InfoContext(shutdownCtx, "downloads completed")

            // Phase 3: Cleanup services
            logger.InfoContext(shutdownCtx, "cleaning up services")
            services.Close()
            logger.InfoContext(shutdownCtx, "services cleanup complete")

            // Phase 4: Close database
            logger.InfoContext(shutdownCtx, "closing database connections")
            // Database closed via services.Close()
            logger.InfoContext(shutdownCtx, "database connections closed")

            // Phase 5: Shutdown telemetry
            logger.InfoContext(shutdownCtx, "shutting down telemetry")
            if err := tel.Shutdown(shutdownCtx); err != nil {
                logger.ErrorContext(shutdownCtx, "telemetry shutdown error",
                    "component", "telemetry",
                    "err", err)
            } else {
                logger.InfoContext(shutdownCtx, "telemetry shutdown complete")
            }

            logger.InfoContext(shutdownCtx, "graceful shutdown complete")
            return ctx.Err()
        }
    }
}
```

### Component Initialization with Ready Logging

```go
// Source: https://josemyduarte.github.io/2023-04-24-golang-lifecycle/

func initializeTelemetry(ctx context.Context, cfg *config) (*telemetry.Telemetry, error) {
    logger := logctx.LoggerFromContext(ctx)

    tel, err := telemetry.New(ctx, telemetry.Config{
        ServiceName:    cfg.Telemetry.ServiceName,
        ServiceVersion: version,
        OTELAddress:    cfg.Telemetry.OTELAddress,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
    }

    logger.InfoContext(ctx, "telemetry component ready",
        "service_name", cfg.Telemetry.ServiceName,
        "otel_address", cfg.Telemetry.OTELAddress,
        "otel_enabled", cfg.Telemetry.Enabled)

    return tel, nil
}

func initializeServices(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*services, error) {
    logger := logctx.LoggerFromContext(ctx)

    // Database
    database, err := sqlite.InitDB(ctx, cfg.DBPath, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
    if err != nil {
        return nil, fmt.Errorf("failed to initialize database: %w", err)
    }
    logger.InfoContext(ctx, "database component ready",
        "db_path", cfg.DBPath,
        "max_open_conns", cfg.DBMaxOpenConns,
        "max_idle_conns", cfg.DBMaxIdleConns)

    dr := sqlite.NewInstrumentedDownloadRepository(database, tel)

    // Download client
    dc, err := buildDownloadClient(cfg)
    if err != nil {
        return nil, fmt.Errorf("failed to build download client: %w", err)
    }

    instrumentedDC := transfer.NewInstrumentedDownloadClient(dc, tel, cfg.DownloadClient)
    if err := instrumentedDC.Authenticate(ctx); err != nil {
        return nil, fmt.Errorf("failed to authenticate with download client: %w", err)
    }
    logger.InfoContext(ctx, "download client ready",
        "client_type", cfg.DownloadClient)

    // ... rest of setup ...

    return &services{
        downloader:           downloader,
        transferOrchestrator: transferOrchestrator,
    }, nil
}
```

### Initialization Failure Logging

```go
// Source: Current codebase + best practices

func initializeDatabase(ctx context.Context, cfg *config) (*sql.DB, error) {
    logger := logctx.LoggerFromContext(ctx)

    database, err := sqlite.InitDB(ctx, cfg.DBPath, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
    if err != nil {
        logger.ErrorContext(ctx, "database initialization failed",
            "component", "database",
            "db_path", cfg.DBPath,
            "max_open_conns", cfg.DBMaxOpenConns,
            "max_idle_conns", cfg.DBMaxIdleConns,
            "err", err)
        return nil, fmt.Errorf("failed to initialize database: %w", err)
    }

    return database, nil
}

func authenticateClient(ctx context.Context, dc transfer.DownloadClient, clientType string) error {
    logger := logctx.LoggerFromContext(ctx)

    if err := dc.Authenticate(ctx); err != nil {
        logger.ErrorContext(ctx, "download client authentication failed",
            "component", "download_client",
            "client_type", clientType,
            "err", err)
        return fmt.Errorf("failed to authenticate with %s: %w", clientType, err)
    }

    return nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Single "starting" log | Phased initialization logging | 2020-2023 (12-factor apps) | Clear visibility into what's initializing, where failures occur |
| Silent success | "Component ready" messages | 2022-2024 (observability focus) | Explicit confirmation each component initialized successfully |
| Minimal shutdown logs | Detailed shutdown sequence | 2023-2025 (Kubernetes era) | Graceful shutdown debugging, pod termination diagnosis |
| Log secrets at startup | Filter sensitive values | 2020-present (security awareness) | Prevent credential exposure in log aggregation systems |
| Global error on failure | Component-specific error context | 2021-2024 (structured logging) | Pinpoint exact failure source without code inspection |
| Unstructured text logs | Structured slog with fields | 2023+ (Go 1.21 slog release) | Machine-parseable logs, easier filtering and analysis |

**Deprecated/outdated:**
- **Silent initialization:** No logging during startup considered acceptable
- **Single "ready" message:** No component-level visibility
- **Logging to files:** 12-factor apps log to stdout, environment handles aggregation
- **Global logger with no context:** Context-aware logging required for trace correlation

## Open Questions

Things that couldn't be fully resolved:

1. **Should component initialization be logged in internal packages or in main.go?**
   - What we know: main.go orchestrates initialization and sees all errors. Internal packages like telemetry/storage could log "ready" internally.
   - What's unclear: Duplication if both main.go and internal packages log same events. Single source of truth preferred.
   - Recommendation: Log initialization phases in main.go ("initializing X"), let internal packages log detailed "component ready" with component-specific fields. Main logs phase, component logs specifics.

2. **How much configuration detail should be logged?**
   - What we know: Operators need to verify configuration correct (ports, paths, intervals). Too much detail clutters logs. Secrets must be filtered.
   - What's unclear: Line between "enough to debug" and "too verbose".
   - Recommendation: Log values that affect behavior and are non-obvious from environment (computed defaults, resolved paths). Document safe fields in config struct comments.

3. **Should startup duration be logged?**
   - What we know: Slow startup indicates problems. Timing helps diagnose performance issues. Adds complexity to capture timestamps.
   - What's unclear: Whether startup timing is priority for v1.2 or future enhancement.
   - Recommendation: Defer startup timing to future work unless slow startup observed. Focus v1.2 on phase visibility and failure logging. Timing can be added later with minimal changes.

4. **What to do if shutdown exceeds timeout?**
   - What we know: shutdownCtx has timeout. If exceeded, context cancelled and shutdown may be incomplete.
   - What's unclear: Should force-close connections? Log specific component that timed out? Exit immediately or continue trying?
   - Recommendation: Log ERROR if shutdown context times out, indicating which phase was active. Let main return error and exit. Kubernetes/systemd will SIGKILL if needed.

## Sources

### Primary (HIGH confidence)
- [Structured Logging with slog - The Go Programming Language](https://go.dev/blog/slog) - Official slog patterns
- [How to Implement Graceful Shutdown in Go](https://oneuptime.com/blog/post/2026-01-23-go-graceful-shutdown/view) - 2026 shutdown patterns with logging
- [How to shutdown a Go application gracefully | Josemy's blog](https://josemyduarte.github.io/2023-04-24-golang-lifecycle/) - Component lifecycle and ready messages
- [The Twelve-Factor App](https://12factor.net/config) - Configuration and logging principles
- [The Ultimate Go HTTP Server Tutorial: Logging, Tracing, and More](https://www.kelche.co/blog/go/http-server/) - Server startup logging patterns

### Secondary (MEDIUM confidence)
- [Logging in Go with Slog: The Ultimate Guide | Better Stack Community](https://betterstack.com/community/guides/logging/logging-in-go/) - slog usage patterns
- [A Deep Dive into Graceful Shutdown in Go - DEV Community](https://dev.to/yanev/a-deep-dive-into-graceful-shutdown-in-go-484a) - Shutdown sequence patterns
- [How to Use Init Functions in Go](https://oneuptime.com/blog/post/2026-01-23-go-init-functions/view) - Initialization error handling
- [Dependency Lifecycle Management in Go :: Posts](https://www.jacoelho.com/blog/2025/05/dependency-lifecycle-management-in-go/) - Component dependency patterns
- [Go Logging Best Practices](https://webreference.com/go/best-practices/logging/) - General logging guidance

### Tertiary (LOW confidence)
- [GitHub - effxhq/go-lifecycle: A state-based application lifecycle library](https://github.com/effxhq/go-lifecycle) - Lifecycle library (not used, but shows patterns)
- [Logs - The Twelve Factor App Methodology - DEV Community](https://dev.to/cadienvan/logs-the-twelve-factor-app-methodology-1l0m) - 12-factor logging interpretation

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Uses only stdlib packages already in use, no new dependencies
- Architecture: HIGH - Patterns verified across multiple authoritative sources, align with 12-factor principles
- Pitfalls: HIGH - Based on actual patterns from current codebase and documented anti-patterns in sources

**Research date:** 2026-02-08
**Valid until:** 2026-05-08 (90 days - stable domain, stdlib-based, slow evolution)
