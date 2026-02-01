# Phase 6: Observability & Testing - Research

**Researched:** 2026-02-01
**Domain:** Go testing, structured logging, and OpenTelemetry metrics
**Confidence:** HIGH

## Summary

Phase 6 adds observability and test coverage to the .torrent file handling feature implemented in Phases 4-5. The research focused on three key areas: structured logging with slog, OpenTelemetry metrics instrumentation, and Go testing patterns.

The existing codebase already uses go.opentelemetry.io/otel v1.38.0 with a comprehensive telemetry package, and log/slog for structured logging. The application follows established patterns for metrics (counter creation, attribute usage) and logging (contextual fields, structured key-value pairs). The testing challenge is unique: validating base64 decoding edge cases, bencode validation logic, integration with real .torrent files, and backward compatibility with existing magnet link behavior.

The standard approach is table-driven unit tests for edge cases (base64, bencode), httptest-based integration tests for handler behavior, and testdata directory for real .torrent file fixtures. Testify's require package (not assert) is the Go ecosystem standard for test assertions because it stops execution on first failure, preventing cascading errors.

**Primary recommendation:** Extend existing telemetry package with torrent_type counter, add structured logging at key decision points (MetaInfo vs FileName), and create comprehensive test suite using table-driven tests with testdata fixtures.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| go.opentelemetry.io/otel/metric | v1.38.0 | OpenTelemetry metrics API | Official OTel Go SDK, production-stable, already in use |
| log/slog | stdlib (Go 1.21+) | Structured logging | Official Go standard library, replaced older logging libraries |
| testing | stdlib | Unit testing framework | Go's built-in testing package |
| github.com/stretchr/testify | v1.11.1 | Test assertions | De facto standard for readable assertions, already in use |
| net/http/httptest | stdlib | HTTP handler testing | Standard library for testing HTTP handlers without network |
| github.com/zeebo/bencode | v1.0.0 | Bencode parsing | Already added in Phase 5, needed for validation tests |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| encoding/base64 | stdlib | Base64 encoding/decoding | Edge case testing, already used in transmission.go |
| crypto/sha1 | stdlib | Hash generation for filenames | Needed for testdata fixture generation |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| testify/require | testify/assert | assert continues on failure (collects multiple failures), require stops immediately (prevents cascading errors). **Use require** - 99% of the time correct choice per community consensus |
| slog | logrus/zap | slog is standard library as of Go 1.21, no dependencies, official path forward |
| OTel SDK | Prometheus client directly | OTel provides vendor-neutral observability, already committed to in codebase |

**Installation:**
```bash
# All dependencies already present in go.mod
# No new dependencies needed for Phase 6
```

## Architecture Patterns

### Recommended Test Structure
```
internal/
├── http/rest/
│   ├── transmission.go
│   ├── transmission_test.go       # Unit + integration tests
│   └── testdata/
│       ├── valid.torrent           # Real .torrent file from tracker
│       ├── invalid-bencode.txt     # Malformed bencode for validation tests
│       └── README.md               # Documents test file sources
├── transfer/
│   └── errors_test.go              # Already exists, validates custom error types
└── telemetry/
    └── telemetry.go                # Extend with torrent_type counter
```

### Pattern 1: Table-Driven Unit Tests with Testify
**What:** Struct slice defining test cases with inputs and expected outputs
**When to use:** Testing multiple edge cases for same function (base64 decoding, bencode validation)
**Example:**
```go
// Source: https://go.dev/wiki/TableDrivenTests
func TestBase64Decoding(t *testing.T) {
    tests := []struct {
        name        string
        input       string
        expectError bool
        errorReason string
    }{
        {
            name:        "valid standard encoding",
            input:       "Zm9v",  // "foo"
            expectError: false,
        },
        {
            name:        "invalid character",
            input:       "Zm9v!@#$",
            expectError: true,
            errorReason: "invalid base64 encoding",
        },
        {
            name:        "incorrect padding",
            input:       "Zm9",  // Missing padding
            expectError: true,
            errorReason: "invalid base64 encoding",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := &TransmissionRequest{
                Arguments: struct{...}{
                    MetaInfo: tt.input,
                },
            }

            _, err := handler.handleTorrentAddByMetaInfo(ctx, req)

            if tt.expectError {
                require.Error(t, err)
                var invalidErr *transfer.InvalidContentError
                require.ErrorAs(t, err, &invalidErr)
                require.Contains(t, invalidErr.Reason, tt.errorReason)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

### Pattern 2: httptest for Handler Testing
**What:** Test HTTP handlers without network calls using httptest.ResponseRecorder
**When to use:** Integration testing of API handlers (Transmission RPC endpoint)
**Example:**
```go
// Source: https://speedscale.com/blog/testing-golang-with-httptest/
func TestHandleTorrentAdd_Integration(t *testing.T) {
    // Create handler with mock Put.io client
    handler := NewTransmissionHandler("user", "pass", mockPutioClient, "label", "/downloads")

    reqBody := `{
        "method": "torrent-add",
        "arguments": {
            "metainfo": "BASE64_ENCODED_TORRENT"
        }
    }`

    req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader(reqBody))
    req.SetBasicAuth("user", "pass")

    w := httptest.NewRecorder()
    handler.HandleRPC(w, req)

    require.Equal(t, http.StatusOK, w.Code)

    var resp TransmissionResponse
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    require.Equal(t, "success", resp.Result)
}
```

### Pattern 3: testdata Directory for Real Files
**What:** Place test fixtures in testdata/ directory alongside test files
**When to use:** Integration tests requiring real .torrent files from trackers
**Example:**
```go
// Source: https://betterstack.com/community/guides/testing/intemediate-go-testing/
func TestRealTorrentFile(t *testing.T) {
    // Go test runner sets working directory to package source directory
    torrentBytes, err := os.ReadFile("testdata/valid.torrent")
    require.NoError(t, err)

    // Validate bencode structure
    err = validateBencodeStructure(torrentBytes)
    require.NoError(t, err)

    // Test filename generation
    filename := generateTorrentFilename(torrentBytes)
    require.True(t, strings.HasSuffix(filename, ".torrent"))
}
```

### Pattern 4: OpenTelemetry Counter with Attributes
**What:** Create Int64Counter instrument and record with low-cardinality attributes
**When to use:** Tracking torrent_type distribution (magnet vs metainfo)
**Example:**
```go
// Source: https://pkg.go.dev/go.opentelemetry.io/otel/metric
// In telemetry.go initializeBusinessMetrics():
t.torrentTypeCounter, err = t.meter.Int64Counter(
    "torrents.type.total",
    metric.WithDescription("Total torrents added by type"),
    metric.WithUnit("{torrent}"),
)

// In transmission.go handleTorrentAdd():
if req.Arguments.MetaInfo != "" {
    telemetry.RecordTorrentType(ctx, "metainfo")
} else if req.Arguments.FileName != "" {
    telemetry.RecordTorrentType(ctx, "magnet")
}

// In telemetry.go:
func (t *Telemetry) RecordTorrentType(ctx context.Context, torrentType string) {
    if t.torrentTypeCounter != nil {
        t.torrentTypeCounter.Add(ctx, 1,
            metric.WithAttributes(
                attribute.String("torrent_type", torrentType),
            ),
        )
    }
}
```

### Pattern 5: Structured Logging with slog
**What:** Add contextual key-value pairs to log entries for filtering/analysis
**When to use:** Logging decision points and error details
**Example:**
```go
// Source: https://betterstack.com/community/guides/logging/logging-in-go/
// Already present pattern in codebase:
logger := logctx.LoggerFromContext(ctx)
logger.Debug("processing torrent add",
    "torrent_type", "metainfo",
    "size_bytes", len(torrentBytes),
)

// For error logging (OBS-03 requirement):
logger.Error("base64 decode failed",
    "err", err,
    "error_type", "invalid_base64",
    "metainfo_length", len(req.Arguments.MetaInfo),
)
```

### Anti-Patterns to Avoid
- **Using assert instead of require:** assert continues after failure, leading to nil pointer panics in subsequent checks. Use require to stop immediately on assertion failure.
- **High-cardinality metric attributes:** Avoid attributes like user_id, transfer_id, or timestamp in metrics. Use only low-cardinality values (torrent_type: "magnet" or "metainfo").
- **Global test fixtures without cleanup:** testdata files are fine, but in-memory fixtures should use t.Cleanup() for proper resource management.
- **Testing implementation details:** Test behavior (does base64 decode fail correctly?), not internals (what exact base64 variant is used?).

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Test assertions | Custom if/error checks | testify/require | Better error messages, stops on first failure, standard ecosystem tool |
| HTTP handler testing | Net listener setup | httptest.ResponseRecorder | No network needed, faster, isolated, standard library |
| Metric cardinality limits | Manual tracking | OTel SDK WithCardinalityLimit | SDK handles aggregation and limit enforcement, spec-compliant |
| Golden file comparison | Manual byte comparison | cmp.Diff or testify/require.Equal | Proper diff output, handles edge cases |
| Base64 edge cases | Custom validation | encoding/base64 stdlib tests | Go's own tests cover RFC 3548/4648 edge cases, reference implementation |

**Key insight:** Go's standard library testing package and testify/require provide comprehensive testing primitives. Custom test frameworks add complexity without benefit. The testdata directory pattern is Go-idiomatic for file fixtures.

## Common Pitfalls

### Pitfall 1: Using assert Instead of require
**What goes wrong:** Test continues after assertion failure, causing nil pointer panics or misleading errors in subsequent checks
**Why it happens:** assert looks similar to require, easy to use wrong one
**How to avoid:** Always use require.* functions from github.com/stretchr/testify/require package
**Warning signs:** Test output shows multiple failures or nil pointer panics after initial assertion failure
**Fix:** Change `assert.NoError(t, err)` to `require.NoError(t, err)`
**Community consensus:** "99% of the time you want to use require instead of assert" - testify documentation

### Pitfall 2: High-Cardinality Metric Attributes
**What goes wrong:** Metrics backend overwhelmed by thousands of unique attribute combinations, causing memory exhaustion and query slowness
**Why it happens:** Adding transfer_id, filename, or timestamp as attributes creates unbounded cardinality
**How to avoid:** Only use low-cardinality attributes with known, limited value sets. For torrent_type: only "magnet" or "metainfo" (cardinality = 2)
**Warning signs:** Memory usage grows over time, metrics queries become slow, backend alerting on cardinality limits
**Specification guidance:** OpenTelemetry spec recommends default cardinality limit of 2000, but keep actual cardinality orders of magnitude lower

### Pitfall 3: testdata Files Without Documentation
**What goes wrong:** Team doesn't know where testdata files came from, can't reproduce or validate fixtures
**Why it happens:** Developers add test files without documenting source
**How to avoid:** Add testdata/README.md documenting: where each file came from (which tracker), how to regenerate, what validation it provides
**Warning signs:** Questions like "where did this .torrent file come from?" or "is this test file still valid?"
**Best practice:** Document test file provenance and regeneration steps

### Pitfall 4: Testing Transmission RPC Without Basic Auth
**What goes wrong:** Integration tests pass but real requests fail due to authentication middleware
**Why it happens:** httptest bypasses middleware unless explicitly called
**How to avoid:** Call handler.Routes().ServeHTTP() instead of handler.HandleRPC() directly, or manually call req.SetBasicAuth() on test requests
**Warning signs:** Tests pass but Sonarr/Radarr requests fail with 401 Unauthorized
**Fix:** Include authentication in integration tests to match real request flow

### Pitfall 5: Forgetting t.Parallel() Safety
**What goes wrong:** Parallel tests share loop variables causing race conditions and flaky test failures
**Why it happens:** Go 1.22+ handles this automatically, but explicitly capturing variables is clearer
**How to avoid:** When using t.Run() with t.Parallel(), ensure loop variables are properly scoped
**Warning signs:** Tests fail intermittently, different results on different runs
**Best practice:** Modern Go (1.22+) fixes this, but explicit capture still recommended for clarity

## Code Examples

Verified patterns from official sources:

### Base64 Decoding Edge Cases
```go
// Source: https://go.dev/src/encoding/base64/base64_test.go
func TestHandleTorrentAddByMetaInfo_Base64EdgeCases(t *testing.T) {
    handler := setupTestHandler(t)
    ctx := context.Background()

    tests := []struct {
        name        string
        metainfo    string
        expectError bool
        errorReason string
    }{
        {
            name:        "empty string",
            metainfo:    "",
            expectError: true,
            errorReason: "invalid base64 encoding",
        },
        {
            name:        "invalid characters",
            metainfo:    "!!!!",
            expectError: true,
            errorReason: "invalid base64 encoding",
        },
        {
            name:        "incorrect padding - extra",
            metainfo:    "YWJjZA=====",
            expectError: true,
            errorReason: "invalid base64 encoding",
        },
        {
            name:        "incorrect padding - misplaced",
            metainfo:    "A=AA",
            expectError: true,
            errorReason: "invalid base64 encoding",
        },
        {
            name:        "valid minimal",
            metainfo:    "Zg==",  // "f"
            expectError: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := &TransmissionRequest{}
            req.Arguments.MetaInfo = tt.metainfo

            _, err := handler.handleTorrentAddByMetaInfo(ctx, req)

            if tt.expectError {
                require.Error(t, err)
                var invalidErr *transfer.InvalidContentError
                require.ErrorAs(t, err, &invalidErr)
                require.Contains(t, invalidErr.Reason, tt.errorReason)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

### Bencode Validation Tests
```go
// Source: Derived from github.com/zeebo/bencode usage patterns
func TestValidateBencodeStructure(t *testing.T) {
    tests := []struct {
        name        string
        data        []byte
        expectError bool
        errorReason string
    }{
        {
            name:        "valid torrent structure",
            data:        []byte("d4:infod4:name4:teste"),  // {"info": {"name": "test"}}
            expectError: false,
        },
        {
            name:        "invalid bencode syntax",
            data:        []byte("not bencode"),
            expectError: true,
            errorReason: "invalid bencode structure",
        },
        {
            name:        "root is not dictionary",
            data:        []byte("l4:teste"),  // ["test"] - list instead of dict
            expectError: true,
            errorReason: "bencode root must be a dictionary",
        },
        {
            name:        "missing info field",
            data:        []byte("d4:name4:teste"),  // {"name": "test"} - no "info"
            expectError: true,
            errorReason: "bencode missing required 'info' dictionary",
        },
        {
            name:        "empty dictionary",
            data:        []byte("de"),  // {}
            expectError: true,
            errorReason: "bencode missing required 'info' dictionary",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateBencodeStructure(tt.data)

            if tt.expectError {
                require.Error(t, err)
                var invalidErr *transfer.InvalidContentError
                require.ErrorAs(t, err, &invalidErr)
                require.Contains(t, invalidErr.Reason, tt.errorReason)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

### Integration Test with Real Torrent File
```go
// Source: https://betterstack.com/community/guides/testing/intemediate-go-testing/
func TestHandleTorrentAdd_RealTorrentFile(t *testing.T) {
    // Read real .torrent file from testdata
    torrentBytes, err := os.ReadFile("testdata/valid.torrent")
    require.NoError(t, err)

    // Encode as base64 (matching Transmission RPC spec)
    metainfo := base64.StdEncoding.EncodeToString(torrentBytes)

    // Create handler with test Put.io client
    mockClient := &mockPutioClient{
        addTransferByBytesFunc: func(ctx context.Context, content []byte, filename, parentName string) (*transfer.Transfer, error) {
            return &transfer.Transfer{
                ID:   "12345",
                Name: "test-transfer",
            }, nil
        },
    }
    handler := NewTransmissionHandler("user", "pass", mockClient, "test-label", "/downloads")

    // Create request
    reqBody := fmt.Sprintf(`{
        "method": "torrent-add",
        "arguments": {
            "metainfo": "%s"
        }
    }`, metainfo)

    req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader(reqBody))
    req.SetBasicAuth("user", "pass")
    w := httptest.NewRecorder()

    // Execute handler
    handler.HandleRPC(w, req)

    // Verify response
    require.Equal(t, http.StatusOK, w.Code)

    var resp TransmissionResponse
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    require.Equal(t, "success", resp.Result)

    // Verify transfer was created
    require.Contains(t, string(resp.Arguments), "torrent-added")
}
```

### Backward Compatibility Test
```go
// Source: Derived from backward compatibility testing patterns
func TestHandleTorrentAdd_MagnetLinkBackwardCompatibility(t *testing.T) {
    mockClient := &mockPutioClient{
        addTransferFunc: func(ctx context.Context, magnetLink, parentName string) (*transfer.Transfer, error) {
            return &transfer.Transfer{
                ID:   "67890",
                Name: "magnet-transfer",
            }, nil
        },
    }
    handler := NewTransmissionHandler("user", "pass", mockClient, "test-label", "/downloads")

    reqBody := `{
        "method": "torrent-add",
        "arguments": {
            "filename": "magnet:?xt=urn:btih:HASH&dn=Test"
        }
    }`

    req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader(reqBody))
    req.SetBasicAuth("user", "pass")
    w := httptest.NewRecorder()

    handler.HandleRPC(w, req)

    require.Equal(t, http.StatusOK, w.Code)

    var resp TransmissionResponse
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
    require.Equal(t, "success", resp.Result)
    require.Contains(t, string(resp.Arguments), "torrent-added")

    // Verify AddTransfer (magnet link method) was called, not AddTransferByBytes
    require.True(t, mockClient.addTransferCalled, "AddTransfer should be called for magnet links")
    require.False(t, mockClient.addTransferByBytesCalled, "AddTransferByBytes should not be called for magnet links")
}
```

### OpenTelemetry Counter Recording
```go
// Source: https://pkg.go.dev/go.opentelemetry.io/otel/metric
// In internal/telemetry/telemetry.go - extend initializeBusinessMetrics():
t.torrentTypeCounter, err = t.meter.Int64Counter(
    "torrents.type.total",
    metric.WithDescription("Total torrents added by type (magnet vs metainfo)"),
    metric.WithUnit("{torrent}"),
)
if err != nil {
    return fmt.Errorf("failed to create torrents.type.total counter: %w", err)
}

// Add to Telemetry struct:
type Telemetry struct {
    // ... existing fields ...
    torrentTypeCounter metric.Int64Counter
}

// Add public method:
func (t *Telemetry) RecordTorrentType(ctx context.Context, torrentType string) {
    if t.torrentTypeCounter != nil {
        t.torrentTypeCounter.Add(ctx, 1,
            metric.WithAttributes(
                attribute.String("torrent_type", torrentType),
            ),
        )
    }
}

// In internal/http/rest/transmission.go - handleTorrentAdd():
func (h *TransmissionHandler) handleTorrentAdd(ctx context.Context, req *TransmissionRequest) (*TransmissionResponse, error) {
    logger := logctx.LoggerFromContext(ctx).With("method", "handle_torrent_add")

    var torrent *transfer.Transfer
    var err error

    if req.Arguments.MetaInfo != "" {
        logger.Debug("received torrent add with metainfo field", "torrent_type", "metainfo")
        h.telemetry.RecordTorrentType(ctx, "metainfo")  // OBS-02
        torrent, err = h.handleTorrentAddByMetaInfo(ctx, req)
    } else if req.Arguments.FileName != "" {
        logger.Debug("received torrent add with filename field", "torrent_type", "magnet")
        h.telemetry.RecordTorrentType(ctx, "magnet")  // OBS-02
        torrent, err = h.dc.AddTransfer(ctx, req.Arguments.FileName, h.label)
    } else {
        return nil, fmt.Errorf("either metainfo or filename must be provided")
    }

    // ... rest of handler
}
```

### Structured Error Logging
```go
// Source: https://betterstack.com/community/guides/logging/logging-in-go/
// In handleTorrentAddByMetaInfo() - extend existing error logging:

// Base64 decode error (OBS-03):
if err != nil {
    logger.Error("failed to decode base64 metainfo",
        "err", err,
        "error_type", "invalid_base64",
        "metainfo_length", len(req.Arguments.MetaInfo),
    )
    return nil, &transfer.InvalidContentError{
        Filename: "metainfo",
        Reason:   fmt.Sprintf("invalid base64 encoding: %v", err),
        Err:      err,
    }
}

// Bencode validation error (OBS-03):
if err := validateBencodeStructure(torrentBytes); err != nil {
    logger.Error("bencode validation failed",
        "err", err,
        "error_type", "invalid_bencode",
        "size_bytes", len(torrentBytes),
    )
    return nil, err
}

// Put.io API error (OBS-03):
torrent, err := h.dc.AddTransferByBytes(ctx, torrentBytes, filename, h.label)
if err != nil {
    logger.Error("failed to add transfer by bytes",
        "err", err,
        "error_type", "api_error",
        "filename", filename,
        "size_bytes", len(torrentBytes),
    )
    return nil, err
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| testify/assert | testify/require | Community consensus 2020+ | Tests stop on first failure instead of cascading errors |
| logrus/zap | log/slog | Go 1.21 (2023) | Standard library structured logging, zero dependencies |
| manual file paths | testdata directory | Go 1.0+ standard | go tool ignores testdata, explicit convention |
| Prometheus client direct | OpenTelemetry SDK | OTel stable 2023+ | Vendor-neutral observability, multiple backends |
| golden files manually | table-driven tests | Go idiom evolved 2015+ | More maintainable, clearer intent, better errors |

**Deprecated/outdated:**
- **testify/assert for most cases**: Use require to stop on first failure. assert is only for collecting multiple failures in rare cases.
- **Custom test frameworks**: Go's testing package + testify/require covers 99% of needs. Custom frameworks add complexity.
- **Embedding test data in code**: Use testdata directory for files. Easier to update, no escaping issues, clearer separation.

## Open Questions

Things that couldn't be fully resolved:

1. **Real .torrent file source for testdata**
   - What we know: Need valid .torrent file from amigos-share tracker for integration tests
   - What's unclear: Legal/licensing implications of committing tracker .torrent file to repository
   - Recommendation: Document in testdata/README.md that developer must obtain their own .torrent file from tracker for integration tests, or generate minimal valid .torrent file programmatically

2. **Telemetry instance injection into TransmissionHandler**
   - What we know: TransmissionHandler needs access to telemetry.Telemetry instance to call RecordTorrentType()
   - What's unclear: Current handler initialization in cmd/seedbox_downloader/main.go - is telemetry instance already available?
   - Recommendation: Add telemetry field to TransmissionHandler struct, pass in NewTransmissionHandler() constructor

3. **Metric cardinality limit configuration**
   - What we know: OTel SDK supports WithCardinalityLimit option, default is 2000
   - What's unclear: Should this application set explicit limit given only 2 possible values for torrent_type?
   - Recommendation: No explicit limit needed - with cardinality of 2, well under any reasonable limit. Document this in implementation.

## Sources

### Primary (HIGH confidence)
- https://pkg.go.dev/go.opentelemetry.io/otel/metric - OpenTelemetry Go Metrics API reference
- https://go.dev/wiki/TableDrivenTests - Official Go table-driven tests guide
- https://go.dev/src/encoding/base64/base64_test.go - Go stdlib base64 test cases (RFC 3548/4648 edge cases)
- https://pkg.go.dev/log/slog - Go standard library structured logging
- https://pkg.go.dev/net/http/httptest - Go standard library HTTP testing
- https://pkg.go.dev/github.com/stretchr/testify/require - Testify require package documentation
- https://pkg.go.dev/github.com/zeebo/bencode - Zeebo bencode library (already in use)

### Secondary (MEDIUM confidence)
- [OpenTelemetry Metrics: Types, Examples & Best Practices](https://www.groundcover.com/opentelemetry/opentelemetry-metrics) - Cardinality best practices
- [Logging in Go with Slog: The Ultimate Guide | Better Stack Community](https://betterstack.com/community/guides/logging/logging-in-go/) - slog patterns and best practices
- [Testing in Go with table drive tests and Testify](https://dev.to/zpeters/testing-in-go-with-table-drive-tests-and-testify-kd4) - Table-driven test patterns
- [Testing Golang with httptest Best Practices | Speedscale](https://speedscale.com/blog/testing-golang-with-httptest/) - httptest integration testing
- [Testing in Go: Intermediate Tips and Techniques | Better Stack Community](https://betterstack.com/community/guides/testing/intemediate-go-testing/) - testdata directory usage
- [Guide — OpenTelemetry and Modern Tooling for High Cardinality | Last9](https://last9.io/guides/high-cardinality/opentelemetry-and-modern-tooling-for-high-cardinality/) - Cardinality management
- [Go Best Practices — Testing](https://medium.com/@sebdah/go-best-practices-testing-3448165a0e18) - General Go testing patterns
- [Golang Testify: require vs assert | FreeThreads](https://medium.com/freethreads/golang-testify-require-vs-assert-b3bbfb4e0b8f) - require vs assert comparison

### Tertiary (LOW confidence)
- None - all key findings verified with official documentation or multiple sources

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - All libraries already in use or standard library, versions confirmed in go.mod
- Architecture patterns: HIGH - Verified with official Go documentation and existing codebase patterns
- Pitfalls: HIGH - Documented in official sources and community consensus (require vs assert, cardinality limits)
- Code examples: HIGH - Derived from official documentation and stdlib test patterns
- Open questions: MEDIUM - Real .torrent file sourcing and telemetry injection need validation during implementation

**Research date:** 2026-02-01
**Valid until:** 2026-03-01 (30 days - stable ecosystem, no fast-moving changes expected)
