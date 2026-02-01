# Stack Research: .torrent File Content Support

**Domain:** Transmission API proxy enhancement
**Researched:** 2026-02-01
**Confidence:** HIGH

## Recommended Stack

### Core Technologies (No Changes)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Go stdlib `encoding/base64` | Go 1.23+ | Decode base64-encoded .torrent content | Built-in, RFC 4648 compliant, zero dependencies |
| Go stdlib `bytes` | Go 1.23+ | Convert decoded bytes to io.Reader | Built-in, efficient for in-memory operations |
| `github.com/putdotio/go-putio` | v1.7.2 (current) | Upload .torrent file content to Put.io | Already in use, supports torrent file uploads via Files.Upload() |

### Supporting Libraries (None Required)

| Library | Purpose | Status |
|---------|---------|--------|
| Torrent parsing libraries | Parse .torrent file structure | **NOT NEEDED** - Put.io accepts raw .torrent bytes |
| File I/O libraries | Save .torrent files to disk | **NOT NEEDED** - Constraint: no file persistence |

## Integration Points

### Existing Code Integration

**Current magnet link handling:**
```go
// internal/dc/putio/client.go:162
t, err := c.putioClient.Transfers.Add(ctx, url, dirID, "")
```

**New .torrent file handling will use:**
```go
// Use Files.Upload instead of Transfers.Add
upload, err := c.putioClient.Files.Upload(ctx, reader, filename, dirID)
// upload.Transfer field will contain the Transfer object (same as Transfers.Add)
```

### Put.io SDK Method Details

**Method:** `FilesService.Upload(ctx, r, filename, parent)`
- **Source:** `/Users/italovietro/go/pkg/mod/github.com/putdotio/go-putio@v1.7.2/files.go:215`
- **Signature:** `func (f *FilesService) Upload(ctx context.Context, r io.Reader, filename string, parent int64) (Upload, error)`
- **Behavior:** "If the uploaded file is a torrent file, Put.io will interpret it as a transfer and Transfer field will be present to represent the status of the transfer."
- **Limitation:** Reads file contents into memory, should only be used for <150MB files (sufficient for .torrent files which are typically <500KB)
- **Return type:** `Upload{File *File, Transfer *Transfer}` - Transfer field populated when uploading .torrent files

### Transmission RPC MetaInfo Format

**Format:** Base64-encoded .torrent file content
- **Source:** [Transmission RPC Spec](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md)
- **Specification:** "base64-encoded .torrent content"
- **Current field:** `internal/http/rest/transmission.go:68` - `MetaInfo string` field exists but unused

## Implementation Pattern

### Data Flow for .torrent Files

```
1. Sonarr/Radarr → Transmission API
   - POST /transmission/rpc with torrent-add method
   - MetaInfo field contains base64-encoded .torrent content

2. Transmission Handler (internal/http/rest/transmission.go)
   - Decode base64 string → []byte
   - Create io.Reader from bytes

3. Put.io Client (internal/dc/putio/client.go)
   - Call Files.Upload() with io.Reader
   - Put.io interprets as transfer, returns Transfer object

4. Return to Sonarr/Radarr
   - Same response format as magnet link flow
```

### Standard Library Usage

**Base64 decoding:**
```go
import "encoding/base64"

decodedBytes, err := base64.StdEncoding.DecodeString(metaInfo)
```

**Convert to io.Reader:**
```go
import "bytes"

reader := bytes.NewReader(decodedBytes)
```

**No external dependencies required** - both are Go standard library packages.

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| Torrent upload method | `Files.Upload()` | `Transfers.Add()` | Transfers.Add() only accepts URLs (string), not file content |
| Base64 decoding | `encoding/base64.StdEncoding` | Third-party libraries | Standard library is RFC 4648 compliant, zero dependencies |
| Bytes to Reader | `bytes.NewReader()` | `bytes.Buffer` | NewReader is read-only, supports Seek/ReadAt, more efficient for our use case |
| Torrent parsing | None (pass raw bytes) | `github.com/anacrolix/torrent/bencode`, `github.com/jackpal/bencode-go` | Put.io accepts raw .torrent bytes; parsing not needed |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Torrent parsing libraries | Put.io handles torrent interpretation; parsing adds complexity and dependencies | Pass raw decoded bytes to Files.Upload() |
| File persistence | Explicit project constraint: no .torrent files on disk | Use in-memory bytes.NewReader() |
| `Transfers.Add()` for .torrent files | Only accepts URL strings, not file content | Use `Files.Upload()` which accepts io.Reader |
| Third-party base64 libraries | Standard library is authoritative and zero-dep | Use `encoding/base64` |

## Version Compatibility

| Package | Current Version | Compatibility Notes |
|---------|-----------------|---------------------|
| `github.com/putdotio/go-putio` | v1.7.2 | Files.Upload() method available since early versions; no upgrade needed |
| Go standard library | Go 1.23.8 | encoding/base64 and bytes packages stable since Go 1.0 |

## Validation Checklist

Before implementation, verify:

- [ ] MetaInfo field is not empty (either filename OR metainfo required per Transmission spec)
- [ ] Base64 decoding succeeds (invalid base64 returns error)
- [ ] Decoded bytes represent valid .torrent file (Put.io will validate structure)
- [ ] Files.Upload() returns Upload.Transfer populated (indicates torrent was recognized)
- [ ] Error handling for Files.Upload() failures (e.g., invalid torrent format)

## Sources

### HIGH Confidence Sources

1. **Put.io Go SDK Source Code**
   - File: `/Users/italovietro/go/pkg/mod/github.com/putdotio/go-putio@v1.7.2/files.go`
   - Method: Files.Upload() at line 215
   - Documentation comment explicitly states torrent file handling

2. **Transmission RPC Specification**
   - [Official Transmission RPC Spec](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md)
   - MetaInfo field format: "base64-encoded .torrent content"

3. **Go Standard Library Documentation**
   - [encoding/base64 package](https://pkg.go.dev/encoding/base64)
   - [bytes package](https://pkg.go.dev/bytes)
   - Both updated January 15, 2026 for Go 1.25.6

4. **Put.io Go SDK Package Documentation**
   - [pkg.go.dev/github.com/putdotio/go-putio/putio](https://pkg.go.dev/github.com/putdotio/go-putio/putio)
   - FilesService.Upload() documentation and Upload type definition

### Additional References

5. **Community Resources** (WebSearch findings)
   - [Go by Example: Base64 Encoding](https://gobyexample.com/base64-encoding)
   - [How to use the io.Reader interface](https://yourbasic.org/golang/io-reader-interface-explained/)
   - Bencode library landscape (for context, though not needed): [anacrolix/torrent](https://pkg.go.dev/github.com/anacrolix/torrent/bencode), [jackpal/bencode-go](https://github.com/jackpal/bencode-go)

---
*Stack research for: .torrent file content support in Transmission API proxy*
*Researched: 2026-02-01*
*Confidence: HIGH - All critical components verified from official sources*
