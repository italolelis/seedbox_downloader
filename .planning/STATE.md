# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-02-22)

**Core value:** The application must run reliably 24/7 without crashes, resource leaks, or silent failures.
**Current focus:** v1.4 Download Pipeline Fixes — folder naming, transfer cleanup, missing transfer handling

## Current Position

Phase: 15 - Missing Transfer Handling
Plan: 01 (complete)
Status: Phase 15 Plan 01 complete
Last activity: 2026-02-22 — Completed 15-01-PLAN.md (missing transfer handling)

Progress: [██████████] 100% (3/3 phases complete)

## Performance Metrics

| Metric | Value |
|--------|-------|
| Phases this milestone | 3 |
| Requirements mapped | 6/6 |
| Plans complete | 3/3 |
| Coverage | 100% |

## Accumulated Context

### Decisions

- Strip only the last extension segment using `filepath.Ext + strings.TrimSuffix` for single-file folder naming (matches Sonarr/Radarr folder-name parsing)
- Guard `stripFileExtension` against empty result: if stripping produces `""` (dot-prefixed files like `.hidden`), return original name unchanged to avoid empty folder path component
- Only the `!file.IsDir()` branch in `getFilesRecursively` is modified; multi-file directory traversal paths are untouched
- Use `TransferInfoer` capability interface on `InstrumentedDownloadClient` instead of direct `*putio.Client` type assertion (assertion always fails through instrumentation wrapper)
- `CleanupTransfer` is synchronous in the immediate path (`seedRatio==0`), called from goroutine in ratio path; uses `backoff.Retry[struct{}]` v5 API with `WithMaxTries(3)`
- `"transfer not found"` from Put.io `RemoveTransfers` treated as success (idempotent cleanup)
- Sentinel errors placed in putio package (not transfer package) — `downloader -> putio -> transfer` is a clean dependency chain, no circular import
- WatchDownloads is the single emission point to OnTransferMissing; DownloadTransfer only returns errors, never emits to channels
- Exactly one warn log per missing-transfer event: ErrTransferNotFound logs in WatchDownloads, ErrTransferFilesNotFound logs in DownloadTransfer at file level
- Nil guard added to all four notification handlers to prevent panic when DISCORD_WEBHOOK_URL is unset
- ClaimTransfer SQL WHERE clause only claims 'pending' or 'failed' — 'missing' status excluded, ensuring once-only notification per transfer

### Pending Todos

None.

### Blockers/Concerns

None.

## Session Continuity

Last session: 2026-02-22
Stopped at: Completed 15-01-PLAN.md (missing transfer handling — all 3 v1.4 phases complete)
Resume file: None
Next step: v1.4 milestone complete — consider milestone archive or new milestone planning
