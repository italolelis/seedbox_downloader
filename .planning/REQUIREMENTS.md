# Requirements: Seedbox Downloader v1.2

**Defined:** 2026-02-01
**Core Value:** The application must run reliably 24/7 without crashes, resource leaks, or silent failures.

## v1.2 Requirements

Requirements for logging improvements milestone. Each maps to roadmap phases.

### Trace Correlation

- [x] **TRACE-01**: All log entries include trace_id when OpenTelemetry tracing is active
- [x] **TRACE-02**: All log entries include span_id when within an active span
- [x] **TRACE-03**: otelslog bridge wraps existing slog handler without breaking current JSON output format
- [x] **TRACE-04**: All logging calls in HTTP handlers use InfoContext/DebugContext/etc (not Info/Debug)
- [x] **TRACE-05**: All goroutines receive context and propagate it to logging calls
- [x] **TRACE-06**: Log entries without trace context are identifiable (missing trace_id indicates propagation bug)

### Lifecycle Visibility

- [x] **LIFECYCLE-01**: Startup logs show clear initialization phases in order (config → telemetry → database → clients → server)
- [x] **LIFECYCLE-02**: Each major component logs "ready" message when initialization complete
- [x] **LIFECYCLE-03**: Application logs final "service ready" message with port and configured label
- [x] **LIFECYCLE-04**: Shutdown logs show graceful cleanup sequence (server stop → downloads finish → cleanup → database close)
- [x] **LIFECYCLE-05**: Component initialization failures log at ERROR with specific failure reason
- [x] **LIFECYCLE-06**: Startup logs include key configuration values (label, polling interval, download directory)

### Log Level Consistency

- [ ] **LEVELS-01**: Lifecycle events (startup, shutdown, component ready) log at INFO level
- [ ] **LEVELS-02**: Normal operations (transfer discovered, download started, import detected) log at INFO level
- [ ] **LEVELS-03**: Detailed progress (file downloaded, bytes transferred, polling tick) logs at DEBUG level
- [ ] **LEVELS-04**: Warning conditions (retries, slow operations, unexpected but handled errors) log at WARN level
- [ ] **LEVELS-05**: Error conditions (failed operations, panics, unhandled errors) log at ERROR level
- [ ] **LEVELS-06**: No INFO-level logs during idle polling (only when transfers found)
- [ ] **LEVELS-07**: Multi-file torrents don't log each file at INFO (only transfer-level events at INFO, files at DEBUG)

### HTTP Request Logging

- [ ] **HTTP-01**: All HTTP requests log method, path, and status code
- [ ] **HTTP-02**: HTTP requests include auto-generated request_id in logs
- [ ] **HTTP-03**: HTTP error responses (5xx) log at ERROR level
- [ ] **HTTP-04**: HTTP client errors (4xx) log at WARN level
- [ ] **HTTP-05**: HTTP success responses (2xx) log at INFO level
- [ ] **HTTP-06**: HTTP request logs include duration_ms for performance tracking

## Future Requirements

Deferred to future milestones.

### Pipeline Flow Tracing

- **FLOW-01**: All logs related to a transfer include consistent transfer_id field
- **FLOW-02**: Pipeline stage transitions log with operation field (discover, claim, download, import, cleanup)
- **FLOW-03**: Transfer state changes log explicitly (downloading → imported → cleaning)
- **FLOW-04**: Single grep for transfer_id shows complete transfer lifecycle

### Advanced Observability

- **ADV-01**: Dynamic log level changes via HTTP endpoint (for production debugging)
- **ADV-02**: Log sampling for high-frequency operations (file downloads, polling ticks)
- **ADV-03**: Structured error taxonomy for common failure modes
- **ADV-04**: Periodic resource utilization logging (active downloads, goroutines, memory)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Log aggregation/storage | External concern (use Loki, CloudWatch, etc.) |
| Log parsing/analysis tools | External concern (use existing tooling) |
| Custom log formatters | slog JSONHandler is standard, no need for custom formats |
| Log rotation | Handled by container orchestration or systemd |
| Log authentication/encryption | Transport layer concern (TLS, VPN) |
| Multiple log outputs | Single JSON output to stdout is standard for containers |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| TRACE-01 | Phase 7 | Complete |
| TRACE-02 | Phase 7 | Complete |
| TRACE-03 | Phase 7 | Complete |
| TRACE-04 | Phase 7 | Complete |
| TRACE-05 | Phase 7 | Complete |
| TRACE-06 | Phase 7 | Complete |
| LIFECYCLE-01 | Phase 8 | Complete |
| LIFECYCLE-02 | Phase 8 | Complete |
| LIFECYCLE-03 | Phase 8 | Complete |
| LIFECYCLE-04 | Phase 8 | Complete |
| LIFECYCLE-05 | Phase 8 | Complete |
| LIFECYCLE-06 | Phase 8 | Complete |
| LEVELS-01 | Phase 9 | Pending |
| LEVELS-02 | Phase 9 | Pending |
| LEVELS-03 | Phase 9 | Pending |
| LEVELS-04 | Phase 9 | Pending |
| LEVELS-05 | Phase 9 | Pending |
| LEVELS-06 | Phase 9 | Pending |
| LEVELS-07 | Phase 9 | Pending |
| HTTP-01 | Phase 10 | Pending |
| HTTP-02 | Phase 10 | Pending |
| HTTP-03 | Phase 10 | Pending |
| HTTP-04 | Phase 10 | Pending |
| HTTP-05 | Phase 10 | Pending |
| HTTP-06 | Phase 10 | Pending |

**Coverage:**
- v1.2 requirements: 25 total
- Mapped to phases: 25 (100%)
- Unmapped: 0

---
*Requirements defined: 2026-02-01*
*Last updated: 2026-02-02 after roadmap creation*
