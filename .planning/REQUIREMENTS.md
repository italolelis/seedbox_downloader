# Requirements: Seedbox Downloader - Critical Fixes

**Defined:** 2026-01-31
**Core Value:** The application must run reliably 24/7 without crashes, resource leaks, or silent failures.

## v1 Requirements

Requirements for this maintenance milestone. Each maps to roadmap phases.

### Bug Fixes

- [x] **BUG-01**: Fix nil pointer dereference when GrabFile HTTP request fails before response is received
- [x] **BUG-02**: Add HTTP status code check in Discord notifier to detect and log webhook failures

### Resource Management

- [x] **RES-01**: Ensure ticker.Stop() is called in all goroutine exit paths (success and context cancellation)
- [x] **RES-02**: Add defer ticker.Stop() in TransferOrchestrator watch loops
- [x] **RES-03**: Add defer ticker.Stop() in Downloader watch loops
- [x] **RES-04**: Add panic recovery to main.go notification loop

### Telemetry

- [x] **TEL-01**: Log warning at startup when telemetry is disabled (OTEL_ADDRESS not set)

### Code Quality

- [x] **CODE-01**: Remove or implement commented-out startup recovery code in transfer.go (lines 96-122)

### Database

- [x] **DB-01**: Add db.Ping() after database initialization to verify connection
- [x] **DB-02**: Configure connection pool limits with SetMaxOpenConns() and SetMaxIdleConns()

## v2 Requirements

Deferred to future maintenance milestones.

### Performance

- **PERF-01**: Parallelize ARR service checks within transfers
- **PERF-02**: Implement HTTP range request support for download resume
- **PERF-03**: Add prepared statements for frequently-called queries
- **PERF-04**: Implement adaptive polling frequency

### Security

- **SEC-01**: Log warning when TLS certificate verification is disabled
- **SEC-02**: Redact credentials from all log output
- **SEC-03**: Treat Discord webhook URLs as secrets

### Testing

- **TEST-01**: Add unit tests for state machine transitions
- **TEST-02**: Add concurrency tests for parallel downloads
- **TEST-03**: Add integration tests for end-to-end flows
- **TEST-04**: Add tests for database repository atomicity

## Out of Scope

Explicitly excluded from this milestone. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Performance optimizations | Not critical for stability; defer to dedicated performance milestone |
| Security hardening | Important but not causing immediate failures; defer to security milestone |
| Test coverage expansion | Should follow fixes, not block them; defer to testing milestone |
| Scaling improvements (PostgreSQL, dynamic parallelism) | Current scale is sufficient; defer until scaling needs arise |
| Fragile areas (state machine validation, nil notifier) | Not causing current issues; defer to robustness milestone |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| BUG-01 | Phase 1 | Complete |
| BUG-02 | Phase 1 | Complete |
| RES-01 | Phase 2 | Complete |
| RES-02 | Phase 2 | Complete |
| RES-03 | Phase 2 | Complete |
| RES-04 | Phase 2 | Complete |
| TEL-01 | Phase 3 | Complete |
| CODE-01 | Phase 3 | Complete |
| DB-01 | Phase 3 | Complete |
| DB-02 | Phase 3 | Complete |

**Coverage:**
- v1 requirements: 10 total
- Mapped to phases: 10
- Unmapped: 0 (100% coverage)

---
*Requirements defined: 2026-01-31*
*Last updated: 2026-01-31 after Phase 2 completion*
