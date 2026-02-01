# Phase 5: Transmission API Handler - Research

**Researched:** 2026-02-01
**Domain:** Transmission RPC API implementation, base64 decoding, bencode validation, HTTP error responses
**Confidence:** HIGH

## Summary

This phase implements a Transmission RPC-compatible webhook handler that accepts .torrent file content via the `metainfo` field in `torrent-add` requests. The research reveals that the Transmission RPC specification requires `metainfo` to contain base64-encoded .torrent content, and that proper implementation must handle field priority (MetaInfo over FileName), validate content before upload, and return Transmission-compatible error responses.

The implementation extends the existing `HandleRPC` method in `internal/http/rest/transmission.go`, which already handles `torrent-add` requests with magnet links via the `FileName` field. The current code structure uses a switch statement to route RPC methods and returns `TransmissionResponse` objects with a `result` field ("success" or error message) following the Transmission RPC specification.

The standard approach uses Go's `encoding/base64` package for decoding with strict validation, bencode libraries for structural validation, and the existing custom error types (`InvalidContentError`) introduced in Phase 4. The handler must detect `MetaInfo` presence, decode base64 content, validate bencode structure, and pass validated bytes to the Put.io client's `AddTransferByBytes` method from Phase 4.

**Primary recommendation:** Extend `handleTorrentAdd()` to detect and prioritize `MetaInfo` field, use `base64.StdEncoding.DecodeString()` with error checking, validate bencode structure with `github.com/zeebo/bencode` or `github.com/jackpal/bencode-go`, and return Transmission-compatible error responses by mapping custom error types to appropriate result strings.

## Standard Stack

### Core Libraries
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| encoding/base64 | stdlib | Base64 decoding with validation | Standard library, no dependencies, strict mode available |
| github.com/zeebo/bencode | v1.0.0+ | Bencode validation and decoding | Most popular Go bencode library, similar API to encoding/json |
| errors | stdlib | Error inspection with errors.As() | Go 1.13+ standard error handling |
| encoding/json | stdlib | JSON request/response handling | Already in use for TransmissionRequest/Response |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| github.com/italolelis/seedbox_downloader/internal/transfer | current | Custom error types (InvalidContentError) | Already defined in Phase 4 |
| github.com/italolelis/seedbox_downloader/internal/dc/putio | current | Put.io client with AddTransferByBytes | Already implemented in Phase 4 |
| net/http | stdlib | HTTP status codes and responses | Already in use for handler |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| github.com/zeebo/bencode | github.com/jackpal/bencode-go | Both work; zeebo has JSON-like API, jackpal has reflection-based API |
| base64.StdEncoding | base64.URLEncoding | StdEncoding is correct for Transmission spec; URLEncoding would reject valid input |
| Bencode validation | Skip validation (trust Put.io) | Validation provides early failure with specific error messages |

**Installation:**
```bash
go get github.com/zeebo/bencode
```

## Architecture Patterns

### Recommended Code Organization
```
internal/
├── http/
│   └── rest/
│       ├── transmission.go        # Extended handleTorrentAdd() method
│       └── transmission_test.go   # NEW: Tests for MetaInfo handling
└── dc/
    └── putio/
        └── client.go              # Uses AddTransferByBytes from Phase 4
```

### Pattern 1: Field Priority Detection

**What:** Check for `MetaInfo` field presence and prioritize it over `FileName` when both exist

**When to use:** In `handleTorrentAdd()` before processing request arguments

**Example:**
```go
// Source: Transmission RPC spec - torrent-add requires either filename or metainfo
// Source: https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md

func (h *TransmissionHandler) handleTorrentAdd(ctx context.Context, req *TransmissionRequest) (*TransmissionResponse, error) {
    logger := logctx.LoggerFromContext(ctx).With("method", "handle_torrent_add")

    var torrent *transfer.Transfer
    var err error

    // Priority: MetaInfo over FileName (requirement API-06)
    if req.Arguments.MetaInfo != "" {
        logger.Debug("received torrent add with metainfo field")
        torrent, err = h.handleTorrentAddByMetaInfo(ctx, req)
    } else if req.Arguments.FileName != "" {
        logger.Debug("received torrent add with filename field (magnet link)")
        torrent, err = h.handleTorrentAddByMagnetLink(ctx, req)
    } else {
        return nil, fmt.Errorf("either metainfo or filename must be provided")
    }

    if err != nil {
        return nil, err
    }

    // Marshal response (existing pattern)
    jsonTorrent, err := json.Marshal(map[string]interface{}{
        "torrent-added": map[string]interface{}{
            "id":         torrent.ID,
            "name":       torrent.Name,
            "hashString": torrent.ID, // Use transfer ID as hash
        },
    })
    if err != nil {
        return nil, fmt.Errorf("failed to marshal torrent: %w", err)
    }

    return &TransmissionResponse{
        Result:    "success",
        Arguments: jsonTorrent,
    }, nil
}
```

### Pattern 2: Base64 Decoding with Validation

**What:** Use `base64.StdEncoding.DecodeString()` with explicit error handling for invalid base64

**When to use:** When decoding `MetaInfo` field content

**Example:**
```go
// Source: https://pkg.go.dev/encoding/base64
// Source: https://www.golinuxcloud.com/golang-base64-encode/

import "encoding/base64"

func (h *TransmissionHandler) handleTorrentAddByMetaInfo(ctx context.Context, req *TransmissionRequest) (*transfer.Transfer, error) {
    logger := logctx.LoggerFromContext(ctx)

    // Decode base64 content (requirement API-02)
    torrentBytes, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
    if err != nil {
        logger.Error("failed to decode base64 metainfo", "err", err)
        return nil, &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   fmt.Sprintf("invalid base64 encoding: %v", err),
            Err:      err,
        }
    }

    logger.Debug("decoded metainfo", "size_bytes", len(torrentBytes))

    // Validate bencode structure (requirement API-03)
    if err := validateBencodeStructure(torrentBytes); err != nil {
        return nil, err
    }

    // Generate filename for Put.io upload
    filename := generateTorrentFilename(torrentBytes)

    // Upload to Put.io using Phase 4 client method
    torrent, err := h.dc.AddTransferByBytes(ctx, torrentBytes, filename, h.label)
    if err != nil {
        logger.Error("failed to add transfer by bytes", "err", err)
        return nil, err
    }

    return torrent, nil
}
```

### Pattern 3: Bencode Structure Validation

**What:** Use bencode library to verify content is valid bencode before upload

**When to use:** After base64 decoding, before calling `AddTransferByBytes`

**Example:**
```go
// Source: https://pkg.go.dev/github.com/zeebo/bencode
// Source: https://zeebo.github.io/bencode/

import "github.com/zeebo/bencode"

func validateBencodeStructure(data []byte) error {
    // Attempt to decode as bencode (requirement API-03)
    var torrentData interface{}
    if err := bencode.DecodeBytes(data, &torrentData); err != nil {
        return &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   fmt.Sprintf("invalid bencode structure: %v", err),
            Err:      err,
        }
    }

    // Optional: Verify it's a dictionary with expected torrent fields
    dict, ok := torrentData.(map[string]interface{})
    if !ok {
        return &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   "bencode root must be a dictionary",
        }
    }

    // Check for required torrent fields (info dictionary)
    if _, hasInfo := dict["info"]; !hasInfo {
        return &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   "bencode missing required 'info' dictionary",
        }
    }

    return nil
}
```

### Pattern 4: Transmission-Compatible Error Responses

**What:** Map custom error types to Transmission RPC error response format

**When to use:** In error handling within `HandleRPC` method

**Example:**
```go
// Source: https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md
// Source: Existing pattern from transmission.go HandleRPC method

func (h *TransmissionHandler) HandleRPC(w http.ResponseWriter, r *http.Request) {
    // ... existing request parsing ...

    response, err := h.handleTorrentAdd(r.Context(), &req)
    if err != nil {
        logger.Error("failed to handle request", "method", req.Method, "err", err)

        // Map custom errors to Transmission-compatible responses (requirement API-04)
        errorMsg := formatTransmissionError(err)

        response = &TransmissionResponse{
            Result: errorMsg, // Transmission uses "result" field for errors
        }

        // Still return 200 OK with error in result field (Transmission convention)
        w.Header().Set("Content-Type", "application/json")
        if encodeErr := json.NewEncoder(w).Encode(response); encodeErr != nil {
            logger.Error("failed to encode error response", "err", encodeErr)
            http.Error(w, "internal server error", http.StatusInternalServerError)
        }
        return
    }

    // Success response (existing pattern)
    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(response); err != nil {
        logger.Error("failed to encode response", "err", err)
        http.Error(w, "failed to encode response", http.StatusInternalServerError)
    }
}

func formatTransmissionError(err error) string {
    var invalidErr *transfer.InvalidContentError
    if errors.As(err, &invalidErr) {
        return fmt.Sprintf("invalid torrent: %s", invalidErr.Reason)
    }

    var networkErr *transfer.NetworkError
    if errors.As(err, &networkErr) {
        return fmt.Sprintf("upload failed: %s", networkErr.APIMessage)
    }

    var dirErr *transfer.DirectoryError
    if errors.As(err, &dirErr) {
        return fmt.Sprintf("directory error: %s", dirErr.Reason)
    }

    var authErr *transfer.AuthenticationError
    if errors.As(err, &authErr) {
        return "authentication failed"
    }

    return fmt.Sprintf("error: %v", err)
}
```

### Pattern 5: Filename Generation from Torrent Content

**What:** Generate a `.torrent` filename from torrent content for Put.io upload

**When to use:** When MetaInfo is provided without an explicit filename

**Example:**
```go
// Source: BitTorrent specification - info hash is SHA1 of info dictionary
// Source: Existing pattern from transmission.go (uses sha1 for hash generation)

import (
    "crypto/sha1"
    "encoding/hex"
    "fmt"
)

func generateTorrentFilename(torrentBytes []byte) string {
    // Generate hash from content for unique filename
    hash := sha1.Sum(torrentBytes)
    hashStr := hex.EncodeToString(hash[:])

    // Use hash as filename to ensure uniqueness and .torrent extension
    return fmt.Sprintf("%s.torrent", hashStr[:16]) // Use first 16 chars of hash
}
```

### Pattern 6: Backward Compatibility Preservation

**What:** Keep existing magnet link behavior unchanged when only `FileName` is present

**When to use:** Always - requirement API-05

**Example:**
```go
// Extract existing magnet link handling into separate method
func (h *TransmissionHandler) handleTorrentAddByMagnetLink(ctx context.Context, req *TransmissionRequest) (*transfer.Transfer, error) {
    logger := logctx.LoggerFromContext(ctx)

    magnetLink := req.Arguments.FileName
    logger.Debug("processing magnet link", "link", magnetLink)

    // Existing logic from current handleTorrentAdd implementation
    torrent, err := h.dc.AddTransferByURL(ctx, magnetLink, h.label)
    if err != nil {
        return nil, fmt.Errorf("failed to add transfer: %w", err)
    }

    return torrent, nil
}
```

### Anti-Patterns to Avoid

- **Don't use base64.URLEncoding:** Transmission spec uses standard base64 encoding, not URL-safe variant
- **Don't skip bencode validation:** Uploading invalid content wastes bandwidth and provides poor error messages
- **Don't return HTTP 400/500 for Transmission errors:** Transmission RPC returns HTTP 200 with error in `result` field
- **Don't parse full torrent metadata:** Only validate structure; Put.io handles torrent processing
- **Don't break existing magnet link behavior:** Keep `FileName` handling unchanged for backward compatibility

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Base64 decoding | Custom base64 decoder | encoding/base64.StdEncoding | Standard library handles padding, invalid chars, RFC compliance |
| Bencode validation | Custom bencode parser | github.com/zeebo/bencode or github.com/jackpal/bencode-go | Complex spec with dictionaries, lists, integers, strings |
| SHA1 hashing | Custom hash function | crypto/sha1 | Cryptographically correct implementation, already used in codebase |
| Error type inspection | String matching on error messages | errors.As() from stdlib | Type-safe, works through error wrapping chains |

**Key insight:** Bencode parsing is deceptively complex. The format has strict ordering requirements for dictionary keys, nested structures, and integer encoding rules. Using a well-tested library prevents subtle bugs and security issues.

## Common Pitfalls

### Pitfall 1: Wrong Base64 Encoding Type

**What goes wrong:** Using `base64.URLEncoding` instead of `base64.StdEncoding` causes valid Transmission requests to fail with decode errors.

**Why it happens:** Multiple base64 variants exist (standard, URL-safe, raw). Transmission spec uses standard encoding with padding.

**How to avoid:**
- Use `base64.StdEncoding.DecodeString()`, not `base64.URLEncoding`
- Don't use `RawStdEncoding` (removes padding)
- Test with real Transmission client requests to verify encoding compatibility

**Warning signs:**
- "illegal base64 data at input byte X" errors on valid torrents
- Decoding works for some torrents but not others (URL-safe chars like `-` and `_` cause issues)

**Example:**
```go
// WRONG - Transmission doesn't use URL-safe encoding
torrentBytes, err := base64.URLEncoding.DecodeString(req.Arguments.MetaInfo)

// CORRECT - Transmission uses standard base64 with padding
torrentBytes, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
```

### Pitfall 2: Returning HTTP Error Status for RPC Errors

**What goes wrong:** Returning `http.Error(w, msg, http.StatusBadRequest)` for invalid torrents breaks Transmission client compatibility.

**Why it happens:** REST API convention uses HTTP status codes, but Transmission RPC returns HTTP 200 with errors in response body.

**How to avoid:**
- Always return HTTP 200 for RPC requests (unless server error)
- Put error message in `TransmissionResponse.Result` field
- Only use `http.Error()` for malformed JSON or internal server errors

**Warning signs:**
- Transmission clients show "connection failed" instead of specific error message
- Logs show HTTP 400/500 responses for torrent validation failures
- Users can't distinguish between server problems and bad torrent files

**Example:**
```go
// WRONG - Returns HTTP 400, breaks Transmission clients
if err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
}

// CORRECT - Returns HTTP 200 with error in result field
if err != nil {
    response = &TransmissionResponse{
        Result: formatTransmissionError(err),
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
    return
}
```

### Pitfall 3: Insufficient Bencode Validation

**What goes wrong:** Accepting any valid bencode structure allows non-torrent data (lists, integers) to reach Put.io, wasting API calls.

**Why it happens:** Bencode is a general-purpose format. Valid bencode isn't necessarily valid torrent metainfo.

**How to avoid:**
- After decoding bencode, verify root is a dictionary
- Check for required `info` field in root dictionary
- Return `InvalidContentError` with specific reason for missing fields

**Warning signs:**
- Put.io API rejects uploads with generic "invalid file" errors
- Users confused why "valid bencode" is rejected
- No client-side validation catches structural issues

**Example:**
```go
// INSUFFICIENT - Only validates bencode syntax
var torrentData interface{}
if err := bencode.DecodeBytes(data, &torrentData); err != nil {
    return &transfer.InvalidContentError{Reason: "invalid bencode"}
}

// SUFFICIENT - Validates torrent structure
var torrentData interface{}
if err := bencode.DecodeBytes(data, &torrentData); err != nil {
    return &transfer.InvalidContentError{Reason: "invalid bencode syntax"}
}

dict, ok := torrentData.(map[string]interface{})
if !ok {
    return &transfer.InvalidContentError{Reason: "bencode must be a dictionary"}
}

if _, hasInfo := dict["info"]; !hasInfo {
    return &transfer.InvalidContentError{Reason: "missing required 'info' field"}
}
```

### Pitfall 4: Not Testing Field Priority

**What goes wrong:** When both `MetaInfo` and `FileName` are present, handler uses `FileName` instead of prioritizing `MetaInfo`.

**Why it happens:** Original code only handled `FileName`, easy to forget priority requirement (API-06).

**How to avoid:**
- Check `MetaInfo != ""` before checking `FileName`
- Add explicit test case with both fields present
- Document priority behavior in comments

**Warning signs:**
- Magnet links processed when .torrent content is provided
- Integration tests show unexpected behavior with both fields
- Users report .torrent files being ignored

**Example:**
```go
// WRONG - FileName checked first, violates API-06
if req.Arguments.FileName != "" {
    return h.handleTorrentAddByMagnetLink(ctx, req)
} else if req.Arguments.MetaInfo != "" {
    return h.handleTorrentAddByMetaInfo(ctx, req)
}

// CORRECT - MetaInfo prioritized per requirement API-06
if req.Arguments.MetaInfo != "" {
    return h.handleTorrentAddByMetaInfo(ctx, req)
} else if req.Arguments.FileName != "" {
    return h.handleTorrentAddByMagnetLink(ctx, req)
}
```

### Pitfall 5: Missing Size Validation Before Bencode Decoding

**What goes wrong:** Attempting to bencode-decode a 50MB malformed file causes memory exhaustion before size limit is checked.

**Why it happens:** Size limit is enforced in `AddTransferByBytes`, but bencode validation happens before that call.

**How to avoid:**
- Check `len(torrentBytes)` immediately after base64 decoding
- Use same 10MB limit as Phase 4 (requirement from Phase 4 context)
- Return `InvalidContentError` before attempting bencode decode

**Warning signs:**
- Memory spikes during request handling
- Handler crashes on malformed large uploads
- Size limit only catches valid torrents, not malformed ones

**Example:**
```go
// Decode base64
torrentBytes, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
if err != nil {
    return nil, &transfer.InvalidContentError{Reason: "invalid base64"}
}

// Check size BEFORE bencode validation (prevents memory exhaustion)
const maxTorrentSize = 10 * 1024 * 1024 // 10MB from Phase 4
if len(torrentBytes) > maxTorrentSize {
    return nil, &transfer.InvalidContentError{
        Filename: "metainfo",
        Reason:   fmt.Sprintf("size %d bytes exceeds maximum %d bytes", len(torrentBytes), maxTorrentSize),
    }
}

// Now safe to validate bencode structure
if err := validateBencodeStructure(torrentBytes); err != nil {
    return nil, err
}
```

## Code Examples

Verified patterns from official sources and existing codebase:

### Complete handleTorrentAdd Implementation

```go
// Source: internal/http/rest/transmission.go (existing structure)
// Source: Transmission RPC spec - torrent-add method

func (h *TransmissionHandler) handleTorrentAdd(ctx context.Context, req *TransmissionRequest) (*TransmissionResponse, error) {
    logger := logctx.LoggerFromContext(ctx).With("method", "handle_torrent_add")

    var torrent *transfer.Transfer
    var err error

    // Requirement API-06: Prioritize MetaInfo when both present
    if req.Arguments.MetaInfo != "" {
        // Requirement API-01: Detect MetaInfo field
        logger.Debug("received torrent add with metainfo field")
        torrent, err = h.handleTorrentAddByMetaInfo(ctx, req)
    } else if req.Arguments.FileName != "" {
        // Requirement API-05: Maintain backward compatibility
        logger.Debug("received torrent add with filename field (magnet link)")
        magnetLink := req.Arguments.FileName
        torrent, err = h.dc.AddTransferByURL(ctx, magnetLink, h.label)
        if err != nil {
            return nil, fmt.Errorf("failed to add transfer: %w", err)
        }
    } else {
        return nil, fmt.Errorf("either metainfo or filename must be provided")
    }

    if err != nil {
        return nil, err
    }

    // Marshal success response (Transmission format)
    jsonTorrent, err := json.Marshal(map[string]interface{}{
        "torrent-added": map[string]interface{}{
            "id":         torrent.ID,
            "name":       torrent.Name,
            "hashString": torrent.ID,
        },
    })
    if err != nil {
        return nil, fmt.Errorf("failed to marshal torrent: %w", err)
    }

    return &TransmissionResponse{
        Result:    "success",
        Arguments: jsonTorrent,
    }, nil
}
```

### MetaInfo Processing Method

```go
// Source: https://pkg.go.dev/encoding/base64
// Source: https://pkg.go.dev/github.com/zeebo/bencode

import (
    "crypto/sha1"
    "encoding/base64"
    "encoding/hex"
    "fmt"

    "github.com/zeebo/bencode"
)

const maxTorrentSize = 10 * 1024 * 1024 // 10MB from Phase 4 requirement

func (h *TransmissionHandler) handleTorrentAddByMetaInfo(ctx context.Context, req *TransmissionRequest) (*transfer.Transfer, error) {
    logger := logctx.LoggerFromContext(ctx)

    // Requirement API-02: Decode base64-encoded .torrent content
    torrentBytes, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
    if err != nil {
        logger.Error("failed to decode base64 metainfo", "err", err)
        return nil, &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   fmt.Sprintf("invalid base64 encoding: %v", err),
            Err:      err,
        }
    }

    logger.Debug("decoded metainfo", "size_bytes", len(torrentBytes))

    // Check size before validation (prevent memory exhaustion)
    if len(torrentBytes) > maxTorrentSize {
        return nil, &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   fmt.Sprintf("size %d bytes exceeds maximum %d bytes", len(torrentBytes), maxTorrentSize),
        }
    }

    // Requirement API-03: Validate bencode structure
    if err := validateBencodeStructure(torrentBytes); err != nil {
        logger.Error("bencode validation failed", "err", err)
        return nil, err
    }

    // Generate filename for Put.io (requires .torrent extension)
    filename := generateTorrentFilename(torrentBytes)
    logger.Debug("generated filename", "filename", filename)

    // Upload to Put.io using Phase 4 client method
    torrent, err := h.dc.AddTransferByBytes(ctx, torrentBytes, filename, h.label)
    if err != nil {
        logger.Error("failed to add transfer by bytes", "err", err)
        return nil, err
    }

    logger.Info("transfer created from metainfo", "transfer_id", torrent.ID, "name", torrent.Name)

    return torrent, nil
}

func validateBencodeStructure(data []byte) error {
    var torrentData interface{}

    // Decode bencode structure
    if err := bencode.DecodeBytes(data, &torrentData); err != nil {
        return &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   fmt.Sprintf("invalid bencode structure: %v", err),
            Err:      err,
        }
    }

    // Verify root is a dictionary
    dict, ok := torrentData.(map[string]interface{})
    if !ok {
        return &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   "bencode root must be a dictionary",
        }
    }

    // Check for required 'info' field
    if _, hasInfo := dict["info"]; !hasInfo {
        return &transfer.InvalidContentError{
            Filename: "metainfo",
            Reason:   "bencode missing required 'info' dictionary",
        }
    }

    return nil
}

func generateTorrentFilename(torrentBytes []byte) string {
    hash := sha1.Sum(torrentBytes)
    hashStr := hex.EncodeToString(hash[:])
    return fmt.Sprintf("%s.torrent", hashStr[:16])
}
```

### Test Cases for MetaInfo Handling

```go
// Source: Existing pattern from internal/dc/deluge/client_test.go
// Source: internal/dc/putio/client_test.go

package rest_test

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/italolelis/seedbox_downloader/internal/http/rest"
    "github.com/italolelis/seedbox_downloader/internal/transfer"
    "github.com/stretchr/testify/assert"
)

func TestHandleTorrentAdd_MetaInfoValid(t *testing.T) {
    // Create valid bencode torrent content
    validTorrent := "d8:announce9:http://example.com4:infod6:lengthi1234e4:name8:test.txt12:piece lengthi16384eee"
    encodedMetaInfo := base64.StdEncoding.EncodeToString([]byte(validTorrent))

    reqBody := rest.TransmissionRequest{
        Method: "torrent-add",
    }
    reqBody.Arguments.MetaInfo = encodedMetaInfo
    reqBody.Arguments.DownloadDir = "test-dir"

    body, _ := json.Marshal(reqBody)
    req := httptest.NewRequest("POST", "/transmission/rpc", strings.NewReader(string(body)))
    w := httptest.NewRecorder()

    // Mock handler setup would go here...
    // handler.HandleRPC(w, req)

    // Assertions:
    // - HTTP 200 status
    // - JSON response with result: "success"
    // - torrent-added field present
}

func TestHandleTorrentAdd_MetaInfoInvalidBase64(t *testing.T) {
    reqBody := rest.TransmissionRequest{
        Method: "torrent-add",
    }
    reqBody.Arguments.MetaInfo = "not-valid-base64!!!"

    body, _ := json.Marshal(reqBody)
    req := httptest.NewRequest("POST", "/transmission/rpc", strings.NewReader(string(body)))
    w := httptest.NewRecorder()

    // Mock handler setup...
    // handler.HandleRPC(w, req)

    // Assertions:
    // - HTTP 200 status (not 400!)
    // - JSON response with result containing "invalid base64"
}

func TestHandleTorrentAdd_MetaInfoInvalidBencode(t *testing.T) {
    invalidBencode := "not bencode at all"
    encodedMetaInfo := base64.StdEncoding.EncodeToString([]byte(invalidBencode))

    reqBody := rest.TransmissionRequest{
        Method: "torrent-add",
    }
    reqBody.Arguments.MetaInfo = encodedMetaInfo

    body, _ := json.Marshal(reqBody)
    req := httptest.NewRequest("POST", "/transmission/rpc", strings.NewReader(string(body)))
    w := httptest.NewRecorder()

    // Mock handler setup...
    // handler.HandleRPC(w, req)

    // Assertions:
    // - HTTP 200 status
    // - JSON response with result containing "invalid bencode"
}

func TestHandleTorrentAdd_MetaInfoPriority(t *testing.T) {
    // When both MetaInfo and FileName present, MetaInfo takes priority
    validTorrent := "d8:announce9:http://example.com4:infod6:lengthi1234e4:name8:test.txt12:piece lengthi16384eee"
    encodedMetaInfo := base64.StdEncoding.EncodeToString([]byte(validTorrent))

    reqBody := rest.TransmissionRequest{
        Method: "torrent-add",
    }
    reqBody.Arguments.MetaInfo = encodedMetaInfo
    reqBody.Arguments.FileName = "magnet:?xt=urn:btih:test"

    // Assertions should verify MetaInfo was used, not FileName
}

func TestHandleTorrentAdd_FileNameBackwardCompatibility(t *testing.T) {
    // When only FileName present, existing behavior should work
    reqBody := rest.TransmissionRequest{
        Method: "torrent-add",
    }
    reqBody.Arguments.FileName = "magnet:?xt=urn:btih:test"

    // Assertions should verify magnet link was processed correctly
}

func TestHandleTorrentAdd_MetaInfoTooLarge(t *testing.T) {
    // Create 11MB of data (exceeds 10MB limit)
    largeTorrent := make([]byte, 11*1024*1024)
    encodedMetaInfo := base64.StdEncoding.EncodeToString(largeTorrent)

    reqBody := rest.TransmissionRequest{
        Method: "torrent-add",
    }
    reqBody.Arguments.MetaInfo = encodedMetaInfo

    // Assertions:
    // - Error response with "exceeds maximum" in result
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Only magnet link support | Support both magnet links and .torrent files | 2026 (this phase) | Enables private tracker support via .torrent files |
| Generic error messages | Custom error types with structured data | Phase 4 (2026) | Programmatic error handling, better diagnostics |
| HTTP status codes for errors | Transmission RPC result field | Transmission RPC v18 | Client compatibility with Transmission protocol |
| Manual base64 handling | encoding/base64 with strict mode | Go 1.15+ (2020) | Built-in validation, RFC compliance |
| Custom bencode parsers | Mature bencode libraries | 2015+ | Handles edge cases, security issues addressed |

**Deprecated/outdated:**
- **base64.NewDecoder for small data:** Use `DecodeString()` for in-memory data; `NewDecoder()` is for streaming
- **Reflection-based bencode unmarshaling without validation:** Always validate structure after decode
- **HTTP error status for RPC failures:** Use HTTP 200 with error in response body per Transmission spec

## Open Questions

1. **Transmission response format version**
   - What we know: Transmission RPC has evolved from legacy format (result string only) to JSON-RPC 2.0 format (error object with code/message)
   - What's unclear: Which format do arr apps (Sonarr/Radarr) expect? Do they support JSON-RPC 2.0 error format?
   - Recommendation: Use legacy format (result string) for maximum compatibility. Can upgrade to JSON-RPC 2.0 in future if needed.

2. **Bencode library choice**
   - What we know: Both `github.com/zeebo/bencode` and `github.com/jackpal/bencode-go` are stable, well-maintained libraries
   - What's unclear: Performance difference for typical .torrent files (<1MB)
   - Recommendation: Use `github.com/zeebo/bencode` for JSON-like API familiarity. Both libraries are equally capable.

3. **Filename generation strategy**
   - What we know: Put.io requires `.torrent` extension for server-side detection
   - What's unclear: Should filename include original torrent name from bencode, or just use hash?
   - Recommendation: Use hash-based filename (simpler, avoids encoding issues with special characters). Put.io extracts actual name from torrent content.

4. **Error response detail level**
   - What we know: Transmission returns error messages in `result` field
   - What's unclear: How much technical detail should be exposed to arr apps? (e.g., "invalid bencode" vs "invalid bencode: unexpected character at position 42")
   - Recommendation: Include high-level reason (e.g., "invalid base64 encoding") without exposing internal details. Detailed errors in logs only.

## Sources

### Primary (HIGH confidence)
- [Transmission RPC Specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md) - torrent-add method, metainfo parameter, response format
- [Go encoding/base64 package](https://pkg.go.dev/encoding/base64) - base64.StdEncoding.DecodeString, strict decoding
- [github.com/zeebo/bencode](https://pkg.go.dev/github.com/zeebo/bencode) - bencode decoding and validation API
- [github.com/jackpal/bencode-go](https://pkg.go.dev/github.com/jackpal/bencode-go) - alternative bencode library
- [Go errors package](https://pkg.go.dev/errors) - errors.As for custom error type inspection
- [GoLinuxCloud: Base64 Best Practices](https://www.golinuxcloud.com/golang-base64-encode/) - validation patterns, encoding type selection

### Secondary (MEDIUM confidence)
- [Transmission Forum: Adding .torrent via metainfo RPC](https://forum.transmissionbt.com/viewtopic.php?t=8023) - real-world usage example
- [Better Stack: HTTP Error Handling in Go](https://blog.questionable.services/article/http-handler-error-handling-revisited/) - error response patterns
- [Kelche.co: Go Base64 Encoding](https://www.kelche.co/blog/go/golang-base64/) - encoding/decoding examples
- [MojoAuth: Base64 with Go](https://mojoauth.com/binary-encoding-decoding/base64-with-go) - binary encoding techniques

### Tertiary (LOW confidence)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification) - referenced by Transmission docs but not required for this phase
- [Transmission Python RPC docs](https://transmissionrpc.readthedocs.io/) - client library examples (demonstrates expected behavior)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - All libraries are Go stdlib or established in Go ecosystem, versions verified via pkg.go.dev
- Architecture: HIGH - Patterns verified against Transmission RPC spec (official docs), existing codebase structure, and Phase 4 implementation
- Pitfalls: HIGH - Based on Transmission RPC spec requirements, base64 encoding specification (RFC 4648), and bencode validation edge cases documented in library issues

**Research date:** 2026-02-01
**Valid until:** 2026-03-01 (30 days - stable domain, Transmission RPC spec unchanged since v18, Go stdlib stable)
