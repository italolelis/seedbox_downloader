# Requirements: Seedbox Downloader

**Defined:** 2026-02-22
**Core Value:** The application must run reliably 24/7 without crashes, resource leaks, or silent failures.

## v1.4 Requirements

Requirements for download pipeline fixes. Each maps to roadmap phases.

### Download Pipeline

- [x] **DL-01**: Single-file transfers create a folder named without file extension (e.g., `the_movie/the_movie.mkv` not `the_movie.mkv/the_movie.mkv`)
- [x] **DL-02**: Folder name stripping works for any file extension (.mkv, .mp4, .avi, .torrent, etc.)

### Transfer Cleanup

- [ ] **CLN-01**: Put.io transfer is removed after Sonarr/Radarr confirms import (not only after seeding stops)
- [ ] **CLN-02**: Put.io file data is deleted alongside transfer removal

### Missing Transfer Handling

- [ ] **MTX-01**: Missing/deleted Put.io transfers are detected and logged at warn level
- [ ] **MTX-02**: Discord notification sent when a tracked transfer no longer exists in Put.io

## Future Requirements

None — focused bug fix milestone.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Multi-file transfer folder renaming | Only single-file transfers have the extension-in-folder bug |
| Transfer retry on failure | Separate concern, defer to future milestone |
| Automatic re-download of missing transfers | Missing transfers should be reported, not automatically retried |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| DL-01 | Phase 13 | Complete |
| DL-02 | Phase 13 | Complete |
| CLN-01 | Phase 14 | Pending |
| CLN-02 | Phase 14 | Pending |
| MTX-01 | Phase 15 | Pending |
| MTX-02 | Phase 15 | Pending |

**Coverage:**
- v1.4 requirements: 6 total
- Mapped to phases: 6
- Unmapped: 0 ✓

---
*Requirements defined: 2026-02-22*
*Last updated: 2026-02-22 after roadmap creation*
