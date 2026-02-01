# Phase 4: Put.io Client Extension - Research

**Researched:** 2026-02-01
**Domain:** Go Put.io SDK integration, error handling patterns, file upload validation
**Confidence:** HIGH

## Summary

This phase extends the existing Put.io client (`internal/dc/putio/client.go`) to support uploading .torrent file content as bytes, enabling automatic transfer creation on Put.io's platform. The research reveals that the Put.io Go SDK (`github.com/putdotio/go-putio v1.7.2`) provides a `Files.Upload()` method that automatically detects .torrent files and creates transfers server-side, requiring no client-side bencode parsing.

The implementation follows established Go patterns for custom error types, using struct-based errors with contextual information to distinguish between four error categories: invalid content, network/API failures, directory resolution issues, and authentication problems. The existing codebase uses `fmt.Errorf` for basic error wrapping but lacks structured error types, making this phase an opportunity to introduce better error handling patterns in the `internal/transfer` package.

File upload patterns in Go use `io.Reader` interfaces with `bytes.NewReader()` to convert byte slices into streams. The Put.io SDK's `Files.Upload()` reads entire content into memory, making it suitable for .torrent files which are typically <1MB (though we'll enforce a 10MB safety limit). Directory resolution follows the existing `findDirectoryID()` helper pattern used for magnet links.

**Primary recommendation:** Implement two explicit methods (`AddTransferByURL` and `AddTransferByBytes`) to replace the existing `AddTransfer` method, define custom error types in `internal/transfer/errors.go` following Go 1.13+ patterns with `errors.Is`/`errors.As` support, and leverage Put.io's automatic .torrent detection without client-side validation.

## Standard Stack

### Core Libraries
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| github.com/putdotio/go-putio | v1.7.2 | Put.io API v2 client | Official Put.io SDK, already in use |
| bytes | stdlib | Convert []byte to io.Reader | Standard library for byte manipulation |
| errors | stdlib | Error wrapping and inspection | Go 1.13+ standard error handling |
| fmt | stdlib | Error formatting with %w | Standard error creation and wrapping |
| path/filepath | stdlib | File extension validation | Standard path manipulation |
| strings | stdlib | String suffix checking | Standard string utilities |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| golang.org/x/oauth2 | (existing) | OAuth2 authentication | Already configured in client |
| net/http | stdlib | HTTP status code constants | Error categorization by status code |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom error types | errors.New() with fmt.Errorf | Custom types provide structured data for error handling, enable type assertions |
| Files.Upload() | UploadService (TUS protocol) | Files.Upload is simpler for small files; TUS is for >150MB resumable uploads |
| bytes.NewReader() | Direct byte slice passing | io.Reader is the standard interface for Put.io SDK |

**Installation:**
Already in project dependencies (`go.mod`). No new external dependencies required.

## Architecture Patterns

### Recommended Project Structure
```
internal/
├── transfer/
│   ├── transfer.go          # Existing interfaces and types
│   ├── errors.go            # NEW: Custom error types
│   └── instrumented_client.go
└── dc/
    └── putio/
        ├── client.go        # Extended with new methods
        └── client_test.go   # NEW: Tests for upload methods
```

### Pattern 1: Explicit Method Naming Over Generic Signatures

**What:** Create two distinct methods instead of one generic method with interface{} type switching

**When to use:** When a method accepts fundamentally different input types (URL string vs byte content)

**Example:**
```go
// BEFORE (current implementation)
func (c *Client) AddTransfer(ctx context.Context, url string, downloadDir string) (*transfer.Transfer, error)

// AFTER (Phase 4 implementation)
func (c *Client) AddTransferByURL(ctx context.Context, url string, downloadDir string) (*transfer.Transfer, error)
func (c *Client) AddTransferByBytes(ctx context.Context, torrentBytes []byte, filename string, downloadDir string) (*transfer.Transfer, error)
```

**Rationale:** Go best practices favor explicit method names over type switching. Makes intent clear at call site.

### Pattern 2: Custom Error Types with Context

**What:** Define struct-based error types implementing the `error` interface with additional contextual fields

**When to use:** When errors need to carry structured data for programmatic handling

**Example:**
```go
// Source: https://betterstack.com/community/guides/scaling-go/golang-errors/
// Source: https://www.digitalocean.com/community/tutorials/creating-custom-errors-in-go

package transfer

import "fmt"

// Base error types for different failure categories
type InvalidContentError struct {
    Filename string
    Reason   string
    Err      error
}

func (e *InvalidContentError) Error() string {
    return fmt.Sprintf("invalid torrent content in %s: %s", e.Filename, e.Reason)
}

func (e *InvalidContentError) Unwrap() error { return e.Err }

type NetworkError struct {
    Operation  string
    StatusCode int
    APIMessage string
    Err        error
}

func (e *NetworkError) Error() string {
    return fmt.Sprintf("network error during %s (HTTP %d): %s", e.Operation, e.StatusCode, e.APIMessage)
}

func (e *NetworkError) Unwrap() error { return e.Err }

type DirectoryError struct {
    DirectoryName string
    Reason        string
    Err           error
}

func (e *DirectoryError) Error() string {
    return fmt.Sprintf("directory error for '%s': %s", e.DirectoryName, e.Reason)
}

func (e *DirectoryError) Unwrap() error { return e.Err }

type AuthenticationError struct {
    Operation string
    Err       error
}

func (e *AuthenticationError) Error() string {
    return fmt.Sprintf("authentication failed during %s", e.Operation)
}

func (e *AuthenticationError) Unwrap() error { return e.Err }
```

### Pattern 3: Error Inspection with errors.Is and errors.As

**What:** Use `errors.Is()` to check for specific error types and `errors.As()` to extract typed errors from wrapped chains

**When to use:** When callers need to handle different error categories differently

**Example:**
```go
// Source: https://betterstack.com/community/guides/scaling-go/golang-errors/

// In handler code
transfer, err := client.AddTransferByBytes(ctx, torrentBytes, filename, downloadDir)
if err != nil {
    var invalidErr *transfer.InvalidContentError
    if errors.As(err, &invalidErr) {
        return fmt.Errorf("upload rejected: %s", invalidErr.Reason)
    }

    var authErr *transfer.AuthenticationError
    if errors.As(err, &authErr) {
        return fmt.Errorf("authentication required: re-authenticate and retry")
    }

    // Default handling
    return fmt.Errorf("upload failed: %w", err)
}
```

### Pattern 4: bytes.NewReader for io.Reader Conversion

**What:** Convert byte slices to `io.Reader` using `bytes.NewReader()`

**When to use:** When an API expects `io.Reader` but you have data as `[]byte`

**Example:**
```go
// Source: https://pkg.go.dev/bytes
// Source: https://victoriametrics.com/blog/go-io-reader-writer/

import "bytes"

func (c *Client) AddTransferByBytes(ctx context.Context, torrentBytes []byte, filename string, downloadDir string) (*transfer.Transfer, error) {
    // Convert []byte to io.Reader
    reader := bytes.NewReader(torrentBytes)

    // Pass to Put.io SDK
    upload, err := c.putioClient.Files.Upload(ctx, reader, filename, parentDirID)
    // ...
}
```

**Note:** `bytes.NewReader()` creates a read-only seeker from a byte slice. Unlike `bytes.Buffer`, it doesn't support writes but is ideal for one-time uploads.

### Pattern 5: Shared Helper Functions for DRY

**What:** Reuse the existing `findDirectoryID()` helper for both URL-based and byte-based transfer methods

**When to use:** When multiple methods need identical directory resolution logic

**Example:**
```go
// Both methods use the same helper
func (c *Client) AddTransferByURL(ctx context.Context, url string, downloadDir string) (*transfer.Transfer, error) {
    dirID, err := c.findDirectoryID(ctx, downloadDir)
    // ...
}

func (c *Client) AddTransferByBytes(ctx context.Context, torrentBytes []byte, filename string, downloadDir string) (*transfer.Transfer, error) {
    dirID, err := c.findDirectoryID(ctx, downloadDir)
    // ...
}

// Existing helper (no changes needed)
func (c *Client) findDirectoryID(ctx context.Context, downloadDir string) (int64, error) {
    // Same logic as current implementation
}
```

### Pattern 6: File Extension Validation

**What:** Validate filename has `.torrent` extension using `strings.HasSuffix()`

**When to use:** Before accepting torrent file uploads

**Example:**
```go
// Source: https://github.com/golang/go/issues/50943
// Source: https://pkg.go.dev/strings

import (
    "path/filepath"
    "strings"
)

func validateTorrentFilename(filename string) error {
    ext := filepath.Ext(filename)
    if !strings.EqualFold(ext, ".torrent") {
        return &InvalidContentError{
            Filename: filename,
            Reason:   "file extension must be .torrent",
        }
    }
    return nil
}
```

**Note:** Use `strings.EqualFold()` for case-insensitive comparison (.torrent vs .TORRENT).

### Anti-Patterns to Avoid

- **Don't parse bencode client-side:** Put.io SDK automatically detects .torrent files and creates transfers server-side. Client-side parsing is unnecessary complexity.
- **Don't use interface{} for method parameters:** Go best practices favor explicit method names (`AddTransferByURL` vs `AddTransferByBytes`) over generic signatures with type switching.
- **Don't use sentinel errors for structured data:** Use custom error types when you need to carry context (status code, filename, etc.). Sentinel errors (`var ErrNotFound = errors.New(...)`) are for static conditions only.
- **Don't ignore error wrapping:** Always use `%w` with `fmt.Errorf()` to maintain error chains for `errors.Is` and `errors.As` inspection.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Bencode parsing | Custom torrent file parser | Put.io SDK auto-detection | Put.io server-side handles .torrent detection and transfer creation |
| OAuth2 authentication | Custom OAuth2 flow | golang.org/x/oauth2 | Already integrated in existing client |
| HTTP client | Custom HTTP wrapper | net/http with oauth2.Client | Standard library sufficient, already in use |
| File upload resumption | Custom chunk upload logic | Put.io UploadService (TUS) | Only needed for files >150MB; .torrent files are <1MB |
| Error categorization from HTTP status | Custom HTTP code mapping | net/http constants + custom types | Standard library provides all status code constants |

**Key insight:** The Put.io SDK handles the complexity of torrent file detection. The `Files.Upload()` method returns an `Upload` struct with either a `File` field (for regular uploads) or a `Transfer` field (for .torrent uploads). This server-side detection eliminates the need for client-side bencode validation.

## Common Pitfalls

### Pitfall 1: Memory Exhaustion from Large .torrent Files

**What goes wrong:** `Files.Upload()` reads entire file content into memory. Malicious or malformed files >10MB could cause memory issues.

**Why it happens:** Put.io SDK documentation states: "This method reads the file contents into the memory, so it should only be used for small files."

**How to avoid:**
- Enforce 10MB size limit before calling `Files.Upload()`
- Validate `len(torrentBytes)` before upload
- Return `InvalidContentError` for oversized files

**Warning signs:**
- Memory usage spikes during upload operations
- Out of memory errors on large file uploads

**Example:**
```go
const maxTorrentSize = 10 * 1024 * 1024 // 10MB

func (c *Client) AddTransferByBytes(ctx context.Context, torrentBytes []byte, filename string, downloadDir string) (*transfer.Transfer, error) {
    if len(torrentBytes) > maxTorrentSize {
        return nil, &InvalidContentError{
            Filename: filename,
            Reason:   fmt.Sprintf("file size %d bytes exceeds maximum %d bytes", len(torrentBytes), maxTorrentSize),
        }
    }
    // Continue with upload...
}
```

### Pitfall 2: Directory Validation Cross-Contamination

**What goes wrong:** In shared Put.io accounts with user-specific folders (e.g., "itv", "jdoe"), files get uploaded to wrong user's directory due to missing validation.

**Why it happens:** The existing `findDirectoryID()` searches for directories by name without validating ownership or folder structure.

**How to avoid:**
- Add validation to ensure `downloadDir` matches expected user folder patterns
- Reject uploads if directory name doesn't match configured user folder
- Log directory validation failures with enough context to debug

**Warning signs:**
- Files appearing in wrong user folders
- User complaints about missing files
- Files in unexpected directories

**Example:**
```go
// In CONTEXT.md: "Each person has a folder with their initials (e.g., 'itv')"
func (c *Client) validateUserDirectory(ctx context.Context, downloadDir string) error {
    // Configuration would specify allowed user folders
    allowedFolders := []string{"itv", "jdoe", "alice"} // From config

    found := false
    for _, allowed := range allowedFolders {
        if downloadDir == allowed {
            found = true
            break
        }
    }

    if !found {
        return &DirectoryError{
            DirectoryName: downloadDir,
            Reason:        fmt.Sprintf("directory not in allowed list: %v", allowedFolders),
        }
    }
    return nil
}
```

### Pitfall 3: Missing .torrent Extension Causing Silent Failures

**What goes wrong:** Put.io doesn't create a transfer when uploaded file lacks `.torrent` extension, resulting in a regular file instead of a transfer.

**Why it happens:** Put.io server-side detection relies on file extension to identify torrent files.

**How to avoid:**
- Validate filename has `.torrent` extension before upload
- Require caller to provide correct filename
- Return `InvalidContentError` for invalid extensions

**Warning signs:**
- Files uploaded but no transfers created
- `Upload.Transfer` field is nil but `Upload.File` field is populated
- User expects transfer but sees uploaded file instead

**Example:**
```go
func validateTorrentFilename(filename string) error {
    ext := filepath.Ext(filename)
    if !strings.EqualFold(ext, ".torrent") {
        return &InvalidContentError{
            Filename: filename,
            Reason:   "file extension must be .torrent (Put.io requires extension for transfer detection)",
        }
    }
    return nil
}
```

### Pitfall 4: Network Errors Not Distinguished from API Errors

**What goes wrong:** All HTTP failures return generic errors, making it impossible to distinguish between network timeouts, API rate limits, and invalid content rejections.

**Why it happens:** Existing code uses `fmt.Errorf("failed to add transfer: %w", err)` without categorizing error types.

**How to avoid:**
- Check HTTP status codes to categorize errors
- Use `NetworkError` for 5xx responses
- Use `AuthenticationError` for 401/403 responses
- Use `InvalidContentError` for 400 responses with API rejection messages
- Include Put.io API error messages in error context

**Warning signs:**
- Users can't tell if they should retry or fix their input
- Logs show generic "failed to add transfer" without actionable details
- Support tickets asking "why did my upload fail?"

**Example:**
```go
func categorizeUploadError(err error, statusCode int, apiMessage string) error {
    if statusCode >= 500 {
        return &NetworkError{
            Operation:  "upload_torrent",
            StatusCode: statusCode,
            APIMessage: apiMessage,
            Err:        err,
        }
    }

    if statusCode == 401 || statusCode == 403 {
        return &AuthenticationError{
            Operation: "upload_torrent",
            Err:       err,
        }
    }

    if statusCode == 400 {
        return &InvalidContentError{
            Reason: apiMessage,
            Err:    err,
        }
    }

    return fmt.Errorf("upload failed: %w", err)
}
```

### Pitfall 5: Deprecated Method Still in Use After Migration

**What goes wrong:** Old `AddTransfer` method remains in use by handlers, preventing adoption of new `AddTransferByURL` and `AddTransferByBytes` methods.

**Why it happens:** Deprecation without migration plan or lack of compile-time enforcement.

**How to avoid:**
- Add `// Deprecated:` comment to old method with migration instructions
- Update all callers in the codebase to use new methods
- Consider removing deprecated method after migration (Go 1 compatibility allows this)
- Add tests for new methods before deprecating old ones

**Warning signs:**
- Both old and new methods exist
- Inconsistent error handling between old and new code paths
- Confusion about which method to use

**Example:**
```go
// Source: https://go.dev/wiki/Deprecated
// Source: https://rakyll.org/deprecated/

// Deprecated: Use AddTransferByURL for magnet/HTTP(S) links or AddTransferByBytes for .torrent file content.
// This method will be removed in a future version.
func (c *Client) AddTransfer(ctx context.Context, url string, downloadDir string) (*transfer.Transfer, error) {
    // Forward to new method for now
    return c.AddTransferByURL(ctx, url, downloadDir)
}
```

## Code Examples

Verified patterns from official sources and existing codebase:

### Put.io SDK Files.Upload() Usage

```go
// Source: https://pkg.go.dev/github.com/putdotio/go-putio
// Existing client initialization pattern from internal/dc/putio/client.go

import (
    "bytes"
    "context"
    "github.com/putdotio/go-putio"
)

func (c *Client) AddTransferByBytes(ctx context.Context, torrentBytes []byte, filename string, downloadDir string) (*transfer.Transfer, error) {
    logger := logctx.LoggerFromContext(ctx).With("filename", filename, "download_dir", downloadDir)

    // Validate size
    if len(torrentBytes) > maxTorrentSize {
        return nil, &transfer.InvalidContentError{
            Filename: filename,
            Reason:   fmt.Sprintf("file size %d bytes exceeds maximum %d bytes", len(torrentBytes), maxTorrentSize),
        }
    }

    // Validate extension
    if err := validateTorrentFilename(filename); err != nil {
        return nil, err
    }

    // Resolve directory (same as magnet links)
    var dirID int64
    if downloadDir != "" {
        var err error
        dirID, err = c.findDirectoryID(ctx, downloadDir)
        if err != nil {
            return nil, &transfer.DirectoryError{
                DirectoryName: downloadDir,
                Reason:        "directory not found or inaccessible",
                Err:           err,
            }
        }
    }

    // Convert bytes to io.Reader
    reader := bytes.NewReader(torrentBytes)

    logger.Info("uploading torrent file to Put.io", "size_bytes", len(torrentBytes))

    // Upload to Put.io
    upload, err := c.putioClient.Files.Upload(ctx, reader, filename, dirID)
    if err != nil {
        return nil, &transfer.NetworkError{
            Operation:  "upload_torrent",
            APIMessage: err.Error(),
            Err:        err,
        }
    }

    // Put.io automatically creates transfer for .torrent files
    if upload.Transfer == nil {
        return nil, &transfer.InvalidContentError{
            Filename: filename,
            Reason:   "Put.io did not create transfer (file may not be valid torrent)",
        }
    }

    logger.Info("transfer created from torrent upload", "transfer_id", upload.Transfer.ID)

    // Convert to internal transfer type (same pattern as existing AddTransfer)
    return &transfer.Transfer{
        ID:                 fmt.Sprintf("%d", upload.Transfer.ID),
        Name:               upload.Transfer.Name,
        Status:             upload.Transfer.Status,
        Size:               int64(upload.Transfer.Size),
        Progress:           float64(upload.Transfer.PercentDone),
        // ... other fields
    }, nil
}
```

### AddTransferByURL Method (Renamed from AddTransfer)

```go
// Explicit method for URL-based transfers (magnet links, HTTP(S) URLs)
func (c *Client) AddTransferByURL(ctx context.Context, url string, downloadDir string) (*transfer.Transfer, error) {
    logger := logctx.LoggerFromContext(ctx).With("url", url, "download_dir", downloadDir)

    var dirID int64
    if downloadDir != "" {
        var err error
        dirID, err = c.findDirectoryID(ctx, downloadDir)
        if err != nil {
            return nil, &transfer.DirectoryError{
                DirectoryName: downloadDir,
                Reason:        "directory not found or inaccessible",
                Err:           err,
            }
        }
    }

    logger.Info("adding transfer to Put.io", "transfer_url", url)

    t, err := c.putioClient.Transfers.Add(ctx, url, dirID, "")
    if err != nil {
        return nil, &transfer.NetworkError{
            Operation:  "add_transfer_by_url",
            APIMessage: err.Error(),
            Err:        err,
        }
    }

    logger.Info("transfer added to Put.io", "transfer_id", t.ID)

    return &transfer.Transfer{
        ID:            fmt.Sprintf("%d", t.ID),
        Name:          t.Name,
        Status:        t.Status,
        // ... existing conversion logic
    }, nil
}
```

### Test Pattern Using httptest

```go
// Source: Existing pattern from internal/dc/deluge/client_test.go

func TestAddTransferByBytes_Success(t *testing.T) {
    // Mock Put.io server response
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Simulate Put.io upload response with transfer creation
        response := map[string]any{
            "transfer": map[string]any{
                "id":           12345,
                "name":         "test.torrent",
                "status":       "DOWNLOADING",
                "size":         1024000,
                "percent_done": 0.0,
            },
        }
        json.NewEncoder(w).Encode(response)
    }))
    defer ts.Close()

    // Create client pointing to mock server
    client := NewClient("test-token")
    // Override base URL for testing

    torrentBytes := []byte("fake torrent content")
    transfer, err := client.AddTransferByBytes(context.Background(), torrentBytes, "test.torrent", "itv")

    assert.NoError(t, err)
    assert.NotNil(t, transfer)
    assert.Equal(t, "12345", transfer.ID)
}

func TestAddTransferByBytes_InvalidExtension(t *testing.T) {
    client := NewClient("test-token")

    torrentBytes := []byte("fake content")
    _, err := client.AddTransferByBytes(context.Background(), torrentBytes, "test.txt", "itv")

    assert.Error(t, err)

    var invalidErr *transfer.InvalidContentError
    assert.True(t, errors.As(err, &invalidErr))
    assert.Contains(t, invalidErr.Reason, ".torrent")
}

func TestAddTransferByBytes_FileTooLarge(t *testing.T) {
    client := NewClient("test-token")

    // Create 11MB of data
    torrentBytes := make([]byte, 11*1024*1024)
    _, err := client.AddTransferByBytes(context.Background(), torrentBytes, "large.torrent", "itv")

    assert.Error(t, err)

    var invalidErr *transfer.InvalidContentError
    assert.True(t, errors.As(err, &invalidErr))
    assert.Contains(t, invalidErr.Reason, "exceeds maximum")
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Single AddTransfer() method | Explicit AddTransferByURL() and AddTransferByBytes() | 2026 (this phase) | Clearer method intent, type-safe parameters |
| Generic errors with fmt.Errorf | Custom error types with context | 2026 (this phase) | Programmatic error handling, better diagnostics |
| Sentinel errors (errors.New) | Struct-based errors with Unwrap() | Go 1.13 (2019) | Error wrapping, errors.Is/As inspection |
| Direct HTTP status code checks | Error categorization by HTTP response | 2025-2026 | Separates concerns, testable error handling |

**Deprecated/outdated:**
- **Generic error strings without wrapping:** Use `fmt.Errorf("context: %w", err)` instead of `fmt.Errorf("context: %s", err.Error())`
- **Type assertion on error strings:** Use `errors.As()` instead of `strings.Contains(err.Error(), "expected")`
- **Ignoring error chains:** Always implement `Unwrap()` on custom error types to maintain error chains

## Open Questions

1. **User folder validation configuration**
   - What we know: Put.io account has user folders with initials (e.g., "itv")
   - What's unclear: Should allowed folders be configured via environment variables, or hardcoded? Should validation be opt-in or mandatory?
   - Recommendation: Add optional validation in Phase 4, make it configurable. Handler integration (Phase 5) can decide enforcement policy.

2. **Backward compatibility for existing AddTransfer callers**
   - What we know: Deprecating AddTransfer requires updating all call sites
   - What's unclear: Are there external integrations that call AddTransfer directly?
   - Recommendation: Keep deprecated AddTransfer as a wrapper to AddTransferByURL for one release cycle before removal. Add deprecation notice in Go doc comments.

3. **Error handling in telemetry wrapper**
   - What we know: `InstrumentedTransferClient` wraps `TransferClient` methods
   - What's unclear: Should custom error types be unwrapped before telemetry, or preserved as-is?
   - Recommendation: Preserve error types through telemetry wrapper. Use `errors.As()` in wrapper to extract error categories for metrics labeling without destroying type information.

## Sources

### Primary (HIGH confidence)
- [Put.io Go SDK v1.7.2 Documentation](https://pkg.go.dev/github.com/putdotio/go-putio) - Files.Upload and Transfers.Add methods
- [Go errors package](https://pkg.go.dev/errors) - errors.Is, errors.As, error wrapping
- [Go bytes package](https://pkg.go.dev/bytes) - bytes.NewReader documentation
- [Better Stack: Golang Errors](https://betterstack.com/community/guides/scaling-go/golang-errors/) - Custom error types, wrapping patterns
- [DigitalOcean: Creating Custom Errors in Go](https://www.digitalocean.com/community/tutorials/creating-custom-errors-in-go) - Struct-based error implementation
- [Leapcell: HTTP Status Codes in Go APIs](https://leapcell.io/blog/crafting-custom-errors-and-http-status-codes-in-go-apis) - Error categorization patterns

### Secondary (MEDIUM confidence)
- [Go Wiki: Deprecated](https://go.dev/wiki/Deprecated) - Deprecation comment syntax
- [BitTorrent Specification](https://wiki.theory.org/BitTorrentSpecification) - Torrent file size expectations (50-75KB typical)
- [VictoriaMetrics: Go I/O Readers](https://victoriametrics.com/blog/go-io-reader-writer/) - io.Reader best practices
- [Go path/filepath HasSuffix proposal](https://github.com/golang/go/issues/50943) - File extension validation discussion

### Tertiary (LOW confidence)
- Web search results for Put.io upload examples - Limited 2026-specific content, relied on SDK docs instead
- General Go file upload tutorials - Used for validation pattern verification only

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Put.io SDK is already in use (v1.7.2), Go stdlib packages verified via pkg.go.dev
- Architecture: HIGH - Custom error type patterns verified via multiple authoritative sources (Better Stack, DigitalOcean), existing codebase reviewed for patterns
- Pitfalls: HIGH - Based on Put.io SDK documentation warnings (memory constraints), real-world context from user (shared account folders), and established Go error handling anti-patterns

**Research date:** 2026-02-01
**Valid until:** 2026-03-01 (30 days - stable domain, Go SDK unlikely to change significantly)
