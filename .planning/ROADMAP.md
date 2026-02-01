# Roadmap: Seedbox Downloader

## Milestones

- ✅ **v1 Critical Fixes** - Phases 1-3 (shipped 2026-01-31)
- 🚧 **v1.1 Torrent File Support** - Phases 4-6 (in progress)

## Phases

<details>
<summary>✅ v1 Critical Fixes (Phases 1-3) - SHIPPED 2026-01-31</summary>

### Phase 1: Crash Prevention
**Goal**: Eliminate nil pointer crashes in HTTP error paths
**Requirements**: CRASH-01, CRASH-02, CRASH-03
**Plans**: 2 plans

Plans:
- [x] 01-01: Fix GrabFile nil pointer crash
- [x] 01-02: Fix Discord notifier nil pointer crash

### Phase 2: Resource Management
**Goal**: Prevent resource leaks in long-running goroutines
**Requirements**: LEAK-01, LEAK-02, LEAK-03
**Plans**: 2 plans

Plans:
- [x] 02-01: Add ticker cleanup with defer pattern
- [x] 02-02: Add context-aware panic recovery

### Phase 3: Operational Hygiene
**Goal**: Improve operational visibility and database reliability
**Requirements**: OPS-01, OPS-02, OPS-03, OPS-04
**Plans**: 2 plans

Plans:
- [x] 03-01: Add database validation with retry
- [x] 03-02: Add telemetry status logging and connection pool config

</details>

### 🚧 v1.1 Torrent File Support (In Progress)

**Milestone Goal:** Enable Sonarr/Radarr to download content from .torrent-only trackers through Put.io proxy

#### Phase 4: Put.io Client Extension
**Goal**: Put.io client can upload .torrent file content and create transfers
**Depends on**: Phase 3 (v1 foundation)
**Requirements**: PUTIO-01, PUTIO-02, PUTIO-03, PUTIO-04
**Success Criteria** (what must be TRUE):
  1. Put.io client can accept .torrent file content as bytes
  2. .torrent content is uploaded to correct parent directory (same logic as magnet links)
  3. Put.io automatically creates transfer when .torrent file is detected
  4. Upload failures return specific error messages (API error vs invalid content)
**Plans**: 2 plans
**Status**: Complete (2026-02-01)

Plans:
- [x] 04-01-PLAN.md — Define custom error types in internal/transfer package
- [x] 04-02-PLAN.md — Extend Put.io client with AddTransferByBytes method

#### Phase 5: Transmission API Handler
**Goal**: Transmission API webhook accepts and processes .torrent file content
**Depends on**: Phase 4
**Requirements**: API-01, API-02, API-03, API-04, API-05, API-06
**Success Criteria** (what must be TRUE):
  1. Handler detects MetaInfo field in torrent-add requests
  2. Base64-encoded .torrent content is decoded correctly
  3. Decoded content is validated as proper bencode structure before upload
  4. Invalid .torrent content returns Transmission-compatible error response with specific reason
  5. Existing magnet link behavior (FileName field) works identically after changes
  6. When both MetaInfo and FileName present, MetaInfo takes priority
**Plans**: 2 plans
**Status**: Complete (2026-02-01)

Plans:
- [x] 05-01-PLAN.md — Add bencode dependency and extend handleTorrentAdd for MetaInfo support
- [x] 05-02-PLAN.md — Fix Transmission-compatible error responses

#### Phase 6: Observability & Testing
**Goal**: Production visibility and test coverage for .torrent file handling
**Depends on**: Phase 5
**Requirements**: OBS-01, OBS-02, OBS-03, TEST-01, TEST-02, TEST-03, TEST-04
**Success Criteria** (what must be TRUE):
  1. Logs show torrent type (magnet vs .torrent file) for every torrent-add request
  2. OpenTelemetry metrics track torrent_type distribution
  3. Error logs include detailed failure reasons (invalid base64, corrupt bencode, API error)
  4. Unit tests verify base64 decoding edge cases (invalid encoding, wrong variant)
  5. Unit tests verify bencode validation (malformed structure, missing fields)
  6. Integration tests verify real .torrent files work with Put.io SDK
  7. Backward compatibility tests verify magnet links still work identically
**Plans**: 3 plans
**Status**: Complete (2026-02-01)

Plans:
- [x] 06-01-PLAN.md — Add torrent type metrics and structured logging (OBS-01, OBS-02, OBS-03)
- [x] 06-02-PLAN.md — Unit tests for bencode validation and helper functions (TEST-01, TEST-02)
- [x] 06-03-PLAN.md — Integration tests for handler with mock client (TEST-03, TEST-04)

## Progress

**Execution Order:**
Phases execute in numeric order: 4 → 5 → 6

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Crash Prevention | v1 | 2/2 | Complete | 2026-01-31 |
| 2. Resource Management | v1 | 2/2 | Complete | 2026-01-31 |
| 3. Operational Hygiene | v1 | 2/2 | Complete | 2026-01-31 |
| 4. Put.io Client Extension | v1.1 | 2/2 | Complete | 2026-02-01 |
| 5. Transmission API Handler | v1.1 | 2/2 | Complete | 2026-02-01 |
| 6. Observability & Testing | v1.1 | 3/3 | Complete | 2026-02-01 |

---
*Last updated: 2026-02-01 after Phase 6 execution*
