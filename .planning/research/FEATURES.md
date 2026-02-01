# Feature Research: .torrent File Support

**Domain:** Transmission RPC webhook with Put.io integration
**Researched:** 2026-02-01
**Confidence:** HIGH

## Feature Landscape

### Table Stakes (Users Expect These)

Features Sonarr/Radarr expect when sending .torrent files to a Transmission-compatible download client. Missing these = integration broken.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Parse base64-encoded MetaInfo field | Transmission RPC spec requires either filename or metainfo field for torrent-add | LOW | Standard base64 decoding, field already present in TransmissionRequest struct (line 68) |
| Upload .torrent content to Put.io | Put.io client supports file upload via io.Reader (FilesService.Upload) | LOW | Use existing go-putio library, no new dependencies |
| Return success response with torrent ID | Transmission RPC spec requires "success" result with torrent details in arguments | LOW | Already implemented for magnet links (lines 250-260), extend to .torrent files |
| Return error response for invalid .torrent | Transmission returns "invalid or corrupt torrent file" when metainfo cannot be parsed | LOW | Standard error handling pattern already exists |
| Support both FileName and MetaInfo in same endpoint | Either field can be provided per Transmission spec, not both simultaneously | LOW | Current code only handles FileName (line 239), add conditional for MetaInfo |

### Differentiators (Competitive Advantage)

Features that improve operational visibility beyond basic .torrent file support.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Log torrent type (magnet vs file) explicitly | Operators need visibility into which content type is being used for debugging tracker-specific issues | LOW | Add log field "torrent_type": "magnet" or "file" in handleTorrentAdd |
| OpenTelemetry metric for torrent type distribution | Track magnet vs .torrent file usage over time to understand tracker behavior patterns | LOW | Add counter metric with "type" label (magnet/file), existing telemetry infrastructure in place |
| Explicit log when MetaInfo field is empty | Silent failures are project anti-pattern (v1 core value: no silent failures) | LOW | Log at Info level when MetaInfo present, Debug when using magnet |
| Include .torrent file size in logs | Helps identify bloated .torrent files from certain trackers | LOW | Log base64-decoded byte count before upload |
| Log Put.io file ID after .torrent upload | Provides traceability for debugging transfer failures | LOW | Put.io Upload response contains File.ID field |

### Anti-Features (Commonly Requested, Often Problematic)

Features that seem good but violate project constraints or create technical debt.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Persist .torrent files to disk | "We should save .torrent files for backup" | Violates v1.1 explicit constraint: "No File Persistence" (PROJECT.md line 96) | Put.io stores the .torrent content, no local persistence needed |
| Watch folder for .torrent files | "Let users drop .torrent files into a folder" | Not how Sonarr/Radarr work (they push via API), scope creep | Defer to future milestone (PROJECT.md line 56) |
| Direct .torrent file upload API | "Bypass Sonarr/Radarr with direct upload" | Adds new API surface area, not needed for milestone goal | Defer to future milestone (PROJECT.md line 57) |
| Deluge .torrent support | "Support .torrent files for both clients" | Webhook API is Put.io only, Deluge not used in webhook path | Explicitly out of scope (PROJECT.md line 58) |
| Cache decoded .torrent content | "Avoid re-decoding same .torrent" | Sonarr/Radarr send each .torrent once, no repeat requests | Unnecessary complexity, YAGNI |

## Feature Dependencies

```
[Base64 decode MetaInfo]
    └──requires──> [Validate .torrent content]
                       └──requires──> [Upload to Put.io via io.Reader]
                                          └──requires──> [Return Transfer ID]

[Log torrent type] ──enhances──> [All torrent-add operations]

[OpenTelemetry metric] ──requires──> [Log torrent type] (use same type determination logic)

[Error handling] ──blocks──> [Upload to Put.io] (must handle before attempting upload)
```

### Dependency Notes

- **Base64 decode requires validation:** Cannot upload invalid .torrent content to Put.io, must validate first
- **Upload requires io.Reader:** go-putio FilesService.Upload expects io.Reader, use bytes.NewReader after decode
- **Logging enhances observability:** Torrent type logging independent of functional requirements but critical for operational visibility (project core value)
- **Error handling blocks upload:** Must detect invalid base64 or corrupt .torrent before API call to Put.io

## MVP Definition

### Launch With (v1.1)

Minimum viable features to enable .torrent-only tracker (amigos-share) support.

- [x] Parse MetaInfo field from Transmission RPC torrent-add request
- [x] Base64 decode MetaInfo content
- [x] Validate .torrent content is not empty after decode
- [x] Upload .torrent content to Put.io using FilesService.Upload
- [x] Return Transmission-compatible success response with Transfer ID
- [x] Return error response for invalid/corrupt .torrent content
- [x] Log explicitly whether magnet or .torrent file is being processed
- [x] Add OpenTelemetry counter metric for torrent type (magnet vs file)

### Add After Validation (v1.2+)

Features to add once core .torrent support is working in production.

- [ ] Log .torrent file size (decoded bytes) — helps identify bloated .torrent files
- [ ] Log Put.io file ID after upload — improves traceability for debugging
- [ ] Test coverage for .torrent file handling — ensure regression prevention
- [ ] Test coverage for error cases (invalid base64, corrupt .torrent) — prevent silent failures

### Future Consideration (v2+)

Features to defer until product-market fit is established or new use cases emerge.

- [ ] Watch folder for .torrent files — requires new architecture (file system monitoring)
- [ ] Direct .torrent file upload API — requires new endpoint, authentication considerations
- [ ] Deluge .torrent support — requires extending Deluge client, not needed for current use case
- [ ] .torrent file validation (bencode parsing) — Put.io validates on their end, duplicate effort
- [ ] Resume support for failed .torrent uploads — low priority, Sonarr/Radarr will retry

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Parse MetaInfo field | HIGH | LOW | P1 |
| Base64 decode MetaInfo | HIGH | LOW | P1 |
| Upload to Put.io | HIGH | LOW | P1 |
| Return success response | HIGH | LOW | P1 |
| Error handling for invalid .torrent | HIGH | LOW | P1 |
| Log torrent type explicitly | HIGH | LOW | P1 |
| OpenTelemetry metric for type | MEDIUM | LOW | P1 |
| Log .torrent file size | LOW | LOW | P2 |
| Log Put.io file ID | LOW | LOW | P2 |
| Test coverage for .torrent handling | HIGH | MEDIUM | P2 |
| Test coverage for error cases | MEDIUM | MEDIUM | P2 |
| Watch folder support | LOW | HIGH | P3 |
| Direct upload API | LOW | HIGH | P3 |
| Deluge .torrent support | LOW | MEDIUM | P3 |

**Priority key:**
- P1: Must have for launch (v1.1 blocker)
- P2: Should have, add when possible (v1.2)
- P3: Nice to have, future consideration (v2+)

## Technical Specification

### Transmission RPC Protocol

**Source:** [Transmission RPC Specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md)

#### torrent-add Method

The `torrent-add` method accepts either `filename` or `metainfo` parameter:

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `filename` | string | URL or magnet link to torrent | `magnet:?xt=urn:btih:...` |
| `metainfo` | string | Base64-encoded .torrent file content | `ZDg6YW5ub3VuY2UzM...` |

**Key behaviors:**
- Either `filename` or `metainfo` must be provided, not both
- `metainfo` contains raw .torrent file bytes encoded as base64
- JSON doesn't allow binary data, hence base64 encoding requirement
- When metainfo is invalid: return `{"result":"invalid or corrupt torrent file"}`

#### Response Format

Success response:
```json
{
  "result": "success",
  "arguments": {
    "torrents": [
      {
        "id": 123,
        "name": "Example Torrent",
        "hashString": "abc123..."
      }
    ]
  }
}
```

Error response:
```json
{
  "result": "invalid or corrupt torrent file"
}
```

### Put.io API Integration

**Source:** [go-putio package documentation](https://pkg.go.dev/github.com/putdotio/go-putio)

#### Upload Method

The `FilesService.Upload` method uploads file content:

```go
func (f *FilesService) Upload(
    ctx context.Context,
    r io.Reader,        // File content stream
    filename string,    // File name (e.g., "download.torrent")
    parent int64        // Parent folder ID (-1 for default)
) (Upload, error)
```

**Response:**
```go
type Upload struct {
    File     *File     // Populated for non-torrent files
    Transfer *Transfer // Populated when uploaded file is a .torrent
}
```

**Key behaviors:**
- When uploaded file is `.torrent`, Put.io automatically creates a Transfer
- `Transfer` field contains transfer ID and status
- `File` field is nil for torrent uploads
- Upload reads content into memory, suitable for .torrent files (typically <1MB)

### Sonarr/Radarr Behavior

**Sources:**
- [Sonarr Download Clients](https://buildarr.github.io/plugins/sonarr/configuration/download-clients/)
- [Radarr Settings](https://wiki.servarr.com/sonarr/settings)

**Observed behavior:**
- Sonarr/Radarr detect if indexer returns magnet link or .torrent file
- For magnet links: send `filename` parameter with magnet URI
- For .torrent files: download .torrent, base64 encode, send `metainfo` parameter
- Expect Transmission-compatible response format
- Retry on error responses with exponential backoff

### Error Handling

**Sources:**
- [Transmission RPC Error Responses](https://forum.transmissionbt.com/viewtopic.php?t=8023)
- Project requirement: "No silent failures" (PROJECT.md line 9)

| Error Condition | Detection | Response | HTTP Status | Log Level |
|----------------|-----------|----------|-------------|-----------|
| MetaInfo field empty | `req.Arguments.MetaInfo == ""` | Process as magnet (FileName field) | 200 OK | Debug |
| Both MetaInfo and FileName empty | Both fields empty string | "filename or metainfo required" | 400 Bad Request | Error |
| Invalid base64 in MetaInfo | `base64.StdEncoding.DecodeString` error | "invalid base64 encoding" | 400 Bad Request | Error |
| Decoded .torrent is empty | `len(decodedBytes) == 0` | "invalid or corrupt torrent file" | 400 Bad Request | Error |
| Put.io upload fails | `FilesService.Upload` returns error | Forward Put.io error message | 400 Bad Request | Error |
| Put.io Upload.Transfer is nil | Upload succeeds but Transfer field nil | "failed to create transfer from torrent" | 500 Internal Server Error | Error |

### Observability Requirements

**Source:** Existing telemetry infrastructure (internal/telemetry/)

#### Logging

Structured logging with fields:

```go
logger.Info("processing torrent add request",
    "torrent_type", "file",              // "magnet" or "file"
    "has_metainfo", true,                // boolean
    "metainfo_size_bytes", 45123,        // decoded byte count (optional, P2)
    "putio_file_id", 789,                // Put.io file ID (optional, P2)
    "putio_transfer_id", 456,            // Put.io transfer ID
)
```

#### OpenTelemetry Metrics

Counter metric for torrent type distribution:

```go
meter := otel.GetMeterProvider().Meter("seedbox_downloader")
torrentAddCounter, _ := meter.Int64Counter(
    "torrent_add_total",
    metric.WithDescription("Total torrent-add requests by type"),
)

torrentAddCounter.Add(ctx, 1,
    metric.WithAttributes(
        attribute.String("type", "file"), // "magnet" or "file"
    ),
)
```

## Implementation Notes

### Existing Code Analysis

**Current behavior (internal/http/rest/transmission.go:230-248):**
- Only handles `FileName` field (line 239)
- When `MetaInfo == ""`, processes as magnet link
- When `MetaInfo != ""`, silently ignores (falls through to nil torrent)
- Returns success response with nil torrent, causing nil pointer on JSON marshal

**Required changes:**
1. Add conditional check for `MetaInfo != ""`
2. Base64 decode MetaInfo content
3. Create `bytes.NewReader` from decoded content
4. Call `h.dc.UploadTorrent(ctx, reader, h.label)` (new method)
5. Log torrent type for observability
6. Add OpenTelemetry counter metric

**New method required in Put.io client:**
```go
// UploadTorrent uploads .torrent file content to Put.io and returns Transfer
func (c *Client) UploadTorrent(ctx context.Context, torrentContent io.Reader, downloadDir string) (*transfer.Transfer, error)
```

### Backward Compatibility

**Constraint:** Must not change existing APIs, configuration, or database schema (PROJECT.md line 92)

**Verification:**
- ✓ No changes to TransmissionRequest struct (MetaInfo field already exists)
- ✓ No changes to configuration (uses existing Put.io client)
- ✓ No database schema changes (transfers stored same way)
- ✓ Existing magnet link path unchanged (conditional preserves existing behavior)
- ✓ Existing deployments work without config updates (feature is additive)

### Testing Strategy

**P2 - Defer to v1.2:**
- Unit test: Valid base64 .torrent content → successful upload
- Unit test: Invalid base64 → error response
- Unit test: Empty decoded content → error response
- Unit test: Put.io upload failure → error response
- Unit test: Magnet link still works (regression prevention)
- Integration test: End-to-end .torrent file flow with real Put.io API (staging environment)

## Sources

### Official Documentation (HIGH confidence)
- [Transmission RPC Specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md) - Official protocol spec
- [go-putio Package Documentation](https://pkg.go.dev/github.com/putdotio/go-putio) - Official Go client library
- [Transmission RPC Forum - MetaInfo Usage](https://forum.transmissionbt.com/viewtopic.php?t=8023) - Official forum discussion on metainfo parameter

### Ecosystem Documentation (MEDIUM confidence)
- [Sonarr Download Clients](https://buildarr.github.io/plugins/sonarr/configuration/download-clients/) - Community documentation
- [Sonarr Settings](https://wiki.servarr.com/sonarr/settings) - Official Servarr wiki
- [OpenTelemetry Metrics](https://opentelemetry.io/docs/concepts/signals/metrics/) - Official OTel documentation

### Implementation Examples (MEDIUM confidence)
- [transmission-rpc Python client](https://transmission-rpc.readthedocs.io/en/v3.3.0/_modules/transmission_rpc/client.html) - Reference implementation showing metainfo usage
- [Put.io API clients](https://github.com/putdotio/go-putio) - Official Go client showing upload patterns

---
*Feature research for: v1.1 .torrent File Support*
*Researched: 2026-02-01*
*Confidence: HIGH - All core findings verified against official documentation*
