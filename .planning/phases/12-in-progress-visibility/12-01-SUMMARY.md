---
phase: 12-in-progress-visibility
plan: 01
subsystem: data-layer
tags: [transfer-api, in-progress-transfers, activity-tab, tdd]

dependency_graph:
  requires:
    - phase: 11
      plan: 01
      artifact: "SaveParentID-based tag matching"
      reason: "Ensures tag matching works for both in-progress and completed transfers"
  provides:
    - artifact: "In-progress transfers in GetTaggedTorrents"
      consumers: ["Activity Tab API", "Phase 12 Plan 02"]
      contract: "Returns all tagged transfers with conditional file population"
  affects:
    - component: "internal/dc/putio/client.go:GetTaggedTorrents"
      change: "FileID==0 filter removed, conditional file population added"
    - component: "internal/transfer/transfer.go:Transfer"
      change: "DownloadSpeed field added"

tech_stack:
  added: []
  patterns:
    - name: "Triple Safety Net"
      description: "IsAvailable() + IsDownloadable() + conditional file population prevents download pipeline from processing in-progress transfers"
      files: ["internal/transfer/transfer.go", "internal/dc/putio/client.go"]

key_files:
  created: []
  modified:
    - path: "internal/transfer/transfer.go"
      why: "Added DownloadSpeed field to Transfer struct"
      lines_changed: 1
    - path: "internal/dc/putio/client.go"
      why: "Removed FileID==0 filter, added conditional file population, mapped DownloadSpeed"
      lines_changed: 37
    - path: "internal/dc/putio/client_test.go"
      why: "Added TDD test cases for in-progress transfer handling"
      lines_changed: 79

decisions:
  - what: "Continue on file fetch errors for completed transfers"
    why: "Transfer should still appear in Activity tab even if file details unavailable. Download pipeline will skip it via IsDownloadable() check."
    alternatives: ["Return error and skip transfer entirely"]
    chosen: "Continue with empty Files array"

metrics:
  duration_seconds: 146
  tasks_completed: 2
  files_modified: 3
  tests_added: 4
  commits: 2
  completed_date: "2026-02-08"
---

# Phase 12 Plan 01: In-Progress Data Layer Summary

JWT auth with refresh rotation using jose library

## One-Liner

Remove FileID==0 filter from GetTaggedTorrents, add conditional file population, and DownloadSpeed field to enable in-progress transfers to flow through the system.

## What Was Built

Modified GetTaggedTorrents to return ALL tagged transfers (in-progress and completed) with conditional file population:

**Key Changes:**
1. **FileID==0 filter removed** - In-progress transfers (FileID==0) now pass through GetTaggedTorrents
2. **Conditional file population** - Files only fetched via getFilesRecursively when FileID != 0
3. **DownloadSpeed field** - Added to Transfer struct and populated from Put.io Transfer.DownloadSpeed (JSON: down_speed)
4. **Triple safety net** - In-progress transfers blocked from download pipeline via:
   - IsAvailable() returns false for "downloading", "in_queue", "waiting" statuses
   - IsDownloadable() returns false for empty Files array
   - Conditional file population prevents API errors

**Transfer Flow:**
- In-progress: SaveParentID matched → returned with empty Files → visible in Activity tab → skipped by download pipeline
- Completed: SaveParentID matched → Files populated → visible in Activity tab → processed by download pipeline

## Deviations from Plan

None - plan executed exactly as written.

## Tests Added

**TDD Flow (RED-GREEN):**

**RED Phase (Commit 13f1998):**
- Updated `fileid_zero` test to `in_progress_fileid_zero_returned` with wantCount=1
- Added `in_progress_transfer_returned` test case
- Added `completed_transfer_has_files` test case
- Added `mixed_inprogress_and_completed` test case
- Verified PeersConnected, PeersSendingToUs, DownloadSpeed population
- Tests FAILED as expected (FileID==0 filter still active)

**GREEN Phase (Commit 0a10944):**
- Implementation made all tests pass
- Full test suite passes (no regressions)
- `go vet ./...` clean
- `go build ./...` compiles cleanly

## Verification Results

**All Success Criteria Met:**
- GetTaggedTorrents returns both in-progress and completed transfers
- In-progress transfers have empty Files array
- Completed transfers have populated Files array
- DownloadSpeed field populated from Put.io Transfer.DownloadSpeed
- Download pipeline safety net verified (IsAvailable + IsDownloadable)
- All existing tests pass (no regressions)

**Triple Safety Net Verified:**
```go
// In orchestrator watchTransfers:
if !transfer.IsAvailable() || !transfer.IsDownloadable() {
    // Skips in-progress transfers
}

// In-progress transfer:
// - Status="downloading" -> IsAvailable()=false
// - Files=[] -> IsDownloadable()=false
// - Result: Skipped by download pipeline

// Completed transfer:
// - Status="completed" -> IsAvailable()=true
// - Files=[...] -> IsDownloadable()=true
// - Result: Processed by download pipeline
```

## Self-Check: PASSED

**Created files verified:**
```bash
[ -f ".planning/phases/12-in-progress-visibility/12-01-SUMMARY.md" ] && echo "FOUND" || echo "MISSING"
FOUND
```

**Commits verified:**
```bash
git log --oneline --all | grep -q "13f1998" && echo "FOUND: 13f1998" || echo "MISSING: 13f1998"
FOUND: 13f1998

git log --oneline --all | grep -q "0a10944" && echo "FOUND: 0a10944" || echo "MISSING: 0a10944"
FOUND: 0a10944
```

**Modified files verified:**
```bash
[ -f "internal/transfer/transfer.go" ] && echo "FOUND: internal/transfer/transfer.go" || echo "MISSING: internal/transfer/transfer.go"
FOUND: internal/transfer/transfer.go

[ -f "internal/dc/putio/client.go" ] && echo "FOUND: internal/dc/putio/client.go" || echo "MISSING: internal/dc/putio/client.go"
FOUND: internal/dc/putio/client.go

[ -f "internal/dc/putio/client_test.go" ] && echo "FOUND: internal/dc/putio/client_test.go" || echo "MISSING: internal/dc/putio/client_test.go"
FOUND: internal/dc/putio/client_test.go
```

All artifacts exist and commits are in git history.

## Next Steps

Phase 12 Plan 02: Activity Tab API implementation - expose in-progress transfers via existing /api/transfers/:clientID endpoint.
