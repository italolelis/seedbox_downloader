# Stack Research: In-Progress Download Support

**Domain:** Transmission RPC proxy - Activity tab visibility
**Researched:** 2026-02-08
**Confidence:** HIGH (Transfer struct), MEDIUM (Status values), MEDIUM (SaveParentID behavior)

## Research Scope

This research focuses on exposing in-progress transfers (FileID == 0) in the Transmission RPC proxy's `torrent-get` response. Current code filters these out at `internal/dc/putio/client.go:52-56`.

## Recommended Stack (No Changes)

| Technology | Version | Purpose | Status |
|-----------|---------|---------|--------|
| github.com/putdotio/go-putio | v1.7.2 | Put.io API SDK | Already in use, no upgrade needed |
| Go stdlib | 1.23 | Core language | Already in use |
| Chi Router | v5 | HTTP routing | Already in use |
| SQLite | - | Transfer state storage | Already in use |
| OpenTelemetry | - | Observability | Already in use |

**No new dependencies required for this milestone.**

## Put.io Transfer Object Structure

### Complete Transfer Struct (github.com/putdotio/go-putio v1.7.2)

Based on SDK usage in existing codebase and pkg.go.dev documentation:

```go
type Transfer struct {
    // Identity & Metadata
    ID             int64  `json:"id"`
    Name           string `json:"name"`
    Source         string `json:"source"`          // Magnet/torrent URL

    // Progress & Timing
    Status         string `json:"status"`           // See Status Values below
    PercentDone    int    `json:"percent_done"`     // 0-100
    Downloaded     int64  `json:"downloaded"`       // Bytes downloaded
    Size           int64  `json:"size"`             // Total size in bytes
    EstimatedTime  int64  `json:"estimated_time"`   // Seconds remaining
    CreatedAt      *Time  `json:"created_at"`
    FinishedAt     *Time  `json:"finished_at"`

    // Destination (CRITICAL for label matching)
    FileID         int64  `json:"file_id"`          // 0 during download, set when complete
    SaveParentID   int64  `json:"save_parent_id"`   // Destination folder ID (set during download)

    // Peer Statistics (for Activity tab display)
    PeersConnected      int `json:"peers_connected"`
    PeersGettingFromUs  int `json:"peers_getting_from_us"`
    PeersSendingToUs    int `json:"peers_sending_to_us"`
    DownloadSpeed       int `json:"download_speed"`   // Bytes/sec

    // Seeding Info
    SecondsSeeding int64 `json:"seconds_seeding"`

    // Additional Fields
    ErrorMessage   string `json:"error_message"`
    CallbackURL    string `json:"callback_url"`
    Extract        bool   `json:"extract"`
    IsPrivate      bool   `json:"is_private"`
}
```

**Key Finding:** `SaveParentID` is available DURING download (before FileID is set).

**Sources:**
- [putio package - github.com/putdotio/go-putio](https://pkg.go.dev/github.com/putdotio/go-putio)
- Verified in existing codebase: `internal/dc/putio/client.go:79-94`

**Confidence:** HIGH - Struct fields verified in existing codebase usage

## Transfer Status Values

Put.io uses lowercase string status values. Based on existing codebase mapping in `internal/http/rest/transmission.go:473-486`:

| Put.io Status | Lifecycle Stage | Transmission Mapping | FileID State |
|---------------|----------------|---------------------|--------------|
| `"downloading"` | Active download | `StatusDownload` (4) | 0 |
| `"in_queue"` | Queued | `StatusDownloadWait` (3) | 0 |
| `"checking"` | Verification | `StatusCheck` (2) | 0 |
| `"completed"` | Download done | `StatusSeed` (6) | Set |
| `"finished"` | Fully complete | `StatusSeed` (6) | Set |
| `"seeding"` | Active seeding | `StatusSeed` (6) | Set |
| `"seedingwait"` | Queued seeding | `StatusSeedWait` (5) | Set |
| `"error"` | Failed | `StatusStopped` (0) | 0 |

**Status Lifecycle:**
```
in_queue → checking → downloading → completed → seeding/finished
                                          ↓
                                        error (at any stage)
```

**Critical Finding:** Transfers with status `"downloading"`, `"in_queue"`, or `"checking"` will have `FileID == 0` because the file hasn't been created yet.

**Sources:**
- Existing code: `internal/transfer/transfer.go:52-64`, `internal/http/rest/transmission.go:473-486`
- Community projects: [putioarr](https://github.com/wouterdebie/putioarr), [plundrio](https://github.com/elsbrock/plundrio)

**Confidence:** MEDIUM - Status values inferred from existing code patterns and community projects

## Label Matching Strategy

### Current Approach (Completed Only)
```go
// internal/dc/putio/client.go:52-76
if t.FileID == 0 {
    continue  // Skips in-progress transfers
}
file, err := c.putioClient.Files.Get(ctx, t.FileID)
parent, err := c.putioClient.Files.Get(ctx, file.ParentID)
if parent.Name != tag {
    continue  // Filter by parent folder name
}
```

**Current API Calls:** `1 + 2N` (1 list + 2 Gets per transfer)

### Recommended Approach (Include In-Progress)

**Key Insight:** Use `SaveParentID` for in-progress transfers, `file.ParentID` for completed.

```go
// Pseudo-code for new logic
for _, t := range transfers {
    var parentID int64

    if t.FileID == 0 {
        // In-progress: Use SaveParentID directly
        parentID = t.SaveParentID
    } else {
        // Completed: Use File's ParentID
        file, err := c.putioClient.Files.Get(ctx, t.FileID)
        if err != nil {
            continue
        }
        parentID = file.ParentID
    }

    // Get parent folder for label matching
    parent, err := c.putioClient.Files.Get(ctx, parentID)
    if err != nil || parent.Name != tag {
        continue
    }

    // Include transfer in response
}
```

**New API Calls:** `1 + N + M` where:
- 1 = Transfers.List()
- N = Files.Get() for completed transfers only
- M = Files.Get() for parent folder lookup

**Optimization:** Reduces API calls for in-progress transfers (no file lookup needed).

**Open Question:** Does Put.io persist `SaveParentID` after transfer completes?
- If YES: Can simplify to always use SaveParentID (1 + N calls total)
- If NO: Must use conditional logic as shown above

**Confidence:** MEDIUM - SaveParentID field confirmed in SDK, but lifecycle behavior needs phase-specific validation

## Fields Available During Download (FileID == 0)

These fields populate the Transmission response for Activity tab:

| Field | Transmission Mapping | Example Value |
|-------|---------------------|---------------|
| `ID` | `id`, `hashString` | `123456789` |
| `Name` | `name` | `"Ubuntu.24.04.iso"` |
| `Status` | `status` | `"downloading"` → `StatusDownload (4)` |
| `PercentDone` | Calculate `leftUntilDone` | `42` (42%) |
| `Downloaded` | `downloadedEver` | `4200000000` |
| `Size` | `totalSize` | `10000000000` |
| `EstimatedTime` | `eta` | `3600` |
| `PeersConnected` | (not mapped) | `25` |
| `DownloadSpeed` | (not mapped) | `5242880` (5MB/s) |
| `SaveParentID` | Derive `downloadDir` | `987654321` |

**Fields NOT Available (FileID == 0):**
- `FileID` - Only set after download completes
- `Files` array - Cannot enumerate until FileID exists

**Implications:**
- `FileCount` will be 0 for in-progress transfers (Sonarr/Radarr handle this)
- `DownloadDir` derived from SaveParentID folder name lookup
- `IsFinished` correctly false for non-completed statuses
- All required Transmission fields can be populated

**Confidence:** HIGH - Existing mapping code already handles these cases

## Transmission RPC Compatibility

### Current Mapping (internal/http/rest/transmission.go:461-508)

```go
TransmissionTorrent{
    ID:             id,                              // Converted to int64
    HashString:     sha1(transfer.ID),               // SHA1 of transfer ID
    Name:           transfer.Name,
    DownloadDir:    transfer.SavePath,               // From label
    TotalSize:      transfer.Size,
    LeftUntilDone:  transfer.Size - transfer.Downloaded,
    IsFinished:     status == "completed" || "seeding",
    ETA:            transfer.EstimatedTime,
    Status:         mapStatus(transfer.Status),      // Already handles "downloading"
    DownloadedEver: transfer.Downloaded,
    FileCount:      len(transfer.Files),             // Will be 0 for in-progress
    SeedRatioLimit: 1.0,
    SeedRatioMode:  1,
    SeedIdleLimit:  100,
    SeedIdleMode:   1,
}
```

**Compatibility Analysis:**
- ✓ `FileCount: 0` - Acceptable for in-progress (Transmission standard)
- ✓ `IsFinished: false` - Automatically set for downloading/checking/in_queue
- ✓ `LeftUntilDone` calculation - Works with partial downloads
- ✓ `Status` mapping - Already includes `"downloading"` → `StatusDownload`

**No changes required** to Transmission mapping logic.

**Confidence:** HIGH - Existing code already compatible

## Implementation Pattern

### Data Flow (Before - Completed Only)
```
Put.io API: Transfers.List()
    ↓
Filter: Skip if FileID == 0
    ↓
Put.io API: Files.Get(FileID) for each transfer
Put.io API: Files.Get(parent.ParentID) for each transfer
    ↓
Filter: Skip if parent.Name != label
    ↓
internal/dc/putio.Client.GetTaggedTorrents()
    ↓
[]*transfer.Transfer (completed only)
    ↓
internal/http/rest.TransmissionHandler.handleTorrentGet()
    ↓
[]TransmissionTorrent
    ↓
Sonarr/Radarr Activity Tab (missing in-progress)
```

### Data Flow (After - Include In-Progress)
```
Put.io API: Transfers.List()
    ↓
Conditional:
  - If FileID == 0: Use SaveParentID
  - If FileID != 0: Files.Get(FileID), use file.ParentID
    ↓
Put.io API: Files.Get(parentID) for label matching
    ↓
Filter: Skip if parent.Name != label
    ↓
internal/dc/putio.Client.GetTaggedTorrents()
    ↓
[]*transfer.Transfer (completed + in-progress)
    ↓
internal/http/rest.TransmissionHandler.handleTorrentGet()
    ↓
[]TransmissionTorrent (FileCount may be 0)
    ↓
Sonarr/Radarr Activity Tab (shows in-progress downloads)
```

**Key Changes:**
1. Remove `if t.FileID == 0 { continue }` filter (line 52-56)
2. Add conditional parent lookup logic
3. Verify status mapping includes in-progress states (already present)

## Integration Points

### Code Changes Required

**File:** `internal/dc/putio/client.go`

**Current code (lines 52-56):**
```go
if t.FileID == 0 {
    logger.DebugContext(ctx, "skipping transfer because it's not a downloadable transfer",
        "transfer_id", t.ID, "status", t.Status)
    continue
}
```

**Replace with:**
```go
var parentID int64

if t.FileID == 0 {
    // In-progress: Use SaveParentID
    logger.DebugContext(ctx, "processing in-progress transfer",
        "transfer_id", t.ID, "status", t.Status, "save_parent_id", t.SaveParentID)
    parentID = t.SaveParentID
} else {
    // Completed: Get file and use its ParentID
    file, err := c.putioClient.Files.Get(ctx, t.FileID)
    if err != nil {
        logger.ErrorContext(ctx, "failed to get file", "transfer_id", t.ID, "err", err)
        continue
    }
    parentID = file.ParentID
}
```

**File:** `internal/dc/putio/client.go` (lines 58-76 become)

**Current code:**
```go
file, err := c.putioClient.Files.Get(ctx, t.FileID)
if err != nil {
    logger.ErrorContext(ctx, "failed to get file", "transfer_id", t.ID, "err", err)
    continue
}

parent, err := c.putioClient.Files.Get(ctx, file.ParentID)
```

**Replace with:**
```go
parent, err := c.putioClient.Files.Get(ctx, parentID)
```

**File:** `internal/dc/putio/client.go` (lines 96-102)

**Current code:**
```go
files, err := c.getFilesRecursively(ctx, file.ID, file.Name)
if err != nil {
    return nil, fmt.Errorf("failed to get files for transfer: %w", err)
}

torrent.Files = append(torrent.Files, files...)
```

**Replace with:**
```go
// Only populate Files for completed transfers
if t.FileID != 0 {
    files, err := c.getFilesRecursively(ctx, t.FileID, t.Name)
    if err != nil {
        return nil, fmt.Errorf("failed to get files for transfer: %w", err)
    }
    torrent.Files = append(torrent.Files, files...)
}
// In-progress transfers have empty Files array (FileCount = 0)
```

### No Changes Required

**File:** `internal/http/rest/transmission.go`
- Status mapping already includes `"downloading"`, `"checking"`, `"in_queue"` (lines 473-486)
- FileCount calculation works with empty Files array (line 502)
- IsFinished logic correctly handles non-completed statuses (line 497)

**File:** `internal/transfer/transfer.go`
- Transfer struct already has all needed fields
- IsAvailable() method may need review (currently only checks completed statuses)

## Validation Checklist

Before implementation:
- [ ] Verify SaveParentID is non-zero for in-progress transfers
- [ ] Confirm Files.Get(SaveParentID) returns parent folder object
- [ ] Test with transfers in `"downloading"`, `"in_queue"`, `"checking"` states
- [ ] Verify Sonarr/Radarr Activity tab displays FileCount==0 entries correctly
- [ ] Confirm API call count reduction for in-progress transfers

During implementation:
- [ ] Add debug logging for SaveParentID values
- [ ] Handle SaveParentID == 0 edge case (skip transfer)
- [ ] Update unit tests to include FileID==0 cases
- [ ] Verify IsAvailable() behavior for in-progress transfers

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| Parent lookup | Conditional (SaveParentID if FileID==0) | Always use SaveParentID | Unknown if SaveParentID persists after completion |
| Parent lookup | Conditional | Add new Put.io API endpoint | No control over Put.io API; use existing |
| File enumeration | Skip for FileID==0 | Return empty placeholder | Cannot enumerate files before download completes |
| Status filtering | Include downloading/checking/in_queue | Create synthetic "pending" status | Sonarr/Radarr expect standard Transmission statuses |

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| SaveParentID cleared after completion | Medium | Use conditional logic (test both paths) |
| SaveParentID == 0 for some transfers | Low | Skip transfer with warning log |
| Files.Get(SaveParentID) fails | Low | Skip transfer, continue processing others |
| FileCount==0 breaks Sonarr/Radarr | Low | Standard Transmission behavior; should be handled |
| Increased API calls | Low | Actually reduced for in-progress transfers |

## Open Questions (Phase-Specific Research)

1. **SaveParentID Persistence:** Does Put.io clear SaveParentID after transfer completes?
   - Impact: Determines if simplified logic possible
   - Resolution: Test with completed transfers, log SaveParentID values
   - Flag: **Validation needed in phase execution**

2. **Files.Get(SaveParentID) Return Value:** Confirms returns parent folder, not transfer?
   - Impact: Critical for label matching
   - Resolution: Test with in-progress transfer, verify parent.Name
   - Flag: **Validation needed in phase execution**

3. **Transfer.IsAvailable() Method:** Should it include in-progress statuses?
   - Current: Only checks completed/seeding/finished
   - Impact: Transfer orchestrator may skip in-progress
   - Resolution: Review usage in TransferOrchestrator
   - Flag: **Review during implementation**

4. **Status Edge Cases:** Are there other statuses besides 8 documented?
   - Impact: Low - default fallback to StatusStopped
   - Resolution: Monitor logs for unknown statuses
   - Flag: **Monitor during rollout**

## Sources

### HIGH Confidence (Verified)
- [putio package - github.com/putdotio/go-putio](https://pkg.go.dev/github.com/putdotio/go-putio) - Transfer struct
- Existing codebase: `internal/dc/putio/client.go:79-94` - Field usage
- Existing codebase: `internal/http/rest/transmission.go:473-486` - Status mapping
- Go module: `go.mod:github.com/putdotio/go-putio v1.7.2`

### MEDIUM Confidence (Inferred)
- [putioarr](https://github.com/wouterdebie/putioarr) - Rust implementation, similar patterns
- [plundrio](https://github.com/elsbrock/plundrio) - Go implementation, Transmission proxy
- [Sonarr forums: put.io support](https://forums.sonarr.tv/t/experimental-put-io-support/8928) - Real-world usage

### LOW Confidence (Needs Verification)
- Put.io official API documentation - Referenced but not directly accessed
- SaveParentID lifecycle behavior - Not documented
- Transfer status enumeration - Inferred from code patterns

---

**Research Complete:** 2026-02-08
**Researcher:** GSD Project Researcher Agent
**Next Step:** Use this research to create detailed phase roadmap for Activity tab in-progress download support

**Key Recommendation:** This milestone requires NO new dependencies or SDK upgrades. All necessary fields exist in the current Put.io SDK v1.7.2. Implementation focuses on logic changes in `internal/dc/putio/client.go` to stop filtering FileID==0 transfers and add conditional parent lookup.
