# Requirements: Seedbox Downloader v1.1

**Defined:** 2026-01-31
**Core Value:** The application must run reliably 24/7 without crashes, resource leaks, or silent failures.

## v1.1 Requirements

Requirements for .torrent file support milestone. Each maps to roadmap phases.

### Transmission API Handling

- [ ] **API-01**: Detect MetaInfo field in torrent-add requests
- [ ] **API-02**: Decode base64-encoded .torrent content from MetaInfo field
- [ ] **API-03**: Validate bencode structure of decoded .torrent content
- [ ] **API-04**: Return Transmission-compatible error responses for invalid .torrent content
- [ ] **API-05**: Maintain backward compatibility with existing magnet link behavior (FileName field)
- [ ] **API-06**: Prioritize MetaInfo field when both MetaInfo and FileName are present

### Put.io Integration

- [ ] **PUTIO-01**: Upload .torrent file content via Files.Upload() method
- [ ] **PUTIO-02**: Auto-create transfer when Put.io detects uploaded .torrent file
- [ ] **PUTIO-03**: Use correct parent directory for .torrent uploads (same logic as magnet links)
- [ ] **PUTIO-04**: Handle Put.io API errors gracefully with user-friendly error messages

### Observability

- [ ] **OBS-01**: Log torrent type (magnet vs .torrent file) for each torrent-add request
- [ ] **OBS-02**: Add OpenTelemetry counter metric with torrent_type attribute
- [ ] **OBS-03**: Log detailed error reasons (invalid base64, corrupt bencode, API errors)

### Testing

- [ ] **TEST-01**: Unit tests for base64 decoding edge cases (invalid encoding, wrong variant)
- [ ] **TEST-02**: Unit tests for bencode validation (malformed structure, missing required fields)
- [ ] **TEST-03**: Integration tests with real .torrent files and Put.io SDK
- [ ] **TEST-04**: Backward compatibility tests ensuring magnet links still work identically

## Future Requirements

Deferred to future milestones.

### Performance

- **PERF-01**: Add memory limits for large .torrent files (prevent OOM under load)
- **PERF-02**: Load tests with large .torrent files (50MB+) under concurrent requests
- **PERF-03**: Instrumentation for upload performance tracking

### Deluge Support

- **DELUGE-01**: Extend .torrent file support to Deluge client
- **DELUGE-02**: Add integration tests for Deluge .torrent handling

## Out of Scope

Explicitly excluded from this milestone. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| File persistence | Explicit v1.1 constraint - .torrent files must not be saved to disk |
| Watch folders for .torrent files | Not needed for Sonarr/Radarr webhook integration |
| Direct .torrent file upload API | Only Transmission RPC protocol support needed |
| Deluge .torrent support | Webhook API is Put.io only, defer to future milestone |
| Torrent metadata parsing | Put.io handles bencode parsing server-side, no need for client-side |
| Large file streaming | Memory-based approach sufficient for typical .torrent sizes (<10MB) |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| API-01 | Phase 5 | Pending |
| API-02 | Phase 5 | Pending |
| API-03 | Phase 5 | Pending |
| API-04 | Phase 5 | Pending |
| API-05 | Phase 5 | Pending |
| API-06 | Phase 5 | Pending |
| PUTIO-01 | Phase 4 | Pending |
| PUTIO-02 | Phase 4 | Pending |
| PUTIO-03 | Phase 4 | Pending |
| PUTIO-04 | Phase 4 | Pending |
| OBS-01 | Phase 6 | Pending |
| OBS-02 | Phase 6 | Pending |
| OBS-03 | Phase 6 | Pending |
| TEST-01 | Phase 6 | Pending |
| TEST-02 | Phase 6 | Pending |
| TEST-03 | Phase 6 | Pending |
| TEST-04 | Phase 6 | Pending |

**Coverage:**
- v1.1 requirements: 17 total
- Mapped to phases: 17 (100%)
- Unmapped: 0

---
*Requirements defined: 2026-01-31*
*Last updated: 2026-02-01 after roadmap creation*
