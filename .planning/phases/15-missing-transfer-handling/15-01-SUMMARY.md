---
phase: 15-missing-transfer-handling
plan: "01"
subsystem: downloader
tags: [putio, discord, notifications, error-handling, sentinel-errors]

# Dependency graph
requires:
  - phase: 14-transfer-cleanup-fix
    provides: CleanupTransfer with backoff retry, transfer not-found idempotent handling
provides:
  - Sentinel errors ErrTransferFilesNotFound and ErrTransferNotFound in putio package
  - Discord embed notification support via NotifyEmbed method on DiscordNotifier
  - MissingTransferEvent with MissingType classification in downloader package
  - handleTransferMissing notification handler in main.go
  - DB 'missing' status that prevents re-claiming of lost transfers
affects: [16-*, any-phase-touching-notifier, any-phase-touching-downloader]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - Sentinel error wrapping for Put.io API failures (ErrTransferFilesNotFound, ErrTransferNotFound)
    - Single emission point pattern: DownloadTransfer returns errors, WatchDownloads emits to channels
    - Discord embed notification for structured error events (distinct from plain Notify text messages)
    - DB status 'missing' as terminal state to prevent re-processing

key-files:
  created: []
  modified:
    - internal/dc/putio/client.go
    - internal/notifier/discord.go
    - internal/downloader/downloader.go
    - cmd/seedbox_downloader/main.go

key-decisions:
  - "Sentinel errors placed in putio package (not transfer package) — no circular dependency since downloader->putio->transfer is a clean chain"
  - "WatchDownloads is the single emission point to OnTransferMissing; DownloadTransfer only returns errors, never emits"
  - "Exactly one warn log per missing-transfer event: ErrTransferNotFound logs in WatchDownloads, ErrTransferFilesNotFound logs in DownloadTransfer at file level"
  - "Discord embed uses color 15158332 (0xE74C3C red) with distinct title/description for files_missing vs transfer_removed"
  - "Nil guard added to all four notification handlers (3 existing + 1 new) to prevent panic when DISCORD_WEBHOOK_URL is unset"
  - "ClaimTransfer SQL WHERE clause only claims 'pending' or 'failed' — 'missing' is excluded, ensuring once-only notification per transfer"

patterns-established:
  - "Sentinel error pattern: define errors.New vars in package, wrap with fmt.Errorf %w, check with errors.Is in callers"
  - "Channel event struct pattern: carry both data and classification (MissingTransferEvent.MissingType) to avoid hardcoding at handler site"
  - "Nil notifier guard: always check notif != nil before calling notification methods"

requirements-completed: [MTX-01, MTX-02]

# Metrics
duration: 8min
completed: 2026-02-22
---

# Phase 15 Plan 01: Missing Transfer Handling Summary

**Sentinel error types for Put.io missing files/transfers with Discord embed notifications, DB 'missing' status, and partial file cleanup — pipeline detects and handles both deletion cases without re-processing**

## Performance

- **Duration:** 8 min
- **Started:** 2026-02-22T01:34:07Z
- **Completed:** 2026-02-22T01:42:10Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- Added `ErrTransferFilesNotFound` and `ErrTransferNotFound` sentinel errors to putio package with concrete detection paths in `GrabFile` and `DownloadTransfer`
- Added `NotifyEmbed` method and `Embed`/`EmbedField` types to Discord notifier interface and implementation for structured embed notifications
- Added `MissingTransferEvent` struct and `OnTransferMissing` channel to Downloader with single emission point in `WatchDownloads`
- Wired `handleTransferMissing` into notification loop with DB status update to `'missing'`, distinct Discord embeds per case, and nil guards on all handlers

## Task Commits

Each task was committed atomically:

1. **Task 1: Add sentinel errors, Discord embed support, and missing-transfer detection in downloader** - `0d09677` (feat)
2. **Task 2: Wire handleTransferMissing notification handler and DB status update** - `ebc1ecf` (feat)

**Plan metadata:** (docs commit below)

## Files Created/Modified
- `internal/dc/putio/client.go` - Added ErrTransferFilesNotFound and ErrTransferNotFound sentinel errors; GrabFile wraps URL failure with ErrTransferFilesNotFound
- `internal/notifier/discord.go` - Added Embed, EmbedField types, NotifyEmbed method to interface and DiscordNotifier
- `internal/downloader/downloader.go` - Added MissingTransferEvent struct, OnTransferMissing channel, detection in DownloadTransfer, routing in WatchDownloads
- `cmd/seedbox_downloader/main.go` - Added handleTransferMissing handler, wired into select loop, nil guards on all four notification handlers

## Decisions Made
- Sentinel errors placed in putio package (not transfer package) — `downloader -> putio -> transfer` is a clean dependency chain, no circular import
- WatchDownloads is the single emission point to OnTransferMissing; DownloadTransfer only returns errors to prevent double-emission on unbuffered channel
- Exactly one warn log per event: `ErrTransferNotFound` logs in WatchDownloads, `ErrTransferFilesNotFound` logs in DownloadTransfer at file level
- Discord embed color 15158332 (0xE74C3C red) with distinct title and description for `files_missing` vs `transfer_removed` cases

## Deviations from Plan

None — plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required. Existing `DISCORD_WEBHOOK_URL` environment variable is used if set.

## Next Phase Readiness
- Missing transfer detection and notification complete
- Pipeline handles both "transfer removed" and "files missing" cases non-repeatingly
- Phase 15 milestone ready to close if 15-01 is the only plan

---
*Phase: 15-missing-transfer-handling*
*Completed: 2026-02-22*
