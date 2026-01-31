# Seedbox Downloader - Critical Fixes

## What This Is

A Go-based automated downloader that orchestrates transfers from seedbox/torrent clients (Deluge, Put.io) to local storage, automatically importing media into Sonarr/Radarr. This maintenance milestone fixes critical bugs and resource leaks that can cause crashes and goroutine/resource exhaustion in long-running deployments.

## Core Value

The application must run reliably 24/7 without crashes, resource leaks, or silent failures.

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

### Active

- [ ] Fix nil pointer dereference in GrabFile error path
- [ ] Fix Discord notifier silently ignoring HTTP failures
- [ ] Ensure all goroutines with tickers properly clean up on context cancellation
- [ ] Log warning when telemetry is disabled (silent fallback to noop)
- [ ] Remove or implement commented-out startup recovery code
- [ ] Add database connection validation with ping and connection pooling

### Out of Scope

- Performance optimizations — Defer to future milestone (sequential ARR checks, polling latency, resume support)
- Security hardening — Defer to future milestone (TLS warnings, credential redaction, webhook protection)
- Test coverage — Defer to future milestone (state machine tests, integration tests, concurrency tests)
- Scaling improvements — Defer to future milestone (PostgreSQL migration, dynamic parallelism, rate limiting)

## Context

**Existing Architecture:**
- Event-driven pipeline: TransferOrchestrator → Downloader → Import Monitor → Cleanup
- Client-agnostic via DownloadClient/TransferClient interfaces
- SQLite for state persistence, channels for event communication
- OpenTelemetry with OTLP/gRPC export

**Tech Stack:**
- Go 1.23, Chi Router v5, SQLite with CGO
- OpenTelemetry v1.38.0 for observability
- Docker deployment with distroless base image

**Known Issues (from codebase analysis):**
- Multiple goroutines spawn tickers but only stop them in success paths, not on context cancellation
- GrabFile closes resp.Body even when resp is nil (network error before response)
- Discord notifier returns nil on all status codes, swallowing webhook failures
- Telemetry system falls back to noop.MeterProvider silently when OTEL_ADDRESS unset
- 25 lines of commented-out code hint at missing startup recovery logic
- Database connection created but never validated with ping or pooling configured

**Deployment Context:**
- Long-running 24/7 service
- Multiple concurrent downloads (default: 5 parallel)
- Polling loops every 10 minutes for transfers and cleanup
- Must survive network errors, client timeouts, and restart without resource exhaustion

## Constraints

- **Backward Compatibility**: Must not change existing APIs, configuration, or database schema
- **No Breaking Changes**: Existing deployments must work without config updates
- **Tech Stack**: Go 1.23, existing dependencies only (no new major dependencies)
- **Deployment**: Docker-based, CGO required for SQLite

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Fix bugs before adding features | Stability foundation required for long-running service | — Pending |
| Address resource leaks in this milestone | Goroutine leaks compound over time in 24/7 deployment | — Pending |
| Defer performance and security to separate milestones | Focus scope on critical reliability issues | — Pending |

---
*Last updated: 2026-01-31 after initialization*
