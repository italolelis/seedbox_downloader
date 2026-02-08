# Phase 12: In-Progress Visibility - Research

**Researched:** 2026-02-08
**Domain:** Go HTTP/RPC API integration with Transmission protocol
**Confidence:** HIGH

## Summary

Phase 12 enables Sonarr/Radarr Activity tab visibility for in-progress downloads by removing the FileID==0 filter in GetTaggedTorrents, adding complete status mapping, populating peer/speed fields, and adding labels support. The existing architecture provides triple protection against false positives in the download pipeline through IsAvailable(), IsDownloadable(), and conditional file population.

Phase 11 already completed SaveParentID-based tag matching (requirement ACTIVITY-02), which is the foundation for this phase. The remaining requirements are straightforward data mapping from Put.io Transfer fields to Transmission RPC response fields.

**Primary recommendation:** Remove FileID==0 filter, add conditional Files array population (only when FileID!=0), map all Put.io statuses to Transmission codes, populate peer/speed fields from existing Put.io data, and add Labels array to TransmissionTorrent struct.

**Risk level:** LOW - Triple protection prevents download pipeline false positives.

## Standard Stack

### Core (Already in Use)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| go-putio | v1.7.2 | Put.io API client | Official Put.io SDK, all required fields present |
| chi/v5 | v5.2.1 | HTTP routing | Lightweight, idiomatic Go router |
| testify | v1.11.1 | Testing assertions | De facto standard for Go testing |
| encoding/json | stdlib | JSON serialization | Standard library, zero deps |

### Supporting (Already in Use)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| net/http/httptest | stdlib | HTTP testing | Mock Put.io API responses |
| strconv | stdlib | String conversion | Parse transfer IDs |
| crypto/sha1 | stdlib | Hash generation | Generate Transmission hash strings |

### No New Dependencies Required

This phase requires ZERO new dependencies. All required fields exist in go-putio v1.7.2:
- `DownloadSpeed` (int) → rateDownload
- `PeersConnected` (int) → peersConnected
- `PeersSendingToUs` (int) → peersSendingToUs
- `PeersGettingFromUs` (int) → peersGettingFromUs
- `ErrorMessage` (string) → errorString
- `SaveParentID` (int64) → already used in Phase 11

## Architecture Patterns

### Current Architecture (No Changes Required)

```
Sonarr/Radarr → handleTorrentGet → GetTaggedTorrents → Put.io API
                       ↓
              TransmissionTorrent response
                       ↓
                Activity Tab Display

TransferOrchestrator.watchTransfers → GetTaggedTorrents → Put.io API
                       ↓
              IsAvailable() filter → IsDownloadable() filter
                       ↓
              Download Pipeline (only completed transfers)
```

**Key insight:** Two independent consumers of GetTaggedTorrents:
1. **Transmission RPC (Activity Tab)** - Wants ALL transfers (completed + in-progress)
2. **TransferOrchestrator (Download Pipeline)** - Wants ONLY completed transfers (via filters)

### Pattern 1: Conditional File Population

**What:** Only populate Files array when FileID != 0 (completed transfers have FileID)

**When to use:** Prevent API errors when fetching files for in-progress transfers

**Example:**
```go
// internal/dc/putio/client.go (lines 52-109)
for _, t := range transfers {
    // Phase 11: SaveParentID tag matching (already implemented)
    if t.SaveParentID == 0 {
        logger.DebugContext(ctx, "skipping transfer with no save parent")
        continue
    }

    parent, err := c.putioClient.Files.Get(ctx, t.SaveParentID)
    if err != nil || parent.Name != tag {
        continue
    }

    torrent := &transfer.Transfer{
        ID:       fmt.Sprintf("%d", t.ID),
        Name:     t.Name,
        Label:    tag,
        Progress: float64(t.PercentDone),
        Status:   t.Status,
        Files:    make([]*transfer.File, 0), // Empty initially
        // ... peer/speed fields from t.PeersConnected, etc.
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
```

**Why this works:**
- In-progress transfers: FileID==0 → empty Files → IsDownloadable() returns false
- Completed transfers: FileID!=0 → populated Files → IsDownloadable() returns true
- Download pipeline uses both IsAvailable() AND IsDownloadable() filters

### Pattern 2: Comprehensive Status Mapping

**What:** Map all Put.io statuses to Transmission RPC status codes

**When to use:** Ensure accurate Activity tab display for all transfer states

**Example:**
```go
// internal/http/rest/transmission.go (lines 470-486)
// Current incomplete mapping
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

// NEW: Complete mapping (Phase 12)
switch strings.ToLower(transfer.Status) {
case "downloading":
    status = StatusDownload // 4
case "in_queue", "waiting":
    status = StatusDownloadWait // 3
case "finishing", "checking":
    status = StatusCheck // 2
case "completed", "finished":
    status = StatusSeed // 6
case "seeding":
    status = StatusSeed // 6
case "seedingwait":
    status = StatusSeedWait // 5
case "error":
    status = StatusStopped // 0
    // Populate errorString from transfer.ErrorMessage
default:
    logger.WarnContext(ctx, "unknown put.io status", "status", transfer.Status)
    status = StatusStopped // 0 (safe fallback)
}
```

**Put.io status strings from go-putio v1.7.2:**
- DOWNLOADING, SEEDING, SEEDINGWAIT, COMPLETED, FINISHING, IN_QUEUE, WAITING, ERROR
- API returns lowercase: "downloading", "seeding", etc.

**Transmission RPC status codes (official spec):**
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

### Pattern 3: Labels Array Population

**What:** Add Labels []string field to TransmissionTorrent, populate with configured proxy label

**When to use:** Sonarr/Radarr may filter/categorize transfers by label

**Example:**
```go
// internal/http/rest/transmission.go (lines 48-66)
type TransmissionTorrent struct {
    ID                 int64                     `json:"id"`
    HashString         string                    `json:"hashString,omitempty"`
    Name               string                    `json:"name"`
    DownloadDir        string                    `json:"downloadDir"`
    TotalSize          int64                     `json:"totalSize"`
    LeftUntilDone      int64                     `json:"leftUntilDone"`
    IsFinished         bool                      `json:"isFinished"`
    ETA                int64                     `json:"eta"`
    Status             TransmissionTorrentStatus `json:"status"`
    SecondsDownloading int64                     `json:"secondsDownloading"`
    ErrorString        *string                   `json:"errorString,omitempty"`
    DownloadedEver     int64                     `json:"downloadedEver"`

    // NEW: Phase 12 additions
    Labels             []string                  `json:"labels,omitempty"`
    PeersConnected     int64                     `json:"peersConnected"`
    PeersSendingToUs   int64                     `json:"peersSendingToUs"`
    PeersGettingFromUs int64                     `json:"peersGettingFromUs"`
    RateDownload       int64                     `json:"rateDownload"`

    // Existing seed limit fields
    SeedRatioLimit     float32                   `json:"seedRatioLimit"`
    SeedRatioMode      uint32                    `json:"seedRatioMode"`
    SeedIdleLimit      uint64                    `json:"seedIdleLimit"`
    SeedIdleMode       uint32                    `json:"seedIdleMode"`
    FileCount          uint32                    `json:"fileCount"`
}

// In handleTorrentGet (lines 460-508)
transmissionTorrents[i] = TransmissionTorrent{
    ID:                 id,
    HashString:         hex.EncodeToString(hashBytes[:]),
    Name:               transfer.Name,
    DownloadDir:        transfer.SavePath,
    Status:             status,
    // ... existing fields ...

    // NEW: Populate from Put.io transfer data
    Labels:             []string{h.label}, // From handler config
    PeersConnected:     transfer.PeersConnected,
    PeersSendingToUs:   transfer.PeersSendingToUs,
    PeersGettingFromUs: transfer.PeersGettingFromUs,
    RateDownload:       int64(t.DownloadSpeed), // Note: go-putio uses int
}
```

### Anti-Patterns to Avoid

- **Anti-pattern:** Removing IsAvailable() or IsDownloadable() filters thinking they're redundant
  - **Why it's bad:** Triple protection (status filter + empty files + IsDownloadable) prevents edge cases
  - **What to do instead:** Keep all three filters, they're complementary safety nets

- **Anti-pattern:** Calling getFilesRecursively for in-progress transfers (FileID==0)
  - **Why it's bad:** API error, wasted API calls, rate limit consumption
  - **What to do instead:** Guard with `if t.FileID != 0` before calling

- **Anti-pattern:** Ignoring unknown Put.io statuses silently
  - **Why it's bad:** New statuses won't be visible, no alerting for investigation
  - **What to do instead:** Warn-log unknown statuses, use StatusStopped fallback

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTTP mocking | Custom HTTP client wrapper | httptest.NewServer | Standard library, zero config |
| JSON response building | String concatenation | json.Marshal with structs | Type-safe, avoids escaping bugs |
| Transfer ID hashing | Custom hash function | crypto/sha1 (already used) | Consistent with existing code |
| Status string normalization | Manual case handling | strings.ToLower (already used) | Simple, clear, sufficient |

**Key insight:** This phase requires zero custom abstractions. All functionality exists in standard library or current dependencies.

## Common Pitfalls

### Pitfall 1: Forgetting FileID==0 Guard for getFilesRecursively

**What goes wrong:** API error when trying to fetch files for in-progress transfer (FileID is 0 or null)

**Why it happens:** Current code always calls getFilesRecursively after FileID filter, new code removes filter

**How to avoid:** Add conditional: `if t.FileID != 0 { /* call getFilesRecursively */ }`

**Warning signs:**
- Error logs: "failed to get file: file not found"
- Empty torrents array returned from GetTaggedTorrents
- API 404 errors from Put.io Files.Get(0)

### Pitfall 2: Incomplete Status Mapping

**What goes wrong:** In-progress transfers show incorrect status in Activity tab (e.g., "stopped" instead of "downloading")

**Why it happens:** Default case in switch statement doesn't handle all Put.io statuses

**How to avoid:**
1. Map all documented Put.io statuses (downloading, in_queue, waiting, finishing, error)
2. Add warn-log for unknown statuses
3. Use StatusStopped (0) as safe fallback

**Warning signs:**
- Activity tab shows "stopped" for active downloads
- Users report downloads appear stuck but are progressing in Put.io
- Warn logs about unknown statuses

### Pitfall 3: Missing ErrorString Population

**What goes wrong:** Error transfers show status "stopped" but no error message, users don't know why

**Why it happens:** ErrorMessage field populated in Transfer struct but not mapped to TransmissionTorrent.ErrorString

**How to avoid:** When status is "error", populate ErrorString from transfer.ErrorMessage

**Warning signs:**
- Activity tab shows stopped transfers with no explanation
- Users ask why downloads stopped
- Error context missing from Transmission response

### Pitfall 4: Breaking Download Pipeline with FileID Filter Removal

**What goes wrong:** In-progress transfers trigger downloads, file not found errors, wasted bandwidth

**Why it happens:** Removing FileID==0 filter without understanding IsAvailable/IsDownloadable protection

**How to avoid:**
1. Verify IsAvailable() returns false for non-completed statuses
2. Verify IsDownloadable() returns false for empty Files arrays
3. Test TransferOrchestrator.watchTransfers with in-progress transfers

**Warning signs:**
- Error logs: "failed to get file" during download attempts
- Database records for in-progress transfers
- Repeated failed download attempts for same transfer

## Code Examples

Verified patterns from codebase and standard library:

### Remove FileID Filter, Add Conditional Files Population

```go
// Source: internal/dc/putio/client.go:52-56 (existing filter to remove)
// Source: internal/dc/putio/client.go:104-109 (existing file population)

// BEFORE (Phase 11)
for _, t := range transfers {
    if t.FileID == 0 {
        logger.DebugContext(ctx, "skipping transfer because it's not a downloadable transfer")
        continue
    }

    // ... SaveParentID tag matching ...

    // Always call getFilesRecursively (fails if FileID==0)
    files, err := c.getFilesRecursively(ctx, t.FileID, file.Name)
    if err != nil {
        return nil, fmt.Errorf("failed to get files for transfer: %w", err)
    }
    torrent.Files = append(torrent.Files, files...)
}

// AFTER (Phase 12)
for _, t := range transfers {
    // REMOVED: FileID==0 filter

    // SaveParentID tag matching (Phase 11, unchanged)
    if t.SaveParentID == 0 {
        logger.DebugContext(ctx, "skipping transfer with no save parent")
        continue
    }

    parent, err := c.putioClient.Files.Get(ctx, t.SaveParentID)
    if err != nil || parent.Name != tag {
        continue
    }

    // Fetch file for completed transfers only (needed for file path)
    var file *putio.File
    if t.FileID != 0 {
        file, err = c.putioClient.Files.Get(ctx, t.FileID)
        if err != nil {
            logger.ErrorContext(ctx, "failed to get file", "transfer_id", t.ID, "err", err)
            continue
        }
    }

    // Create transfer with empty Files initially
    torrent := &transfer.Transfer{
        ID:                 fmt.Sprintf("%d", t.ID),
        Name:               t.Name,
        Label:              tag,
        Progress:           float64(t.PercentDone),
        Files:              make([]*transfer.File, 0), // Empty for in-progress
        Size:               int64(t.Size),
        Source:             t.Source,
        Status:             t.Status,
        EstimatedTime:      t.EstimatedTime,
        SavePath:           "/" + tag,
        PeersConnected:     int64(t.PeersConnected),
        PeersGettingFromUs: int64(t.PeersGettingFromUs),
        PeersSendingToUs:   int64(t.PeersSendingToUs),
        Downloaded:         int64(t.Downloaded),
    }

    // NEW: Conditional file population (only when FileID != 0)
    if t.FileID != 0 && file != nil {
        files, err := c.getFilesRecursively(ctx, file.ID, file.Name)
        if err != nil {
            logger.ErrorContext(ctx, "failed to get files for completed transfer",
                "transfer_id", t.ID, "file_id", t.FileID, "err", err)
            // Continue anyway - transfer visible but without files
        } else {
            torrent.Files = append(torrent.Files, files...)
        }
    }

    torrents = append(torrents, torrent)
}
```

### Complete Status Mapping with Error Handling

```go
// Source: internal/http/rest/transmission.go:470-486 (existing incomplete mapping)

// Map status with complete coverage
var status TransmissionTorrentStatus
var errorString *string

switch strings.ToLower(transfer.Status) {
case "downloading":
    status = StatusDownload // 4
case "in_queue", "waiting":
    status = StatusDownloadWait // 3
case "finishing", "checking":
    status = StatusCheck // 2
case "completed", "finished":
    status = StatusSeed // 6
case "seeding":
    status = StatusSeed // 6
case "seedingwait":
    status = StatusSeedWait // 5
case "error":
    status = StatusStopped // 0
    if transfer.ErrorMessage != "" {
        errorString = &transfer.ErrorMessage
    }
default:
    logger.WarnContext(ctx, "unknown put.io status, defaulting to stopped",
        "status", transfer.Status, "transfer_id", transfer.ID)
    status = StatusStopped // 0
}

// Use errorString in TransmissionTorrent construction
transmissionTorrents[i] = TransmissionTorrent{
    // ... existing fields ...
    Status:      status,
    ErrorString: errorString, // nil for successful transfers, populated for errors
    // ... remaining fields ...
}
```

### Add Peer/Speed Fields to TransmissionTorrent Response

```go
// Source: internal/http/rest/transmission.go:490-508 (existing torrent conversion)

transmissionTorrents[i] = TransmissionTorrent{
    ID:             id,
    HashString:     hex.EncodeToString(hashBytes[:]),
    Name:           transfer.Name,
    DownloadDir:    transfer.SavePath,
    TotalSize:      transfer.Size,
    LeftUntilDone:  transfer.Size - transfer.Downloaded,
    IsFinished:     strings.ToLower(transfer.Status) == "completed" ||
                    strings.ToLower(transfer.Status) == "seeding",
    ETA:            transfer.EstimatedTime,
    Status:         status,
    ErrorString:    errorString, // From status mapping above
    DownloadedEver: transfer.Downloaded,
    FileCount:      uint32(len(transfer.Files)),

    // NEW: Phase 12 peer/speed fields
    PeersConnected:     transfer.PeersConnected,
    PeersSendingToUs:   transfer.PeersSendingToUs,
    PeersGettingFromUs: transfer.PeersGettingFromUs,
    RateDownload:       int64(t.DownloadSpeed), // Put.io field: DownloadSpeed (int)

    // NEW: Phase 12 labels
    Labels:             []string{h.label}, // From TransmissionHandler config

    // Existing seed limit fields
    SeedRatioLimit: 1.0,
    SeedRatioMode:  1,
    SeedIdleLimit:  100,
    SeedIdleMode:   1,
}
```

### Test Pattern: Verify In-Progress Transfers Filtered from Download Pipeline

```go
// Source: internal/dc/putio/client_test.go:203-211 (existing test pattern)

func TestTransferOrchestrator_SkipsInProgressTransfers(t *testing.T) {
    // Setup mock server with in-progress transfer
    mux := http.NewServeMux()
    mux.HandleFunc("/v2/transfers/list", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, `{"transfers":[{
            "id":1,"name":"in-progress","file_id":0,"save_parent_id":200,
            "status":"DOWNLOADING","percent_done":50,"size":2000,
            "downloaded":1000,"peers_connected":5
        }]}`)
    })
    mux.HandleFunc("/v2/files/200", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, `{"file":{"id":200,"name":"mytag","file_type":"FOLDER"}}`)
    })

    server := httptest.NewServer(mux)
    defer server.Close()

    client := newTestClient(server.URL)
    transfers, err := client.GetTaggedTorrents(context.Background(), "mytag")

    require.NoError(t, err)
    require.Len(t, transfers, 1) // In-progress transfer returned

    // Verify filters work
    transfer := transfers[0]
    assert.False(t, transfer.IsAvailable(), "in-progress should not be available")
    assert.False(t, transfer.IsDownloadable(), "in-progress should not be downloadable (empty files)")
    assert.Empty(t, transfer.Files, "in-progress should have empty files array")
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| FileID-based tag matching | SaveParentID-based tag matching | Phase 11 (2026-02-08) | Enables in-progress transfer matching |
| Filter in-progress at GetTaggedTorrents | Filter in-progress at IsAvailable/IsDownloadable | Phase 12 | Enables Activity tab visibility |
| Incomplete status mapping | Complete Put.io status coverage | Phase 12 | Accurate status display for all states |
| No peer/speed data | Full peer/speed population | Phase 12 | Activity tab shows live download metrics |
| No labels support | Labels array in response | Phase 12 | Enables Sonarr/Radarr category filtering |

**Not deprecated, but notable:**
- Triple protection (IsAvailable + IsDownloadable + conditional files) is intentional defense-in-depth
- Empty Files array for in-progress transfers is feature, not bug

## Open Questions

None. All information available from:
1. Phase 11 implementation (SaveParentID tag matching validated)
2. go-putio v1.7.2 Transfer struct (all required fields present)
3. Transmission RPC spec (official status codes documented)
4. Existing codebase (patterns established)

## Sources

### Primary (HIGH confidence)

- go-putio v1.7.2 Transfer struct - /Users/italovietro/go/pkg/mod/github.com/putdotio/go-putio@v1.7.2/types.go:60-100
  - All required fields present: DownloadSpeed, PeersConnected, PeersSendingToUs, PeersGettingFromUs, ErrorMessage
- Phase 11 implementation - internal/dc/putio/client.go:58-77
  - SaveParentID tag matching validated with 6 httptest scenarios
- Transfer struct - internal/transfer/transfer.go:27-44
  - IsAvailable(), IsDownloadable() filter methods
- TransferOrchestrator - internal/transfer/transfer.go:138-186
  - watchTransfers applies both filters before download
- Transmission RPC handler - internal/http/rest/transmission.go:444-523
  - handleTorrentGet converts transfers to Transmission format

### Secondary (MEDIUM confidence)

- Transmission RPC specification - https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md
  - Official status codes: 0=stopped, 1=checkwait, 2=check, 3=downloadwait, 4=download, 5=seedwait, 6=seed
- Architecture research - .planning/research/ARCHITECTURE-ACTIVITY-TAB.md
  - Phase build order, risk analysis, testing strategy validated

### Tertiary (LOW confidence)

- None. All findings verified against primary sources.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - No new dependencies, all fields exist in go-putio v1.7.2
- Architecture: HIGH - Phase 11 validates SaveParentID approach, triple protection verified
- Pitfalls: HIGH - Derived from code analysis and Phase 11 test scenarios

**Research date:** 2026-02-08
**Valid until:** 90 days (stable domain, no fast-moving dependencies)
