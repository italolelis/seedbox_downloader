---
phase: 10-http-request-logging
plan: 01
subsystem: telemetry
tags: [http, middleware, logging, request-id, chi]

dependency-graph:
  requires:
    - "07-trace-correlation: TraceHandler provides trace_id/span_id injection"
    - "logctx: Context-based logger retrieval"
  provides:
    - "RequestID middleware for request correlation"
    - "HTTPLogging middleware for structured HTTP request logging"
    - "GetRequestID helper for request_id extraction"
  affects:
    - "10-02: Integration plan will wire these middlewares into main.go"

tech-stack:
  added: []
  patterns:
    - "Response writer wrapper for status code capture"
    - "Context-based request ID propagation"
    - "Status-based log level selection"

key-files:
  created:
    - internal/telemetry/request_id.go
    - internal/telemetry/http_logging.go
  modified: []

decisions:
  - id: "HTTP-CTX-KEY"
    choice: "Use private ctxKey type for context keys"
    reason: "Prevents collisions with other packages using string keys"
  - id: "HTTP-DEFAULT-200"
    choice: "Default status to 200 in wrapper"
    reason: "Handles implicit 200 OK when handler writes without calling WriteHeader"
  - id: "HTTP-REQUEST-ID-EMPTY"
    choice: "Return empty string when request_id not found"
    reason: "Defensive programming - caller can check for empty rather than nil"

metrics:
  duration: 54s
  completed: 2026-02-08
---

# Phase 10 Plan 01: HTTP Request Logging Middleware Summary

**One-liner:** RequestID and HTTPLogging middleware for Chi router with UUID generation, status capture, and level-based logging

## What Was Built

### RequestID Middleware (`internal/telemetry/request_id.go`)

- **Context key type:** Private `ctxKey` type prevents context key collisions
- **RequestIDHeader constant:** `X-Request-ID` exported for use by other packages
- **RequestID middleware:** Chi-compatible `func(http.Handler) http.Handler`
  - Checks for existing X-Request-ID header (upstream propagation)
  - Generates UUID v4 when no header present
  - Sets X-Request-ID response header for client visibility
  - Stores request_id in context for downstream use
- **GetRequestID helper:** Extracts request_id from context, returns empty string if not found

### HTTPLogging Middleware (`internal/telemetry/http_logging.go`)

- **responseWriter wrapper:** Captures status code from handler
  - Embeds http.ResponseWriter for transparent delegation
  - Tracks `status` field (defaults to 200)
  - `wroteHeader` flag prevents double WriteHeader
  - Write method handles implicit 200 OK
- **wrapResponseWriter factory:** Creates wrapper with proper defaults
- **HTTPLogging middleware:** Chi-compatible `func(http.Handler) http.Handler`
  - Gets logger from context via `logctx.LoggerFromContext`
  - Records start time before handler execution
  - Wraps response writer to capture status
  - After handler: calculates duration_ms
  - Logs with fields: method, path, status, duration_ms, request_id
  - Log level by status: ERROR (5xx), WARN (4xx), INFO (<400)

## Requirements Satisfied

| Requirement | Implementation |
|-------------|----------------|
| HTTP-01 | Logs method, path, status as structured fields |
| HTTP-02 | RequestID middleware generates UUID v4, stored in context |
| HTTP-03 | `logger.ErrorContext` for status >= 500 |
| HTTP-04 | `logger.WarnContext` for status >= 400 |
| HTTP-05 | `logger.InfoContext` for status < 400 |
| HTTP-06 | `duration.Milliseconds()` as duration_ms field |

## Integration Pattern

```go
// Middleware order in main.go (plan 10-02):
r.Use(telemetry.RequestID)                           // 1. Generate request_id
r.Use(telemetry.NewHTTPMiddleware(cfg.ServiceName))  // 2. Create trace span
r.Use(telemetry.HTTPLogging)                         // 3. Log with all context
```

## Deviations from Plan

None - plan executed exactly as written.

## What's Next

Plan 10-02 will integrate these middlewares into `cmd/seedbox_downloader/main.go`:
- Add RequestID middleware before otelhttp
- Add HTTPLogging middleware after otelhttp
- Test full request flow with trace correlation

## Commits

| Hash | Type | Description |
|------|------|-------------|
| 542bc68 | feat | Add RequestID middleware for request correlation |
| 770dd42 | feat | Add HTTPLogging middleware with status-based log levels |
