# Phase 4: Put.io Client Extension - Context

**Gathered:** 2026-02-01
**Status:** Ready for planning

<domain>
## Phase Boundary

Build Put.io client methods to upload .torrent file content (as bytes) to Put.io and create transfers automatically. This is a client layer integration wrapping Put.io SDK's Files.Upload() method. Handler integration happens in Phase 5.

</domain>

<decisions>
## Implementation Decisions

### Method Signature & Interface
- **Deprecate existing AddTransfer method** — migrate all callers to explicit new methods
- **Two new methods for clarity:**
  - `AddTransferByURL(ctx, url string, downloadDir string)` — for magnet links and HTTP(S) URLs
  - `AddTransferByBytes(ctx, torrentBytes []byte, filename string, downloadDir string)` — for .torrent file content
- **Filename parameter required** — helps Put.io recognize .torrent files, caller provides name
- **Follow Go naming conventions** — explicit method names over interface{} type switching

### Error Handling Strategy
- **Detailed error types** — specific error categories for different failure modes
- **Define in internal/transfer package** — shared error types usable by all transfer clients
- **Four error categories to distinguish:**
  1. Invalid content errors (malformed .torrent file, Put.io rejection)
  2. Network/API errors (timeout, 5xx responses)
  3. Directory resolution errors (target directory not found)
  4. Authentication errors (invalid/expired API token)
- **Comprehensive error messages** including:
  - Specific failure reason (e.g., "torrent file rejected: invalid bencode structure")
  - Put.io API details (HTTP status code, API error message)
  - Context about the operation (filename, directory, transfer size)
  - Suggested remediation (hints like "check torrent file validity" or "verify API token")

### Directory Resolution
- **Exact same logic as magnet links** — use downloadDir parameter, resolve via findDirectoryID()
- **Share findDirectoryID helper** — both AddTransferByURL and AddTransferByBytes call the same helper (DRY principle)
- **Match magnet behavior for missing directories** — do whatever existing magnet link logic does (don't diverge)
- **Validate user folders** — check that target directory matches expected user folder patterns (prevents accidental cross-contamination between shared account users like 'itv', etc.)

### Validation & Safety
- **Trust caller for content validation** — don't validate bencode structure, let Put.io API reject if invalid
- **Enforce 10 MB size limit** — reject .torrent files larger than 10 MB to prevent memory issues (typical .torrent files are <1MB)
- **Require .torrent extension** — validate filename ends with .torrent, return error otherwise
- **No bencode parsing** — Put.io handles server-side, client just uploads bytes

### Claude's Discretion
- Exact error type naming conventions (align with Go standards)
- Whether to use custom error types vs errors.As/Is patterns
- Logging level and verbosity for each operation
- Whether to add retries for network errors or fail-fast

</decisions>

<specifics>
## Specific Ideas

**User folder structure:**
- Put.io account is shared among friends
- Each person has a folder with their initials (e.g., "itv")
- Directory validation should prevent files from going to wrong user's folder
- This is a real operational concern, not theoretical

**Design principles:**
- "Transfer in Put.io is independent from files" — keep TransferClient interface clean
- Methods should be "elegant and follow correct design patterns"
- Backward compatibility constraint exists but willing to deprecate for cleaner design

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 04-put-io-client-extension*
*Context gathered: 2026-02-01*
