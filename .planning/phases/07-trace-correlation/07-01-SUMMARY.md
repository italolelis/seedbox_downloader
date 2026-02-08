---
phase: 07-trace-correlation
plan: 01
subsystem: observability
tags: [opentelemetry, slog, trace-correlation, logging]

# Dependency graph
requires:
  - phase: 06-observability
    provides: OpenTelemetry telemetry initialization
provides:
  - TraceHandler slog.Handler wrapper for automatic trace context injection
  - Trace correlation infrastructure (trace_id/span_id in all logs)
affects: [08-context-propagation, logging, observability]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "slog.Handler wrapper pattern for cross-cutting concerns"
    - "Trace context extraction using trace.SpanFromContext"

key-files:
  created:
    - internal/logctx/trace_handler.go
    - internal/logctx/trace_handler_test.go
  modified:
    - cmd/seedbox_downloader/main.go

key-decisions:
  - "Only inject trace_id/span_id when span context is valid (fields absent when no span, not empty strings)"
  - "Use trace.SpanFromContext for span extraction (not otelslog bridge which sends to OTLP collectors)"
  - "Handler chain: TraceHandler -> JSONHandler -> stdout preserves existing JSON output format"

patterns-established:
  - "Handler wrapper pattern: wrap existing handler and inject additional fields in Handle()"
  - "Test with mock TracerProvider to ensure valid span contexts in tests"
  - "Panic on nil handler in constructor to fail fast on misconfiguration"

# Metrics
duration: 2min
completed: 2026-02-08
---

# Phase 07 Plan 01: Trace Correlation Summary

**slog.Handler wrapper automatically injects trace_id and span_id from OpenTelemetry spans into all log entries**

## Performance

- **Duration:** 2 min
- **Started:** 2026-02-08T16:15:27Z
- **Completed:** 2026-02-08T16:17:12Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments
- TraceHandler slog.Handler wrapper created with full interface implementation
- Comprehensive test coverage including span/no-span scenarios and mock TracerProvider
- Integrated into logger initialization with handler chain: TraceHandler -> JSONHandler -> stdout
- JSON output format preserved with optional trace_id/span_id fields

## Task Commits

Each task was committed atomically:

1. **Task 1: Create TraceHandler wrapper** - `4a9ccdb` (feat)
2. **Task 2: Integrate TraceHandler into logger initialization** - `93628e4` (feat)

## Files Created/Modified
- `internal/logctx/trace_handler.go` - slog.Handler wrapper that injects trace_id/span_id from OpenTelemetry span context
- `internal/logctx/trace_handler_test.go` - Comprehensive tests with mock TracerProvider for valid span scenarios
- `cmd/seedbox_downloader/main.go` - Logger initialization updated to wrap JSONHandler with TraceHandler

## Decisions Made

**1. Omit trace fields when span invalid, not empty strings**
- Rationale: Cleaner log output, easier to detect when tracing is active
- Implementation: Only call r.AddAttrs() when spanCtx.IsValid() is true

**2. Use trace.SpanFromContext, not otelslog bridge**
- Rationale: otelslog sends logs to OTLP collectors which breaks TRACE-03 requirement for JSON stdout
- Implementation: Direct extraction from span context in Handle() method

**3. Panic on nil handler in constructor**
- Rationale: Fail fast on misconfiguration rather than runtime errors
- Implementation: Guard check at start of NewTraceHandler()

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

**NoopTracerProvider doesn't create valid spans in tests**
- Problem: trace.NewNoopTracerProvider() creates spans where SpanContext().IsValid() returns false
- Solution: Created mock TracerProvider with hardcoded valid trace IDs for testing
- Impact: TestTraceHandler_WithValidSpan provides proper coverage of trace injection

## Next Phase Readiness

**Ready for Phase 07 Plan 02 (HTTP context propagation)**

Foundations complete:
- TraceHandler wrapper operational
- All logs will include trace context when spans are active
- Tests verify both with-span and without-span scenarios

Next steps:
- Add HTTP middleware to propagate trace context in requests
- Ensure context flows through transfer operations
- Verify trace correlation works end-to-end

No blockers.

---
*Phase: 07-trace-correlation*
*Completed: 2026-02-08*
