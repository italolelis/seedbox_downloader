# Phase 1: Critical Safety - Context

**Gathered:** 2026-01-31
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix crashes and silent failures in error handling paths. Specifically:
- Eliminate nil pointer panics in HTTP operations
- Fix silent Discord webhook notification failures

This phase does NOT add new features or change application functionality — it makes existing error paths safe.

</domain>

<decisions>
## Implementation Decisions

### Error visibility
- **HTTP failures:** Log with detailed context (URL, method, relevant IDs, timing), then return error to caller
- **Structured logging:** Use structured logging fields (error_type, operation, url, status_code) for filtering/querying
- **Discord failures:** Log HTTP status code and error message (not full response body or request details)
- **Error propagation:** Errors are returned to callers for handling up the chain

### Recovery behavior
- **Retry strategy:** Operation-dependent
  - Some operations retry (network/timeout errors: connection refused, timeout, DNS failures)
  - Others fail fast (4xx client errors, validation failures)
  - Do NOT retry on 5xx server errors
- **Batch processing:** Continue processing remaining items when one fails (log failure, skip item, continue)
- **Discord notifications:** Best-effort only
  - Try once
  - Log if it fails
  - Continue execution (notifications are nice-to-have, not critical)

### Claude's Discretion
- Exact nil check placement and validation approach
- Retry attempt count and backoff timing for operations that do retry
- Specific structured logging field names (following existing codebase patterns)
- Which specific operations qualify for retry vs fail-fast

</decisions>

<specifics>
## Specific Ideas

No specific requirements — open to standard approaches that follow the decisions above.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope (bug fixes only, no new features).

</deferred>

---

*Phase: 01-critical-safety*
*Context gathered: 2026-01-31*
