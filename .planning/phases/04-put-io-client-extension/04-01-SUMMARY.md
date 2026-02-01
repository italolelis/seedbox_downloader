---
phase: 04-put-io-client-extension
plan: 01
type: summary
subsystem: transfer
tags: [error-handling, go-stdlib, custom-errors]
requires:
  - phases: []
  - plans: []
provides:
  - Four custom error types for categorized transfer failure handling
  - Go 1.13+ error wrapping and inspection patterns
affects:
  - phases: [04-put-io-client-extension]
  - plans: [04-02]
tech-stack:
  added: []
  patterns:
    - "Custom error types with contextual fields"
    - "Error wrapping with Unwrap() implementation"
    - "Programmatic error inspection with errors.As()"
key-files:
  created:
    - internal/transfer/errors.go
    - internal/transfer/errors_test.go
  modified: []
decisions:
  - what: "Use struct-based error types over sentinel errors"
    why: "Enables carrying contextual data (filename, status code, operation) for better diagnostics"
    phase: 04
    plan: 01
  - what: "Implement Unwrap() on all custom error types"
    why: "Maintains error chains for errors.Is() and errors.As() compatibility"
    phase: 04
    plan: 01
metrics:
  duration: "92 seconds"
  completed: 2026-02-01
  tasks: 2
  files-changed: 2
  tests-added: 13
---

# Phase 4 Plan 01: Transfer Error Types Foundation

**One-liner:** Struct-based error types with contextual fields enabling programmatic failure categorization via errors.As() inspection.

## What Was Built

Created four custom error types in `internal/transfer/errors.go` following Go 1.13+ error handling patterns:

1. **InvalidContentError** - For malformed .torrent files, size limit violations, and Put.io content rejection
   - Fields: Filename, Reason, Err
   - Use case: File >10MB, missing .torrent extension, invalid torrent format

2. **NetworkError** - For network failures, API errors, 5xx responses, and rate limiting
   - Fields: Operation, StatusCode, APIMessage, Err
   - Use case: Connection timeouts, Put.io 503 errors, network unavailability

3. **DirectoryError** - For directory resolution failures
   - Fields: DirectoryName, Reason, Err
   - Use case: Directory not found, access denied, invalid path

4. **AuthenticationError** - For authentication/authorization failures
   - Fields: Operation, Err
   - Use case: 401 Unauthorized, 403 Forbidden, expired tokens

All types implement:
- `Error() string` - Formatted error messages with context
- `Unwrap() error` - Error chain traversal for errors.Is()/errors.As()

## Implementation Details

### Error Message Formatting

Each error type produces actionable messages:
- InvalidContentError: `"invalid torrent content in {filename}: {reason}"`
- NetworkError: `"network error during {operation} (HTTP {status}): {message}"`
- DirectoryError: `"directory error for '{directory}': {reason}"`
- AuthenticationError: `"authentication failed during {operation}"`

### Testing Coverage

Created 13 unit tests in `internal/transfer/errors_test.go`:
- 4 tests for Error() method formatting
- 4 tests for Unwrap() error chain traversal
- 4 tests for errors.As() type extraction from wrapped chains
- 1 test for nil error handling across all types

All tests verify Go 1.13+ error patterns work correctly:
- errors.Unwrap() extracts wrapped errors
- errors.Is() works through error chains
- errors.As() extracts typed errors with field values preserved

### Key Patterns Used

**Pattern 1: Contextual Error Fields**
Struct fields carry diagnostic information without requiring string parsing:
```go
err := &NetworkError{
    Operation: "upload_torrent",
    StatusCode: 503,
    APIMessage: "service unavailable",
}
```

**Pattern 2: Error Chain Preservation**
Unwrap() maintains error chains for inspection:
```go
func (e *NetworkError) Unwrap() error { return e.Err }
```

**Pattern 3: Type-Safe Error Inspection**
Callers can programmatically detect error categories:
```go
var netErr *transfer.NetworkError
if errors.As(err, &netErr) {
    // Retry on network errors
}
```

## Decisions Made

### Decision 1: Struct-Based Over Sentinel Errors
**Context:** Need to carry contextual data (filename, status code, operation) with errors

**Options:**
- A) Sentinel errors (`var ErrInvalidContent = errors.New(...)`)
- B) Struct-based custom types with fields

**Chose:** B (struct-based custom types)

**Rationale:** Sentinel errors are appropriate for static conditions but cannot carry variable context. Struct-based errors enable:
- Actionable error messages with specific values (which file, what status code)
- Programmatic inspection of error fields
- Richer diagnostics for logging and user feedback

**Impact:** Handlers in Phase 5 can inspect error fields to make intelligent retry/reporting decisions.

### Decision 2: Pointer Receivers for Error Methods
**Context:** Error type methods can use pointer or value receivers

**Options:**
- A) Value receivers `(e NetworkError) Error() string`
- B) Pointer receivers `(e *NetworkError) Error() string`

**Chose:** B (pointer receivers)

**Rationale:** Go convention for error types is pointer receivers because:
- Errors are typically created with `&ErrorType{...}` (pointer syntax)
- Consistent with standard library error patterns
- Avoids unnecessary copying of error structs

**Impact:** Error creation uses `&InvalidContentError{...}` consistently across codebase.

### Decision 3: Optional Err Field (Not Required)
**Context:** Should wrapped errors be mandatory or optional?

**Options:**
- A) Require wrapped error (Err field non-nil)
- B) Allow nil Err field for standalone errors

**Chose:** B (optional wrapped error)

**Rationale:** Not all errors have underlying causes:
- InvalidContentError for "file too large" has no underlying error
- NetworkError from Put.io may not have a wrapped Go error
- Optional Err provides flexibility without forcing artificial wrapping

**Impact:** Unwrap() returns nil when Err is nil (tested explicitly). Error messages remain informative without wrapped errors.

## Verification Results

All verification criteria met:

✅ `go build ./...` - Compilation successful
✅ `go vet ./internal/transfer/...` - No vet warnings
✅ `go test ./internal/transfer/...` - All 13 tests pass
✅ Error types exported and usable from other packages
✅ errors.As() works for type inspection
✅ errors.Is() works through error chains

## Files Changed

### Created
1. **internal/transfer/errors.go** (71 lines)
   - Four custom error types
   - Error() and Unwrap() implementations
   - Comprehensive documentation comments

2. **internal/transfer/errors_test.go** (304 lines)
   - 13 unit tests covering all error types
   - Error formatting tests
   - Error chain traversal tests
   - Type extraction tests

### Modified
None.

## Commits

| Hash    | Message                                           |
|---------|---------------------------------------------------|
| 3e444e3 | feat(04-01): create custom error types           |
| 6ce6b15 | test(04-01): add unit tests for custom error types |

## Deviations from Plan

None - plan executed exactly as written.

## Next Phase Readiness

### For Phase 4 Plan 02 (Put.io Client Methods)

**Ready to proceed:**
- ✅ Error types available for use in `internal/dc/putio/client.go`
- ✅ InvalidContentError ready for file validation failures
- ✅ NetworkError ready for API error categorization
- ✅ DirectoryError ready for directory resolution errors
- ✅ AuthenticationError ready for auth failure handling

**Integration points:**
```go
// Example usage in Plan 02
import "github.com/italolelis/seedbox_downloader/internal/transfer"

if len(torrentBytes) > maxTorrentSize {
    return nil, &transfer.InvalidContentError{
        Filename: filename,
        Reason:   "file size exceeds 10MB limit",
    }
}

if statusCode >= 500 {
    return nil, &transfer.NetworkError{
        Operation:  "upload_torrent",
        StatusCode: statusCode,
        APIMessage: apiErr.Message,
        Err:        err,
    }
}
```

**No blockers or concerns.**

### For Phase 5 (Transmission API Handler)

**Foundation established:**
- ✅ Error types enable structured error responses to Sonarr/Radarr
- ✅ Handler can inspect error types to return appropriate HTTP status codes
- ✅ Error messages provide actionable feedback for webhook retry logic

### For Phase 6 (Testing & Observability)

**Testing foundation:**
- ✅ Error type tests demonstrate patterns for mocking failures
- ✅ Type inspection enables testing different error scenarios
- ✅ Error fields provide structured data for telemetry

## Lessons Learned

### What Went Well
1. **Clean separation of concerns** - Error types defined independently from client logic
2. **Test-first mindset** - Comprehensive tests caught nil error handling edge case
3. **Go idioms followed** - Pointer receivers, Unwrap() patterns align with stdlib
4. **Documentation clarity** - Error types are self-documenting with clear field purposes

### What Could Be Improved
None - straightforward implementation with no complications.

### Reusable Patterns
1. **Struct-based errors template** - Four error types demonstrate pattern reusable in other packages
2. **Test structure** - Error formatting, unwrapping, and type extraction test pattern
3. **Field naming** - Operation, Reason, Err field names provide clear semantics

## Dependencies

### Required By
- internal/dc/putio/client.go (Plan 04-02) will use these error types
- internal/svc/transmission/handler.go (Plan 05-*) will inspect these errors

### Requires
- Go stdlib `errors` package (error wrapping, errors.As/Is)
- Go stdlib `fmt` package (error message formatting)

## Success Metrics

- **Execution time:** 92 seconds (1.5 minutes)
- **Code quality:** 0 vet warnings, 100% test pass rate
- **Test coverage:** 13 tests across 4 error types (all paths exercised)
- **Documentation:** All error types and methods have doc comments
- **Patterns:** Go 1.13+ error handling fully adopted

## Technical Debt

None introduced.

## Future Considerations

### Potential Enhancements (Not Needed Now)
1. **HTTP status code mapping helper** - Function to map error types to HTTP status codes (for Phase 5)
2. **Telemetry integration** - Error type extraction for metrics labeling (for Phase 6)
3. **Error categorization enum** - If more error types added, consider ErrorCategory type

### Maintenance Notes
- Error types are stable - breaking changes would impact all handlers
- New error types should follow same pattern (struct with fields, Error(), Unwrap())
- Tests should verify errors.As() extraction for any new types
