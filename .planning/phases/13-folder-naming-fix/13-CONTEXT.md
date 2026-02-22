# Phase 13: Folder Naming Fix - Context

**Gathered:** 2026-02-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix the local folder name when downloading single-file transfers from Put.io. Currently `getFilesRecursively` uses the file name (including extension) as the base path, creating folders like `the_movie.mkv/the_movie.mkv`. Sonarr/Radarr expect the folder name without extension: `the_movie/the_movie.mkv`.

</domain>

<decisions>
## Implementation Decisions

### Folder naming
- When a transfer results in a single file, the folder name must be the file name WITHOUT the extension
- Example: `the_movie.mkv` → folder `the_movie/`, file inside `the_movie.mkv`
- Must work for any file extension (.mkv, .mp4, .avi, .torrent, .nzb, etc.)
- Multi-file transfers are NOT affected — their folder structure stays as-is

### Extension stripping
- Strip only the final extension (last `.xxx` segment)
- Files with multiple dots keep everything except the last extension: `the.movie.2024.mkv` → folder `the.movie.2024/`
- This matches how Sonarr/Radarr parse folder names (they look for the movie/show name in the folder)

### Claude's Discretion
- Exact detection logic for single-file vs multi-file transfers
- How to handle edge cases (files with no extension, hidden files)
- Whether to strip extension in the Put.io client path building or in the downloader

</decisions>

<specifics>
## Specific Ideas

- The fix is in the path-building logic, not a post-download rename
- Sonarr/Radarr import by scanning the folder name for the media title — extension in folder name breaks this matching

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 13-folder-naming-fix*
*Context gathered: 2026-02-22*
