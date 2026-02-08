# Domain Pitfalls: Activity Tab Support

**Domain:** Adding in-progress download visibility to Transmission RPC proxy (Put.io → Transmission)
**Researched:** 2026-02-08
**Confidence:** HIGH

## Critical Pitfalls

Mistakes that cause rewrites, data corruption, or breaking existing functionality.

### Pitfall 1: Breaking Existing Download Pipeline by Changing GetTaggedTorrents Filter

**What goes wrong:**
The existing download pipeline (TransferOrchestrator → Downloader → Import Monitor → Cleanup) depends on `GetTaggedTorrents()` returning **only completed transfers** via the `IsAvailable()` filter. This filter currently checks `status == "completed" || status == "seeding" || status == "seedingwait"`. If you modify this method to also return in-progress transfers (status == "downloading"), the download orchestrator will attempt to download incomplete files from Put.io, resulting in:
- Partial file downloads that fail import
- Database corruption (transfers marked as downloaded when they aren't)
- Duplicate download attempts when the transfer actually completes
- Sonarr/Radarr rejecting imports due to incomplete files

**Why it happens:**
The Transmission RPC proxy and the internal download pipeline both call the same `GetTaggedTorrents()` method. Developers see "we need in-progress transfers for the Activity tab" and modify the filter without realizing the download orchestrator also uses this method. The temptation is to change the shared filter because it's the simplest code path, avoiding duplication.

**Consequences:**
- Download orchestrator processes incomplete transfers → partial downloads → failed imports
- Database contains wrong state (transfer marked "downloaded" but file incomplete)
- Transfer completes later → orchestrator sees it again → duplicate download attempt
- Sonarr/Radarr show import failures in Activity tab
- Cleanup logic may delete in-progress transfers from Put.io

**Prevention:**
1. **Create separate methods:** Keep `GetTaggedTorrents()` for completed-only (download pipeline), add `GetAllTransfersByTag()` for Transmission proxy (includes in-progress)
2. **Document method contracts:** Add godoc comments specifying which callers depend on completed-only behavior
3. **Add integration tests:** Verify download orchestrator only processes completed transfers
4. **Review all call sites:** Before changing a filter, grep for all callers and understand their requirements

**Detection (warning signs):**
- Download orchestrator logs showing "transfer ready for download" for transfers with status != "completed"
- Import failures in Sonarr/Radarr Activity tab
- Database queries showing transfers in "pending" state that have been "downloaded"
- Put.io transfers disappearing before completion (cleanup ran prematurely)
- Trace logs showing `IsAvailable() = true` for transfers with status "downloading"

**Which phase should address this:**
Phase 1 (Design & Contracts) - Define interface contracts before implementation

---

### Pitfall 2: Incorrect Status Mapping Between Put.io and Transmission State Machines

**What goes wrong:**
Put.io and Transmission use different status vocabularies and state machines. Put.io uses status strings like "DOWNLOADING", "SEEDING", "COMPLETING", "ERROR", "WAITING", while Transmission uses integer codes 0-6 (stopped, check wait, check, download wait, download, seed wait, seed). The existing proxy maps only completed states:
```go
case "completed", "finished": status = StatusSeed
case "seeding": status = StatusSeed
case "downloading": status = StatusDownload
default: status = StatusStopped
```

This creates several failure modes:
1. **Put.io "COMPLETING" → Transmission "stopped":** Transfer is finishing (writing to storage) but shows as stopped in Sonarr/Radarr Activity tab
2. **Put.io "WAITING" → Transmission "stopped":** Transfer is queued but shows as stopped, making it look like it failed
3. **Put.io "ERROR" → Transmission "stopped":** Transfer failed but shows as stopped, hiding the error state
4. **No queue states:** Transmission has dedicated queue states (StatusDownloadWait = 3, StatusSeedWait = 5) which are never used

**Why it happens:**
The developer doesn't understand both state machines completely. Put.io's API documentation may not list all possible status values, so the mapping is based on observed states. The default case catches unmapped states and assigns StatusStopped, which is incorrect for transient states like "COMPLETING" or "WAITING".

**Consequences:**
- Sonarr/Radarr Activity tab shows misleading status ("stopped" when actually downloading)
- Operators can't distinguish between queued, downloading, completing, and errored transfers
- Error states are hidden (transfer shows "stopped" instead of error message)
- No visual feedback for queue position in Activity tab

**Prevention:**
1. **Document all Put.io status values:** Research complete list from API docs, go-putio source, or by logging observed values
2. **Map to appropriate Transmission states:**
   - "DOWNLOADING" → StatusDownload (4)
   - "SEEDING" → StatusSeed (6)
   - "WAITING", "IN_QUEUE" → StatusDownloadWait (3)
   - "COMPLETING", "FINISHING" → StatusSeedWait (5) or StatusCheck (2)
   - "ERROR" → StatusStopped (0) BUT with ErrorString populated
   - "COMPLETED" → StatusSeed (6)
3. **Log unmapped states:** Add warning log when default case is hit with the actual status value
4. **Add telemetry:** Track frequency of each Put.io status to discover unmapped states

**Detection (warning signs):**
- Activity tab showing "stopped" for transfers that are actively downloading in Put.io UI
- Log warnings: "unknown status encountered: COMPLETING"
- Telemetry showing high counts for default status mapping case
- User reports: "Sonarr says download is stopped but it's still downloading"

**Which phase should address this:**
Phase 1 (Design & Contracts) - Complete status mapping matrix before implementation

---

### Pitfall 3: Progress Calculation Mismatch Between Put.io PercentDone and Transmission leftUntilDone

**What goes wrong:**
Put.io provides `PercentDone` (0-100 float) while Transmission uses `leftUntilDone` (bytes remaining). The current proxy calculates:
```go
LeftUntilDone: transfer.Size - transfer.Downloaded
```

This creates accuracy problems:
1. **Negative values:** If `Downloaded > Size` (happens when Put.io reports compressed vs uncompressed), `leftUntilDone` becomes negative, breaking Activity tab UI
2. **Rounding errors:** Put.io's `PercentDone` (e.g., 53.7%) doesn't align with byte-level `Downloaded` value, showing inconsistent progress
3. **No verification:** Put.io transfers include verification after download, but `Downloaded` reaches `Size` before verification completes, showing 100% when still verifying
4. **Missing relationship:** Transmission expects `percentDone = (sizeWhenDone - leftUntilDone) / sizeWhenDone`, but Put.io's PercentDone may calculate differently

**Why it happens:**
The proxy tries to synthesize Transmission fields from Put.io fields without understanding their precise definitions. The assumption is simple arithmetic: "if 1GB total and 500MB downloaded, then 500MB left" but this doesn't account for:
- Compression ratios (reported size vs actual download size)
- Verification phase (download complete but transfer not finished)
- Put.io's internal calculation of PercentDone (may include verification time)

**Consequences:**
- Activity tab showing negative progress (e.g., "-50% remaining")
- Progress jumping backwards (verification phase shows as incomplete)
- Sonarr/Radarr removing transfers from queue when progress appears invalid
- Inconsistent ETA calculations (based on incorrect remaining bytes)

**Prevention:**
1. **Use Put.io's PercentDone as source of truth:** Calculate `leftUntilDone = size * (1 - percentDone/100)`
2. **Clamp to valid range:** Ensure `leftUntilDone >= 0` and `leftUntilDone <= size`
3. **Handle edge cases explicitly:**
   - If PercentDone == 100 but status != "completed", set leftUntilDone = 0
   - If Downloaded > Size, use PercentDone for calculation instead of Downloaded
4. **Add validation:** Assert relationships in tests: `percentDone = 100 * (size - leftUntilDone) / size`
5. **Log discrepancies:** When Downloaded and PercentDone disagree significantly, log for investigation

**Detection (warning signs):**
- Activity tab showing negative progress values
- Progress jumping from 100% back to 95% (verification phase)
- Logs showing: `Downloaded=1100MB Size=1000MB` (impossible state)
- Transfers showing "completed" in Transmission but still downloading in Put.io

**Which phase should address this:**
Phase 2 (Implementation) - Progress calculation with validation and edge case handling

---

### Pitfall 4: Excessive Polling Frequency Hitting Put.io Rate Limits

**What goes wrong:**
Adding in-progress transfer visibility means Sonarr/Radarr will poll the Transmission proxy more frequently (every 1-2 seconds for Activity tab updates) instead of the current 10-minute interval (only when checking for completion). Each proxy request calls `GetTaggedTorrents()` which makes:
1. `Transfers.List()` API call to Put.io
2. For each transfer with FileID: `Files.Get(fileID)` to get file metadata
3. For each file: `Files.Get(parentID)` to check parent folder name

With 5 active transfers, this is 11 API calls per poll. At 2-second intervals, that's **330 API calls per minute** just for the Activity tab. Put.io's rate limit is typically 100-200 requests per minute per account.

**Why it happens:**
The existing polling architecture was designed for low-frequency checks (every 10 minutes). The code makes synchronous API calls without caching or batching. Developers don't realize Sonarr/Radarr poll the Activity tab endpoint much more aggressively than the periodic checks. The problem surfaces only in production when rate limit errors start appearing.

**Consequences:**
- HTTP 429 (Too Many Requests) responses from Put.io
- Activity tab showing stale data or "connection failed"
- Other Put.io integrations (web UI, mobile app) also hit rate limits
- Exponential backoff causing longer delays between updates
- Account temporarily blocked if rate limit violations are severe

**Prevention:**
1. **Implement response caching:** Cache `GetTaggedTorrents()` response for 5-10 seconds, serve cached data to multiple clients
2. **Batch API calls:** Use `Files.List(parentID)` instead of individual `Files.Get()` calls when fetching multiple files
3. **Rate limit protection:** Track API calls per minute, reject requests if approaching limit (return cached data instead)
4. **Add metrics:** Track `api_calls_per_minute` counter, alert when approaching rate limit
5. **Optimize FileID check:** Skip File API calls for transfers without FileID (currently filtered but still requires API call)
6. **Use conditional requests:** If Put.io supports ETags, use If-None-Match headers to skip unchanged responses

**Detection (warning signs):**
- HTTP 429 errors in logs from Put.io API
- Activity tab showing "Failed to load queue" intermittently
- Logs showing `api_calls_per_minute > 150`
- Put.io web UI becoming slow or unresponsive
- Exponential backoff delays in logs

**Which phase should address this:**
Phase 2 (Implementation) - Add caching layer before exposing to Activity tab

---

### Pitfall 5: FileID Requirement Creates Timing Gap for Fresh Transfers

**What goes wrong:**
The current filter skips transfers with `FileID == 0`:
```go
if t.FileID == 0 {
    logger.DebugContext(ctx, "skipping transfer because it's not a downloadable transfer")
    continue
}
```

When Sonarr/Radarr adds a new transfer via `torrent-add`, Put.io creates the transfer immediately but doesn't assign a FileID until the transfer starts downloading. This creates a timing gap (5-30 seconds) where:
1. Sonarr/Radarr adds torrent → receives success response with transfer ID
2. Sonarr polls Activity tab → transfer has FileID == 0 → filtered out
3. Activity tab shows "queue is empty" despite transfer existing
4. Transfer starts downloading → FileID assigned → appears in Activity tab

**Why it happens:**
Put.io's transfer creation is asynchronous. The transfer enters a "WAITING" state while Put.io fetches metadata, finds peers, and allocates storage. Only when download begins does Put.io create the file structure and assign FileID. The filter was designed to skip stuck transfers (those that never get FileID) but it also filters fresh transfers.

**Consequences:**
- Activity tab shows "queue is empty" immediately after adding torrent
- User confusion: "I just added it, where is it?"
- GitHub issues: "Activity tab not showing in-progress downloads"
- Race condition: If transfer starts quickly (<2 seconds), it might appear immediately; if slow (>10 seconds), it disappears temporarily
- Inconsistent UX between fast and slow trackers

**Prevention:**
1. **Remove FileID == 0 filter for in-progress status:** Allow transfers with status "WAITING" or "DOWNLOADING" even if FileID == 0
2. **Keep FileID filter for completed status:** Only completed transfers must have FileID (because download orchestrator needs files)
3. **Add status-based filtering:**
   ```go
   if t.FileID == 0 && (status == "completed" || status == "seeding") {
       continue // Completed without files = stuck transfer
   }
   // Allow FileID == 0 for "waiting" and "downloading" states
   ```
4. **Populate Files array conditionally:** If FileID == 0, set `Files: []*File{}` (empty array, not nil)
5. **Add logging:** Log when FileID == 0 transfers are included, track how long they stay in that state

**Detection (warning signs):**
- User reports: "Activity tab empty after adding torrent"
- Logs showing: "skipping transfer because it's not a downloadable transfer" for transfers in "WAITING" status
- Telemetry showing high count of FileID == 0 transfers in first 30 seconds after creation
- Activity tab flickering (transfer appears, disappears, reappears as FileID is assigned)

**Which phase should address this:**
Phase 2 (Implementation) - Status-aware FileID filtering logic

---

## Moderate Pitfalls

Mistakes that cause delays, confusion, or workarounds but don't break core functionality.

### Pitfall 6: Sonarr/Radarr Queue Filter Expects Category/Label Matching

**What goes wrong:**
Sonarr/Radarr filter the Activity queue by the download client's "category" or "label" setting. If the Transmission proxy returns torrents without matching labels, or if the label doesn't match the configured value in Sonarr/Radarr settings, the Activity tab shows "queue is empty" even though torrents are returned.

The current proxy uses the configured label for filtering Put.io transfers (via parent folder name) but doesn't set the `Labels` field in the Transmission response:
```go
TransmissionTorrent{
    // ... other fields
    // Missing: Labels field
}
```

**Why it happens:**
The Transmission RPC spec includes a `labels` field (array of strings) that clients use for categorization. The proxy developer focuses on getting the status, progress, and name correct but overlooks the labels field. Transmission itself doesn't heavily use labels, so it's easy to miss. Sonarr/Radarr's category matching is documented but not enforced by the proxy (it will work without labels if category matching is disabled).

**Consequences:**
- Activity tab showing "queue is empty" despite torrents being returned
- Filtering not working (all downloads shown instead of just the configured category)
- User must disable category matching in Sonarr/Radarr settings (non-obvious workaround)
- Multiple Sonarr/Radarr instances can't share same proxy (can't filter by category)

**Prevention:**
1. **Add labels to TransmissionTorrent struct:**
   ```go
   type TransmissionTorrent struct {
       // ... existing fields
       Labels []string `json:"labels"`
   }
   ```
2. **Set label from proxy configuration:**
   ```go
   transmissionTorrents[i] = TransmissionTorrent{
       // ... other fields
       Labels: []string{h.label}, // Use configured label
   }
   ```
3. **Validate in tests:** Check that returned torrents have labels matching configuration
4. **Document limitation:** If proxy serves multiple clients, they must use same label

**Detection (warning signs):**
- Activity tab empty despite log showing "fetched torrents from download client, count: 5"
- Forum posts: "Transmission activity tab not showing, need to disable category filter"
- Debug logs from Sonarr showing: "Category mismatch, skipping torrent"

**Which phase should address this:**
Phase 2 (Implementation) - Add labels field to response struct

---

### Pitfall 7: Race Condition When Transfers Complete Between Polls

**What goes wrong:**
Sonarr/Radarr polls the Activity tab every 1-2 seconds. If a transfer completes between two polls:
1. Poll #1: Transfer shows in queue at 99%, status "downloading"
2. **Transfer completes on Put.io**
3. **Download orchestrator processes it, marks as downloaded, removes from Put.io**
4. Poll #2: Transfer no longer in Put.io (404 or missing from list)
5. Sonarr/Radarr sees transfer disappeared without reaching "completed" status
6. Activity tab shows "import failed" or "connection error"

This is especially problematic for small files that complete quickly (<2 seconds) or when the download orchestrator polling interval aligns with Sonarr polling.

**Why it happens:**
There's no synchronization between:
- The download orchestrator (polls every 10 minutes, processes completed transfers)
- The Transmission proxy (polled every 1-2 seconds by Sonarr/Radarr)
- Put.io cleanup (transfers may be removed immediately after download)

The proxy expects transfers to follow a linear progression (queued → downloading → seeding → removed) but the download orchestrator can skip the "seeding" state by removing transfers immediately.

**Consequences:**
- Activity tab showing "import failed" for successfully downloaded transfers
- Confusion: Transfer disappeared from queue but did import correctly
- Users report: "Activity tab unreliable for small files"
- False negative alerts (transfer succeeded but shown as failed)

**Prevention:**
1. **Check database before reporting missing:** If transfer not in Put.io, check if it's in database as "downloaded" before returning error
2. **Cache recently completed transfers:** Keep 5-minute cache of completed transfer IDs, return cached status even after removal from Put.io
3. **Add transition state:** When transfer reaches 100%, set status to "seeding" briefly before orchestrator processes
4. **Coordinate polling:** Align orchestrator and proxy polling (e.g., orchestrator checks at :00, :10, :20; proxy caches for 2 minutes)
5. **Return completed status from database:** If transfer in database with status "downloaded", synthesize completed Transmission torrent

**Detection (warning signs):**
- Activity tab showing "import failed" but History showing successful import
- Logs: "transfer not found in Put.io list" for recently completed transfers
- Small files (<10MB) more likely to trigger this race
- Timing correlation: Issues occur within 10 seconds of orchestrator poll time

**Which phase should address this:**
Phase 3 (Integration) - Add database lookup for missing transfers

---

### Pitfall 8: Missing Peer Information Fields for In-Progress Downloads

**What goes wrong:**
Sonarr/Radarr Activity tab displays peer information (seeds/peers connected, upload/download rates) to show download health. The Transmission RPC spec includes these fields, but the current proxy only populates basic fields:
- ✓ Populated: `totalSize`, `leftUntilDone`, `name`, `status`
- ✗ Missing: `rateDownload`, `rateUpload`, `peersConnected`, `peersGettingFromUs`, `peersSendingToUs`

Put.io provides this data in the Transfer struct: `PeersConnected`, `PeersGettingFromUs`, `PeersSendingToUs` but the proxy doesn't map them.

**Why it happens:**
The proxy was initially built for completed-only transfers where peer info is irrelevant. When adding in-progress support, developers focus on progress and status but overlook the peer fields because Transmission's API has many optional fields and it's unclear which are essential for Activity tab display.

**Consequences:**
- Activity tab shows "0 of 0 peers connected" for active downloads
- No download/upload rate displayed
- Can't distinguish between healthy download (50 peers) and stalled download (0 peers)
- Users can't diagnose slow downloads ("is it slow because few peers or network issue?")

**Prevention:**
1. **Map peer fields from Put.io to Transmission:**
   ```go
   transmissionTorrents[i] = TransmissionTorrent{
       // ... existing fields
       PeersConnected:     transfer.PeersConnected,
       PeersGettingFromUs: transfer.PeersGettingFromUs,
       PeersSendingToUs:   transfer.PeersSendingToUs,
   }
   ```
2. **Calculate rates if not provided:** If Put.io doesn't provide rates, estimate from progress deltas between polls
3. **Add to TransmissionTorrent struct if missing:**
   ```go
   type TransmissionTorrent struct {
       // ... existing fields
       RateDownload       int64 `json:"rateDownload"`       // bytes/sec
       RateUpload         int64 `json:"rateUpload"`         // bytes/sec
       PeersConnected     int64 `json:"peersConnected"`
       PeersSendingToUs   int64 `json:"peersSendingToUs"`
       PeersGettingFromUs int64 `json:"peersGettingFromUs"`
   }
   ```

**Detection (warning signs):**
- Activity tab always shows "0 of 0 peers" for active downloads
- Download rate column empty or showing "0 KB/s"
- User complaints: "Can't see download speed in Activity tab"
- Comparison with real Transmission shows rich peer data, proxy shows minimal

**Which phase should address this:**
Phase 2 (Implementation) - Map all relevant Transfer fields to Transmission response

---

### Pitfall 9: No Error State Visibility When Put.io Transfer Fails

**What goes wrong:**
When Put.io transfers fail (invalid torrent, no peers, storage quota exceeded), the status changes to "ERROR" but the current status mapping sets:
```go
default: status = StatusStopped
```

Transmission uses `StatusStopped` for user-initiated pauses, not errors. The `ErrorString` field is set but:
1. Not populated in the current mapping (always `&transfer.ErrorMessage`, which may be empty)
2. Transmission's error field semantics differ from Put.io's

This means failed transfers look identical to stopped transfers in the Activity tab, hiding critical failures.

**Why it happens:**
Error state handling requires understanding both Put.io's error model and Transmission's error representation. The default case in the status mapping is a catch-all that treats unknown states as "stopped" instead of checking if it's an error condition.

**Consequences:**
- Failed transfers appear as "stopped" in Activity tab
- No visible error message explaining why transfer failed
- User assumes transfer was manually paused, doesn't investigate
- Accumulation of stuck transfers in "stopped" state
- Support burden: "Why did my download stop?"

**Prevention:**
1. **Explicitly handle ERROR status:**
   ```go
   case "error":
       status = StatusStopped // Keep stopped status
       // But ensure ErrorString is populated
   ```
2. **Populate ErrorString field:**
   ```go
   var errorString *string
   if transfer.Status == "ERROR" || transfer.ErrorMessage != "" {
       errorString = &transfer.ErrorMessage
   }
   ```
3. **Log error states prominently:** When ERROR status detected, log at WARN level with transfer details
4. **Add telemetry:** Track error_count metric by error type

**Detection (warning signs):**
- Transfers stuck in "stopped" state indefinitely
- Put.io UI shows errors but Activity tab shows "stopped"
- ErrorString field always nil/empty even for failed transfers
- User reports: "Transfer stopped, don't know why"

**Which phase should address this:**
Phase 2 (Implementation) - Explicit error state handling

---

## Minor Pitfalls

Mistakes that cause minor annoyance or confusion but don't significantly impact functionality.

### Pitfall 10: Inconsistent Time Unit Between ETA Fields

**What goes wrong:**
Put.io returns `EstimatedTime` in seconds, Transmission expects `eta` in seconds, but the mapping might misinterpret the units or apply timezone conversions incorrectly. Additionally, when a transfer is near completion, ETA can show invalid values (negative numbers, overflow).

**Why it happens:**
Time fields are easy to misconfigure. Developer might assume EstimatedTime is milliseconds or minutes without checking Put.io docs. Edge cases (ETA when transfer is paused, ETA when no peers) are not tested.

**Prevention:**
1. **Verify Put.io EstimatedTime units from docs:** Confirm it's seconds, not milliseconds
2. **Direct mapping if units match:**
   ```go
   ETA: transfer.EstimatedTime
   ```
3. **Handle edge cases:**
   ```go
   eta := transfer.EstimatedTime
   if eta < 0 {
       eta = -1 // Transmission uses -1 for "unknown"
   }
   ```
4. **Test with stalled transfers:** Verify ETA shows -1 when transfer has no peers

**Detection (warning signs):**
- ETA showing "5000 hours" for small files (milliseconds interpreted as seconds)
- Negative ETA values in Activity tab
- ETA not updating as download progresses

**Which phase should address this:**
Phase 2 (Implementation) - Field mapping with unit validation

---

### Pitfall 11: SecondsDownloading vs SecondsSeeding Confusion

**What goes wrong:**
The Transmission response includes `secondsDownloading` (time spent downloading) but the proxy might populate it incorrectly for seeding transfers or not populate it at all for in-progress transfers.

**Why it happens:**
Put.io doesn't track "time spent downloading" separately from "total transfer time". The proxy needs to calculate this or leave it as 0.

**Prevention:**
1. **Set to 0 for in-progress transfers if exact value unavailable:**
   ```go
   SecondsDownloading: 0 // Put.io doesn't provide this
   ```
2. **Document limitation:** Note in comments that SecondsDownloading is not available
3. **Use transfer age as approximation if needed:**
   ```go
   SecondsDownloading: time.Since(transfer.CreatedAt).Seconds()
   ```

**Detection (warning signs):**
- Activity tab showing incorrect time values
- Users reporting: "Says downloading for 3 days but I just added it"

**Which phase should address this:**
Phase 2 (Implementation) - Document and handle unavailable fields

---

## Technical Debt Patterns

Shortcuts that seem reasonable now but create long-term maintenance burden.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Reuse GetTaggedTorrents for both pipelines | Less code, no duplication | Breaking download pipeline when Activity tab needs change | **Never** - separate concerns |
| Map all unknown statuses to "stopped" | Simple default case, no crashes | Hides errors, confuses users | Only with warning log and telemetry |
| Skip FileID == 0 check for in-progress | Simpler logic | Fresh transfers invisible for 5-30 seconds | **Never** - breaks user experience |
| No caching, always poll Put.io | Real-time data, no staleness | Rate limit violations, API overuse | Only for completed-only queries (<1 req/min) |
| Calculate leftUntilDone from Downloaded | Direct arithmetic | Negative values when compression mismatches | Only if clamped to [0, size] range |
| Set ErrorString to empty for all statuses | Avoid nil pointer issues | Error states invisible | Only if explicit ERROR status check added |
| Use 0 for all peer/rate fields | Faster implementation | Users can't diagnose stalled downloads | Only temporarily, document in TODO comments |

## Integration Gotchas

Common mistakes when connecting Transmission proxy to Sonarr/Radarr.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Activity tab polling | Assume low frequency (10 min intervals) | Design for 1-2 second intervals with caching |
| Category/label filtering | Return torrents without labels field | Always populate labels array with configured label |
| Status interpretation | Use StatusStopped for everything non-completed | Map to appropriate queue states (DownloadWait, SeedWait, Check) |
| Progress tracking | Trust Downloaded field exclusively | Prefer PercentDone, validate consistency, clamp to [0, 100] |
| Error reporting | Return nil ErrorString | Populate ErrorString when status is ERROR |
| Transfer disappearance | Assume transfer exists in Put.io | Check database for recently completed transfers |
| Peer information | Omit peer/rate fields | Map PeersConnected, RateDownload from Put.io |

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| No caching on Activity tab polls | Excessive API calls to Put.io | Cache responses for 5-10 seconds | >5 active transfers + 2 second poll interval |
| Synchronous File API calls for each transfer | Slow response times, rate limits | Batch File.List instead of individual File.Get | >10 active transfers |
| Fresh database queries on every poll | Database lock contention | Cache recent completions in memory | >100 req/min to proxy |
| Unlimited concurrency for Put.io calls | Rate limit violations | Semaphore limiting concurrent API calls | >50 transfers in list |
| No rate limit tracking | Sudden 429 errors without warning | Track calls/min, proactive throttling | Approaching Put.io limit (100-200 req/min) |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **Status mapping completeness:** Did you map ALL Put.io statuses? (WAITING, ERROR, COMPLETING, not just completed/downloading)
- [ ] **FileID == 0 handling:** Does it work for fresh transfers before FileID is assigned?
- [ ] **Progress calculation validation:** Are leftUntilDone values always in [0, totalSize] range? Tested with compression?
- [ ] **Caching implementation:** Is there a cache to prevent rate limit violations? (Critical for Activity tab)
- [ ] **Labels field population:** Does TransmissionTorrent include labels array for category filtering?
- [ ] **Error state visibility:** Are ERROR status transfers distinguished from stopped transfers?
- [ ] **Peer information:** Are peersConnected and rate fields populated from Put.io data?
- [ ] **Race condition handling:** What happens when transfer completes between polls? Database lookup implemented?
- [ ] **Pipeline separation:** Is there a separate method for Activity tab vs download orchestrator? (Critical!)
- [ ] **Integration testing:** Have you tested with real Sonarr/Radarr instances polling every 2 seconds?

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Broke download pipeline by changing filter | **HIGH** | Immediately revert filter change, add integration tests for orchestrator, create separate method for Activity tab |
| Incorrect status mapping | **LOW** | Add missing status cases to switch statement, deploy hotfix, log unmapped statuses |
| Progress calculation errors | **MEDIUM** | Switch from Downloaded-based to PercentDone-based calculation, add clamping, validate in tests |
| Rate limit violations | **MEDIUM** | Add response caching immediately (emergency), implement batching and rate limiting in next release |
| FileID == 0 filtering too strict | **LOW** | Add status check to filter logic, allow FileID == 0 for WAITING/DOWNLOADING statuses |
| Missing race condition handling | **MEDIUM** | Add database lookup for missing transfers, cache recently completed transfers |
| No error visibility | **LOW** | Add explicit ERROR case to status mapping, populate ErrorString field, deploy update |
| Activity tab empty (labels missing) | **LOW** | Add labels field to struct, populate with configured label, redeploy proxy |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Breaking download pipeline | Phase 1: Design & Contracts | Create separate GetAllTransfersByTag method, integration tests verify orchestrator behavior unchanged |
| Incorrect status mapping | Phase 1: Design & Contracts | Document complete Put.io → Transmission status matrix, unit test each mapping |
| Progress calculation errors | Phase 2: Implementation | Unit tests for edge cases (compression, negative values), validation assertions |
| Rate limit violations | Phase 2: Implementation | Add caching layer, load test with 2-second polls, monitor api_calls_per_minute metric |
| FileID == 0 filtering | Phase 2: Implementation | Test with fresh transfers, verify they appear in Activity tab within 2 seconds |
| Race condition on completion | Phase 3: Integration | Test small file completion during poll, verify no "import failed" false positives |
| Missing error visibility | Phase 2: Implementation | Test ERROR status transfers, verify ErrorString appears in Activity tab |
| Labels missing | Phase 2: Implementation | Verify labels field populated, test category filtering in Sonarr/Radarr |

## Sources

### High Confidence (Official Documentation)

**Transmission RPC Specification:**
- [Transmission RPC Status Codes](https://transmission-rpc.readthedocs.io/en/v3.2.7/torrent.html) - Status codes 0-6 definitions
- [Transmission RPC torrent-get fields](https://pythonhosted.org/transmissionrpc/reference/transmissionrpc.html) - leftUntilDone, percentDone, ETA field definitions
- [GitHub transmission/transmission RPC spec](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md) - Official RPC protocol specification

**Sonarr/Radarr Activity Tab:**
- [Radarr Activity Documentation](https://wiki.servarr.com/radarr/activity) - Queue vs History behavior
- [Sonarr Activity tab empty with Transmission](https://forums.sonarr.tv/t/activity-queue-is-suddenly-completely-empty-transmission-downloads-dont-move-when-finished/23288) - Category filtering issues

**Put.io API:**
- [go-putio package documentation](https://pkg.go.dev/github.com/putdotio/go-putio) - Transfer struct fields
- [Put.io "Finishing" status explanation](https://help.put.io/en/articles/8005984-what-does-finishing-mean) - Transfer state when writing to storage
- [Put.io API documentation](https://api.put.io/) - Official API reference

**Similar Proxy Implementations:**
- [plundrio - Put.io Transmission RPC proxy](https://github.com/elsbrock/plundrio) - Reference implementation
- [transmissio - Put.io download service](https://github.com/anonfunc/transmissio) - Alternative proxy approach

### Medium Confidence (API Best Practices)

**API Polling Best Practices:**
- [7 Best Practices for API Polling](https://www.merge.dev/blog/api-polling-best-practices) - Exponential backoff, caching strategies
- [API Rate Limiting Best Practices](https://testfully.io/blog/api-rate-limit/) - Request throttling, monitoring
- [Webhooks vs Polling](https://zapier.com/blog/api-rate-limiting/) - When to cache vs real-time

**Progress Calculation Issues:**
- [Transmission percentDone inaccuracies](https://trac.transmissionbt.com/ticket/2299) - Known issue with percent done calculation
- [Transmission progress calculation](https://transmission-rpc.readthedocs.io/en/v3.3.2/_modules/transmission_rpc/torrent.html) - Fallback formula: 100.0 * (sizeWhenDone - leftUntilDone) / sizeWhenDone

**Activity Tab Issues:**
- [Downloads not appearing in Activity Queue](https://forums.sonarr.tv/t/downloads-in-transmission-not-appearing-in-activity-queue/21656) - Common category mismatch issue
- [Activity Queue showing empty](https://forums.sonarr.tv/t/transmission-activity-queue-empty/10867) - Connection and filtering problems

---

*Pitfalls research for: Activity Tab Support (in-progress download visibility)*
*Researched: 2026-02-08*
*Confidence: HIGH - Based on codebase analysis, official docs, and similar proxy implementations*
