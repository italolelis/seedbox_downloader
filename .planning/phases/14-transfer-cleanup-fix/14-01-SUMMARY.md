---
phase: 14-transfer-cleanup-fix
plan: "01"
subsystem: downloader
tags: [putio, backoff, cleanup, seeding, transfer, envconfig]

# Dependency graph
requires:
  - phase: 13-folder-naming-fix
    provides: putio.Client with correct file path handling used by cleanup pipeline
provides:
  - Put.io transfer cleanup after Sonarr/Radarr import confirmation
  - PUTIO_SEED_RATIO config field for optional seeding window before cleanup
  - CleanupTransfer method with exponential backoff and "not found" = success semantics
  - GetTransferInfo method computing upload ratio from live Put.io API
  - TransferInfoer interface for runtime capability detection via InstrumentedDownloadClient
affects: [putio-client, downloader, main-config]

# Tech tracking
tech-stack:
  added: [cenkalti/backoff/v5 (already in go.mod, now used in downloader)]
  patterns:
    - backoff.Retry[T any] v5 API for retrying cleanup with max tries
    - TransferInfoer capability interface for optional DownloadClient methods
    - Immediate cleanup path (seedRatio==0) vs polling path (seedRatio>0)

key-files:
  created: []
  modified:
    - cmd/seedbox_downloader/main.go
    - internal/dc/putio/client.go
    - internal/downloader/downloader.go
    - internal/transfer/instrumented_client.go

key-decisions:
  - "Use TransferInfoer interface on InstrumentedDownloadClient to proxy GetTransferInfo instead of type-asserting to *putio.Client (which would always fail since d.dc is wrapped)"
  - "CleanupTransfer is synchronous (not goroutine) when called from immediate path; WatchForSeeding goroutine calls CleanupTransfer internally for ratio path"
  - "GetTransferInfo uses Transfers.List + linear scan (not a direct get-by-ID) to match how RemoveTransfers works — consistent with existing Put.io client patterns"

patterns-established:
  - "Capability interface pattern: TransferInfoer interface checked at runtime via type assertion on d.dc, enabling optional features without breaking the DownloadClient interface"
  - "Backoff v5 pattern: backoff.Retry[struct{}](ctx, func, backoff.WithMaxTries(N)) for retrying side-effecting operations"
  - "Not-found = success pattern: strings.Contains(err.Error(), 'transfer not found') treated as idempotent success"

requirements-completed: [CLN-01, CLN-02]

# Metrics
duration: 3min
completed: 2026-02-22
---

# Phase 14 Plan 01: Transfer Cleanup Fix Summary

**Put.io transfer and file data deleted after Sonarr/Radarr import via immediate cleanup or configurable ratio-based seeding window with exponential backoff retry**

## Performance

- **Duration:** 3 min
- **Started:** 2026-02-22T01:02:33Z
- **Completed:** 2026-02-22T01:05:36Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- Fixed stale polling bug: `WatchForSeeding` no longer checks `t.IsSeeding()` (local snapshot that never changes) — instead polls live upload ratio from Put.io API
- Implemented two cleanup paths: immediate (`PUTIO_SEED_RATIO=0`) and ratio-based (`PUTIO_SEED_RATIO>0`), both using `CleanupTransfer` with 3-attempt exponential backoff
- Added `GetTransferInfo` to `putio.Client` computing upload ratio from `Uploaded/Downloaded` fields with zero-denominator guard
- Added `TransferInfoer` capability interface so `InstrumentedDownloadClient` can proxy `GetTransferInfo` without breaking the existing `DownloadClient` interface

## Task Commits

Each task was committed atomically:

1. **Task 1: Add PUTIO_SEED_RATIO config and route handleTransferImported** - `f2cfcbc` (feat)
2. **Task 2: Add GetTransferInfo, CleanupTransfer and fix WatchForSeeding** - `9b11829` (feat)

**Plan metadata:** (docs commit — see below)

## Files Created/Modified

- `cmd/seedbox_downloader/main.go` - Added `PutioSeedRatio` config field; updated `handleTransferImported` to route between `CleanupTransfer` and `WatchForSeeding`; threaded `seedRatio` through `setupNotificationForDownloader`
- `internal/dc/putio/client.go` - Added `GetTransferInfo` method computing upload ratio from live `Transfers.List` call
- `internal/downloader/downloader.go` - Added `CleanupTransfer` with backoff retry; rewrote `WatchForSeeding` to use live `TransferInfoer.GetTransferInfo` polling; removed stale `t.IsSeeding()` check
- `internal/transfer/instrumented_client.go` - Added `TransferInfoer` interface; implemented `GetTransferInfo` on `InstrumentedDownloadClient` to proxy to underlying client if supported

## Decisions Made

- **TransferInfoer interface over direct type assertion**: `d.dc` in the downloader is `*transfer.InstrumentedDownloadClient`, not `*putio.Client`. Direct type assertion (`d.dc.(*putio.Client)`) would always fail. Added a `TransferInfoer` interface that `InstrumentedDownloadClient` proxies — keeping the instrumentation layer intact while enabling the capability.
- **CleanupTransfer is synchronous**: When `seedRatio == 0`, `handleTransferImported` calls `CleanupTransfer` directly (blocking until cleanup completes or retries exhausted). `WatchForSeeding` runs in a goroutine and calls `CleanupTransfer` internally when ratio is reached.
- **"transfer not found" = success**: The Put.io `RemoveTransfers` returns an error when the transfer is gone. Detecting this via `strings.Contains` and treating it as success makes cleanup idempotent.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added TransferInfoer interface and InstrumentedDownloadClient.GetTransferInfo**
- **Found during:** Task 2 (WatchForSeeding implementation)
- **Issue:** Plan specified `d.dc.(*putio.Client)` type assertion, but `d.dc` is `*transfer.InstrumentedDownloadClient` (wrapping putio.Client). This assertion would always fail, making WatchForSeeding permanently broken.
- **Fix:** Added `TransferInfoer` interface to `transfer` package; implemented `GetTransferInfo` on `InstrumentedDownloadClient` that delegates to underlying client if it implements `TransferInfoer`; updated `WatchForSeeding` to check `d.dc.(transfer.TransferInfoer)` instead
- **Files modified:** `internal/transfer/instrumented_client.go`, `internal/downloader/downloader.go`
- **Verification:** `go build ./...` and `go test ./...` pass; `GetTransferInfo` correctly proxied through instrumentation layer
- **Committed in:** `9b11829` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 missing critical — instrumentation layer bypass)
**Impact on plan:** Essential for correctness — without this fix WatchForSeeding would log an error and exit immediately on every tick. No scope creep.

## Issues Encountered

None - plan executed cleanly once the instrumentation wrapper issue was resolved via the deviation above.

## User Setup Required

Optional: set `PUTIO_SEED_RATIO` environment variable to a float (e.g., `2.0` for 2x upload ratio) to enable seeding before cleanup. When unset or `0`, cleanup fires immediately after Sonarr/Radarr import confirmation.

## Next Phase Readiness

- CLN-01 and CLN-02 complete — transfers and file data are cleaned from Put.io after import
- Ready for phase 15 (missing transfer handling, if planned)
- No blockers

## Self-Check: PASSED

All files verified present on disk. All task commits verified in git log.

---
*Phase: 14-transfer-cleanup-fix*
*Completed: 2026-02-22*
