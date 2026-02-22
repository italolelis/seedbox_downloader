# Roadmap: Seedbox Downloader

## Milestones

- ✅ **v1 Critical Fixes** - Phases 1-3 (shipped 2026-01-31)
- ✅ **v1.1 Torrent File Support** - Phases 4-6 (shipped 2026-02-01)
- ✅ **v1.2 Logging Improvements** - Phases 7-10 (shipped 2026-02-08)
- ✅ **v1.3 Activity Tab Support** - Phases 11-12 (shipped 2026-02-08)
- **v1.4 Download Pipeline Fixes** - Phases 13-15 (in progress)

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

<details>
<summary>✅ v1.2 Logging Improvements (Phases 7-10) - SHIPPED 2026-02-08</summary>

**Milestone Goal:** Make logs tell the story of what the application is doing during its lifecycle

### Phase 7: Trace Correlation
**Goal**: Bridge OpenTelemetry traces with structured logs for end-to-end request correlation
**Plans**: 4 plans

Plans:
- [x] 07-01: Create TraceHandler wrapper and integrate into logger initialization
- [x] 07-02: Migrate core components (downloader, transfer, main.go) to context-aware logging
- [x] 07-03: Migrate client components (deluge, putio, transmission) to context-aware logging
- [x] 07-04: [Gap closure] Complete Put.io client migration (8 remaining non-context calls)

### Phase 8: Lifecycle Visibility
**Goal**: Clear visibility into application startup, shutdown, and component initialization
**Plans**: 2 plans

Plans:
- [x] 08-01: Add phased startup logging with component ready messages and service ready
- [x] 08-02: Add shutdown sequence logging and error context enhancement

### Phase 9: Log Level Consistency
**Goal**: Consistent log level usage across all components to reduce noise and improve signal
**Plans**: 2 plans

Plans:
- [x] 09-01: Apply silent-when-idle and per-file DEBUG to core pipeline
- [x] 09-02: Ensure consistent authentication logging across clients

### Phase 10: HTTP Request Logging
**Goal**: Complete visibility into HTTP API usage with structured request/response logging
**Plans**: 2 plans

Plans:
- [x] 10-01: Create HTTP middleware components (RequestID + HTTPLogging)
- [x] 10-02: Integrate middleware into Chi router

</details>

<details>
<summary>✅ v1.3 Activity Tab Support (Phases 11-12) - SHIPPED 2026-02-08</summary>

**Milestone Goal:** Make Sonarr/Radarr Activity tab show in-progress downloads with accurate progress, status, and peer info by returning all Put.io transfers from the Transmission RPC proxy

### Phase 11: SaveParentID Tag Matching
**Goal**: Tag matching works via SaveParentID for all transfers, validated before removing the FileID filter
**Plans**: 1 plan

Plans:
- [x] 11-01: Test and validate SaveParentID-based tag matching

### Phase 12: In-Progress Visibility
**Goal**: Sonarr/Radarr Activity tab displays in-progress downloads with accurate progress, status, peer counts, speed, and labels
**Plans**: 2 plans

Plans:
- [x] 12-01: Remove FileID filter, conditional file population, DownloadSpeed mapping
- [x] 12-02: Complete status mapping, peer/speed fields, labels in Transmission response

</details>

---

## v1.4 Download Pipeline Fixes

**Milestone Goal:** Fix download folder naming for single-file transfers, ensure Put.io transfer cleanup after import, and handle missing transfers gracefully

### Phase 13: Folder Naming Fix

**Goal:** Single-file transfers download into correctly named folders without file extensions

**Dependencies:** None

**Requirements:** DL-01, DL-02

**Success Criteria:**
1. A single-file transfer (e.g., `the_movie.mkv`) creates a folder named `the_movie/`, not `the_movie.mkv/`
2. The folder naming fix applies to any file extension (.mkv, .mp4, .avi, .torrent, etc.)
3. Multi-file transfers are unaffected — their folder names remain unchanged
4. Sonarr/Radarr can successfully import content from the corrected folder path

Plans:
- [x] 13-01: Strip file extension from single-file transfer folder name

---

### Phase 14: Transfer Cleanup Fix

**Goal:** Put.io transfers and their file data are removed after Sonarr/Radarr confirms import

**Dependencies:** None

**Requirements:** CLN-01, CLN-02

**Success Criteria:**
1. After Sonarr/Radarr confirms an import, the Put.io transfer is deleted (not left pending seeding stop)
2. The Put.io file data associated with the transfer is deleted alongside the transfer
3. Transfers that fail import are not prematurely deleted

Plans:
- [ ] 14-01: Fix Put.io cleanup trigger and file deletion after import confirmation

---

### Phase 15: Missing Transfer Handling

**Goal:** Missing or deleted Put.io transfers are detected, logged, and reported via Discord

**Dependencies:** None

**Requirements:** MTX-01, MTX-02

**Success Criteria:**
1. When a tracked transfer no longer exists in Put.io, a warn-level log entry is emitted with transfer details
2. When a tracked transfer is missing, a Discord notification is sent identifying the transfer
3. The pipeline continues processing other transfers when one is found missing — it does not crash or stall
4. Transfers that are present are not affected by the missing-transfer detection logic

Plans:
- [ ] 15-01: Detect missing transfers, log warning, and send Discord notification

---

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Nil Pointer Safety | v1 | 2/2 | Complete | 2026-01-31 |
| 2. Resource Management | v1 | 2/2 | Complete | 2026-01-31 |
| 3. Database Reliability | v1 | 2/2 | Complete | 2026-01-31 |
| 4. Error Handling Foundation | v1.1 | 1/1 | Complete | 2026-02-01 |
| 5. Torrent File Upload | v1.1 | 3/3 | Complete | 2026-02-01 |
| 6. Observability & Testing | v1.1 | 3/3 | Complete | 2026-02-01 |
| 7. Trace Correlation | v1.2 | 4/4 | Complete | 2026-02-08 |
| 8. Lifecycle Visibility | v1.2 | 2/2 | Complete | 2026-02-08 |
| 9. Log Level Consistency | v1.2 | 2/2 | Complete | 2026-02-08 |
| 10. HTTP Request Logging | v1.2 | 2/2 | Complete | 2026-02-08 |
| 11. SaveParentID Tag Matching | v1.3 | 1/1 | Complete | 2026-02-08 |
| 12. In-Progress Visibility | v1.3 | 2/2 | Complete | 2026-02-08 |
| 13. Folder Naming Fix | v1.4 | 1/1 | Complete | 2026-02-22 |
| 14. Transfer Cleanup Fix | v1.4 | 0/1 | Pending | — |
| 15. Missing Transfer Handling | v1.4 | 0/1 | Pending | — |
