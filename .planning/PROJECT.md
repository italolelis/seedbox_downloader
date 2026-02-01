# Seedbox Downloader

## What This Is

A Go-based automated downloader that orchestrates transfers from seedbox/torrent clients (Deluge, Put.io) to local storage, automatically importing media into Sonarr/Radarr. Runs reliably 24/7 with proper error handling, resource cleanup, and operational observability.

## Core Value

The application must run reliably 24/7 without crashes, resource leaks, or silent failures.

## Latest Milestone: v1.1 Torrent File Support (Shipped: 2026-02-01)

**Goal:** Enable Sonarr/Radarr to download content from .torrent-only trackers through Put.io proxy

**Delivered:**
- ✓ Process base64-encoded .torrent file content from Transmission API MetaInfo field
- ✓ Upload .torrent content directly to Put.io (no file persistence)
- ✓ Explicit error logging when .torrent files cannot be processed
- ✓ Test coverage for .torrent file handling (33 tests, 56.2% coverage)
- ✓ Observability for .torrent vs magnet link usage (structured logs + metrics)

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

### Active

(None — ready for next milestone planning)

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

**Shipped v1.1 (2026-02-01):**
- 4,558 lines of Go across 32 files (+6,542 insertions from v1)
- All 17 v1.1 requirements satisfied (.torrent file support)
- Production-ready with enhanced observability

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

---
*Last updated: 2026-02-01 after v1.1 milestone completion*
