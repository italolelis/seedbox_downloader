# Architecture Research: .torrent File Content Integration

**Domain:** Webhook-triggered torrent transfer proxy
**Researched:** 2026-02-01
**Confidence:** HIGH

## Standard Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    HTTP Handler Layer                        │
│  Receives webhook → Validates input → Routes to method       │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────┐   │
│  │         handleTorrentAdd (transmission.go)           │   │
│  │  - Parse request                                     │   │
│  │  - Validate fields (FileName XOR MetaInfo)           │   │
│  │  - Base64 decode MetaInfo                            │   │
│  │  - Extract label from request                        │   │
│  │  - Route to client layer                             │   │
│  └────────────────┬─────────────────────────────────────┘   │
│                   │                                          │
├───────────────────┴──────────────────────────────────────────┤
│                    Client Adapter Layer                      │
│  Adapts domain operations to external service APIs           │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────┐   │
│  │           Put.io Client (putio/client.go)            │   │
│  │                                                       │   │
│  │  AddTransfer(ctx, url, downloadDir)                  │   │
│  │  - Handles magnet links (existing)                   │   │
│  │                                                       │   │
│  │  [NEW] UploadTorrent(ctx, torrentData, downloadDir)  │   │
│  │  - Wraps torrent bytes in io.Reader                  │   │
│  │  - Calls Files.Upload() with .torrent filename       │   │
│  │  - Returns Transfer struct                           │   │
│  └──────────────────┬───────────────────────────────────┘   │
│                     │                                        │
├─────────────────────┴────────────────────────────────────────┤
│                 Put.io SDK Layer                             │
│  Official go-putio library (external dependency)             │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────┐   │
│  │            putio.FilesService.Upload()               │   │
│  │  - Creates multipart/form-data request              │   │
│  │  - Detects .torrent file extension                   │   │
│  │  - Automatically creates Transfer                    │   │
│  │  - Returns Upload struct with Transfer field         │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| handleTorrentAdd | HTTP request validation, base64 decoding, routing | Parse TransmissionRequest, validate mutually exclusive fields, decode base64, call appropriate client method |
| Put.io Client | Domain adapter for Put.io operations | Wraps Put.io SDK with domain-specific interface (TransferClient), maps SDK responses to domain models |
| Put.io SDK (Files.Upload) | Multipart file upload to Put.io API | Creates multipart request, detects .torrent extension, auto-creates Transfer |
| TransferClient interface | Contract for transfer operations | AddTransfer(url) for magnets, [NEW optional] UploadTorrent(data) for files |

## Recommended Integration Architecture

### Option A: Keep TransferClient Unchanged (RECOMMENDED)

**Decision:** Add new UploadTorrent method to Put.io Client WITHOUT changing TransferClient interface.

**Rationale:**
- TransferClient is satisfied by multiple implementations (Put.io, Deluge, instrumented wrapper)
- Deluge doesn't support .torrent webhook API (out of scope for v1.1)
- Adding UploadTorrent to interface would force no-op implementation in Deluge client
- Interface segregation principle: clients shouldn't depend on methods they don't use

**Implementation:**
```go
// internal/dc/putio/client.go - EXISTING
type Client struct {
    putioClient *putio.Client
}

// Satisfies TransferClient interface (unchanged)
func (c *Client) AddTransfer(ctx, url, downloadDir) (*transfer.Transfer, error)

// NEW method - NOT on TransferClient interface
func (c *Client) UploadTorrent(ctx context.Context, torrentData []byte, downloadDir string) (*transfer.Transfer, error) {
    // Find or create directory
    dirID := c.findDirectoryID(ctx, downloadDir)

    // Wrap bytes in reader
    reader := bytes.NewReader(torrentData)

    // Use Put.io SDK Files.Upload
    upload, err := c.putioClient.Files.Upload(ctx, reader, "transfer.torrent", dirID)
    if err != nil {
        return nil, fmt.Errorf("failed to upload torrent: %w", err)
    }

    // Extract Transfer from Upload response
    if upload.Transfer == nil {
        return nil, fmt.Errorf("no transfer created from torrent upload")
    }

    // Map to domain Transfer struct
    return &transfer.Transfer{
        ID: fmt.Sprintf("%d", upload.Transfer.ID),
        Name: upload.Transfer.Name,
        // ... map other fields
    }, nil
}
```

**Handler integration:**
```go
// internal/http/rest/transmission.go
func (h *TransmissionHandler) handleTorrentAdd(ctx, req) (*TransmissionResponse, error) {
    logger := logctx.LoggerFromContext(ctx)

    var torrent *transfer.Transfer
    var err error

    // Route based on which field is present
    if req.Arguments.MetaInfo != "" {
        logger.Debug("received torrent add with metainfo")

        // Base64 decode
        torrentData, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
        if err != nil {
            return nil, fmt.Errorf("invalid base64 metainfo: %w", err)
        }

        // Type assertion to access Put.io-specific method
        putioClient, ok := h.dc.(*putio.Client)
        if !ok {
            return nil, fmt.Errorf("torrent file upload only supported with Put.io client")
        }

        torrent, err = putioClient.UploadTorrent(ctx, torrentData, h.label)
    } else if req.Arguments.FileName != "" {
        logger.Debug("received torrent add magnet link")

        // Existing magnet link path
        torrent, err = h.dc.AddTransfer(ctx, req.Arguments.FileName, h.label)
    } else {
        return nil, fmt.Errorf("either filename or metainfo must be provided")
    }

    if err != nil {
        return nil, fmt.Errorf("failed to add transfer: %w", err)
    }

    // Return success response
    // ... existing response marshaling
}
```

### Option B: Extend TransferClient Interface (NOT RECOMMENDED)

**What:** Add UploadTorrent method to TransferClient interface

**Why NOT recommended:**
- Forces Deluge client to implement method it can't support
- Violates interface segregation principle
- Creates false contract - Deluge would need runtime error "not supported"
- No benefit: only Put.io supports this webhook API in v1.1

**When to reconsider:** If Deluge webhook support is added in future milestone AND Deluge supports .torrent file uploads

### Option C: Overload AddTransfer with Data Parameter (NOT RECOMMENDED)

**What:** Change AddTransfer signature to accept both URL and raw data

**Why NOT recommended:**
- Breaking change to existing interface
- Violates backward compatibility constraint
- Would require changes to Deluge client and instrumented wrapper
- More complex signature (url string OR data []byte is ambiguous)

## Data Flow

### Magnet Link Flow (Existing)

```
Sonarr/Radarr
    ↓ (webhook: filename="magnet:...")
[TransmissionHandler.handleTorrentAdd]
    ↓ (validate FileName present)
[Put.io Client.AddTransfer]
    ↓ (url=magnetLink, dirID)
[Put.io SDK Transfers.Add]
    ↓ (POST /v2/transfers/add)
[Put.io API]
    ↓ (creates transfer)
[Response: Transfer struct]
```

### .torrent File Flow (NEW)

```
Sonarr/Radarr
    ↓ (webhook: metainfo="<base64 torrent>")
[TransmissionHandler.handleTorrentAdd]
    ↓ (validate MetaInfo present)
    ↓ (base64 decode)
    ↓ (type assert to *putio.Client)
[Put.io Client.UploadTorrent]
    ↓ (wrap bytes in reader)
    ↓ (filename="transfer.torrent", dirID)
[Put.io SDK Files.Upload]
    ↓ (multipart/form-data POST /v2/files/upload)
[Put.io API]
    ↓ (detects .torrent extension)
    ↓ (auto-creates transfer)
[Response: Upload struct with Transfer field]
    ↓ (extract Transfer)
[Map to domain Transfer struct]
```

### Key Data Flow Characteristics

1. **Validation occurs at handler layer:** Base64 decoding and field validation happen before calling client
2. **No file persistence:** Bytes flow from memory (request) → memory (buffer) → network (upload)
3. **Client abstraction preserved:** Magnet flow continues through TransferClient interface
4. **Put.io specificity isolated:** .torrent flow uses concrete type, fails fast for non-Put.io clients
5. **Error propagation:** Each layer wraps errors with context for debugging

## Recommended Component Changes

### Modified Components

#### 1. internal/http/rest/transmission.go (handleTorrentAdd method)

**Changes:**
- Add MetaInfo field validation (mutually exclusive with FileName)
- Add base64 decoding with error handling
- Add type assertion to *putio.Client for .torrent path
- Add explicit error message when non-Put.io client used
- Route to UploadTorrent for MetaInfo, AddTransfer for FileName

**Backward compatibility:**
- ✓ Existing FileName path unchanged
- ✓ No signature changes to handler
- ✓ No config changes required

#### 2. internal/dc/putio/client.go (Put.io Client)

**Changes:**
- Add UploadTorrent method (not on interface)
- Reuse existing findDirectoryID helper
- Map putio.Upload response to domain transfer.Transfer

**Backward compatibility:**
- ✓ AddTransfer method unchanged
- ✓ TransferClient interface still satisfied
- ✓ No changes to existing callers

### Unchanged Components

#### 1. internal/transfer/transfer.go (TransferClient interface)

**Why unchanged:**
- Only Put.io supports .torrent webhook upload in v1.1
- Deluge client has no use for this method
- Interface segregation: don't force clients to implement unused methods
- Instrumented wrapper doesn't need modification

#### 2. internal/dc/deluge/client.go (Deluge Client)

**Why unchanged:**
- Deluge webhook API is out of scope for v1.1
- No caller will invoke UploadTorrent on Deluge client
- Existing AddTransfer sufficient for Deluge use cases

#### 3. internal/transfer/instrumented_client.go (Instrumented Wrapper)

**Why unchanged:**
- Wraps TransferClient interface methods only
- UploadTorrent not on interface, so not wrapped
- Telemetry for .torrent uploads added at handler layer instead

## Architectural Patterns

### Pattern 1: Handler Layer Validation

**What:** HTTP handlers validate and transform input before calling business logic

**When to use:** All HTTP endpoints that accept complex input formats (base64, multipart, etc.)

**Trade-offs:**
- ✓ Pro: Failures return proper HTTP error codes immediately
- ✓ Pro: Business logic receives clean, validated data
- ✓ Pro: Easier to test - mock at client boundary with clean data
- ✗ Con: Handler has more responsibilities (but appropriate for HTTP concerns)

**Example:**
```go
func (h *Handler) handleTorrentAdd(ctx, req) error {
    // VALIDATE: Mutually exclusive fields
    hasMetaInfo := req.Arguments.MetaInfo != ""
    hasFileName := req.Arguments.FileName != ""

    if hasMetaInfo == hasFileName {
        return fmt.Errorf("exactly one of metainfo or filename required")
    }

    // TRANSFORM: Base64 decode
    if hasMetaInfo {
        data, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
        if err != nil {
            return fmt.Errorf("invalid base64: %w", err)
        }
        // Now pass clean []byte to client
    }
}
```

### Pattern 2: Type Assertion for Implementation-Specific Features

**What:** Use type assertion to access methods not on interface

**When to use:** When feature only supported by subset of implementations

**Trade-offs:**
- ✓ Pro: Avoids polluting interface with implementation-specific methods
- ✓ Pro: Clear fail-fast behavior for unsupported clients
- ✓ Pro: Maintains interface segregation principle
- ✗ Con: Handler knows about concrete type (acceptable for routing logic)

**Example:**
```go
// Client declared as interface
type Handler struct {
    dc transfer.TransferClient  // Interface type
}

// Feature detection and routing
func (h *Handler) handleFeature(ctx) error {
    // Try to access implementation-specific method
    if putioClient, ok := h.dc.(*putio.Client); ok {
        return putioClient.ImplementationSpecificMethod()
    }
    return fmt.Errorf("feature only supported with Put.io client")
}
```

### Pattern 3: SDK Auto-Detection of File Types

**What:** Put.io SDK automatically detects .torrent files and creates Transfers

**When to use:** When upstream API provides intelligent content detection

**Trade-offs:**
- ✓ Pro: No explicit "add transfer from file" API call needed
- ✓ Pro: Single upload endpoint handles multiple use cases
- ✓ Pro: Less code to maintain
- ✗ Con: Relies on filename extension (must use .torrent suffix)
- ✗ Con: Behavior is implicit (must read docs to understand)

**Example:**
```go
// Upload with .torrent extension triggers auto-detection
upload, err := client.Files.Upload(ctx, reader, "transfer.torrent", dirID)

// Response contains Transfer field if .torrent detected
if upload.Transfer != nil {
    // Transfer was created automatically
}
```

### Pattern 4: Bytes-in-Memory for No-Persistence Constraint

**What:** Process file content entirely in memory without disk writes

**When to use:** When persistence is explicitly forbidden (security, cleanup, simplicity)

**Trade-offs:**
- ✓ Pro: No cleanup required
- ✓ Pro: No disk I/O overhead
- ✓ Pro: No file permissions or path concerns
- ✗ Con: Memory pressure for large files (mitigated: .torrent files are small <1MB)

**Example:**
```go
// Decode from request (already in memory)
torrentData, err := base64.StdEncoding.DecodeString(metainfo)

// Wrap in reader (still in memory)
reader := bytes.NewReader(torrentData)

// Upload directly (no intermediate file)
upload, err := sdk.Files.Upload(ctx, reader, "file.torrent", dirID)
```

## Anti-Patterns

### Anti-Pattern 1: Adding Interface Methods with Partial Support

**What people do:** Add UploadTorrent to TransferClient interface

**Why it's wrong:**
- Forces Deluge client to implement method it can't support
- Creates runtime errors instead of compile-time safety
- Violates interface segregation principle (clients depend on methods they don't use)
- Misleads API consumers - interface suggests all implementations support feature

**Do this instead:**
- Keep method on concrete type only
- Use type assertion for feature detection
- Document which clients support which features
- Fail fast with clear error message

### Anti-Pattern 2: File Persistence "Just in Case"

**What people do:** Save .torrent to disk, then upload from disk

**Why it's wrong:**
- Violates explicit v1.1 constraint (no file persistence)
- Adds cleanup complexity (disk full, permissions, stale files)
- Creates security surface (file path traversal, sensitive trackers)
- No benefit: SDK accepts io.Reader, bytes.Reader works

**Do this instead:**
- Decode base64 to []byte in memory
- Wrap in bytes.NewReader
- Pass directly to SDK Upload method
- No cleanup needed - garbage collector handles it

### Anti-Pattern 3: Overloading AddTransfer with Optional Parameters

**What people do:** AddTransfer(ctx, urlOrData string, isFile bool, downloadDir string)

**Why it's wrong:**
- Breaking change to existing interface
- Ambiguous signature (string could be URL or base64)
- Type-unsafe (can't distinguish magnet from base64 at compile time)
- Requires changes to all implementations and callers

**Do this instead:**
- Keep AddTransfer for URL-based transfers (magnet links)
- Add separate UploadTorrent for byte-based transfers (.torrent files)
- Clear separation of concerns at type system level

### Anti-Pattern 4: Decoding Base64 in Client Layer

**What people do:** Pass base64 string to client, let client decode it

**Why it's wrong:**
- Mixes HTTP concerns (base64 encoding) with domain logic
- Harder to test client in isolation
- Can't return proper HTTP 400 error codes from client layer
- Client shouldn't know about HTTP encoding formats

**Do this instead:**
- Decode base64 in handler (HTTP concern)
- Validate decoding success immediately
- Return HTTP 400 if invalid base64
- Pass clean []byte to client layer

## Integration Points

### Handler → Client Integration

| Integration | Pattern | Error Handling |
|-------------|---------|----------------|
| Magnet link (existing) | Interface method call: `h.dc.AddTransfer(url)` | Return error, handler wraps with context |
| .torrent file (NEW) | Type assertion + concrete method: `putioClient.UploadTorrent(data)` | Type assertion failure = explicit error, upload failure = wrapped error |

### Client → SDK Integration

| Integration | Pattern | Notes |
|-------------|---------|-------|
| Magnet link | `putioClient.Transfers.Add(ctx, url, dirID, "")` | Existing, returns Transfer directly |
| .torrent file | `putioClient.Files.Upload(ctx, reader, "file.torrent", dirID)` | Returns Upload struct, extract Transfer field |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| Handler ↔ Client | Method calls on interface or concrete type | Handler does type assertion for feature routing |
| Client ↔ SDK | Direct method calls on SDK structs | Client wraps SDK responses in domain models |
| Handler ↔ Logger | Context-aware structured logging | Log validation failures, routing decisions, errors |

## Suggested Build Order

### Phase 1: Client Layer (Foundation)

**Goal:** Implement UploadTorrent method in Put.io client

**Tasks:**
1. Add UploadTorrent method to `internal/dc/putio/client.go`
2. Reuse findDirectoryID helper (already exists)
3. Call `Files.Upload` with bytes.NewReader and .torrent filename
4. Map putio.Upload response to domain transfer.Transfer
5. Add error wrapping with context

**Test strategy:**
- Unit test with mock Put.io SDK
- Verify correct dirID resolution
- Verify Transfer extraction from Upload response
- Verify error cases (upload failure, no Transfer in response)

**Exit criteria:**
- UploadTorrent method compiles and passes tests
- No changes to TransferClient interface
- Backward compatibility maintained

### Phase 2: Handler Layer (Integration)

**Goal:** Route .torrent requests to UploadTorrent

**Tasks:**
1. Modify handleTorrentAdd in `internal/http/rest/transmission.go`
2. Add validation: FileName XOR MetaInfo (mutually exclusive)
3. Add base64 decoding for MetaInfo field
4. Add type assertion to *putio.Client
5. Route MetaInfo → UploadTorrent, FileName → AddTransfer
6. Add structured logging for routing decisions

**Test strategy:**
- Integration test with test HTTP server
- Test FileName path (existing magnet behavior)
- Test MetaInfo path (new .torrent behavior)
- Test validation failures (both fields, neither field)
- Test base64 decode failures
- Test non-Put.io client error

**Exit criteria:**
- Both paths work end-to-end
- Existing magnet link behavior unchanged
- Clear error messages for all failure modes
- Structured logs for debugging

### Phase 3: Observability (Instrumentation)

**Goal:** Add metrics and logging for .torrent vs magnet usage

**Tasks:**
1. Add counter metric: torrent_add_total{type="magnet"|"file"}
2. Add logging: which path taken (magnet vs file)
3. Add error metric: torrent_add_errors_total{type="...", reason="..."}
4. Verify metrics exported via OTLP

**Test strategy:**
- Send test requests for both types
- Verify metrics increment correctly
- Verify logs contain routing information
- Check Prometheus scrape endpoint

**Exit criteria:**
- Metrics distinguish magnet from .torrent
- Errors categorized by type and reason
- Logs sufficient for production debugging

### Dependency Order

```
Phase 1 (Client)
    ↓ (depends on: Put.io SDK - already exists)

Phase 2 (Handler)
    ↓ (depends on: Phase 1 UploadTorrent method)

Phase 3 (Observability)
    ↓ (depends on: Phase 2 routing logic)
```

**Rationale:**
- Bottom-up approach: client first, handler second
- Each phase independently testable
- Clear integration point between phases
- Observability last (doesn't block functionality)

## Error Handling Strategy

### Handler Layer Errors

| Error | Handling | HTTP Response |
|-------|----------|---------------|
| Neither FileName nor MetaInfo | Return error immediately | 400 Bad Request |
| Both FileName and MetaInfo | Return error immediately | 400 Bad Request |
| Invalid base64 in MetaInfo | Return error immediately | 400 Bad Request |
| Type assertion fails (non-Put.io) | Return explicit error | 400 Bad Request with message |

### Client Layer Errors

| Error | Handling | Propagation |
|-------|----------|-------------|
| findDirectoryID fails | Wrap error with context | Return to handler → 400 Bad Request |
| Files.Upload fails | Wrap error with context | Return to handler → 500 Internal Server Error |
| Upload.Transfer is nil | Return explicit error | Return to handler → 500 Internal Server Error |

### Logging Strategy

**Handler layer:**
- Log routing decision (magnet vs file) at Debug level
- Log validation failures at Debug level
- Log client errors at Error level with full context

**Client layer:**
- Log UploadTorrent call at Info level with downloadDir
- Log SDK responses at Debug level
- Log errors at Error level with transfer context

## Sources

### HIGH Confidence (Official Documentation & Source Code)

- [Put.io Go SDK - Files.Upload method](https://github.com/putdotio/go-putio) - Official SDK source code showing multipart upload with auto-detection of .torrent files
- [Put.io Go SDK - Transfers.Add method](https://github.com/putdotio/go-putio) - Official SDK source code showing URL-based transfer addition
- [Go encoding/base64 package](https://pkg.go.dev/encoding/base64) - Standard library documentation for base64 decoding

### MEDIUM Confidence (Architecture Patterns)

- [Go REST API Architecture](https://medium.com/@janishar.ali/how-to-architecture-good-go-backend-rest-api-services-14cc4730c05b) - Layer responsibility patterns
- [Layered Architecture in Go](https://medium.com/@shershnev/layered-architecture-implementation-in-golang-6318a72c1e10) - Validation placement guidance
- [Clean Architecture in Go](https://depshub.com/blog/clean-architecture-in-go/) - Interface segregation patterns

### Codebase Analysis (Existing Implementation)

- `/Users/italovietro/projects/seedbox_downloader/internal/http/rest/transmission.go` - Current handler implementation
- `/Users/italovietro/projects/seedbox_downloader/internal/dc/putio/client.go` - Current Put.io client implementation
- `/Users/italovietro/projects/seedbox_downloader/internal/transfer/transfer.go` - TransferClient interface definition
- `/Users/italovietro/projects/seedbox_downloader/.planning/PROJECT.md` - Project constraints and requirements

---
*Architecture research for: .torrent file content integration*
*Researched: 2026-02-01*
