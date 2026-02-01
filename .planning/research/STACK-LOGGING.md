# Stack Research: Logging Improvements

**Domain:** Go Service Structured Logging Enhancement
**Researched:** 2026-02-01
**Confidence:** HIGH

## Current Stack (Already Validated)

| Technology | Version | Purpose | Status |
|------------|---------|---------|--------|
| log/slog | Go 1.23 stdlib | Structured JSON logging | ✓ In use |
| OpenTelemetry | v1.38.0 | Distributed tracing | ✓ In use |
| Chi Router | v5 | HTTP routing | ✓ In use |

## Recommended Stack Additions

### Core Enhancement: OpenTelemetry Log Bridge

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| go.opentelemetry.io/contrib/bridges/otelslog | v0.14.0+ | Automatic trace context injection into logs | Official OTel bridge for slog. Adds <1% overhead. Automatically includes trace_id and span_id in all log entries for correlation with distributed traces. Latest semantic conventions (v1.34.0 as of Jan 2026). |

**Rationale:** This is the official OpenTelemetry approach for bridging slog with trace context. It eliminates the need for custom handler wrapping while maintaining full compatibility with existing slog usage. Since the application already uses OpenTelemetry v1.38.0 for tracing, adding this bridge is a natural extension.

### Supporting Pattern: Handler Composition (Optional)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| github.com/samber/slog-multi | Latest | Advanced handler composition (fanout, routing, middleware) | Only if you need to send logs to multiple destinations with different filtering rules. Not needed for basic trace correlation. |
| github.com/golang-cz/devslog | Latest | Pretty console output for development | Only for local development if JSON logs are hard to read. Should NOT be used in production. |

## Installation

```bash
# Core: OpenTelemetry slog bridge
go get go.opentelemetry.io/contrib/bridges/otelslog

# Optional: Development pretty logging (local only)
go get github.com/golang-cz/devslog
```

## Configuration Pattern

### Production Configuration (Recommended)

```go
import (
    "log/slog"
    "os"
    "go.opentelemetry.io/contrib/bridges/otelslog"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

// 1. Initialize OpenTelemetry LoggerProvider
logProvider := sdklog.NewLoggerProvider(
    sdklog.WithResource(res), // Use same resource as trace provider
)

// 2. Create otelslog handler with appropriate level
opts := &slog.HandlerOptions{
    Level: slog.LevelInfo, // Use LevelDebug only in non-production
    AddSource: true, // Include file/line for errors
}

// 3. Wrap with otelslog to get trace context injection
handler := otelslog.NewHandler(
    "seedbox_downloader",
    otelslog.WithLoggerProvider(logProvider),
)

// 4. Set as default logger
logger := slog.New(handler)
slog.SetDefault(logger)
```

### Development Configuration (Optional)

```go
// For local development, use TextHandler for readability
var handler slog.Handler
if os.Getenv("APP_ENV") == "production" {
    handler = otelslog.NewHandler("seedbox_downloader",
        otelslog.WithLoggerProvider(logProvider))
} else {
    handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
        AddSource: true,
    })
}
```

## Usage Patterns

### CRITICAL: Always Use Context-Aware Logging

```go
// ✓ CORRECT: Includes trace context
slog.InfoContext(ctx, "transfer downloaded",
    "transfer_id", transfer.ID,
    "transfer_name", transfer.Name,
    "duration_ms", duration.Milliseconds())

// ✗ WRONG: Missing trace context, no correlation
slog.Info("transfer downloaded",
    "transfer_id", transfer.ID)
```

### Lifecycle Logging Pattern

```go
// Startup: Info level with structured context
logger.InfoContext(ctx, "service starting",
    "component", "main",
    "version", version,
    "log_level", cfg.LogLevel)

logger.InfoContext(ctx, "database initialized",
    "component", "database",
    "path", cfg.DBPath,
    "max_open_conns", cfg.DBMaxOpenConns)

// Shutdown: Info level with reason
logger.InfoContext(ctx, "service shutting down",
    "component", "main",
    "reason", "signal_received")
```

### Error Logging with Stack Traces

```go
import "runtime/debug"

// For unexpected errors, include stack trace
if err != nil {
    logger.ErrorContext(ctx, "unexpected error",
        "component", "transfer_orchestrator",
        "operation", "poll_transfers",
        "err", err,
        "stack", string(debug.Stack()))
}
```

### Structured Fields Best Practices

```go
// Group related fields logically
logger.InfoContext(ctx, "download started",
    // Identity fields
    "transfer_id", transfer.ID,
    "transfer_name", transfer.Name,

    // Metrics fields
    "file_count", len(transfer.Files),
    "total_size_bytes", transfer.Size,

    // Operational fields
    "download_dir", downloadDir,
    "max_parallel", maxParallel)
```

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Custom trace context handler | Reinventing the wheel, maintenance burden | Official otelslog bridge |
| Handler wrapping for trace context | Complex, error-prone, unofficial | otelslog.NewHandler |
| github.com/go-slog/otelslog (third-party) | Community package, prefer official | go.opentelemetry.io/contrib/bridges/otelslog |
| JSONHandler in development | Hard to read, slows debugging | TextHandler or devslog for local only |
| TextHandler in production | Can't be parsed by log aggregators | JSONHandler or otelslog |
| slog.Info() without Context | Loses trace correlation | slog.InfoContext(ctx, ...) |
| Logging full payloads | Noise, performance overhead | Log IDs, sizes, counts only |
| Debug level in production | Performance overhead, noise | Info level minimum |

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| otelslog (official bridge) | Custom slog.Handler with trace extraction | Never - custom handlers add complexity and maintenance burden |
| Standard slog levels (Debug/Info/Warn/Error) | Custom log levels | Never - stick to standard levels for tooling compatibility |
| JSONHandler (production) | TextHandler (production) | Never in production - JSON required for log aggregation |
| Single logger with handler composition | Multiple logger instances | Never - single source of truth is clearer |

## Stack Patterns by Environment

**Production:**
- Use otelslog.NewHandler for automatic trace context
- Set level to slog.LevelInfo (not Debug)
- Enable AddSource: true for error tracking
- Output JSON to stdout (captured by container runtime)

**Development:**
- Use slog.NewTextHandler for readability OR
- Use devslog for pretty colors (optional)
- Set level to slog.LevelDebug
- Output to stdout for immediate feedback

**CI/CD:**
- Use JSONHandler (no otelslog needed if no tracing)
- Set level to slog.LevelDebug for test failures
- Parse JSON for test analytics

## Version Compatibility

| Package | Compatible With | Notes |
|---------|-----------------|-------|
| otelslog@v0.14.0+ | go.opentelemetry.io/otel@v1.38.0 | Requires OTEL SDK v1.24.0+ |
| otelslog@v0.14.0+ | log/slog (Go 1.21+) | Uses standard slog.Handler interface |
| slog handlers | Any slog.Handler composition | Handlers can be wrapped/chained |

## Integration with Existing Stack

### With OpenTelemetry Tracing (Already In Use)

```go
// Reuse the same resource for logs and traces
res, err := resource.New(ctx,
    resource.WithAttributes(
        semconv.ServiceName(cfg.Telemetry.ServiceName),
        semconv.ServiceVersion(cfg.Telemetry.ServiceVersion),
    ),
)

// Traces (existing)
tracerProvider := trace.NewTracerProvider(
    trace.WithResource(res),
    // ... existing trace config
)

// Logs (new addition)
logProvider := sdklog.NewLoggerProvider(
    sdklog.WithResource(res), // Same resource
)
```

### With Chi Router HTTP Middleware (Already In Use)

The telemetry middleware already injects trace context into request contexts. No changes needed - just use context-aware logging:

```go
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context() // Already has trace context from middleware

    logger := slog.Default()
    logger.InfoContext(ctx, "handling request",
        "method", r.Method,
        "path", r.URL.Path)
    // trace_id and span_id automatically included
}
```

### With logctx Package (Already In Use)

The existing `internal/logctx` package can be enhanced or replaced:

**Option 1: Keep logctx, enhance with otelslog**
```go
// In main.go
logger := slog.New(otelslog.NewHandler("seedbox_downloader"))
ctx = logctx.WithLogger(ctx, logger)
```

**Option 2: Remove logctx, use slog.Default()**
```go
// Set default once at startup
slog.SetDefault(slog.New(otelslog.NewHandler("seedbox_downloader")))

// Everywhere else, just use slog directly
logger := slog.Default()
logger.InfoContext(ctx, "message", "key", "value")
```

**Recommendation:** Option 2 is simpler. The `logctx` package adds indirection without clear benefit once otelslog is in place.

## Migration Path

### Phase 1: Add otelslog (No Breaking Changes)

1. Install `go.opentelemetry.io/contrib/bridges/otelslog`
2. Initialize LoggerProvider alongside existing TracerProvider
3. Wrap existing JSON handler with otelslog
4. Deploy - existing logs now have trace_id/span_id

### Phase 2: Audit Logging Calls

1. Find all `logger.Info()` calls (no Context)
2. Replace with `logger.InfoContext(ctx, ...)`
3. Ensure all goroutines receive context with trace info

### Phase 3: Lifecycle Logging

1. Add structured startup logs for each component
2. Add structured shutdown logs
3. Use consistent "component" and "operation" fields

### Phase 4: Level Tuning (Optional)

1. Set production to slog.LevelInfo
2. Use slog.LevelDebug only for specific investigations
3. Consider dynamic level adjustment via LevelVar

## Performance Considerations

| Concern | Impact | Mitigation |
|---------|--------|------------|
| otelslog overhead | <1% CPU, minimal memory | No mitigation needed - overhead is negligible |
| Trace context extraction | ~100ns per log call | Already paid by OpenTelemetry instrumentation |
| JSON encoding | ~1-5µs per log entry | Standard cost, unavoidable for structured logs |
| Debug level logs | High volume in long-running service | Disable Debug in production (use Info minimum) |

## Sources

**Official Documentation:**
- [OpenTelemetry Slog Bridge Package](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog) - Official API documentation
- [Go slog Official Blog](https://go.dev/blog/slog) - Structured Logging introduction
- [Go slog Package Documentation](https://pkg.go.dev/log/slog) - Standard library reference

**Best Practices & Guides:**
- [Logging in Go with Slog: The Ultimate Guide | Better Stack](https://betterstack.com/community/guides/logging/logging-in-go/) - Comprehensive best practices
- [OpenTelemetry Slog Integration | Uptrace](https://uptrace.dev/guides/opentelemetry-slog) - Practical setup guide
- [How to Set Up Structured Logging in Go with OpenTelemetry | OneUpTime](https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view) - January 2026 guide
- [Logging in Go with Slog: A Practitioner's Guide | Dash0](https://www.dash0.com/guides/logging-in-go-with-slog) - Production patterns
- [Complete Guide to Logging in Golang with slog | SigNoz](https://signoz.io/guides/golang-slog/) - Comprehensive tutorial

**Handler Composition:**
- [slog-multi Package](https://pkg.go.dev/github.com/samber/slog-multi) - Advanced composition patterns
- [Writing a Slog Handler Part 1: The Wrapper | WillAbides](https://willabides.com/posts/go-slog-handler-part-1/) - Custom handler patterns

**Lifecycle Management:**
- [How to shutdown a Go application gracefully | Josemy's blog](https://josemyduarte.github.io/2023-04-24-golang-lifecycle/) - Graceful shutdown patterns
- [go-lifecycle Package](https://pkg.go.dev/github.com/g4s8/go-lifecycle) - Lifecycle management library

---
*Stack research for: Logging improvements in Go service*
*Researched: 2026-02-01*
*Confidence: HIGH - Official docs, package documentation, and multiple authoritative guides consulted*
