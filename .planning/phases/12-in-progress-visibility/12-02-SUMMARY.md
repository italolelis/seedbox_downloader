---
phase: 12-in-progress-visibility
plan: 02
subsystem: http-api
tags: [transmission-rpc, activity-tab, status-mapping, tdd]

dependency_graph:
  requires:
    - phase: 12
      plan: 01
      artifact: "In-progress transfers in GetTaggedTorrents"
      reason: "Need transfer data with DownloadSpeed and peer counts populated"
  provides:
    - artifact: "Complete Transmission RPC torrent-get response"
      consumers: ["Sonarr/Radarr Activity Tab"]
      contract: "All Put.io statuses mapped to Transmission codes, peer/speed/labels populated"
  affects:
    - component: "internal/http/rest/transmission.go:TransmissionTorrent"
      change: "Added Labels, PeersConnected, PeersSendingToUs, PeersGettingFromUs, RateDownload fields"
    - component: "internal/http/rest/transmission.go:handleTorrentGet"
      change: "Complete status mapping, conditional ErrorString, peer/speed/label population"

tech_stack:
  added: []
  patterns:
    - name: "Complete Status Mapping"
      description: "All Put.io statuses (downloading, in_queue, waiting, finishing, checking, completed, finished, seeding, seedingwait, error) mapped to Transmission status codes with fallback for unknown statuses"
      files: ["internal/http/rest/transmission.go"]
    - name: "Conditional Error String"
      description: "ErrorString only populated for error transfers with non-empty ErrorMessage, nil for all others"
      files: ["internal/http/rest/transmission.go"]

key_files:
  created: []
  modified:
    - path: "internal/http/rest/transmission.go"
      why: "Added 5 new fields to TransmissionTorrent, complete status mapping, conditional ErrorString, peer/speed/label population"
      lines_changed: 28
    - path: "internal/http/rest/transmission_test.go"
      why: "Added TDD tests for status mapping, error strings, peer/speed fields, and labels"
      lines_changed: 227

decisions:
  - what: "Labels field always present (not omitempty)"
    why: "Sonarr/Radarr expect the labels field to always be present in the JSON response. Empty array is acceptable, but omitted field could cause client-side issues."
    alternatives: ["Use omitempty and let clients handle missing field"]
    chosen: "Always present with json:\"labels\" (no omitempty)"

metrics:
  duration_seconds: 151
  tasks_completed: 2
  files_modified: 2
  tests_added: 4
  commits: 2
  completed_date: "2026-02-08"
---

# Phase 12 Plan 02: Activity Tab API Summary

Complete Transmission RPC status mapping and peer/speed/label population for Activity Tab visibility

## One-Liner

Add Labels, peer counts, download speed fields to TransmissionTorrent; complete Put.io status mapping (11 statuses); conditional error string handling.

## What Was Built

Completed the Activity Tab integration by adding all missing fields to the Transmission RPC torrent-get response and implementing complete status mapping:

**Key Changes:**

1. **New TransmissionTorrent Fields:**
   - `Labels []string` - Always present (not omitempty), populated with configured proxy label
   - `PeersConnected int64` - Total peer count from transfer.PeersConnected
   - `PeersSendingToUs int64` - Download peer count from transfer.PeersSendingToUs
   - `PeersGettingFromUs int64` - Upload peer count from transfer.PeersGettingFromUs
   - `RateDownload int64` - Download speed in bytes/sec from transfer.DownloadSpeed

2. **Complete Status Mapping (11 Put.io Statuses):**
   - `downloading` → StatusDownload (4)
   - `in_queue`, `waiting` → StatusDownloadWait (3)
   - `finishing`, `checking` → StatusCheck (2)
   - `completed`, `finished` → StatusSeed (6)
   - `seeding` → StatusSeed (6)
   - `seedingwait` → StatusSeedWait (5)
   - `error` → StatusStopped (0) + ErrorString
   - Unknown statuses → StatusStopped (0) + warning log

3. **Conditional ErrorString Handling:**
   - Only populated for error transfers with non-empty ErrorMessage
   - Nil for all other transfers (omitted from JSON via omitempty)
   - Prevents empty error strings from appearing in Activity tab

4. **IsFinished Logic Update:**
   - Added "finished" to the completion check (alongside "completed" and "seeding")
   - Ensures all completed states properly marked as finished

**Activity Tab Now Displays:**
- In-progress downloads with accurate status (downloading/waiting/checking)
- Real-time download speed and peer counts
- Configured label for filtering/organization
- Error messages for failed transfers
- Accurate completion state

## Deviations from Plan

None - plan executed exactly as written.

## Tests Added

**TDD Flow (RED-GREEN):**

**RED Phase (Commit 3738630):**
- Added `TestHandleTorrentGet_StatusMapping`: Table-driven test for all 11 Put.io statuses
- Added `TestHandleTorrentGet_ErrorStringPopulated`: Verify ErrorString only set for error transfers (3 test cases)
- Added `TestHandleTorrentGet_PeerAndSpeedFields`: Verify peer counts and download speed populated
- Added `TestHandleTorrentGet_LabelsPopulated`: Verify labels array contains configured label
- Updated mockPutioClient with getTaggedTorrentsFunc for test control
- Tests FAILED as expected (status mapping incomplete, fields not populated)

**GREEN Phase (Commit 7b561c7):**
- Implementation made all tests pass
- Full test suite passes (no regressions)
- Unknown status test verified warning log behavior
- All existing tests pass (backward compatibility maintained)

## Verification Results

**All Success Criteria Met:**
- All 8 Put.io statuses correctly mapped to Transmission status codes
- Error transfers include ErrorString from transfer.ErrorMessage
- Unknown statuses produce warning log and default to StatusStopped
- Labels array populated with configured label for all transfers
- PeersConnected, PeersSendingToUs, PeersGettingFromUs, RateDownload populated
- Existing torrent-add, torrent-remove, authentication tests still pass
- Full project test suite passes (no regressions)

**Status Mapping Verified:**
```go
// All 11 Put.io statuses mapped:
downloading      -> StatusDownload (4)
in_queue/waiting -> StatusDownloadWait (3)
finishing/checking -> StatusCheck (2)
completed/finished/seeding -> StatusSeed (6)
seedingwait      -> StatusSeedWait (5)
error            -> StatusStopped (0) + ErrorString
unknown          -> StatusStopped (0) + warning log
```

**Field Population Verified:**
```go
// Test verified these fields populated correctly:
Labels:             []string{h.label}           // Always present
PeersConnected:     transfer.PeersConnected     // From transfer
PeersSendingToUs:   transfer.PeersSendingToUs   // From transfer
PeersGettingFromUs: transfer.PeersGettingFromUs // From transfer
RateDownload:       transfer.DownloadSpeed      // From transfer
ErrorString:        errorString                 // Conditional (error only)
```

**go vet and go build:** Clean (no issues)

## Self-Check: PASSED

**Created files verified:**
```bash
[ -f ".planning/phases/12-in-progress-visibility/12-02-SUMMARY.md" ] && echo "FOUND" || echo "MISSING"
FOUND
```

**Commits verified:**
```bash
git log --oneline --all | grep -q "3738630" && echo "FOUND: 3738630" || echo "MISSING: 3738630"
FOUND: 3738630

git log --oneline --all | grep -q "7b561c7" && echo "FOUND: 7b561c7" || echo "MISSING: 7b561c7"
FOUND: 7b561c7
```

**Modified files verified:**
```bash
[ -f "internal/http/rest/transmission.go" ] && echo "FOUND: internal/http/rest/transmission.go" || echo "MISSING: internal/http/rest/transmission.go"
FOUND: internal/http/rest/transmission.go

[ -f "internal/http/rest/transmission_test.go" ] && echo "FOUND: internal/http/rest/transmission_test.go" || echo "MISSING: internal/http/rest/transmission_test.go"
FOUND: internal/http/rest/transmission_test.go
```

All artifacts exist and commits are in git history.

## Next Steps

Phase 12 complete! v1.3 Activity Tab Support milestone complete - all in-progress transfers now visible in Sonarr/Radarr Activity tab with accurate status, speed, peer counts, and labels.
