# Roadmap: Seedbox Downloader

## Milestones

- ✅ **v1 Critical Fixes** - Phases 1-3 (shipped 2026-01-31)
- ✅ **v1.1 Torrent File Support** - Phases 4-6 (shipped 2026-02-01)
- 🚧 **v1.2 Logging Improvements** - Phases 7-10 (in progress)

## Phases

<details>
<summary>✅ v1 Critical Fixes (Phases 1-3) - SHIPPED 2026-01-31</summary>

### Phase 1: Nil Pointer Safety
**Goal**: Eliminate crash vectors in HTTP error paths
**Plans**: 2 plans

Plans:
- [x] 01-01: Fix GrabFile nil pointer crash
- [x] 01-02: Fix Discord webhook nil pointer crash

### Phase 2: Resource Management
**Goal**: Prevent resource leaks in long-running goroutines
**Plans**: 2 plans

Plans:
- [x] 02-01: Add ticker cleanup to polling loops
- [x] 02-02: Add panic recovery with context-aware restart

### Phase 3: Database Reliability
**Goal**: Ensure database availability at startup and runtime
**Plans**: 2 plans

Plans:
- [x] 03-01: Add connection validation with retry
- [x] 03-02: Add connection pool configuration

</details>

<details>
<summary>✅ v1.1 Torrent File Support (Phases 4-6) - SHIPPED 2026-02-01</summary>

### Phase 4: Error Handling Foundation
**Goal**: Structured error handling for transfer operations
**Plans**: 1 plan

Plans:
- [x] 04-01: Custom error types for transfer operations

### Phase 5: Torrent File Upload
**Goal**: Process and upload .torrent files to Put.io
**Plans**: 3 plans

Plans:
- [x] 05-01: Extend Put.io client with .torrent upload
- [x] 05-02: Implement Transmission API handler with base64 decoding
- [x] 05-03: Add bencode validation

### Phase 6: Observability & Testing
**Goal**: Test coverage and metrics for .torrent support
**Plans**: 3 plans

Plans:
- [x] 06-01: Add structured logging for torrent types
- [x] 06-02: Add OpenTelemetry metrics
- [x] 06-03: Add test coverage for .torrent handling

</details>

### 🚧 v1.2 Logging Improvements (In Progress)

**Milestone Goal:** Make logs tell the story of what the application is doing during its lifecycle

#### Phase 7: Trace Correlation
**Goal**: Bridge OpenTelemetry traces with structured logs for end-to-end request correlation
**Depends on**: Phase 6
**Requirements**: TRACE-01, TRACE-02, TRACE-03, TRACE-04, TRACE-05, TRACE-06
**Success Criteria** (what must be TRUE):
  1. All log entries include trace_id when OpenTelemetry tracing is active
  2. All log entries include span_id when within an active span
  3. All logging calls use context-aware methods (InfoContext/DebugContext/etc)
  4. All goroutines receive context and propagate it to logging calls
  5. Log entries without trace context are identifiable (missing trace_id indicates propagation bug)
**Plans**: 3 plans

Plans:
- [ ] 07-01: Create TraceHandler wrapper and integrate into logger initialization
- [ ] 07-02: Migrate core components (downloader, transfer, main.go) to context-aware logging
- [ ] 07-03: Migrate client components (deluge, putio, transmission) to context-aware logging

#### Phase 8: Lifecycle Visibility
**Goal**: Clear visibility into application startup, shutdown, and component initialization
**Depends on**: Phase 7
**Requirements**: LIFECYCLE-01, LIFECYCLE-02, LIFECYCLE-03, LIFECYCLE-04, LIFECYCLE-05, LIFECYCLE-06
**Success Criteria** (what must be TRUE):
  1. Startup logs show initialization phases in order (config → telemetry → database → clients → server)
  2. Each major component logs "ready" message when initialization complete
  3. Application logs final "service ready" message with port and configured label
  4. Shutdown logs show graceful cleanup sequence
  5. Component initialization failures log at ERROR with specific failure reason
  6. Startup logs include key configuration values (label, polling interval, download directory)
**Plans**: TBD

Plans:
- [ ] 08-01: [Brief description]

#### Phase 9: Log Level Consistency
**Goal**: Consistent log level usage across all components to reduce noise and improve signal
**Depends on**: Phase 8
**Requirements**: LEVELS-01, LEVELS-02, LEVELS-03, LEVELS-04, LEVELS-05, LEVELS-06, LEVELS-07
**Success Criteria** (what must be TRUE):
  1. Lifecycle events log at INFO level
  2. Normal operations log at INFO level (only when work happens)
  3. Detailed progress logs at DEBUG level
  4. Warning conditions log at WARN level
  5. Error conditions log at ERROR level
  6. No INFO-level logs during idle polling (silent when nothing to do)
  7. Multi-file torrents log transfer-level events at INFO, per-file at DEBUG
**Plans**: TBD

Plans:
- [ ] 09-01: [Brief description]

#### Phase 10: HTTP Request Logging
**Goal**: Complete visibility into HTTP API usage with structured request/response logging
**Depends on**: Phase 9
**Requirements**: HTTP-01, HTTP-02, HTTP-03, HTTP-04, HTTP-05, HTTP-06
**Success Criteria** (what must be TRUE):
  1. All HTTP requests log method, path, and status code
  2. HTTP requests include auto-generated request_id in logs
  3. HTTP error responses (5xx) log at ERROR level
  4. HTTP client errors (4xx) log at WARN level
  5. HTTP success responses (2xx) log at INFO level
  6. HTTP request logs include duration_ms for performance tracking
**Plans**: TBD

Plans:
- [ ] 10-01: [Brief description]

## Progress

**Execution Order:**
Phases execute in numeric order: 7 → 8 → 9 → 10

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Nil Pointer Safety | v1 | 2/2 | Complete | 2026-01-31 |
| 2. Resource Management | v1 | 2/2 | Complete | 2026-01-31 |
| 3. Database Reliability | v1 | 2/2 | Complete | 2026-01-31 |
| 4. Error Handling Foundation | v1.1 | 1/1 | Complete | 2026-02-01 |
| 5. Torrent File Upload | v1.1 | 3/3 | Complete | 2026-02-01 |
| 6. Observability & Testing | v1.1 | 3/3 | Complete | 2026-02-01 |
| 7. Trace Correlation | v1.2 | 0/3 | Not started | - |
| 8. Lifecycle Visibility | v1.2 | 0/TBD | Not started | - |
| 9. Log Level Consistency | v1.2 | 0/TBD | Not started | - |
| 10. HTTP Request Logging | v1.2 | 0/TBD | Not started | - |
