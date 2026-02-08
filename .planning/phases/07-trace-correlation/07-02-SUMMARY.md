---
phase: 07-trace-correlation
plan: 02
subsystem: logging
tags: [slog, context, trace-correlation, observability]

# Dependency graph
requires:
  - phase: 07-01
    provides: TraceHandler infrastructure for context-aware logging
provides:
  - Context-aware logging in core pipeline components (downloader, transfer, main)
  - All logging calls propagate trace context for correlation
affects: [07-03, future-phases-using-logging]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "All logging uses *Context methods for trace propagation"
    - "Shutdown contexts use fresh context.Background() for logging after cancellation"

key-files:
  created: []
  modified:
    - internal/downloader/downloader.go
    - internal/transfer/transfer.go
    - cmd/seedbox_downloader/main.go

key-decisions:
  - "Use shutdownCtx for shutdown logging in main.go (original context is cancelled)"
  - "Add ctx parameter to ensureTargetDir helper for consistent context propagation"

patterns-established:
  - "Pattern: logger.*Context(ctx, ...) for all structured logging calls"
  - "Pattern: Pass context to helper functions that need logging"

# Metrics
duration: 4min
completed: 2026-02-08
---

# Phase 07 Plan 02: Core Pipeline Context Migration Summary

**All logging in downloader, transfer orchestrator, and main application now uses context-aware methods for trace correlation**

## Performance

- **Duration:** 4 min
- **Started:** 2026-02-08T16:19:11Z
- **Completed:** 2026-02-08T16:23:15Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments
- Migrated all logging calls in downloader.go to context-aware methods (13 Info, 5 Debug, 7 Error calls)
- Migrated all logging calls in transfer.go to context-aware methods (11 calls)
- Migrated all logging calls in main.go to context-aware methods (14 calls including startup, shutdown, and notification loops)
- All goroutines now propagate context to logging calls
- Trace IDs will now appear in logs throughout the core pipeline when tracing is active

## Task Commits

Each task was committed atomically:

1. **Task 1: Migrate downloader.go to context-aware logging** - `c98c4ee` (refactor)
2. **Task 2: Migrate transfer.go and main.go to context-aware logging** - `670dc1e` (refactor)

## Files Created/Modified
- `internal/downloader/downloader.go` - All logging calls now use InfoContext/DebugContext/ErrorContext
- `internal/transfer/transfer.go` - All logging calls now use context-aware methods
- `cmd/seedbox_downloader/main.go` - All logging calls now use context-aware methods, including shutdown logic

## Decisions Made

**1. Shutdown context handling in main.go**
- **Decision:** Use shutdownCtx (created via context.Background()) for shutdown logging instead of original ctx
- **Rationale:** When ctx.Done() triggers, the original context is cancelled. Logging with a cancelled context could lose trace correlation. Fresh shutdownCtx ensures clean shutdown logging.

**2. Context propagation to helpers**
- **Decision:** Add ctx parameter to ensureTargetDir helper function
- **Rationale:** Maintains consistent context propagation pattern - all functions that log should receive context

## Deviations from Plan

None - plan executed exactly as written. All logging calls in the three core files were migrated to context-aware methods as specified.

## Issues Encountered

None - migration was straightforward. All functions already had access to context, requiring only method name changes (Info → InfoContext, etc.) and adding ctx as first parameter.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Core pipeline components now ready for trace correlation
- Remaining components (deluge, putio, arr, http) can be migrated in 07-03
- Trace correlation infrastructure complete once all components migrated
- Ready for end-to-end trace testing with trace-enabled requests

---
*Phase: 07-trace-correlation*
*Completed: 2026-02-08*
