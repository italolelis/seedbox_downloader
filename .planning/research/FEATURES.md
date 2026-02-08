# Feature Landscape: Sonarr/Radarr Activity Tab via Transmission RPC

**Domain:** Transmission RPC proxy for Sonarr/Radarr Activity tab monitoring
**Researched:** 2026-02-08
**Confidence:** MEDIUM (verified via official sources and Sonarr source code inspection)

## Context

Sonarr and Radarr poll the Transmission RPC `torrent-get` endpoint to populate their Activity tab with in-progress downloads. The Activity tab shows download progress, ETA, speed, and status for active transfers. Our proxy currently returns only completed/seeding transfers, causing the Activity tab to appear empty or show only post-download activity.

This research identifies what fields and behaviors Sonarr/Radarr expect from `torrent-get` for the Activity tab to function correctly.

## Table Stakes

Features that Sonarr/Radarr absolutely require for the Activity tab to display in-progress downloads. Missing any of these = broken Activity tab.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Return in-progress transfers from torrent-get** | Activity tab queries all torrents; if only completed transfers returned, in-progress downloads invisible | Low | Current code filters to completed only; need to include "downloading" status |
| **percentDone or calculated progress** | Progress bar display requires completion percentage | Low | Calculate from `(totalSize - leftUntilDone) / totalSize` if `percentDone` not directly available |
| **leftUntilDone field** | Used with totalSize to calculate progress and display remaining data | Low | Put.io provides `size - downloaded`; map to `leftUntilDone` |
| **totalSize field** | Required for progress calculation and size display | Low | Already implemented; Put.io `size` field maps directly |
| **status field (0-6)** | Determines Activity tab status text and icon | Medium | Must map Put.io states to Transmission status codes correctly |
| **eta field** | Shows estimated time remaining in Activity tab | Low | Put.io provides `estimated_time`; already mapped in current code |
| **name field** | Display torrent name in Activity list | Low | Already implemented |
| **id and hashString fields** | Identify torrents for subsequent operations (remove, set) | Low | Already implemented (hashString from SHA1 of ID) |
| **downloadDir field** | Shows download location in Activity details | Low | Already implemented; maps to Put.io save path |

## Differentiators

Features that enhance the Activity tab experience but aren't strictly required for basic functionality.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **rateDownload field** | Shows current download speed (B/s) | Medium | Put.io may provide download rate; verify API availability |
| **rateUpload field** | Shows current upload speed (B/s) | Medium | Put.io tracks upload in seeding transfers; may be 0 during download |
| **secondsDownloading field** | Shows time spent downloading | Low | Can calculate from transfer start time if Put.io provides it |
| **uploadedEver field** | Shows total uploaded data for seeding ratio | Medium | Put.io may track uploads; verify API availability |
| **isStalled field** | Indicates if download has stalled (no progress) | Medium | Requires tracking progress over time; may need state |
| **errorString field** | Shows specific error messages in Activity tab | Low | Already implemented; populate with Put.io error messages |
| **peersConnected field** | Shows peer count for active transfers | Medium | Put.io may provide peer/seed counts; verify API availability |
| **metadataPercentComplete field** | Shows metadata download progress (magnet links) | Low | Likely not applicable to Put.io; can omit or always return 1.0 |
| **recheckProgress field** | Shows file verification progress | Low | Not applicable to Put.io (no local rechecking); can omit |

## Anti-Features

Features to explicitly NOT implement. These would cause confusion, incorrect behavior, or maintenance burden.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| **Return transfers without configured label** | Sonarr/Radarr use labels to filter their torrents; returning unlabeled transfers pollutes Activity tab | Always filter `torrent-get` by label (current behavior correct) |
| **Implement percentDone separately from leftUntilDone** | Transmission uses both fields; inconsistency breaks progress display | Calculate percentDone from leftUntilDone/totalSize; keep them synchronized |
| **Return transfers from all users (shared Put.io)** | Multi-user Put.io accounts would leak transfer visibility | Filter by label/parent directory (current behavior correct) |
| **Implement real-time WebSocket updates** | Sonarr/Radarr poll on 60-90 second intervals; WebSocket adds complexity without value | Stick to stateless HTTP polling (current approach correct) |
| **Cache torrent-get responses** | Stale data breaks Activity tab accuracy; polling interval already conservative | Return fresh data on every request (current behavior correct) |
| **Implement fields argument filtering** | Sonarr requests specific fields via "fields" array; returning all fields is simpler and bandwidth-negligible | Return all supported fields on every request; ignore "fields" argument |
| **Support Transmission queue states** | Put.io doesn't have queue; mapping queue states (StatusDownloadWait) would be misleading | Only use active states: StatusDownload, StatusSeed, StatusStopped, StatusCheck |

## Status Code Mapping

Transmission status codes and their meaning for Activity tab display.

| Status Code | Transmission Meaning | Sonarr/Radarr Display | When to Use |
|-------------|---------------------|----------------------|-------------|
| 0 (StatusStopped) | Torrent paused/stopped | "Paused" or no display | Put.io transfer paused or errored |
| 1 (StatusCheckWait) | Queued for verification | "Queued" | Not applicable to Put.io; avoid |
| 2 (StatusCheck) | Verifying local files | "Verifying" | Not applicable to Put.io; avoid |
| 3 (StatusDownloadWait) | Queued for download | "Queued" | Not applicable to Put.io; avoid |
| 4 (StatusDownload) | Actively downloading | "Downloading" with progress bar | Put.io status: "DOWNLOADING" |
| 5 (StatusSeedWait) | Queued for seeding | "Queued" | Not applicable to Put.io; avoid |
| 6 (StatusSeed) | Actively seeding | "Seeding" or "Completed" | Put.io status: "COMPLETED", "SEEDING" |

**Current mapping (from transmission.go lines 473-486):**
- "completed", "finished" → StatusSeed (6) ✓ Correct
- "seedingwait" → StatusSeedWait (5) - Not used by Put.io
- "seeding" → StatusSeed (6) ✓ Correct
- "downloading" → StatusDownload (4) ✓ Correct
- "checking" → StatusCheck (2) - Not used by Put.io
- Default → StatusStopped (0) ✓ Correct fallback

**Gap:** Put.io likely provides additional status strings not yet mapped. Need to verify Put.io API documentation for complete status enum.

## Progress Calculation

Sonarr/Radarr Activity tab calculates and displays progress using:

**Method 1: percentDone (preferred)**
- Field: `percentDone` (float, 0.0 to 1.0)
- Display: `percentDone * 100` = progress percentage
- Current implementation: Not provided in current TransmissionTorrent struct

**Method 2: leftUntilDone (fallback)**
- Fields: `totalSize` (int64) and `leftUntilDone` (int64)
- Calculation: `((totalSize - leftUntilDone) / totalSize) * 100`
- Current implementation: ✓ Both fields provided (lines 495-496)

**Recommendation:** Continue using Method 2 (leftUntilDone). Put.io provides `size` and `downloaded`, which map cleanly to `totalSize` and `totalSize - leftUntilDone`. Adding `percentDone` is optional but would be redundant.

**Activity tab uses:**
- Progress bar width: `percentDone * 100` or calculated from leftUntilDone
- Remaining size display: `leftUntilDone` formatted as GB/MB
- Total size display: `totalSize` formatted as GB/MB
- Progress text: "{downloaded} / {total} ({percent}%)"

## Polling Behavior

**Sonarr/Radarr polling characteristics:**

| Characteristic | Value | Source |
|---------------|-------|--------|
| Polling interval | 60-90 seconds | Servarr forums, GitHub issues |
| Task name | "Check For Finished Downloads" | Sonarr source code |
| Interval variance | ±30 seconds depending on task execution time | User reports |
| Fields requested | Specific field list via "fields" argument | Sonarr TransmissionProxy.cs |
| Response format | JSON with "torrents" array | Transmission RPC spec |

**Fields requested by Sonarr (from source inspection):**

Based on [Sonarr TransmissionProxy.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/Clients/Transmission/TransmissionProxy.cs), Sonarr requests these fields:

- `id` - Torrent ID
- `hashString` - Torrent info hash
- `name` - Torrent name
- `downloadDir` - Download directory path
- `totalSize` - Total size in bytes
- `leftUntilDone` - Bytes remaining
- `isFinished` - Boolean completion flag
- `eta` - Estimated time remaining (seconds, -1 = unknown, -2 = unavailable)
- `status` - Status code (0-6)
- `secondsDownloading` - Time spent downloading (seconds)
- `secondsSeeding` - Time spent seeding (seconds)
- `errorString` - Error message (empty string if no error)
- `uploadedEver` - Total uploaded bytes
- `downloadedEver` - Total downloaded bytes (can differ from progress if restarted)
- `seedRatioLimit` - Seed ratio limit
- `seedRatioMode` - Seed ratio mode (0=global, 1=single, 2=unlimited)
- `seedIdleLimit` - Idle time limit (minutes)
- `seedIdleMode` - Idle time mode (0=global, 1=single, 2=unlimited)
- `fileCount` or `file-count` - Number of files in torrent
- `labels` - Array of label strings (Transmission 3.x+)

**Current implementation status:**
- ✓ Implemented: id, hashString, name, downloadDir, totalSize, leftUntilDone, isFinished, eta, status, errorString, downloadedEver, fileCount, seedRatioLimit, seedRatioMode, seedIdleLimit, seedIdleMode
- ✗ Missing: secondsDownloading, secondsSeeding, uploadedEver, labels
- ✗ Missing: rateDownload, rateUpload (not in requested fields but useful for speed display)

## Error Handling

Sonarr/Radarr Activity tab handles these error states:

| Field | Value | Activity Tab Behavior |
|-------|-------|----------------------|
| `errorString` | Non-empty string | Shows warning icon with error message on hover |
| `errorString` | Empty or null | No error displayed |
| `isStalled` | true | May show "Stalled" warning (client-dependent) |
| `status` | 0 (Stopped) | Shows "Paused" or grays out entry |
| `eta` | -1 | Shows "Unknown" for ETA |
| `eta` | -2 | Shows "N/A" for ETA |
| `leftUntilDone` | 0 but isFinished=false | May show as stalled or checking |

**Current implementation:**
- ✓ errorString populated from Put.io transfer.ErrorMessage (line 500)
- ✗ isStalled not implemented (differentiator, not required)
- ✓ eta mapped from Put.io estimated_time (line 498)
- ✓ status mapped correctly for stopped state (line 485)

## Feature Dependencies

```
Core Activity Display
├── Return in-progress transfers (BLOCKS all other features)
├── status field (REQUIRES status mapping logic)
├── Progress calculation
│   ├── totalSize (ALREADY IMPLEMENTED)
│   └── leftUntilDone (ALREADY IMPLEMENTED)
├── ETA display (ALREADY IMPLEMENTED)
└── Error display (ALREADY IMPLEMENTED)

Enhanced Activity Display (optional)
├── Speed display
│   ├── rateDownload (REQUIRES Put.io API verification)
│   └── rateUpload (REQUIRES Put.io API verification)
├── Time tracking
│   ├── secondsDownloading (REQUIRES start time or duration from Put.io)
│   └── secondsSeeding (REQUIRES seeding duration from Put.io)
└── Stall detection
    └── isStalled (REQUIRES progress tracking state)
```

## MVP Recommendation

For MVP (minimal Activity tab functionality), prioritize:

1. **Return in-progress transfers** - Currently filtered out; must include downloading status
2. **Verify status mapping** - Current mapping looks correct but needs Put.io status enum verification
3. **Verify progress fields** - totalSize and leftUntilDone already implemented; validate calculation

**Reasoning:** These three changes enable basic Activity tab display. Current code already has 90% of required fields implemented.

## Post-MVP Enhancements

Defer to future milestones:

1. **Speed display** (rateDownload, rateUpload) - Requires Put.io API research; nice-to-have
2. **Time tracking** (secondsDownloading, secondsSeeding) - Requires state persistence; nice-to-have
3. **Stall detection** (isStalled) - Requires progress monitoring; nice-to-have
4. **Upload tracking** (uploadedEver) - Requires Put.io API research; needed for seeding ratio display
5. **Labels array** - Transmission 3.x+ feature; verify if Sonarr uses for filtering

## Research Confidence Assessment

| Area | Confidence | Evidence | Gaps |
|------|------------|----------|------|
| Required fields | HIGH | Sonarr source code inspection, Transmission RPC spec | None |
| Status code mapping | HIGH | Transmission RPC spec, Sonarr source code | Put.io status enum verification needed |
| Progress calculation | HIGH | Transmission RPC spec, multiple sources agree | None |
| Polling interval | MEDIUM | User reports, Sonarr forum discussions | No official documentation found |
| Speed fields (rate*) | LOW | Transmission RPC spec only; Put.io API not verified | Put.io API documentation needed |
| Time tracking fields | LOW | Transmission RPC spec only; Put.io API not verified | Put.io API documentation needed |

## Open Questions

1. **What is the complete Put.io transfer status enum?** - Current mapping handles common states but may miss edge cases
2. **Does Put.io provide download/upload rate?** - Would enable speed display in Activity tab
3. **Does Put.io provide transfer start time or duration?** - Would enable secondsDownloading/secondsSeeding
4. **Does Put.io track uploaded bytes during seeding?** - Would enable uploadedEver and seed ratio display
5. **Should we implement the "fields" argument?** - Sonarr passes specific field list; currently ignored and all fields returned
6. **Does Sonarr/Radarr use the labels array?** - Transmission 3.x added labels; verify if Sonarr filters by labels or just checks category

## Next Steps for Implementation

1. **Verify Put.io API** - Research Put.io API documentation for:
   - Complete status enum
   - Download/upload rate availability
   - Transfer duration/start time
   - Upload byte tracking

2. **Modify GetTaggedTorrents filter** - Currently filters to completed only; must include in-progress

3. **Add missing fields** - If Put.io provides:
   - secondsDownloading (from transfer duration)
   - uploadedEver (from seeding stats)
   - rateDownload / rateUpload (from current speeds)

4. **Test with Sonarr/Radarr** - Verify Activity tab displays correctly with test transfers

5. **Handle edge cases** - Verify status mapping for:
   - Paused transfers
   - Failed/errored transfers
   - Magnet link metadata download phase

## Sources

### HIGH Confidence Sources (Official/Authoritative)

- [Transmission RPC Specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md) - Official RPC protocol documentation
- [Sonarr TransmissionProxy.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/Clients/Transmission/TransmissionProxy.cs) - Sonarr source code for Transmission integration
- [Transmission RPC Status Codes Discussion](https://github.com/transmission/transmission/discussions/5343) - Official status code meanings
- [Transmission-RPC Python Documentation](https://transmission-rpc.readthedocs.io/en/v3.4.0/torrent.html) - Field definitions and descriptions

### MEDIUM Confidence Sources (Community/Wiki)

- [Sonarr Activity Wiki](https://wiki.servarr.com/sonarr/activity) - Official Servarr wiki for Activity tab
- [Sonarr Activity Queue Refresh Discussion](https://forums.sonarr.tv/t/activity-queue-refresh-time/21798) - Polling interval information
- [Transmission RPC Markdown Spec](https://gist.github.com/RobertAudi/807ec699037542646584) - Community-formatted RPC spec

### LOW Confidence Sources (Unverified)

- Various Sonarr/Radarr GitHub issues mentioning Activity tab behavior
- Forum discussions about Transmission integration challenges

---
*Feature research for: Activity Tab Support (v1.3)*
*Researched: 2026-02-08*
*Confidence: MEDIUM (verified via source code + official specs; Put.io API details needed)*
