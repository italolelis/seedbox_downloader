# Phase 14: Transfer Cleanup Fix - Context

**Gathered:** 2026-02-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix Put.io transfer and file data cleanup after Sonarr/Radarr confirms import. Currently transfers stay on Put.io forever after import. Cleanup must be reliable with configurable seeding behavior.

</domain>

<decisions>
## Implementation Decisions

### Cleanup trigger
- Default behavior: clean up Put.io transfer **immediately** after Sonarr/Radarr confirms import
- Configurable seed ratio: if a seed ratio env var is set, wait until that ratio is reached before cleaning up
- When no seed ratio is configured, skip the seeding watch entirely — cleanup fires in the import confirmation path
- Always delete both the transfer AND file data (clean slate) — no option to keep files

### Seed ratio configuration
- Env var name and format: Claude's discretion, consistent with existing config patterns
- When ratio is set: WatchForSeeding monitors until ratio is reached, then cleans up
- When ratio is unset/zero: immediate cleanup after import

### Failure handling
- If Put.io cleanup API call fails: retry with exponential backoff (2-3 attempts), consistent with existing retry patterns in the codebase
- After retries exhausted: log error and continue — don't block the pipeline
- If transfer is already deleted on Put.io (manual deletion or expiry): log at info level and treat as success — the desired state is achieved

### Claude's Discretion
- Exact env var name and format for seed ratio
- Whether to refactor WatchForSeeding or add cleanup logic alongside it
- Retry implementation details (use existing backoff library or inline)
- How to detect "transfer already deleted" vs other API errors

</decisions>

<specifics>
## Specific Ideas

- The current WatchForSeeding flow appears to never fire properly — root cause investigation is part of this fix
- Existing codebase uses cenkalti/backoff v5 for retry logic — reuse that
- Transfer + file data deletion should be atomic (both or neither, with cleanup of partial state)

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 14-transfer-cleanup-fix*
*Context gathered: 2026-02-22*
