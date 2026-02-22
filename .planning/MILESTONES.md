# Project Milestones: Seedbox Downloader

## v1.1 Torrent File Support (Shipped: 2026-02-01)

**Delivered:** Enable Sonarr/Radarr to download content from .torrent-only trackers through Put.io proxy

**Phases completed:** 4-6 (7 plans total)

**Key accomplishments:**

- Custom error types for structured error handling across transfer operations
- Extended Put.io client with `.torrent` file upload capability via `AddTransferByBytes`
- Transmission API handler with base64 decoding and bencode validation
- Structured logging and OpenTelemetry metrics for torrent type tracking (magnet vs .torrent)
- 33 tests added (25 unit + 8 integration) with 56.2% coverage of handler package
- Maintained backward compatibility with existing magnet link workflows

**Stats:**

- 32 files modified
- 4,558 lines of Go total (+6,542 insertions, -85 deletions)
- 3 phases, 7 plans, 17 requirements
- Same day implementation (2026-02-01, ~3 hours)
- 17/17 v1.1 requirements satisfied (100%)
- 0 critical gaps, 0 technical debt

**Git range:** `3e444e3` (feat(04-01)) → `25c8769` (chore: Phase 6 complete)

**What's next:** Production deployment and real-world validation with Sonarr/Radarr webhooks

---

## v1.2 Logging Improvements (Shipped: 2026-02-08)

**Delivered:** Make logs tell the story of what the application is doing during its lifecycle

**Phases completed:** 7-10 (10 plans total)

**Key accomplishments:**

- OpenTelemetry trace/span correlation in all structured logs via TraceHandler wrapper
- Complete migration to context-aware logging (InfoContext/DebugContext/etc) across all components
- Phased startup logging with component ready messages and service ready summary
- Graceful shutdown sequence logging with error context enhancement
- Silent-when-idle polling (no INFO logs during idle cycles)
- HTTP request logging middleware with request_id, status codes, and duration_ms

**Stats:**

- 4 phases, 10 plans, 25 requirements
- Same day implementation (2026-02-08)
- 25/25 v1.2 requirements satisfied (100%)

**Git range:** v1.2 Logging Improvements phases 7-10

**What's next:** Activity Tab Support (v1.3) to show in-progress downloads in Sonarr/Radarr

---

## v1 Critical Fixes (Shipped: 2026-01-31)

**Delivered:** Production-ready maintenance release ensuring 24/7 reliability without crashes, resource leaks, or silent failures

**Phases completed:** 1-3 (6 plans total)

**Key accomplishments:**

- Eliminated nil pointer crashes in HTTP error paths (GrabFile and Discord notifier)
- Implemented resource cleanup with defer pattern across all long-running goroutines
- Added panic recovery with context-aware restart for 24/7 stability
- Database connection validation with exponential backoff retry (3 attempts)
- Connection pool configuration via environment variables (25 open, 5 idle conns)
- Telemetry status logging for operational visibility

**Stats:**

- 25 files modified
- 3,177 lines of Go
- 3 phases, 6 plans, 11 tasks
- < 1 day from start to ship (2026-01-31)
- All 10 v1 requirements satisfied (100%)

**Git range:** `161fd67` (fix(01-01)) → `961e16b` (feat(03-02))

**What's next:** Continue production operation with improved stability and operational hygiene

---

## v1.3 Activity Tab Support (Shipped: 2026-02-08)

**Delivered:** Sonarr/Radarr Activity tab shows in-progress downloads with accurate progress, status, peer counts, speed, and labels

**Phases completed:** 11-12 (3 plans total)

**Key accomplishments:**

- Switched to SaveParentID-based tag matching, eliminating FileID dependency for label resolution
- Removed FileID==0 filter so in-progress transfers appear in torrent-get responses
- Added conditional file population with triple safety net (IsAvailable + IsDownloadable + conditional files)
- Complete Put.io status mapping (11 statuses to 7 Transmission codes) with warn-log for unknown statuses
- Populated peer counts, download speed, and labels in Transmission RPC response
- TDD approach with 18+ new test cases across Put.io client and Transmission handler

**Stats:**

- 13 files modified
- +1,968 insertions, -40 deletions
- 2 phases, 3 plans, 7 requirements
- Same day implementation (2026-02-08)
- 7/7 v1.3 requirements satisfied (100%)

**Git range:** `e4160cc` (feat(11-01)) → `7b561c7` (feat(12-02))

**What's next:** Production deployment to verify Activity tab integration with Sonarr/Radarr

---


## v1.4 Download Pipeline Fixes (Shipped: 2026-02-22)

**Delivered:** Fix download folder naming, automate Put.io transfer cleanup after import, and detect missing transfers with Discord notifications

**Phases completed:** 13-15 (3 plans total)

**Key accomplishments:**

- Fixed single-file folder naming — Put.io single-file transfers now create correctly named folders without file extension (e.g., `the_movie/` not `the_movie.mkv/`), enabling Sonarr/Radarr import
- Automated Put.io transfer cleanup after import — transfers and file data automatically deleted after Sonarr/Radarr confirms import, with configurable seed ratio window via `PUTIO_SEED_RATIO`
- Missing transfer detection — distinguishes "transfer removed" from "files missing" with distinct warn-level logs and Discord embed notifications (red sidebar, structured fields)
- Pipeline resilience — missing transfers marked in DB to prevent re-processing, partial local files cleaned up, nil guards on all notification handlers to prevent crash when Discord webhook not configured

**Stats:**

- 23 files changed, 2,185 insertions, 63 deletions
- 6,072 lines of Go total
- 3 phases, 3 plans, 6 tasks
- Single session implementation (2026-02-22)
- 6/6 v1.4 requirements satisfied (100%)
- 0 critical gaps, 1 minor tech debt item

**Git range:** `d57d40f` (docs(13)) → `4c6f37f` (docs: milestone v1.4 audit)

**What's next:** Production deployment to validate folder naming fix, cleanup behavior, and Discord notifications

---

