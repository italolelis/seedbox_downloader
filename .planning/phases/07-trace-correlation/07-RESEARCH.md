# Phase 7: Trace Correlation - Research

**Researched:** 2026-02-02
**Domain:** OpenTelemetry trace correlation with Go structured logging (slog)
**Confidence:** HIGH

## Summary

Trace correlation bridges OpenTelemetry distributed tracing with structured logs by automatically injecting trace_id and span_id into log entries. This enables end-to-end request correlation across services and components.

The standard approach in Go uses the official `go.opentelemetry.io/contrib/bridges/otelslog` bridge (v0.14.0 as of Dec 2025). This bridge implements `slog.Handler` to extract trace context from the provided `context.Context` and append it as structured attributes to each log record. The key requirement is using context-aware logging methods (`InfoContext`, `DebugContext`, etc.) instead of basic methods (`Info`, `Debug`), and propagating context through all function calls including goroutines.

**Primary recommendation:** Use official otelslog bridge wrapping existing JSONHandler to preserve current output format while adding automatic trace correlation. Replace all logging calls with context-aware equivalents and ensure all goroutines receive and propagate context.

## Standard Stack

The established libraries/tools for trace correlation in Go:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| go.opentelemetry.io/contrib/bridges/otelslog | v0.14.0+ | Bridge slog with OpenTelemetry | Official OpenTelemetry bridge, <1% overhead, maintained by OTel community |
| go.opentelemetry.io/otel/sdk/log | v0.14.0+ | OpenTelemetry log SDK | Required for LoggerProvider configuration |
| go.opentelemetry.io/otel/trace | v1.38.0+ | Trace API for span extraction | Core OpenTelemetry tracing primitives |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp | v0.63.0 | HTTP middleware with auto-instrumentation | Already in use - provides automatic span creation for HTTP requests |
| log/slog | stdlib (Go 1.21+) | Structured logging | Already in use - standard library structured logging |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Official otelslog bridge | Custom slog.Handler wrapper | Custom wrapper provides flexibility but adds maintenance burden and may not track OTel spec changes |
| Official otelslog bridge | Third-party bridges (go-slog/otelslog, remychantenay/slog-otel) | Community projects may have extra features but lack official support and standardization |
| Context-aware methods | Manual trace ID extraction per call | Manual extraction is error-prone, verbose, and defeats purpose of automatic correlation |

**Installation:**
```bash
go get go.opentelemetry.io/contrib/bridges/otelslog
```

## Architecture Patterns

### Recommended Approach: Two-Layer Handler

The project already uses `slog.NewJSONHandler` for structured JSON output. The otelslog bridge should wrap this existing handler to add trace correlation without changing output format.

```
Request → otelhttp middleware (creates span) → context with span
         ↓
Log call with context → otelslog.Handler (extracts trace/span IDs)
         ↓
Existing JSONHandler (formats as JSON with added trace fields)
         ↓
stdout (JSON with trace_id, span_id fields)
```

### Pattern 1: Bridge Setup with Wrapped Handler

**What:** Configure otelslog to wrap existing JSONHandler preserving output format

**When to use:** When migrating existing logging to add trace correlation without breaking log consumers

**Example:**
```go
// Source: https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog
import (
    "log/slog"
    "os"
    "go.opentelemetry.io/contrib/bridges/otelslog"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

// During telemetry initialization
func setupLogging(logProvider *sdklog.LoggerProvider) *slog.Logger {
    // Preserve existing JSON handler configuration
    jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
        AddSource: true, // Include source location
    })

    // Wrap with otelslog for trace correlation
    otelHandler := otelslog.NewHandler("seedbox_downloader",
        otelslog.WithLoggerProvider(logProvider),
        otelslog.WithVersion("v1.2.0"),
    )

    // Note: otelslog acts as primary handler, not wrapper
    // It sends logs to OTLP, doesn't wrap JSONHandler
    logger := slog.New(otelHandler)
    slog.SetDefault(logger)

    return logger
}
```

**CRITICAL CORRECTION:** The otelslog bridge does NOT wrap existing handlers like JSONHandler. It replaces them entirely, converting logs to OpenTelemetry log records sent via OTLP protocol. To preserve JSON stdout output while adding trace correlation, use a custom wrapper pattern instead.

### Pattern 2: Custom Handler Wrapper (Recommended for This Project)

**What:** Implement custom slog.Handler that wraps JSONHandler and injects trace context

**When to use:** When preserving existing JSON output format is required (requirement TRACE-03)

**Example:**
```go
// Source: https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view
import (
    "context"
    "log/slog"
    "go.opentelemetry.io/otel/trace"
)

type TraceHandler struct {
    handler slog.Handler
}

func NewTraceHandler(h slog.Handler) *TraceHandler {
    return &TraceHandler{handler: h}
}

func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
    return h.handler.Enabled(ctx, level)
}

func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
    // Extract span from context
    span := trace.SpanFromContext(ctx)
    spanCtx := span.SpanContext()

    // Add trace context if span is valid
    if spanCtx.IsValid() {
        r.AddAttrs(
            slog.String("trace_id", spanCtx.TraceID().String()),
            slog.String("span_id", spanCtx.SpanID().String()),
            slog.Bool("trace_sampled", spanCtx.IsSampled()),
        )
    }

    return h.handler.Handle(ctx, r)
}

func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    return &TraceHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *TraceHandler) WithGroup(name string) slog.Handler {
    return &TraceHandler{handler: h.handler.WithGroup(name)}
}
```

### Pattern 3: Context-Aware Logging

**What:** Replace all non-context logging calls with context-aware equivalents

**When to use:** Always - required for trace correlation to work

**Example:**
```go
// Source: https://uptrace.dev/guides/opentelemetry-slog

// BEFORE (no trace context)
slog.Info("transfer discovered", "transfer_id", id)

// AFTER (with trace context)
slog.InfoContext(ctx, "transfer discovered", "transfer_id", id)

// BEFORE (logger from context, no trace)
logger := logctx.LoggerFromContext(ctx)
logger.Info("downloading", "file", name)

// AFTER (context-aware method)
logger := logctx.LoggerFromContext(ctx)
logger.InfoContext(ctx, "downloading", "file", name)
```

### Pattern 4: Context Propagation to Goroutines

**What:** Pass context to goroutines to maintain trace lineage

**When to use:** Every goroutine that performs work related to a traced request

**Example:**
```go
// Source: https://go.dev/blog/context

// BEFORE (context lost in goroutine)
go func() {
    // No ctx available - logs lack trace_id
    slog.Info("background work started")
    doWork()
}()

// AFTER (context propagated)
go func(ctx context.Context) {
    // ctx maintains trace lineage
    slog.InfoContext(ctx, "background work started")
    doWork(ctx)
}(ctx)

// SPECIAL CASE: Background work outliving request
go func(ctx context.Context) {
    // Detach from parent cancellation but keep trace context
    // Use context.WithoutCancel (Go 1.21+) or extract values manually
    detachedCtx := context.WithoutCancel(ctx)
    slog.InfoContext(detachedCtx, "long-running background work")
    doLongWork(detachedCtx)
}(ctx)
```

### Anti-Patterns to Avoid

- **Using non-context logging methods:** `slog.Info()` instead of `slog.InfoContext(ctx)` breaks trace correlation
- **Not passing context to goroutines:** Spawned goroutines lose trace lineage, logs appear unrelated
- **Using context.Background() in request handlers:** Creates new trace root instead of continuing parent trace
- **Storing logger in context instead of passing separately:** Context should carry trace context, not logger instance
- **Assuming global logger configuration is enough:** Must use context-aware methods even with configured global logger

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Extracting trace/span IDs for logs | Manual span.SpanContext() calls at each log site | Custom slog.Handler wrapper or otelslog bridge | 100s of call sites, easy to miss, automatic extraction is zero-cost abstraction |
| Propagating trace context through calls | Custom trace ID in context.Value | Standard context with OpenTelemetry span | OTel ecosystem expects standard format, W3C traceparent interop required |
| Detecting missing trace context | Manual checks for zero trace IDs | Missing fields in logs | Absence of trace_id field clearly indicates propagation bug, explicit checks add noise |
| Converting trace IDs to strings | Custom formatting logic | SpanContext().TraceID().String() | Standard format required for correlation with tracing backends |

**Key insight:** Trace correlation is cross-cutting concern affecting every log call. Manual solutions are unmaintainable at scale. Handler-based approach centralizes logic and guarantees consistency.

## Common Pitfalls

### Pitfall 1: Forgetting Context Parameter in Logging Calls

**What goes wrong:** Using `slog.Info()` instead of `slog.InfoContext(ctx)` means the handler receives no context, cannot extract span, and logs lack trace_id/span_id.

**Why it happens:** Old habit from pre-context logging, slog API allows both forms, no compile-time error.

**How to avoid:**
- Establish code review guideline: all slog calls must use *Context methods
- Use linter rules to detect non-context logging calls (staticcheck, revive)
- Search codebase for patterns: `slog\.(Info|Debug|Warn|Error)\(` without `Context`

**Warning signs:**
- Logs during HTTP requests missing trace_id field
- Cannot correlate logs with traces in observability platform
- Some logs have trace_id, others don't within same request flow

### Pitfall 2: Not Propagating Context to Goroutines

**What goes wrong:** Goroutine receives no context parameter or uses `context.Background()`, creating trace gap. Logs from goroutine lack trace correlation even though parent had it.

**Why it happens:** Goroutines often run background work, developers assume they don't need request context. Or concern about context cancellation affecting background work.

**How to avoid:**
- Every goroutine should accept `context.Context` as parameter
- If work must outlive request, use `context.WithoutCancel(ctx)` (Go 1.21+) to preserve trace while removing cancellation
- For truly independent work, document why trace correlation is not needed

**Warning signs:**
- Log entries show trace_id suddenly disappearing mid-request
- Cannot trace request flow through async operations
- Goroutine work appears orphaned in distributed traces

### Pitfall 3: Assuming Global Logger Configuration is Sufficient

**What goes wrong:** Developer configures `slog.SetDefault()` with otelslog handler, but uses `logger.Info()` from instance thinking it's enough. Trace context still not included because method doesn't receive context parameter.

**Why it happens:** Confusion between handler configuration (global) and method usage (per-call). Handler CAN extract trace context, but only if context is passed to method.

**How to avoid:**
- Clearly document: handler wrapping is necessary but not sufficient
- All logging calls must use *Context methods regardless of handler
- Training/documentation emphasizing two-part requirement

**Warning signs:**
- Handler configured correctly but logs still missing trace fields
- Works in some places (using *Context) but not others (using non-context methods)

### Pitfall 4: Breaking JSON Output Format

**What goes wrong:** Replacing JSONHandler entirely with otelslog bridge changes output from JSON-to-stdout to OpenTelemetry OTLP protocol. Existing log consumers (container orchestration, log aggregators expecting JSON) break.

**Why it happens:** Misunderstanding otelslog purpose - it's for sending logs to OTel collectors, not for adding fields to existing formats.

**How to avoid:**
- Read requirement TRACE-03: "wraps existing slog handler without breaking current JSON output format"
- Use custom wrapper pattern, not direct otelslog bridge
- Verify output format matches existing structure with added trace fields

**Warning signs:**
- Logs stop appearing in container logs
- JSON parsing errors in log aggregation pipeline
- Log format completely changed after implementation

### Pitfall 5: Zero Trace IDs in Production

**What goes wrong:** Trace IDs appear as "00000000000000000000000000000000" in logs, indicating invalid span context. Correlation appears to work but all traces show zeros.

**Why it happens:**
- OpenTelemetry not properly initialized (no tracer provider)
- otelhttp middleware not applied to HTTP handlers
- Context created manually without span (e.g., `context.Background()` in handler)
- Trace sampling configured to 0% (no traces created)

**How to avoid:**
- Initialize OpenTelemetry SDK before creating handlers
- Apply otelhttp middleware to all HTTP routes
- Verify span creation: `trace.SpanFromContext(ctx).SpanContext().IsValid()` returns true
- Set appropriate sampling rate (100% for development, configured for production)

**Warning signs:**
- All log entries have same zero trace_id
- Traces don't appear in tracing backend but logs have trace_id field
- `span.SpanContext().IsValid()` returns false

## Code Examples

Verified patterns from official sources:

### Complete Handler Wrapper Implementation

```go
// Source: Composite pattern from https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view
// and https://pkg.go.dev/log/slog

package logctx

import (
    "context"
    "log/slog"
    "go.opentelemetry.io/otel/trace"
)

// TraceHandler wraps an slog.Handler to inject OpenTelemetry trace context
type TraceHandler struct {
    handler slog.Handler
}

// NewTraceHandler creates a handler that adds trace_id and span_id to logs
func NewTraceHandler(h slog.Handler) *TraceHandler {
    if h == nil {
        panic("nil handler")
    }
    return &TraceHandler{handler: h}
}

// Enabled reports whether the handler handles records at the given level
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
    return h.handler.Enabled(ctx, level)
}

// Handle adds trace context and delegates to wrapped handler
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
    span := trace.SpanFromContext(ctx)
    if span.SpanContext().IsValid() {
        r.AddAttrs(
            slog.String("trace_id", span.SpanContext().TraceID().String()),
            slog.String("span_id", span.SpanContext().SpanID().String()),
        )
    }
    return h.handler.Handle(ctx, r)
}

// WithAttrs returns a new handler with additional attributes
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    return NewTraceHandler(h.handler.WithAttrs(attrs))
}

// WithGroup returns a new handler with a group name
func (h *TraceHandler) WithGroup(name string) slog.Handler {
    return NewTraceHandler(h.handler.WithGroup(name))
}
```

### Logger Initialization with Wrapper

```go
// Source: https://go.dev/blog/slog

package main

import (
    "log/slog"
    "os"
    "github.com/italolelis/seedbox_downloader/internal/logctx"
)

func initializeLogger(level slog.Level) *slog.Logger {
    // Create base JSON handler (preserves existing format)
    jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level:     level,
        AddSource: true,
    })

    // Wrap with trace correlation
    traceHandler := logctx.NewTraceHandler(jsonHandler)

    // Create and set as default
    logger := slog.New(traceHandler)
    slog.SetDefault(logger)

    return logger
}
```

### Context-Aware Logging Migration

```go
// Source: https://uptrace.dev/guides/opentelemetry-slog

// BEFORE: Non-context logging
func processTransfer(t *transfer.Transfer) error {
    slog.Info("processing transfer", "id", t.ID)
    // ... work ...
    slog.Info("transfer complete", "id", t.ID)
    return nil
}

// AFTER: Context-aware logging
func processTransfer(ctx context.Context, t *transfer.Transfer) error {
    slog.InfoContext(ctx, "processing transfer", "id", t.ID)
    // ... work ...
    slog.InfoContext(ctx, "transfer complete", "id", t.ID)
    return nil
}

// BEFORE: Logger from context without trace
func downloadFile(ctx context.Context, file *File) error {
    logger := logctx.LoggerFromContext(ctx)
    logger.Info("downloading file", "name", file.Name)
    // ... work ...
    return nil
}

// AFTER: Context-aware method on logger instance
func downloadFile(ctx context.Context, file *File) error {
    logger := logctx.LoggerFromContext(ctx)
    logger.InfoContext(ctx, "downloading file", "name", file.Name)
    // ... work ...
    return nil
}
```

### Goroutine Context Propagation

```go
// Source: https://go.dev/blog/context

// Pattern 1: Short-lived goroutine (inherits cancellation)
func handleRequest(ctx context.Context, req *Request) {
    slog.InfoContext(ctx, "handling request")

    // Goroutine inherits context - will stop when parent cancels
    go func(ctx context.Context) {
        slog.InfoContext(ctx, "async processing started")
        doAsyncWork(ctx)
        slog.InfoContext(ctx, "async processing complete")
    }(ctx)

    // ... rest of handler ...
}

// Pattern 2: Long-lived goroutine (detach cancellation, keep trace)
func startBackgroundJob(ctx context.Context, job *Job) {
    slog.InfoContext(ctx, "starting background job")

    // Detach cancellation but keep trace context for correlation
    detachedCtx := context.WithoutCancel(ctx)

    go func(ctx context.Context) {
        slog.InfoContext(ctx, "background job running")
        // This work continues even after request completes
        // But logs still have trace_id for correlation
        doLongRunningWork(ctx)
        slog.InfoContext(ctx, "background job finished")
    }(detachedCtx)
}

// Pattern 3: Truly independent goroutine (no trace context)
func startHealthCheck() {
    // Use Background context - no trace correlation needed
    ctx := context.Background()

    go func(ctx context.Context) {
        // These logs intentionally lack trace_id
        // They are not part of any user request
        slog.InfoContext(ctx, "health check started")
        doHealthCheck(ctx)
    }(ctx)
}
```

### HTTP Middleware Integration

```go
// Source: https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp

package main

import (
    "net/http"
    "log/slog"
    "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func setupServer(serviceName string) *http.Server {
    router := chi.NewRouter()

    // otelhttp middleware creates spans automatically
    // Context passed to handlers includes span
    router.Use(otelhttp.NewMiddleware(serviceName))

    router.Get("/api/transfers", func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context() // Contains span from otelhttp

        // This log will have trace_id and span_id automatically
        slog.InfoContext(ctx, "listing transfers")

        // Pass context to all functions
        transfers, err := listTransfers(ctx)
        if err != nil {
            slog.ErrorContext(ctx, "failed to list transfers", "error", err)
            http.Error(w, "Internal error", 500)
            return
        }

        // ... response ...
    })

    return &http.Server{Handler: router}
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Manual trace ID injection per log call | Automatic via slog.Handler wrapper | 2023-2024 (slog added Go 1.21) | Reduced boilerplate, guaranteed consistency |
| Global log correlation fields | Context-aware per-request correlation | 2020-2022 (OTel matured) | Proper distributed tracing vs. global request ID |
| Custom trace propagation | W3C Trace Context standard | 2021 (W3C standard) | Cross-vendor interoperability |
| Logrus/zap bridges | Native slog bridges | 2024-2025 (otelslog bridge added) | Standard library support, reduced dependencies |
| Separate log aggregation | OpenTelemetry unified observability | 2023-2025 (OTLP logs stable) | Single pipeline for logs, metrics, traces |

**Deprecated/outdated:**
- **opentracing-go**: Replaced by OpenTelemetry (OTel is merger of OpenTracing and OpenCensus)
- **opencensus-go**: Replaced by OpenTelemetry in 2021
- **Manual traceparent header parsing**: Use otelhttp automatic propagation
- **Storing trace ID as separate context value**: Use OpenTelemetry span context directly

## Open Questions

Things that couldn't be fully resolved:

1. **Should otelslog bridge or custom wrapper be used?**
   - What we know: Project requires preserving JSON output to stdout (TRACE-03). otelslog bridge sends logs to OTLP collector, not stdout. Custom wrapper preserves format while adding trace fields.
   - What's unclear: Does the project plan to eventually move to OTLP-based log collection?
   - Recommendation: Use custom wrapper for v1.2 to preserve JSON stdout. Otelslog bridge can be added later for dual output if OTLP collection is adopted.

2. **How to handle goroutines that outlive requests?**
   - What we know: `context.WithoutCancel()` (Go 1.21+) detaches cancellation while preserving values. Goroutines for cleanup, notifications, etc. should continue after request completes.
   - What's unclear: Should these long-lived goroutines keep trace context for correlation? Trade-off: correlation vs. trace span lifetime.
   - Recommendation: Keep trace context in detached context for correlation. Trace backend should handle completed traces referenced by active logs.

3. **What log entries legitimately lack trace context?**
   - What we know: Startup/shutdown, health checks, background jobs unrelated to requests won't have trace_id. This is expected, not a bug.
   - What's unclear: Clear criteria for "this should have trace_id" vs. "this shouldn't"
   - Recommendation: Document decision: request-scoped work MUST have trace_id. Infrastructure/lifecycle events (startup, shutdown, health) MAY lack trace_id. Use requirement TRACE-06 to identify propagation bugs.

## Sources

### Primary (HIGH confidence)
- [go.opentelemetry.io/contrib/bridges/otelslog - Go Packages](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog) - Official otelslog bridge API documentation
- [OpenTelemetry Slog Setup & Examples | Uptrace](https://uptrace.dev/guides/opentelemetry-slog) - Setup guide with code examples
- [Go Concurrency Patterns: Context - The Go Programming Language](https://go.dev/blog/context) - Official Go context propagation patterns
- [Structured Logging with slog - The Go Programming Language](https://go.dev/blog/slog) - Official slog handler patterns
- [otelhttp package - Go Packages](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp) - HTTP middleware automatic instrumentation

### Secondary (MEDIUM confidence)
- [How to Set Up Structured Logging in Go with OpenTelemetry](https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view) - Custom handler wrapper pattern (Jan 2026)
- [OpenTelemetry Context Propagation | Uptrace](https://uptrace.dev/get/opentelemetry-go/propagation) - W3C Trace Context propagation
- [Logging in Go with Slog: The Ultimate Guide | Better Stack](https://betterstack.com/community/guides/logging/logging-in-go/) - slog handler implementation patterns
- [How to Implement Request Context Propagation in Go Microservices](https://oneuptime.com/blog/post/2026-02-01-go-context-propagation-microservices/view) - Context propagation best practices (Feb 2026)

### Tertiary (LOW confidence)
- [GitHub Discussion #2712 - log traceId and spanId](https://github.com/open-telemetry/opentelemetry-go/discussions/2712) - Unanswered question, shows common confusion point
- [go-slog/otelslog](https://github.com/go-slog/otelslog) - Third-party wrapper alternative (not official)
- [remychantenay/slog-otel](https://github.com/remychantenay/slog-otel) - Third-party wrapper with extra features

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Official OpenTelemetry packages, well-documented, actively maintained
- Architecture: HIGH - Custom wrapper pattern verified in multiple sources, official slog documentation
- Pitfalls: MEDIUM - Derived from GitHub issues, discussions, and best practice guides; not all explicitly documented

**Research date:** 2026-02-02
**Valid until:** 2026-05-02 (90 days - relatively stable domain, OpenTelemetry is mature)
