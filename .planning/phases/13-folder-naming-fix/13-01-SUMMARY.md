---
phase: 13-folder-naming-fix
plan: 01
subsystem: api
tags: [putio, filepath, go, testing]

# Dependency graph
requires: []
provides:
  - "Fixed getFilesRecursively that strips extension from basePath when the root node is a single file"
  - "stripFileExtension helper with empty-result guard for dot-prefixed files"
  - "Unit tests: TestStripFileExtension (8 cases) and TestGetFilesRecursively_SingleFileExtensionStrip (4 cases)"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "stripFileExtension: strip only the last extension segment, return original name if stripping would produce empty string"
    - "Single-file vs multi-file detection via file.IsDir() at the root node level"

key-files:
  created: []
  modified:
    - internal/dc/putio/client.go
    - internal/dc/putio/client_test.go

key-decisions:
  - "Strip only the last extension segment using filepath.Ext + strings.TrimSuffix (matches Sonarr/Radarr folder-name parsing)"
  - "Guard against empty folder name: if TrimSuffix produces empty string (dot-prefixed files like .hidden), return original name unchanged"
  - "Multi-file directory traversal paths are untouched — only the !file.IsDir() single-file branch is modified"

patterns-established:
  - "stripFileExtension: private helper placed near other private helpers (findDirectoryID, filterMatchingTransferIds)"

requirements-completed:
  - DL-01
  - DL-02

# Metrics
duration: 2min
completed: 2026-02-22
---

# Phase 13: Folder Naming Fix Summary

**Single-file Put.io transfers now produce extension-free folder names (the_movie/the_movie.mkv) required by Sonarr/Radarr title matching, fixed via stripFileExtension helper in getFilesRecursively**

## Performance

- **Duration:** ~2 min
- **Started:** 2026-02-22T00:38:55Z
- **Completed:** 2026-02-22T00:40:26Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- Fixed the folder naming bug: single-file transfers now produce `the_movie/the_movie.mkv` instead of `the_movie.mkv/the_movie.mkv`
- Added `stripFileExtension` private helper using `filepath.Ext` + `strings.TrimSuffix`, with guard against empty results
- Added 12 new unit tests: 8 for `stripFileExtension` edge cases and 4 end-to-end path assertion tests via mock HTTP server
- Multi-file directory traversal paths remain completely untouched

## Task Commits

Each task was committed atomically:

1. **Task 1: Strip file extension from basePath in getFilesRecursively for single-file transfers** - `2f0cfd5` (fix)
2. **Task 2: Add unit tests for the extension-stripping behavior** - `29baf15` (test)

**Plan metadata:** (docs: complete plan — see final commit)

## Files Created/Modified
- `internal/dc/putio/client.go` - Added `stripFileExtension` helper; modified `getFilesRecursively` `!file.IsDir()` branch to use `stripFileExtension(basePath)` as folder name
- `internal/dc/putio/client_test.go` - Added `TestStripFileExtension` (8 table-driven cases) and `TestGetFilesRecursively_SingleFileExtensionStrip` (4 end-to-end cases)

## Decisions Made
- Used `filepath.Ext` + `strings.TrimSuffix` — strips only the last extension segment, matching Sonarr/Radarr folder-name parsing expectations
- Added guard: if stripping produces empty string (dot-prefixed files like `.hidden` where `filepath.Ext` returns the entire name), return original name to avoid empty folder path component

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Guard against empty folder name for dot-prefixed files**
- **Found during:** Task 2 (unit test execution)
- **Issue:** `filepath.Ext(".hidden")` returns `".hidden"` (not `""`), so `strings.TrimSuffix(".hidden", ".hidden")` = `""`. An empty folder name would produce a path like `/the_movie.mkv` with no folder component, breaking the download pipeline.
- **Fix:** Added guard in `stripFileExtension`: if the result of TrimSuffix is `""`, return the original `name` unchanged.
- **Files modified:** `internal/dc/putio/client.go`
- **Verification:** `TestStripFileExtension/dot_prefix_no_ext` passes; full test suite green.
- **Committed in:** `29baf15` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - Bug)
**Impact on plan:** Essential correctness fix — empty folder name would be a silent runtime bug. No scope creep.

## Issues Encountered
- Plan spec stated `filepath.Ext(".hidden")` returns `""` (incorrect). Actual Go behavior: `filepath.Ext(".hidden")` = `".hidden"`, producing empty string after TrimSuffix. Caught by test execution and auto-fixed inline.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Folder naming bug fully resolved with tests. Ready for next phase (transfer cleanup or missing transfer handling).
- No blockers.

---
*Phase: 13-folder-naming-fix*
*Completed: 2026-02-22*
