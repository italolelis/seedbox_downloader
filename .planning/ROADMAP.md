# Roadmap: Seedbox Downloader - Critical Fixes

## Overview

This maintenance milestone fixes critical bugs and resource leaks across three focused phases: eliminating crashes and silent failures, preventing ticker resource leaks in long-running goroutines, and establishing operational hygiene through connection validation and observability. The work ensures 24/7 reliability without breaking existing deployments.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Critical Safety** - Eliminate crashes and silent failures
- [ ] **Phase 2: Resource Leak Prevention** - Stop ticker resource leaks in goroutines
- [ ] **Phase 3: Operational Hygiene** - Validate connections and improve observability

## Phase Details

### Phase 1: Critical Safety
**Goal**: Application handles errors without crashing or silently failing
**Depends on**: Nothing (first phase)
**Requirements**: BUG-01, BUG-02
**Success Criteria** (what must be TRUE):
  1. Application handles HTTP request failures without nil pointer panics
  2. Failed Discord webhook notifications are logged with HTTP status codes
  3. Error paths in file transfer operations complete safely without crashes
**Plans**: 1 plan

Plans:
- [x] 01-01-PLAN.md — Fix nil pointer in GrabFile and add Discord status validation

### Phase 2: Resource Leak Prevention
**Goal**: Goroutines with tickers clean up resources on all exit paths
**Depends on**: Nothing (independent work)
**Requirements**: RES-01, RES-02, RES-03, RES-04
**Success Criteria** (what must be TRUE):
  1. All goroutines with tickers call defer ticker.Stop()
  2. Ticker cleanup occurs on both success paths and context cancellation
  3. Long-running service does not accumulate leaked goroutines over time
  4. Resource cleanup and panic recovery is consistent across TransferOrchestrator, Downloader, and notification loops
**Plans**: 3 plans

Plans:
- [ ] 02-01-PLAN.md — Add ticker cleanup and panic recovery to TransferOrchestrator
- [ ] 02-02-PLAN.md — Add ticker cleanup and panic recovery to Downloader watch loops
- [ ] 02-03-PLAN.md — Add panic recovery to notification loop

### Phase 3: Operational Hygiene
**Goal**: Application validates dependencies at startup and logs operational status
**Depends on**: Nothing (independent work)
**Requirements**: TEL-01, CODE-01, DB-01, DB-02
**Success Criteria** (what must be TRUE):
  1. Startup logs clearly indicate telemetry enablement status
  2. Database connection failures are detected immediately at application startup
  3. Connection pool limits prevent database resource exhaustion
  4. Commented-out recovery code is either implemented or removed
**Plans**: TBD

Plans:
- [ ] 03-01: TBD (plan-phase will decompose)

## Progress

**Execution Order:**
Phases execute in numeric order: 1 -> 2 -> 3

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Critical Safety | 1/1 | Complete | 2026-01-31 |
| 2. Resource Leak Prevention | 0/3 | Planned | - |
| 3. Operational Hygiene | 0/0 | Not started | - |

---
*Created: 2026-01-31*
*Last updated: 2026-01-31 after Phase 2 planning*
