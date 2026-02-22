# Phase 15: Missing Transfer Handling - Context

**Gathered:** 2026-02-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Detect when tracked Put.io transfers or their files are missing during download attempts, log at warn level, send a Discord notification with details, clean up local state, and continue processing other transfers without crashing or stalling.

</domain>

<decisions>
## Implementation Decisions

### Detection trigger
- Detect missing transfers only in the error path — when a download/file-grab attempt fails because Put.io reports the transfer or files as gone
- Do NOT add periodic polling or pre-download list checks — only react when an operation fails
- Distinguish between "transfer itself removed from Put.io" vs "transfer exists but files were deleted" — different log messages and Discord notifications for each case
- Once detected as missing, treat it as final — no retry, no waiting, just log + notify + move on

### Notification content
- Discord notification is **detailed**: transfer name, transfer ID, what operation failed, and timestamp
- **Distinct messages** for the two cases: "transfer removed" vs "files missing" — different wording so the user knows exactly what happened
- Log at **warn level** (slog.Warn), not error — notable but not a crash
- Discord notification uses an **embed** format with colored sidebar and structured fields

### Pipeline response
- **Clean up** any partial local files for the missing transfer
- **Mark in DB** as missing/failed so the pipeline doesn't try to process the same transfer again on the next cycle
- **Isolate** from other transfers — a missing transfer does not affect other transfers in the same batch
- **Notify once per transfer** — the DB mark prevents re-processing, so Discord notification fires only on first detection

### Claude's Discretion
- How to detect "transfer removed" vs "files missing" from Put.io API responses
- Exact Discord embed field layout and color choice
- Which DB field/status to use for marking transfers as missing
- Error message wording

</decisions>

<specifics>
## Specific Ideas

- The existing Discord webhook in the codebase should be reused for notifications
- The existing `CleanupTransfer` "not found = success" pattern from Phase 14 handles the cleanup side; this phase focuses on the detection and notification side when we actually expected the transfer to be there

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 15-missing-transfer-handling*
*Context gathered: 2026-02-22*
