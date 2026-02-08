# Project Research Summary

**Project:** Seedbox Downloader - Activity Tab Support
**Milestone:** v1.3
**Domain:** Transmission RPC proxy enhancement (Put.io -> Sonarr/Radarr Activity tab)
**Researched:** 2026-02-08
**Confidence:** HIGH

## Executive Summary

The seedbox downloader's Transmission RPC proxy currently filters out in-progress downloads (transfers with `FileID == 0`), rendering Sonarr/Radarr Activity tabs empty during active downloads. Research across all four dimensions converges on one conclusion: **this is a surgical change, not a rewrite.** The existing codebase already has 90% of the required infrastructure -- status mapping, progress fields, Transmission struct conversion -- all present and correct. The core fix is removing a single filter in `GetTaggedTorrents` and using `SaveParentID` instead of `FileID` for label matching on in-progress transfers. No new dependencies are required.

The recommended approach modifies `internal/dc/putio/client.go` to stop skipping `FileID == 0` transfers, uses the `SaveParentID` field (available during download) for parent folder tag matching, and conditionally populates the `Files` array only for completed transfers. The existing `IsAvailable()` and `IsDownloadable()` methods in the download pipeline provide a double safety net -- in-progress transfers will appear in the Activity tab but will never accidentally trigger the download orchestrator. The Transmission status mapping already handles "downloading" -> `StatusDownload (4)`, and the progress calculation via `leftUntilDone = size - downloaded` is already implemented.

The primary risks are: (1) accidentally breaking the download pipeline by removing the FileID filter without understanding that the orchestrator depends on it, (2) hitting Put.io rate limits as Sonarr/Radarr poll the Activity tab every 1-2 seconds instead of the current low-frequency pattern, and (3) the `SaveParentID` lifecycle behavior being undocumented (unclear if it persists after completion). All three are manageable: the pipeline has double protection via `IsAvailable()` + `IsDownloadable()`, rate limits can be addressed with short-lived response caching if needed, and the SaveParentID gap can be validated during Phase 1 implementation.

## Key Findings

### Recommended Stack

No new dependencies required. The existing stack is fully sufficient.

**Core technologies (unchanged):**
- **go-putio v1.7.2:** Put.io SDK -- Transfer struct already contains `SaveParentID`, `PercentDone`, `DownloadSpeed`, `PeersConnected`, and all fields needed for Activity tab display
- **Go 1.23 stdlib:** Core language -- no version upgrade needed
- **Chi v5 router:** HTTP routing for Transmission RPC -- no changes needed
- **SQLite:** Transfer state storage -- no schema changes required
- **OpenTelemetry:** Observability -- existing instrumentation covers the change

**Key stack finding:** The Put.io Transfer struct has all the fields needed for in-progress display. `SaveParentID` is the critical field that enables label matching without `FileID`. Peer stats (`PeersConnected`, `PeersSendingToUs`, `PeersGettingFromUs`) and speed (`DownloadSpeed`) are available in the SDK but not currently mapped to Transmission fields.

### Expected Features

**Must have (table stakes -- Activity tab is broken without these):**
- Return in-progress transfers from `torrent-get` -- currently filtered out; this is THE blocker
- Correct `status` field mapping -- "downloading" -> StatusDownload (4) already works
- Progress calculation via `totalSize` and `leftUntilDone` -- already implemented
- `eta` field -- already mapped from Put.io `EstimatedTime`
- `name`, `id`, `hashString`, `downloadDir` -- all already implemented

**Should have (enhanced Activity tab experience):**
- `rateDownload` / `rateUpload` -- Put.io provides `DownloadSpeed`; enables speed display
- `peersConnected` / `peersSendingToUs` -- Put.io provides these; enables peer health display
- `errorString` populated for ERROR status -- enables error visibility in Activity tab
- `labels` array in Transmission response -- needed for Sonarr/Radarr category filtering
- Complete status mapping including `in_queue` -> StatusDownloadWait (3) and `error` -> StatusStopped with ErrorString

**Defer (v2+):**
- `secondsDownloading` / `secondsSeeding` -- Put.io does not natively track these durations
- `isStalled` detection -- requires progress tracking state over time
- `uploadedEver` -- requires Put.io upload byte tracking verification
- Response caching layer -- only add if rate limit issues surface in production
- `labels` field support for multi-instance filtering -- verify Sonarr actually uses this

### Architecture Approach

The architecture is already well-structured for this change. The Transmission RPC proxy path (`GetTaggedTorrents` -> `handleTorrentGet` -> Sonarr) and the download pipeline path (`GetTaggedTorrents` -> `TransferOrchestrator` -> `IsAvailable()` filter -> Download) are independent consumers of the same data source. The key insight: **modify the data source to return everything, rely on existing downstream filters to protect the pipeline.**

**Major components (changes needed):**
1. **GetTaggedTorrents** (`putio/client.go`) -- Remove `FileID == 0` filter, add SaveParentID-based tag matching, conditional file population (~30 lines changed)
2. **handleTorrentGet** (`transmission.go`) -- Improve status mapping to cover `in_queue`, `waiting`, `error`, `finishing` states (~15 lines changed)

**Major components (NO changes needed):**
3. **TransferOrchestrator** (`transfer/transfer.go`) -- `IsAvailable()` already filters non-completed statuses; `IsDownloadable()` already filters empty Files arrays. Double safety net remains intact.
4. **TransmissionTorrent struct** (`transmission.go`) -- Existing fields support in-progress display. Optional: add peer/rate fields.

### Critical Pitfalls

1. **Breaking the download pipeline** -- The most dangerous pitfall. `GetTaggedTorrents` is shared between Activity display and the download orchestrator. If in-progress transfers leak into the download path, partial files get downloaded, imports fail, and database state corrupts. **Prevention:** `IsAvailable()` + `IsDownloadable()` provide double protection. Verify both filters in integration tests before shipping.

2. **Put.io rate limit exhaustion** -- Sonarr/Radarr poll Activity every 1-2 seconds. Each poll triggers `Transfers.List()` + N `Files.Get()` calls. With 5 active transfers at 2-second intervals, that is ~330 API calls/minute against a ~100-200 req/min limit. **Prevention:** Monitor API call rates in initial rollout. Add response caching (5-10 second TTL) if limits are approached.

3. **Incomplete status mapping** -- `in_queue` and `error` statuses fall through to `StatusStopped` default, making queued transfers look paused and hiding errors. **Prevention:** Map all known Put.io statuses explicitly; log warnings for unmapped statuses.

4. **FileID timing gap** -- Fresh transfers have `FileID == 0` and `SaveParentID` set simultaneously. But the current code skips them entirely. After the fix, there is still a 5-30 second window where transfers might have `SaveParentID == 0` if Put.io has not assigned a destination folder yet. **Prevention:** Handle `SaveParentID == 0` gracefully by skipping with a debug log.

5. **Progress calculation edge cases** -- `Downloaded > Size` can produce negative `leftUntilDone` (compression mismatch). **Prevention:** Clamp `leftUntilDone` to `[0, size]` range; prefer Put.io `PercentDone` as source of truth.

## Consensus

All four research dimensions agree on these points:

- **No new dependencies required.** Stack, Architecture, and Features research all confirm the existing codebase has everything needed.
- **The core change is ~30 lines in `putio/client.go`.** All researchers converge on removing the `FileID == 0` filter and using `SaveParentID` for tag matching.
- **The download pipeline is safe.** `IsAvailable()` + `IsDownloadable()` provide a double safety net that all researchers identify and validate.
- **Status mapping already mostly works.** The existing `transmission.go` mapping handles `"downloading"` -> `StatusDownload (4)` correctly. Only gap states (`in_queue`, `error`, `finishing`) need additions.
- **`handleTorrentGet` needs no structural changes.** The Transmission response struct already supports all required fields for Activity tab display.
- **SaveParentID is the key enabler.** All researchers identify this field as the solution to label matching without FileID.

## Disagreements / Tensions

### Separate method vs. modify GetTaggedTorrents

**PITFALLS research** recommends creating a separate `GetAllTransfersByTag()` method for the Activity tab, keeping `GetTaggedTorrents()` for completed-only (download pipeline). This is the defensive approach -- explicit separation of concerns.

**ARCHITECTURE research** recommends modifying the existing `GetTaggedTorrents()` to return all transfers, relying on `IsAvailable()` + `IsDownloadable()` downstream filters. This is the simpler approach -- less code, no duplication.

**Resolution:** ARCHITECTURE wins. The double safety net (`IsAvailable()` returns false for "downloading" status AND `IsDownloadable()` returns false for empty Files) makes the separate method unnecessary. Creating a second method would duplicate tag matching logic, API calls, and Transfer conversion -- violating DRY for marginal safety. The integration tests should verify the safety net holds.

### Rate limiting: proactive caching vs. monitor-first

**PITFALLS research** warns urgently about rate limits (330 calls/min) and recommends implementing caching BEFORE exposing the Activity tab.

**FEATURES research** recommends sticking to stateless HTTP polling (current approach) and explicitly lists caching as an anti-feature because "stale data breaks Activity tab accuracy."

**Resolution:** Monitor-first approach, but with a ready-to-deploy caching plan. Sonarr/Radarr's actual polling interval may be 60-90 seconds (not 1-2 seconds as PITFALLS warns). The 1-2 second figure is for the Activity tab UI refresh, not the API polling interval. Implement monitoring for API call rates in Phase 2; prepare a 5-second TTL cache as a contingency for Phase 3 if rate limits are hit.

### Queue states: use them or avoid them

**FEATURES research** recommends avoiding queue states (StatusDownloadWait, StatusSeedWait) because "Put.io doesn't have a queue."

**ARCHITECTURE and STACK research** recommend mapping `in_queue`/`waiting` to `StatusDownloadWait (3)` for more accurate display.

**Resolution:** ARCHITECTURE wins. Put.io DOES have queue states (`in_queue`, `waiting`). Mapping them to `StatusDownloadWait` is more accurate than the current fallthrough to `StatusStopped`. The Activity tab will display "Queued" instead of "Paused" -- a clear improvement.

## Risk Matrix

| Risk | Severity | Likelihood | Phase | Mitigation |
|------|----------|------------|-------|------------|
| Download pipeline processes incomplete transfers | CRITICAL | VERY LOW | 2 | IsAvailable() + IsDownloadable() double safety net; integration tests |
| Put.io rate limit exhaustion | HIGH | LOW-MEDIUM | 2-3 | Monitor API call rates; prepare 5s TTL cache as contingency |
| SaveParentID == 0 for fresh transfers | MEDIUM | LOW | 2 | Skip transfer with debug log; handle gracefully |
| Negative leftUntilDone from compression | MEDIUM | LOW | 2 | Clamp to [0, size]; prefer PercentDone as source of truth |
| Unmapped Put.io status falls to StatusStopped | MEDIUM | MEDIUM | 2 | Map all known statuses; warn-log unknown statuses |
| Transfer completes between polls (race condition) | MEDIUM | LOW | 3 | Database lookup for recently completed transfers |
| Missing labels field breaks Sonarr category filter | LOW | MEDIUM | 2 | Add labels array to TransmissionTorrent response |
| SaveParentID not persisted after completion | LOW | UNKNOWN | 1 | Conditional logic: SaveParentID for in-progress, FileID->ParentID for completed |
| Peer/rate fields showing 0 for active downloads | LOW | HIGH | 2 | Map Put.io PeersConnected and DownloadSpeed to Transmission fields |
| ETA unit mismatch | LOW | LOW | 2 | Verify Put.io EstimatedTime is seconds; handle negative values |

## Open Questions

### Must resolve before implementation

1. **Does SaveParentID persist after transfer completion?**
   - If YES: simplify to always use SaveParentID for tag matching (fewer API calls)
   - If NO: keep conditional logic (SaveParentID for in-progress, FileID->ParentID for completed)
   - **Resolution plan:** Log SaveParentID values for completed transfers during Phase 1 validation

2. **What is Sonarr/Radarr's actual API polling interval?**
   - FEATURES research says 60-90 seconds; PITFALLS research warns about 1-2 seconds
   - **Resolution plan:** Enable request logging and measure actual polling frequency in Phase 2

3. **Does Files.Get(SaveParentID) reliably return the parent folder?**
   - Critical for tag matching on in-progress transfers
   - **Resolution plan:** Validate with a real in-progress transfer during Phase 1

### Can defer to implementation

4. **Does Put.io provide download/upload rate fields?** -- DownloadSpeed exists in SDK struct; verify it is populated for active transfers
5. **Complete list of Put.io transfer status strings?** -- 8 known statuses mapped; add warn-log for discovery of unknown statuses
6. **Should TransmissionTorrent include labels array?** -- Verify if Sonarr uses labels for category filtering
7. **Does Transfer.IsAvailable() need review?** -- Currently checks completed/seeding/finished; confirm it does NOT include "downloading"

## Implications for Roadmap

Based on the combined research, this milestone should be structured as three phases with an optional fourth. The total scope is small (~50 lines of production code) but the risk surface requires careful ordering.

### Phase 1: SaveParentID Tag Matching (Foundation)
**Rationale:** Validates the core assumption (SaveParentID works for tag matching) before touching the FileID filter. Minimizes blast radius -- changes only the folder lookup method, not the filter logic.
**Delivers:** Tag matching works via SaveParentID for completed transfers (existing behavior preserved, just different code path).
**Addresses:** Open Question #1 (SaveParentID persistence) and #3 (Files.Get reliability)
**Avoids:** Breaking download pipeline (Pitfall #1) -- FileID filter stays in place during this phase
**Risk:** VERY LOW -- only changes how parent folder is looked up, not what is returned

### Phase 2: In-Progress Visibility (Core Feature)
**Rationale:** With SaveParentID validated in Phase 1, safely remove the FileID filter and add conditional file population. This is the actual feature delivery.
**Delivers:** Activity tab shows in-progress downloads with progress, status, ETA. Download pipeline remains unaffected.
**Addresses:** All table-stakes features (in-progress visibility, status mapping, progress display)
**Implements:** GetTaggedTorrents modification, complete status mapping, peer/rate field mapping, labels field, progress clamping
**Avoids:** Pipeline false positives (Pitfall #1), status mapping errors (Pitfall #2), progress calculation errors (Pitfall #3), FileID timing gap (Pitfall #5)
**Risk:** LOW -- IsAvailable() + IsDownloadable() double safety net

### Phase 3: Validation and Edge Cases
**Rationale:** With the feature working, verify edge cases and add robustness measures.
**Delivers:** Integration-tested Activity tab with Sonarr/Radarr, error state visibility, race condition handling.
**Addresses:** Error state visibility (Pitfall #9), transfer completion race condition (Pitfall #7), API call rate monitoring
**Avoids:** Rate limit violations (Pitfall #4) -- measured and addressed here
**Risk:** VERY LOW -- validation and polish only

### Phase 4: Performance Optimization (Contingency)
**Rationale:** Only needed if Phase 3 monitoring reveals rate limit issues.
**Delivers:** Response caching with 5-10 second TTL, API call batching.
**Addresses:** Rate limit prevention (Pitfall #4), performance at scale
**Risk:** LOW -- standard caching pattern
**Note:** Skip this phase if monitoring shows rate limits are not a concern.

### Phase Ordering Rationale

- **Phase 1 first** because SaveParentID is the foundational assumption. If it does not work as expected, the entire approach needs rethinking.
- **Phase 2 second** because it delivers the actual user-visible feature and depends on Phase 1 validation.
- **Phase 3 third** because edge cases and integration testing require the feature to be functional.
- **Phase 4 contingent** because rate limits may not be an issue -- measure before optimizing.

### Research Flags

Phases likely needing validation during implementation:
- **Phase 1:** Needs empirical validation of SaveParentID behavior (undocumented lifecycle). Run manual tests with real Put.io transfers.
- **Phase 3:** Needs real Sonarr/Radarr instance to verify Activity tab rendering. Cannot be validated with unit tests alone.

Phases with standard patterns (skip deeper research):
- **Phase 2:** Well-documented Transmission RPC spec. Status mapping is mechanical. File population logic is straightforward conditional.
- **Phase 4:** Standard HTTP response caching pattern. Many reference implementations available.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | No new dependencies. All required fields verified in existing go-putio v1.7.2 SDK and codebase. |
| Features | HIGH | Sonarr source code inspected (TransmissionProxy.cs). Transmission RPC spec consulted. Required fields clearly documented. |
| Architecture | HIGH | Existing codebase analysis confirms clean separation between Activity display and download pipeline. IsAvailable()/IsDownloadable() double safety net verified in source. |
| Pitfalls | HIGH | Based on codebase analysis, official API docs, and real community projects (putioarr, plundrio). Rate limit concern is the main uncertainty. |

**Overall confidence:** HIGH

The research is well-grounded because the change is small and the existing codebase is well-understood. The primary uncertainty is SaveParentID lifecycle behavior, which is empirically testable in Phase 1.

### Gaps to Address

- **SaveParentID lifecycle:** Not documented by Put.io. Must validate empirically during Phase 1 by logging values for both in-progress and completed transfers.
- **Actual polling frequency:** Conflicting information (60-90s vs 1-2s). Must measure during Phase 2/3 with request logging already in place from v1.2 milestone.
- **Put.io rate limits:** Exact limits undocumented. Community reports suggest 100-200 req/min. Must monitor during Phase 3.
- **Labels field usage:** Unclear if Sonarr filters by Transmission `labels` array or relies solely on `downloadDir` for category matching. Test during Phase 3 integration.

## Sources

### Primary (HIGH confidence)
- [Transmission RPC Specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md) -- Status codes, torrent-get fields, response format
- [Sonarr TransmissionProxy.cs](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/Clients/Transmission/TransmissionProxy.cs) -- Fields Sonarr requests from Transmission
- [go-putio package](https://pkg.go.dev/github.com/putdotio/go-putio) -- Transfer struct fields, SDK v1.7.2
- Existing codebase analysis: `internal/dc/putio/client.go`, `internal/http/rest/transmission.go`, `internal/transfer/transfer.go`

### Secondary (MEDIUM confidence)
- [putioarr](https://github.com/wouterdebie/putioarr) -- Rust Put.io proxy, similar patterns
- [plundrio](https://github.com/elsbrock/plundrio) -- Go Put.io Transmission proxy, reference implementation
- [Servarr Wiki - Activity](https://wiki.servarr.com/sonarr/activity) -- Activity tab behavior
- [Sonarr forums](https://forums.sonarr.tv/) -- Polling intervals, category filtering issues
- [Put.io "Finishing" status](https://help.put.io/en/articles/8005984-what-does-finishing-mean) -- Transfer state explanations

### Tertiary (LOW confidence)
- Put.io official API docs (referenced but not directly accessed for rate limit specifics)
- SaveParentID lifecycle behavior (undocumented, needs empirical validation)
- Sonarr/Radarr actual polling interval (community reports vary)

---
*Research completed: 2026-02-08*
*Ready for roadmap: yes*
