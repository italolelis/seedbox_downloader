# Phase 10: HTTP Request Logging - Research

**Researched:** 2026-02-08
**Domain:** HTTP request/response logging middleware for Go web applications
**Confidence:** HIGH

## Summary

HTTP request logging provides visibility into API usage by automatically logging method, path, status code, duration, and request identifiers for every HTTP request. This enables debugging, performance monitoring, and request correlation across distributed systems.

The standard approach in Go uses middleware that wraps the HTTP handler chain, capturing request details before handler execution and response details after. The project already uses `otelhttp.NewMiddleware` for OpenTelemetry instrumentation (creating spans with trace context), but this does not produce structured log output. A complementary logging middleware is needed that logs request/response details at appropriate levels (INFO for 2xx, WARN for 4xx, ERROR for 5xx) with automatic request_id generation.

Two well-established options exist: (1) `go-chi/httplog` - official Chi package built on slog with zero dependencies, or (2) `samber/slog-chi` - community middleware with extensive configuration including trace/span ID extraction. Given the project's existing trace correlation via `TraceHandler` (Phase 7), a simpler custom middleware following established patterns can integrate cleanly with the existing logging infrastructure while avoiding additional dependencies.

**Primary recommendation:** Create a custom HTTP logging middleware in `internal/telemetry` that wraps response writers to capture status codes, logs at appropriate levels based on status code ranges, generates request_id using UUID, and records duration_ms. This integrates with existing `logctx.LoggerFromContext` pattern and `TraceHandler` for automatic trace correlation.

## Standard Stack

The established libraries/tools for HTTP request logging in Go:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| log/slog | stdlib (Go 1.21+) | Structured logging | Already in use, context-aware methods enable trace correlation |
| github.com/google/uuid | v1.6.0 | UUID v4 generation | Already in go.mod, industry standard for request IDs |
| time | stdlib | Duration measurement | Standard `time.Since(start)` pattern |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp | v0.63.0 | HTTP span creation | Already in use for tracing |
| go.opentelemetry.io/otel/trace | v1.38.0 | Span context access | Already in use via TraceHandler |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom middleware | go-chi/httplog | httplog adds 3rd party dependency; custom integrates better with existing TraceHandler pattern |
| Custom middleware | samber/slog-chi | slog-chi has rich features but duplicates trace extraction already done by TraceHandler |
| UUID v4 request_id | chi middleware.RequestID | Chi's RequestID uses custom format (host/random-counter), not standard UUID |
| Per-request logging | otelhttp only | otelhttp creates spans but does not produce structured log output |

**Installation:**
No additional dependencies required - uses existing google/uuid already in go.mod.

## Architecture Patterns

### Recommended Approach: Response Writer Wrapper + Middleware

```
Request → RequestID Middleware (generates/injects request_id)
         ↓
         otelhttp Middleware (creates span, adds trace context)
         ↓
         Logging Middleware (wraps response writer, logs after handler)
         ↓
         Handler (business logic)
         ↓
         Response written to wrapped writer
         ↓
         Logging Middleware logs: method, path, status, duration_ms, request_id
```

### Pattern 1: Response Writer Wrapper

**What:** Capture status code and bytes written by wrapping http.ResponseWriter
**When to use:** Any middleware that needs to inspect response details after handler execution

**Example:**
```go
// Source: https://blog.questionable.services/article/guide-logging-middleware-go/

type responseWriter struct {
    http.ResponseWriter
    status      int
    wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
    return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
    if rw.wroteHeader {
        return
    }
    rw.status = code
    rw.ResponseWriter.WriteHeader(code)
    rw.wroteHeader = true
}

// Write captures implicit 200 OK when WriteHeader not called
func (rw *responseWriter) Write(b []byte) (int, error) {
    if !rw.wroteHeader {
        rw.WriteHeader(http.StatusOK)
    }
    return rw.ResponseWriter.Write(b)
}
```

### Pattern 2: Request ID Middleware

**What:** Generate unique request_id for correlation, store in context and response header
**When to use:** Before logging middleware to ensure request_id is available

**Example:**
```go
// Source: https://dev.to/kittipat1413/understanding-request-id-why-its-essential-for-modern-apis-1916

import "github.com/google/uuid"

type ctxKey string
const RequestIDKey ctxKey = "request_id"
const RequestIDHeader = "X-Request-ID"

func RequestID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Check for existing request_id (propagation from upstream)
        requestID := r.Header.Get(RequestIDHeader)
        if requestID == "" {
            requestID = uuid.New().String()
        }

        // Add to response header
        w.Header().Set(RequestIDHeader, requestID)

        // Add to context
        ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func GetRequestID(ctx context.Context) string {
    if id, ok := ctx.Value(RequestIDKey).(string); ok {
        return id
    }
    return ""
}
```

### Pattern 3: HTTP Logging Middleware with Level Selection

**What:** Log request completion with appropriate level based on status code
**When to use:** After request_id middleware, after otelhttp middleware

**Example:**
```go
// Source: Composite from https://github.com/go-chi/httplog and project patterns

func HTTPLogging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        logger := logctx.LoggerFromContext(ctx)
        start := time.Now()

        wrapped := wrapResponseWriter(w)
        next.ServeHTTP(wrapped, r)

        duration := time.Since(start)
        status := wrapped.status
        requestID := GetRequestID(ctx)

        // Select log level based on status code
        attrs := []any{
            "method", r.Method,
            "path", r.URL.Path,
            "status", status,
            "duration_ms", duration.Milliseconds(),
            "request_id", requestID,
        }

        switch {
        case status >= 500:
            logger.ErrorContext(ctx, "http request completed", attrs...)
        case status >= 400:
            logger.WarnContext(ctx, "http request completed", attrs...)
        default:
            logger.InfoContext(ctx, "http request completed", attrs...)
        }
    })
}
```

### Pattern 4: Middleware Ordering

**What:** Correct ordering of middleware ensures request_id and trace context available
**When to use:** When setting up HTTP router

**Example:**
```go
// Source: https://pkg.go.dev/github.com/go-chi/chi/middleware

func setupServer(cfg *config, tel *telemetry.Telemetry) *http.Server {
    r := chi.NewRouter()

    // Order matters:
    // 1. RequestID - generates request_id, adds to context and header
    r.Use(telemetry.RequestID)

    // 2. otelhttp - creates span, trace context available in r.Context()
    r.Use(telemetry.NewHTTPMiddleware(cfg.Telemetry.ServiceName))

    // 3. HTTPLogging - logs with request_id, trace_id/span_id from TraceHandler
    r.Use(telemetry.HTTPLogging)

    // ... routes ...
}
```

### Anti-Patterns to Avoid

- **Logging before handler completes:** Cannot capture status code or duration; must wrap response writer and log after
- **Not wrapping Write method:** Handler may never call WriteHeader (implicit 200 OK), missing status capture
- **Using chi middleware.RequestID:** Uses non-standard format (host/random-counter), not UUID
- **Duplicating trace extraction:** TraceHandler already injects trace_id/span_id; don't extract again in HTTP logging middleware
- **Logging at single level:** All requests at INFO loses ability to filter by severity; use status-based levels
- **Blocking on log writes:** For high-traffic endpoints, consider async logging (out of scope for this project)

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| UUID generation | Manual random string | github.com/google/uuid | RFC 4122 compliance, collision resistance, already in go.mod |
| Response status capture | Custom http.ResponseWriter | Wrapper pattern with embedded ResponseWriter | Standard pattern, handles all ResponseWriter methods via embedding |
| Duration measurement | Manual timestamp math | time.Since(start) | Clear intent, handles clock edge cases |
| Request ID propagation | Custom header parsing | Context value + X-Request-ID header | Standard pattern, works with distributed tracing |

**Key insight:** HTTP logging middleware is a solved problem with well-documented patterns. The implementation is straightforward but requires careful attention to response writer wrapping and middleware ordering.

## Common Pitfalls

### Pitfall 1: Missing Status Code for Implicit 200

**What goes wrong:** Handler never calls WriteHeader(), middleware logs status 0 or wrong value.
**Why it happens:** When handler writes body directly without calling WriteHeader(), Go implicitly sends 200 OK.
**How to avoid:** Override both WriteHeader AND Write methods in wrapper; Write sets status 200 if WriteHeader not yet called.
**Warning signs:** Logs showing status=0 or status missing for successful requests.

### Pitfall 2: Middleware Order with otelhttp

**What goes wrong:** trace_id/span_id missing from HTTP logs because otelhttp runs after logging middleware.
**Why it happens:** Middleware runs in order added; otelhttp creates span which TraceHandler extracts.
**How to avoid:** Order: RequestID -> otelhttp -> HTTPLogging. HTTPLogging runs last to have all context.
**Warning signs:** HTTP logs missing trace_id while other logs have it; cannot correlate HTTP requests to traces.

### Pitfall 3: Request ID Not Propagated to Response Header

**What goes wrong:** Client cannot see request_id for debugging; downstream services don't receive it.
**Why it happens:** Storing request_id in context only; forgetting to set response header.
**How to avoid:** Set X-Request-ID response header in RequestID middleware before calling next handler.
**Warning signs:** Response headers don't include X-Request-ID; clients can't reference request_id in bug reports.

### Pitfall 4: Logging Sensitive Data

**What goes wrong:** Authorization headers, tokens, or request bodies containing PII logged.
**Why it happens:** Logging all headers or bodies without filtering.
**How to avoid:** Log only method, path, status, duration, request_id. Don't log headers or bodies by default. If needed, whitelist safe headers.
**Warning signs:** Tokens visible in logs; GDPR/privacy concerns.

### Pitfall 5: Duplicate Logging from otelhttp

**What goes wrong:** Each request logged twice - once by otelhttp span events, once by custom middleware.
**Why it happens:** otelhttp may emit span events that appear in some backends as logs.
**How to avoid:** otelhttp creates spans (traces), not logs. TraceHandler injects trace context into structured logs. These are complementary, not duplicates. Verify in observability backend that traces and logs are separate.
**Warning signs:** Apparent duplicate entries; one with trace format, one with log format.

## Code Examples

Verified patterns from official sources:

### Complete Response Writer Wrapper

```go
// Source: https://blog.questionable.services/article/guide-logging-middleware-go/
// Location: internal/telemetry/http_logging.go

package telemetry

import (
    "net/http"
)

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
    http.ResponseWriter
    status      int
    wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
    // Default to 200 OK (implicit if handler doesn't call WriteHeader)
    return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
    if rw.wroteHeader {
        return // Prevent multiple WriteHeader calls
    }
    rw.status = code
    rw.wroteHeader = true
    rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
    if !rw.wroteHeader {
        rw.WriteHeader(http.StatusOK) // Implicit 200 OK
    }
    return rw.ResponseWriter.Write(b)
}
```

### Request ID Middleware

```go
// Source: https://dev.to/kittipat1413/understanding-request-id-why-its-essential-for-modern-apis-1916
// Location: internal/telemetry/request_id.go

package telemetry

import (
    "context"
    "net/http"

    "github.com/google/uuid"
)

type ctxKey string

const (
    requestIDKey    ctxKey = "request_id"
    RequestIDHeader        = "X-Request-ID"
)

// RequestID middleware generates unique request_id for each request
func RequestID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Check for existing request_id (upstream propagation)
        requestID := r.Header.Get(RequestIDHeader)
        if requestID == "" {
            requestID = uuid.New().String()
        }

        // Set response header for client visibility
        w.Header().Set(RequestIDHeader, requestID)

        // Add to context for logging
        ctx := context.WithValue(r.Context(), requestIDKey, requestID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// GetRequestID retrieves request_id from context
func GetRequestID(ctx context.Context) string {
    if id, ok := ctx.Value(requestIDKey).(string); ok {
        return id
    }
    return ""
}
```

### HTTP Logging Middleware with Level Selection

```go
// Source: Composite from https://github.com/go-chi/httplog and project patterns
// Location: internal/telemetry/http_logging.go

package telemetry

import (
    "net/http"
    "time"

    "github.com/italolelis/seedbox_downloader/internal/logctx"
)

// HTTPLogging middleware logs HTTP requests with appropriate level based on status code
// Requirements: HTTP-01, HTTP-02, HTTP-03, HTTP-04, HTTP-05, HTTP-06
func HTTPLogging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        logger := logctx.LoggerFromContext(ctx)
        start := time.Now()

        // Wrap response writer to capture status
        wrapped := wrapResponseWriter(w)

        // Execute handler
        next.ServeHTTP(wrapped, r)

        // Calculate duration
        duration := time.Since(start)
        status := wrapped.status
        requestID := GetRequestID(ctx)

        // Build log attributes (HTTP-01, HTTP-02, HTTP-06)
        attrs := []any{
            "method", r.Method,
            "path", r.URL.Path,
            "status", status,
            "duration_ms", duration.Milliseconds(),
            "request_id", requestID,
        }

        // Select log level based on status code (HTTP-03, HTTP-04, HTTP-05)
        switch {
        case status >= 500:
            // HTTP-03: 5xx at ERROR level
            logger.ErrorContext(ctx, "http request completed", attrs...)
        case status >= 400:
            // HTTP-04: 4xx at WARN level
            logger.WarnContext(ctx, "http request completed", attrs...)
        default:
            // HTTP-05: 2xx at INFO level
            logger.InfoContext(ctx, "http request completed", attrs...)
        }
    })
}
```

### Main.go Integration

```go
// Source: Project existing patterns
// Location: cmd/seedbox_downloader/main.go (setupServer function)

func setupServer(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*http.Server, error) {
    r := chi.NewRouter()

    // Middleware order is critical:
    // 1. RequestID - generates request_id, stores in context
    r.Use(telemetry.RequestID)

    // 2. otelhttp - creates span, adds trace context to r.Context()
    r.Use(telemetry.NewHTTPMiddleware(cfg.Telemetry.ServiceName))

    // 3. HTTPLogging - logs after handler completes with request_id, trace_id, span_id
    r.Use(telemetry.HTTPLogging)

    // ... rest of setup unchanged ...
}
```

### Expected Log Output

```json
{
  "time": "2026-02-08T10:30:45.123Z",
  "level": "INFO",
  "msg": "http request completed",
  "method": "POST",
  "path": "/transmission/rpc",
  "status": 200,
  "duration_ms": 45,
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "trace_id": "abc123def456...",
  "span_id": "1234abcd..."
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Print-based logging | Structured slog logging | 2023 (Go 1.21) | Machine-parseable, consistent format |
| Single INFO level | Status-based level selection | 2024+ | Filter errors/warnings, reduce noise |
| Custom request IDs | UUID v4 standard | 2020+ | Universal format, collision resistance |
| Separate middleware packages | Built-in patterns | 2024+ | Zero dependencies, full control |
| Manual trace correlation | Context-propagated trace_id | 2023+ (OTel matured) | Automatic correlation |

**Deprecated/outdated:**
- **go.uber.org/zap for HTTP logging:** slog is now standard, no performance need for zap in logging middleware
- **logrus:** Replaced by slog as Go standard library solution
- **Custom request ID formats:** UUID v4 is industry standard
- **Logging before handler completes:** Cannot capture status/duration

## Open Questions

Things that couldn't be fully resolved:

1. **Should request body be logged for failed requests?**
   - What we know: Body logging useful for debugging 4xx/5xx, but privacy/size concerns
   - What's unclear: Are there sensitive data in Transmission RPC requests?
   - Recommendation: Don't log bodies in v1.2. Add as future feature with explicit opt-in and size limits.

2. **Should 3xx redirects log at INFO or DEBUG?**
   - What we know: This API doesn't use redirects. Chi returns 3xx for redirect middleware.
   - What's unclear: If redirects occur, are they normal flow (INFO) or unusual (WARN)?
   - Recommendation: Log at INFO for now (status < 400). Adjust if redirects become common.

3. **Should authentication failures (401) log differently?**
   - What we know: 401 is 4xx so logs at WARN. basicAuthMiddleware handles auth.
   - What's unclear: Should auth failures be ERROR (security concern) or WARN (client error)?
   - Recommendation: Keep at WARN (client error). Repeated failures could be detected by log analysis, not log level.

## Sources

### Primary (HIGH confidence)
- [go-chi/httplog - GitHub](https://github.com/go-chi/httplog) - Official Chi HTTP logging package built on slog
- [chi middleware package - Go Packages](https://pkg.go.dev/github.com/go-chi/chi/middleware) - RequestID and Logger middleware reference
- [A Guide To Writing Logging Middleware in Go](https://blog.questionable.services/article/guide-logging-middleware-go/) - Response writer wrapper pattern
- [google/uuid - Go Packages](https://pkg.go.dev/github.com/google/uuid) - UUID v4 generation

### Secondary (MEDIUM confidence)
- [samber/slog-chi - GitHub](https://github.com/samber/slog-chi) - slog-chi middleware features and configuration
- [Understanding Request ID: Why It's Essential for Modern APIs](https://dev.to/kittipat1413/understanding-request-id-why-its-essential-for-modern-apis-1916) - Request ID best practices
- [How to Create Custom Logger with Context in Go](https://oneuptime.com/blog/post/2026-01-30-how-to-create-custom-logger-with-context-in-go/view) - Context-based logging patterns

### Tertiary (LOW confidence)
- WebSearch results for Go HTTP middleware patterns 2026
- Codebase analysis of existing TraceHandler and telemetry middleware

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Uses existing google/uuid and stdlib, well-documented patterns
- Architecture: HIGH - Response writer wrapper is standard Go pattern, documented in official sources
- Pitfalls: MEDIUM - Derived from multiple sources and common mistakes; not all explicitly documented
- Code examples: HIGH - Based on verified patterns from official sources adapted to project structure

**Research date:** 2026-02-08
**Valid until:** 2026-08-08 (180 days - stable domain, middleware patterns rarely change)

## Requirements Mapping

| Requirement | Implementation |
|-------------|----------------|
| HTTP-01: Log method, path, status | HTTPLogging middleware logs all three as structured fields |
| HTTP-02: Auto-generated request_id | RequestID middleware generates UUID v4, adds to context |
| HTTP-03: 5xx at ERROR level | HTTPLogging uses `logger.ErrorContext` for status >= 500 |
| HTTP-04: 4xx at WARN level | HTTPLogging uses `logger.WarnContext` for status >= 400 |
| HTTP-05: 2xx at INFO level | HTTPLogging uses `logger.InfoContext` for status < 400 |
| HTTP-06: duration_ms field | HTTPLogging calculates `time.Since(start).Milliseconds()` |
