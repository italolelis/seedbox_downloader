# Architecture Research: Activity Tab Support

**Milestone:** v1.3 Activity Tab Support
**Research Date:** 2026-02-08
**Confidence:** HIGH

## Executive Summary

The existing architecture cleanly separates Activity display (Transmission RPC proxy) from the download pipeline (TransferOrchestrator). This enables adding in-progress transfer visibility without breaking the existing download functionality. The key insight: **GetTaggedTorrents should return ALL transfers**, while **IsAvailable() continues to filter for downloads**.

This is achieved through:
1. Removing FileID!=0 filter in GetTaggedTorrents (enables in-progress visibility)
2. Using SaveParentID for tag matching (FileID-independent label filtering)
3. Empty Files array for in-progress transfers (preserves download pipeline safety)
4. IsAvailable() filter remains unchanged (download pipeline unaffected)

**Minimal change, maximum compatibility.**

## Current Architecture Analysis

### Component Boundaries

| Component | Responsibility | Relevant Code |
|-----------|---------------|---------------|
| **GetTaggedTorrents** | Fetch Put.io transfers matching label | putio/client.go:38-109 |
| **handleTorrentGet** | Convert transfers to Transmission format | transmission.go:444-523 |
| **TransferOrchestrator** | Poll for completed transfers, queue downloads | transfer/transfer.go:93-186 |
| **IsAvailable()** | Filter completed transfers for download | transfer/transfer.go:60-64 |

### Current Data Flow

```
Put.io API → GetTaggedTorrents → handleTorrentGet → Sonarr/Radarr Activity Tab
                    ↓
            TransferOrchestrator.watchTransfers()
                    ↓
            IsAvailable() filter → Download pipeline
```

**Key finding:** The two paths are **independent**. The Transmission RPC endpoint calls GetTaggedTorrents directly, while the download pipeline calls it through TransferOrchestrator with an additional IsAvailable() filter.

## Problem Analysis

### Current Limitations

**GetTaggedTorrents filters by FileID!=0** (putio/client.go:52-56)
```go
if t.FileID == 0 {
    logger.DebugContext(ctx, "skipping transfer because it's not a downloadable transfer")
    continue
}
```

This filter excludes in-progress transfers because:
- In-progress transfers have `FileID == 0` until completion
- FileID only gets set when Put.io finishes downloading the torrent
- Without FileID, can't fetch parent folder for tag matching

**Impact:**
- Activity tab shows nothing until transfer completes
- Users see downloads in Put.io web UI but not in Sonarr/Radarr
- No visibility into download progress or ETA during active downloads

### Tag Matching Challenge

**Current approach** (putio/client.go:58-76):
1. Get file via `t.FileID`
2. Get parent folder via `file.ParentID`
3. Check if `parent.Name == tag`

**Problem:** In-progress transfers have `FileID == 0`, so steps 1-2 fail.

**Solution:** Use `SaveParentID` field instead. From Put.io API documentation:
- `SaveParentID`: The folder ID where transfer will be saved (set on transfer creation)
- Available immediately, even when `FileID == 0`
- Directly provides parent folder without file lookup

## Proposed Architecture Changes

### Option Analysis

| Approach | Pros | Cons | Verdict |
|----------|------|------|---------|
| **Option 1: Modify GetTaggedTorrents** | Single source of truth, consistent behavior | Requires careful FileID handling | ✓ **RECOMMENDED** |
| Option 2: Separate GetInProgressTorrents | Explicit separation, no shared logic | Code duplication, two maintenance points | ✗ Not recommended |
| Option 3: Add includeInProgress parameter | Explicit control, backward compatible | Boolean parameter increases complexity | ✗ Unnecessary complexity |

**Recommendation:** Modify GetTaggedTorrents to include in-progress transfers. The IsAvailable() filter already provides the separation needed for the download pipeline.

### Modified GetTaggedTorrents Flow

```go
func (c *Client) GetTaggedTorrents(ctx context.Context, tag string) ([]*transfer.Transfer, error) {
    transfers, err := c.putioClient.Transfers.List(ctx)
    // ... error handling ...

    for _, t := range transfers {
        // NEW: Get parent folder for tag matching (works for all transfers)
        parent, err := c.putioClient.Files.Get(ctx, t.SaveParentID)
        if err != nil || !parent.IsDir() || parent.Name != tag {
            continue // Skip non-matching transfers
        }

        // Convert to internal Transfer struct
        torrent := &transfer.Transfer{
            ID: fmt.Sprintf("%d", t.ID),
            Name: t.Name,
            Label: tag,
            Progress: float64(t.PercentDone),
            Status: t.Status,
            // ... peer info, speed, etc ...
            Files: make([]*transfer.File, 0), // Empty for in-progress
        }

        // NEW: Only populate files for completed transfers
        if t.FileID != 0 {
            files, err := c.getFilesRecursively(ctx, t.FileID, file.Name)
            if err == nil {
                torrent.Files = append(torrent.Files, files...)
            }
        }

        torrents = append(torrents, torrent)
    }

    return torrents, nil
}
```

### Key Changes Explained

**1. Tag Matching via SaveParentID**
- **Before:** Fetch file via FileID, then parent via file.ParentID
- **After:** Fetch parent directly via SaveParentID
- **Benefit:** Works for both in-progress and completed transfers

**2. Conditional File Population**
- **Before:** Always call getFilesRecursively (fails for FileID=0)
- **After:** Only populate files when FileID!=0
- **Benefit:** In-progress transfers return with empty Files array

**3. IsAvailable() Safety Net**
- **Unchanged:** Returns true only for completed/seeding status
- **Effect:** TransferOrchestrator.watchTransfers() skips in-progress transfers
- **Benefit:** Download pipeline unaffected by new data

### Impact on Download Pipeline

**TransferOrchestrator.watchTransfers()** (transfer/transfer.go:138-186)
```go
for _, transfer := range transfers {
    // UNCHANGED: This filter still skips in-progress transfers
    if !transfer.IsAvailable() || !transfer.IsDownloadable() {
        continue
    }
    // ... rest of download logic ...
}
```

**IsAvailable() method** (transfer/transfer.go:60-64)
```go
func (t *Transfer) IsAvailable() bool {
    status := strings.ToLower(t.Status)
    return status == "completed" || status == "seeding" ||
           status == "seedingwait" || status == "finished"
}
```

**IsDownloadable() method** (transfer/transfer.go:56-58)
```go
func (t *Transfer) IsDownloadable() bool {
    return len(t.Files) > 0
}
```

**Result:**
- In-progress transfers have status "downloading" → `IsAvailable()` returns false → skipped
- In-progress transfers have empty Files array → `IsDownloadable()` returns false → skipped
- **Double safety net:** Both filters protect download pipeline

## Status Mapping Corrections

### Current Implementation Issues

**transmission.go:470-486** maps Put.io status to Transmission status:
```go
switch strings.ToLower(transfer.Status) {
case "completed", "finished":
    status = StatusSeed
case "seedingwait":
    status = StatusSeedWait
case "seeding":
    status = StatusSeed
case "downloading":
    status = StatusDownload
case "checking":
    status = StatusCheck
default:
    status = StatusStopped
}
```

**Problem:** Incomplete mapping of Put.io states to Transmission states.

### Put.io Transfer States

From Put.io API and go-putio library:
- **DOWNLOADING**: Active download, peers connected
- **SEEDING**: Upload phase after completion
- **SEEDINGWAIT**: Queued for seeding
- **COMPLETED**: Download finished (Put.io specific)
- **FINISHING**: Post-download processing (verification, etc.)
- **IN_QUEUE**: Waiting to start
- **WAITING**: Similar to IN_QUEUE
- **ERROR**: Transfer failed

### Transmission Status Codes

From [Transmission RPC specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md):

```go
const (
    StatusStopped      = 0  // Torrent is stopped
    StatusCheckWait    = 1  // Queued to check files
    StatusCheck        = 2  // Checking files
    StatusDownloadWait = 3  // Queued to download
    StatusDownload     = 4  // Downloading
    StatusSeedWait     = 5  // Queued to seed
    StatusSeed         = 6  // Seeding
)
```

### Recommended Mapping

```go
func mapPutioStatusToTransmission(putioStatus string) TransmissionTorrentStatus {
    switch strings.ToLower(putioStatus) {
    case "downloading":
        return StatusDownload // 4
    case "in_queue", "waiting":
        return StatusDownloadWait // 3
    case "finishing", "checking":
        return StatusCheck // 2
    case "completed", "finished":
        return StatusSeed // 6 (Put.io auto-seeds after completion)
    case "seeding":
        return StatusSeed // 6
    case "seedingwait":
        return StatusSeedWait // 5
    case "error":
        return StatusStopped // 0
    default:
        return StatusStopped // 0 (safe fallback)
    }
}
```

**Key improvements:**
- IN_QUEUE/WAITING → StatusDownloadWait (more accurate than StatusStopped)
- FINISHING → StatusCheck (Put.io verifies after download)
- ERROR → StatusStopped (explicit error state)

## File Changes Required

### Priority 1: Core Changes

| File | Changes | Risk | Lines Changed |
|------|---------|------|---------------|
| `internal/dc/putio/client.go` | Remove FileID!=0 filter, use SaveParentID for tag matching, conditional file population | **LOW** | ~30 lines |
| `internal/http/rest/transmission.go` | Improve status mapping with complete Put.io state coverage | **LOW** | ~15 lines |

### Priority 2: Optional Enhancements

| File | Changes | Risk | Lines Changed |
|------|---------|------|---------------|
| `internal/transfer/transfer.go` | Add helper methods (IsInProgress, PercentComplete) for clarity | **VERY LOW** | ~10 lines |

### No Changes Required

**These files remain unchanged:**
- `internal/transfer/transfer.go` (Transfer struct, IsAvailable, IsDownloadable)
- `internal/downloader/downloader.go` (download pipeline logic)
- `internal/storage/sqlite/download_repository.go` (state persistence)
- `cmd/seedbox_downloader/main.go` (initialization)

## Build Order

### Phase 1: Tag Matching Fix (Foundation)

**Goal:** Enable SaveParentID-based tag matching

**Changes:**
1. Modify `GetTaggedTorrents` to fetch parent via SaveParentID instead of FileID→ParentID chain
2. Keep FileID!=0 filter temporarily (minimize blast radius)
3. Test with existing completed transfers

**Verification:**
- Existing tests pass (no behavior change for completed transfers)
- Can fetch parent folder without FileID

**Risk:** VERY LOW (only changes folder lookup method)

### Phase 2: In-Progress Visibility (Core Feature)

**Goal:** Return in-progress transfers from GetTaggedTorrents

**Changes:**
1. Remove FileID!=0 filter
2. Add conditional file population (only when FileID!=0)
3. Improve status mapping with complete Put.io state coverage

**Verification:**
- handleTorrentGet returns in-progress transfers
- TransferOrchestrator still skips them (IsAvailable filter works)
- Download pipeline unchanged (no false positives)

**Risk:** LOW (IsAvailable and IsDownloadable provide safety nets)

### Phase 3: Testing and Validation

**Goal:** Verify both paths work correctly

**Test scenarios:**
1. **Activity tab:** Shows in-progress transfers with accurate progress/status
2. **Download pipeline:** Only processes completed transfers
3. **Empty files array:** In-progress transfers have no files, fail IsDownloadable
4. **Status transitions:** Transfer moves from "downloading" to "seeding" correctly
5. **Label filtering:** Only returns transfers matching SaveParentID folder name

**Risk:** VERY LOW (validation only)

## Interface Changes

### No Breaking Changes

**DownloadClient interface** (internal/transfer/transfer.go:15-19)
- No changes required
- GetTaggedTorrents signature unchanged
- Return type unchanged

**Transfer struct** (internal/transfer/transfer.go:27-44)
- No new fields required (all needed fields already exist)
- Optional: Add helper methods for readability

**TransmissionTorrent** (internal/http/rest/transmission.go:48-66)
- No changes required
- Existing fields support in-progress display

## Backward Compatibility

### Configuration

**No changes required:**
- Environment variables unchanged
- Database schema unchanged
- API endpoints unchanged
- Transmission RPC protocol unchanged

### Behavior

**Unchanged:**
- Download pipeline continues to process only completed transfers
- Transfer claiming logic unaffected
- File download logic unaffected
- Import monitoring unaffected

**New (additive only):**
- Activity tab now shows in-progress transfers
- More accurate status mapping for all transfer states

### Data

**Transfer records:**
- Existing database records unaffected
- No migration required
- In-progress transfers never claimed (IsAvailable filter)

## Risk Mitigation

### Risk 1: Download Pipeline False Positives

**Risk:** In-progress transfers accidentally trigger downloads

**Mitigation:**
- **Primary:** IsAvailable() filters out non-completed statuses
- **Secondary:** IsDownloadable() filters out empty Files arrays
- **Tertiary:** getFilesRecursively only called for FileID!=0

**Likelihood:** VERY LOW (triple protection)

### Risk 2: Tag Matching Failures

**Risk:** SaveParentID doesn't match expected folder structure

**Mitigation:**
- SaveParentID is set on transfer creation by Put.io
- Matches existing folder structure (same as FileID→ParentID approach)
- Add debug logging for mismatches

**Likelihood:** LOW (Put.io API guarantees)

### Risk 3: Missing File Data

**Risk:** Completed transfers return with empty Files array

**Mitigation:**
- Only populate Files when FileID!=0 (completed transfers have FileID)
- Log error if getFilesRecursively fails for completed transfer
- IsDownloadable() filter catches edge cases

**Likelihood:** VERY LOW (FileID!=0 check is explicit)

### Risk 4: Status Mapping Errors

**Risk:** Unknown Put.io status breaks Activity tab display

**Mitigation:**
- Default case returns StatusStopped (safe fallback)
- Add logging for unknown statuses
- Comprehensive mapping covers all documented states

**Likelihood:** LOW (exhaustive mapping with fallback)

## Testing Strategy

### Unit Tests

**putio/client_test.go:**
```go
func TestGetTaggedTorrents_InProgress(t *testing.T) {
    // Verify in-progress transfers returned with empty Files
}

func TestGetTaggedTorrents_TagMatchingViaSaveParentID(t *testing.T) {
    // Verify SaveParentID used for tag matching
}

func TestGetTaggedTorrents_CompletedWithFiles(t *testing.T) {
    // Verify completed transfers still populate Files
}
```

**transmission_test.go:**
```go
func TestStatusMapping_AllPutioStates(t *testing.T) {
    // Verify all Put.io statuses map to valid Transmission codes
}

func TestHandleTorrentGet_InProgressDisplay(t *testing.T) {
    // Verify in-progress transfers converted correctly
}
```

**transfer_test.go:**
```go
func TestIsAvailable_InProgressFiltered(t *testing.T) {
    // Verify "downloading" status returns false
}

func TestIsDownloadable_EmptyFilesFiltered(t *testing.T) {
    // Verify empty Files array returns false
}
```

### Integration Tests

**Scenario 1: End-to-end Activity Display**
1. Add transfer via Transmission API
2. Query torrent-get immediately
3. Verify in-progress transfer shown with progress/status
4. Wait for completion
5. Verify status changes to "seeding"

**Scenario 2: Download Pipeline Isolation**
1. Add transfer via Transmission API
2. Wait for TransferOrchestrator poll
3. Verify transfer NOT queued for download
4. Wait for completion
5. Verify transfer IS queued after completion

**Scenario 3: Multi-Label Filtering**
1. Add transfers to different folders (labels)
2. Query torrent-get with specific label
3. Verify only matching transfers returned (via SaveParentID)

## Performance Considerations

### API Calls

**Before:**
- GetTaggedTorrents: 1 + (2 × N completed transfers)
  - 1 List call
  - N × Get file
  - N × Get parent

**After:**
- GetTaggedTorrents: 1 + (1 × N all transfers) + (1 × N completed transfers)
  - 1 List call
  - N × Get parent (SaveParentID)
  - N completed × Get files recursively

**Impact:** Minimal increase (one extra Get per in-progress transfer)

### Memory

**Before:**
- N completed Transfer structs with Files arrays

**After:**
- N completed Transfer structs with Files arrays
- M in-progress Transfer structs with empty Files arrays

**Impact:** Negligible (Files array overhead removed for in-progress)

### Polling

**No change:**
- TransferOrchestrator polling frequency unchanged
- handleTorrentGet triggered by Sonarr/Radarr (on-demand)

## Monitoring and Observability

### Logging Additions

**putio/client.go:**
```go
// When SaveParentID doesn't match tag
logger.DebugContext(ctx, "skipping transfer, parent folder doesn't match tag",
    "transfer_id", t.ID,
    "save_parent_id", t.SaveParentID,
    "parent_name", parent.Name,
    "expected_tag", tag)

// When returning in-progress transfer
logger.DebugContext(ctx, "including in-progress transfer",
    "transfer_id", t.ID,
    "status", t.Status,
    "percent_done", t.PercentDone)
```

**transmission.go:**
```go
// When mapping unknown status
logger.WarnContext(ctx, "unknown put.io status, defaulting to stopped",
    "status", transfer.Status,
    "transfer_id", transfer.ID)
```

### Metrics Additions

**Optional (if telemetry extended):**
- `transfers_returned_total{status="downloading|seeding|completed"}`
- `transfers_filtered_total{reason="not_available|not_downloadable"}`

## Alternative Approaches Considered

### Alternative 1: GetInProgressTorrents Method

**Approach:** Add separate method `GetInProgressTorrents(ctx, tag)` that only returns downloading transfers.

**Pros:**
- Explicit separation of concerns
- No risk to existing GetTaggedTorrents callers

**Cons:**
- Code duplication (tag matching, API calls, conversion logic)
- Two maintenance points for same functionality
- Unclear which method to call from where

**Verdict:** ✗ Rejected - unnecessary complexity, violates DRY

### Alternative 2: IncludeInProgress Parameter

**Approach:** Add boolean parameter `GetTaggedTorrents(ctx, tag, includeInProgress bool)`

**Pros:**
- Explicit control over filtering
- Backward compatible (default to false)

**Cons:**
- API signature change (breaks interface)
- Boolean parameter increases cognitive load
- IsAvailable filter already provides needed separation

**Verdict:** ✗ Rejected - unnecessary when IsAvailable filter exists

### Alternative 3: Transfer Struct Extension

**Approach:** Add `IsInProgress bool` field to Transfer struct, set based on FileID.

**Pros:**
- Explicit flag for filtering
- Clearer intent than status string comparison

**Cons:**
- Redundant with Status field
- Increases struct size
- Status already provides this information

**Verdict:** ✗ Rejected - redundant, Status field sufficient

## Confidence Assessment

| Area | Confidence | Source | Notes |
|------|------------|--------|-------|
| Put.io API behavior | HIGH | go-putio library docs, existing code | SaveParentID field confirmed in Transfer struct |
| Transmission status codes | HIGH | Official RPC spec | Complete mapping documented |
| Download pipeline isolation | HIGH | Code analysis | IsAvailable + IsDownloadable provide double protection |
| FileID behavior | HIGH | Existing code patterns | Line 52-56 shows FileID==0 for in-progress |
| Tag matching approach | MEDIUM | Inference from API | SaveParentID usage assumed to match FileID→ParentID |

### Gaps and Unknowns

**SaveParentID validation:**
- Assumed: SaveParentID always set when transfer created with parent folder
- Validation: Add nil/0 check in production code
- Mitigation: Skip transfer if SaveParentID == 0 or parent fetch fails

**Put.io status strings:**
- Assumed: Status values from go-putio are lowercase
- Validation: strings.ToLower() used consistently
- Mitigation: Case-insensitive comparison already implemented

## Summary and Recommendations

### Recommended Approach

**Modify GetTaggedTorrents to:**
1. Use SaveParentID for tag matching (enables FileID-independent filtering)
2. Remove FileID!=0 filter (enables in-progress visibility)
3. Conditionally populate Files array (only when FileID!=0)

**Result:**
- Activity tab shows in-progress downloads
- Download pipeline unchanged (IsAvailable filter protects)
- Minimal code changes (~30 lines in one file)
- No breaking changes, fully backward compatible

### Build Order

1. **Phase 1:** Implement SaveParentID tag matching (foundation)
2. **Phase 2:** Remove FileID filter + conditional file population (feature)
3. **Phase 3:** Improve status mapping (polish)

### Risk Level: LOW

**Protections:**
- IsAvailable() filters non-completed transfers from download pipeline
- IsDownloadable() filters empty Files arrays from download pipeline
- SaveParentID tag matching preserves existing label filtering behavior
- No interface changes, no database changes, no config changes

### Success Criteria

**Activity Tab (Primary Goal):**
- [ ] In-progress transfers visible in Sonarr/Radarr Activity tab
- [ ] Accurate progress percentage displayed
- [ ] Accurate ETA displayed
- [ ] Correct status (Downloading, Queued, etc.)
- [ ] Peer information visible (connected, sending, receiving)

**Download Pipeline (Safety):**
- [ ] Only completed transfers queued for download
- [ ] In-progress transfers never claimed
- [ ] No false positives in download logs
- [ ] Existing test suite passes

**Observability:**
- [ ] Debug logs show in-progress transfers returned
- [ ] Debug logs show in-progress transfers filtered by IsAvailable
- [ ] Warn logs for unknown Put.io statuses

---

## Sources

- [Transmission RPC specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md)
- [Transmission RPC torrent status codes](https://transmission-rpc.readthedocs.io/en/v3.4.0/torrent.html)
- [Put.io API documentation](https://pkg.go.dev/github.com/putdotio/go-putio)
- [Put.io go-putio library Transfer struct](https://pkg.go.dev/github.com/putdotio/go-putio)

**Research confidence:** HIGH
**Architecture analysis:** 2026-02-08
