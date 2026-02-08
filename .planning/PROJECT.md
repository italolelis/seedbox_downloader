# Seedbox Downloader

## What This Is

A Go-based automated downloader that orchestrates transfers from seedbox/torrent clients (Deluge, Put.io) to local storage, automatically importing media into Sonarr/Radarr. Runs reliably 24/7 with proper error handling, resource cleanup, and operational observability. Sonarr/Radarr Activity tab integration shows in-progress downloads with accurate status, progress, and peer info.

## Core Value

The application must run reliably 24/7 without crashes, resource leaks, or silent failures.

## Previous Milestone: v1.3 Activity Tab Support (Shipped: 2026-02-08)

**Goal:** Show in-progress downloads in Sonarr/Radarr Activity tab via the Transmission RPC proxy

**Delivered:**
- ✓ SaveParentID-based tag matching for all transfers (in-progress and completed)
- ✓ In-progress transfers visible in torrent-get responses alongside completed transfers
- ✓ Complete Put.io status mapping (11 statuses to 7 Transmission codes)
- ✓ Peer counts, download speed, and labels populated in Transmission RPC response
- ✓ Triple safety net prevents download pipeline from processing in-progress transfers

## Requirements

### Validated

- ✓ Downloads transfers from Deluge or Put.io based on labels — existing
- ✓ Parallel file downloads with semaphore-based concurrency control — existing
- ✓ Atomic transfer claiming prevents duplicate processing across instances — existing
- ✓ Monitors *arr services (Sonarr/Radarr) for import completion — existing
- ✓ Removes transfers from seedbox after import confirmation — existing
- ✓ SQLite state persistence with instance locking — existing
- ✓ OpenTelemetry instrumentation throughout — existing
- ✓ Discord webhook notifications for transfer state changes — existing
- ✓ Transmission-compatible REST API for webhook triggers — existing
- ✓ Graceful shutdown with context cancellation — existing
- ✓ Nil pointer safety in HTTP error paths — v1
- ✓ Discord webhook status code validation — v1
- ✓ Ticker cleanup on all goroutine exit paths — v1
- ✓ Panic recovery with context-aware restart — v1
- ✓ Database connection validation with retry — v1
- ✓ Connection pool configuration via environment variables — v1
- ✓ Telemetry status logging at startup — v1
- ✓ Clean codebase without commented-out dead code — v1
- ✓ Handle base64-encoded .torrent file content in Transmission API MetaInfo field — v1.1
- ✓ Upload .torrent content to Put.io without file persistence — v1.1
- ✓ Log explicit errors when .torrent files cannot be processed — v1.1
- ✓ Add test coverage for .torrent file handling — v1.1 (33 tests)
- ✓ Add observability metrics for torrent type (magnet vs file) — v1.1
- ✓ Return in-progress Put.io transfers from torrent-get endpoint — v1.3
- ✓ Show download progress, ETA, and peer info for active transfers — v1.3
- ✓ Filter in-progress transfers by label without requiring FileID — v1.3 (SaveParentID)
- ✓ Correct Transmission status mapping for all Put.io transfer states — v1.3

### Active

(None — define next milestone with `/gsd:new-milestone`)

### Out of Scope

- File persistence — .torrent files will not be saved to disk (v1.1 explicit constraint)
- Watch folders for .torrent files — Defer to future milestone
- Direct .torrent file upload API — Defer to future milestone
- Deluge .torrent support — Webhook API remains Put.io only
- Performance optimizations — Defer to future milestone (sequential ARR checks, polling latency, resume support)
- Security hardening — Defer to future milestone (TLS warnings, credential redaction, webhook protection)
- Test coverage expansion — Defer to future milestone (state machine tests, integration tests, concurrency tests)
- Scaling improvements — Defer to future milestone (PostgreSQL migration, dynamic parallelism, rate limiting)

## Context

**Shipped v1.3 (2026-02-08):**
- 4 milestones shipped (v1, v1.1, v1.2, v1.3) across 12 phases, 26 plans
- Full Activity tab integration — Sonarr/Radarr can monitor in-progress downloads
- Complete structured logging with trace correlation, lifecycle visibility, and HTTP request logging
- Production-ready with comprehensive observability and reliability

**Architecture:**
- Event-driven pipeline: TransferOrchestrator → Downloader → Import Monitor → Cleanup
- Client-agnostic via DownloadClient/TransferClient interfaces
- Transmission RPC API webhook for Sonarr/Radarr integration
- SQLite for state persistence with connection pooling (25 open, 5 idle conns)
- OpenTelemetry with OTLP/gRPC export (status logged at startup)
- Panic recovery with context-aware restart on all long-running goroutines

**Tech Stack:**
- Go 1.23, Chi Router v5, SQLite with CGO
- OpenTelemetry v1.38.0, cenkalti/backoff v5 for retry logic
- Bencode library (github.com/zeebo/bencode v1.0.0) for .torrent validation
- Docker deployment with distroless base image

**Deployment:**
- Long-running 24/7 service
- Multiple concurrent downloads (default: 5 parallel)
- Polling loops every 10 minutes for transfers and cleanup
- Transmission webhook endpoint for Sonarr/Radarr
- Database validation on startup with 3-retry exponential backoff
- Resource cleanup on all goroutine exit paths (context cancellation, completion, panic)

**Previous Milestones:**
- v1 Critical Fixes (2026-01-31): 3 phases, 6 plans — crash prevention, resource management, operational hygiene

## Constraints

- **Backward Compatibility**: Must not change existing APIs, configuration, or database schema
- **No Breaking Changes**: Existing deployments must work without config updates
- **Tech Stack**: Go 1.23, existing dependencies only (no new major dependencies)
- **Deployment**: Docker-based, CGO required for SQLite
- **No File Persistence**: .torrent files must not be saved to disk (v1.1 requirement)

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Fix bugs before adding features | Stability foundation required for long-running service | ✓ Good - v1 eliminates crashes and silent failures |
| Address resource leaks in this milestone | Goroutine leaks compound over time in 24/7 deployment | ✓ Good - defer pattern prevents ticker leaks |
| Defer performance and security to separate milestones | Focus scope on critical reliability issues | ✓ Good - maintained tight scope, shipped quickly |
| Use defer ticker.Stop() pattern | Guarantees cleanup on all exit paths (LIFO order) | ✓ Good - consistent across all goroutines |
| Context-aware panic restart | Only restart goroutines if context not cancelled | ✓ Good - prevents restart loops during shutdown |
| Log telemetry status at Info level | Operators need visibility, not a warning condition | ✓ Good - silent when enabled, informative when disabled |
| Database validation with exponential backoff | Fail-fast on critical dependency with retry | ✓ Good - 3 attempts before exit, consistent with HTTP retries |
| Modify existing GetTaggedTorrents instead of new method | IsAvailable()+IsDownloadable() double safety net is sufficient | ✓ Good - simpler, no code duplication |
| SaveParentID for tag matching | FileID unavailable during in-progress transfers | ✓ Good - validated with 6 httptest scenarios in Phase 11 |
| Triple safety net for download pipeline | IsAvailable + IsDownloadable + conditional files | ✓ Good - defense-in-depth prevents false positives |
| Monitor-first for rate limits | Defer caching until production data shows issues | — Pending - deployed, monitoring |
| Labels field always present (not omitempty) | Sonarr/Radarr expect field to always be present | ✓ Good - empty array acceptable |

---
*Last updated: 2026-02-08 after v1.3 milestone completion*
