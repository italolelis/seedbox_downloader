# Pitfalls Research

**Domain:** Adding .torrent file support to Transmission API proxy
**Researched:** 2026-02-01
**Confidence:** HIGH

## Critical Pitfalls

### Pitfall 1: Base64 Encoding Variant Mismatch

**What goes wrong:**
The Transmission RPC specification doesn't explicitly state which base64 encoding variant to use (StdEncoding vs RawStdEncoding vs URLEncoding). Using the wrong variant causes "invalid or corrupt torrent file" errors even when the .torrent content is valid.

**Why it happens:**
Go's `encoding/base64` provides four variants (StdEncoding, RawStdEncoding, URLEncoding, RawURLEncoding). The Transmission RPC spec simply says "base64-encoded" without specifying padding or URL-safety. Developers default to `base64.StdEncoding.DecodeString()` without verifying what Sonarr/Radarr actually send.

**Consequences:**
- Sonarr/Radarr webhooks fail silently
- Error messages say "corrupt torrent" when the problem is decoding
- Magnet links continue working, creating confusion about what's broken
- No .torrent downloads work despite correct implementation elsewhere

**Prevention:**
1. **Verify with real Sonarr/Radarr payloads:** Capture actual webhook requests and test which encoding variant they use
2. **Check for padding:** Standard base64 uses `=` padding, raw variants don't
3. **Log the raw MetaInfo length:** Before decoding, log string length to detect padding/truncation
4. **Try standard first:** Transmission implementations typically use `StdEncoding` with padding
5. **Add explicit error handling:**
```go
decoded, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
if err != nil {
    logger.Error("base64 decode failed", "error", err, "metainfo_length", len(req.Arguments.MetaInfo))
    return nil, fmt.Errorf("invalid base64 encoding in metainfo field: %w", err)
}
```

**Warning signs:**
- All .torrent uploads fail with "corrupt" or "invalid" errors
- Error occurs immediately on decode, not during bencode parsing
- Error message: "illegal base64 data at input byte X"
- Magnet links work perfectly but .torrent files fail 100% of the time

**Phase to address:**
Phase 1 (Base64 Decoding Implementation) — Must verify encoding variant before building further

---

### Pitfall 2: Bencode Validation Skipped Before Upload

**What goes wrong:**
Decoded base64 data is uploaded to Put.io without validating it's actually valid bencoded torrent content. Put.io rejects the upload, but the error message is generic ("invalid transfer") without indicating the torrent was malformed.

**Why it happens:**
The temptation is to decode base64 and immediately pass to Put.io's API since "validation is Put.io's job." However, Put.io's error responses don't distinguish between malformed torrents, network issues, quota problems, or API errors.

**Consequences:**
- Debugging becomes impossible — can't tell if issue is encoding, corruption, or Put.io rejection
- Users see "transfer failed" with no actionable information
- No way to distinguish between your bug and Put.io's problem
- Support burden increases as every failure looks the same

**Prevention:**
1. **Validate bencode structure after decoding:**
```go
import "github.com/jackpal/bencode-go" // or zeebo/bencode

decoded, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
if err != nil {
    return nil, fmt.Errorf("base64 decode failed: %w", err)
}

// Validate bencode structure
var metainfo interface{}
if err := bencode.Unmarshal(decoded, &metainfo); err != nil {
    logger.Error("invalid bencode in torrent", "error", err, "size_bytes", len(decoded))
    return nil, fmt.Errorf("torrent content is not valid bencode: %w", err)
}

logger.Debug("validated torrent bencode", "size_bytes", len(decoded))
```

2. **Check for required torrent fields:** Minimally verify `announce` or `announce-list` exists
3. **Log validation checkpoints:** Separate log statements for "decoded", "validated bencode", "uploaded to Put.io"
4. **Return specific error messages:** "invalid base64" vs "corrupt bencode" vs "Put.io rejected upload"

**Warning signs:**
- Put.io API returns generic errors
- Can't reproduce failures locally
- Error rate doesn't correlate with Put.io status
- Same torrent works in other clients but not yours

**Phase to address:**
Phase 1 (Base64 Decoding Implementation) — Validation must be added immediately with decode logic

---

### Pitfall 3: MetaInfo vs FileName Field Precedence Undefined

**What goes wrong:**
The Transmission RPC spec states "either filename or metainfo must be included" but doesn't specify what happens when both are sent. Your implementation silently ignores MetaInfo when it's present, always using FileName, breaking .torrent-only trackers.

**Why it happens:**
The current code at line 235 of transmission.go checks `if req.Arguments.MetaInfo == ""` and only handles the else (magnet link) case. When Sonarr sends both fields (common behavior), the code falls through with nil torrent, causing a panic or silent failure.

**Consequences:**
- .torrent uploads silently ignored if FileName also populated
- Different behavior from real Transmission client
- Sonarr/Radarr may send both fields for compatibility
- Panic on nil dereference when marshaling torrent response

**Prevention:**
1. **Define explicit precedence:** MetaInfo should take precedence over FileName (matches Transmission behavior)
2. **Handle all four cases explicitly:**
```go
if req.Arguments.MetaInfo != "" {
    // .torrent file content (new path)
    logger.Debug("received torrent add with metainfo", "filename_also_present", req.Arguments.FileName != "")
    torrent, err = h.handleTorrentFileUpload(ctx, req.Arguments.MetaInfo, h.label)
    if err != nil {
        return nil, fmt.Errorf("failed to upload torrent file: %w", err)
    }
} else if req.Arguments.FileName != "" {
    // Magnet link (existing path)
    logger.Debug("received torrent add with filename (magnet link)")
    torrent, err = h.dc.AddTransfer(ctx, req.Arguments.FileName, h.label)
    if err != nil {
        return nil, fmt.Errorf("failed to add transfer: %w", err)
    }
} else {
    return nil, fmt.Errorf("torrent-add requires either metainfo or filename field")
}
```

3. **Log when both fields present:** Important for debugging Sonarr/Radarr behavior
4. **Test with both fields:** Explicitly test case where MetaInfo and FileName both non-empty
5. **Document precedence:** Add comment explaining MetaInfo takes priority

**Warning signs:**
- .torrent downloads never trigger despite MetaInfo field populated
- Nil pointer dereferences in torrent marshaling
- Logs show "received torrent add magnet link" even for .torrent files
- Integration tests pass but real Sonarr/Radarr requests fail

**Phase to address:**
Phase 1 (Base64 Decoding Implementation) — Must fix control flow before implementing MetaInfo handling

---

### Pitfall 4: Put.io API Doesn't Support Torrent Content Upload

**What goes wrong:**
Put.io's `Transfers.Add()` method signature is `Add(ctx, urlStr string, parent int64, callbackURL string)` — it only accepts URLs (magnet links or HTTP URLs to .torrent files), not raw torrent file content. There's no API to upload base64-decoded .torrent bytes directly.

**Why it happens:**
Assumption that because Transmission supports MetaInfo field, Put.io must support torrent content upload. The go-putio library and API documentation show only URL-based transfer creation. The "no file persistence" constraint conflicts with Put.io's API design.

**Consequences:**
- **BLOCKING ISSUE:** Cannot implement .torrent support without file persistence or external hosting
- Entire v1.1 milestone may be infeasible under current architecture constraints
- Need to either:
  1. Violate "no file persistence" constraint by temporarily saving .torrent files
  2. Find alternative Put.io API endpoint for direct content upload (may not exist)
  3. Host .torrent files externally and pass URLs to Put.io
  4. Scope v1.1 to Deluge only (if Deluge client supports torrent content)

**Prevention:**
1. **Verify Put.io API capabilities FIRST:** Check official API docs for torrent content upload
2. **Research go-putio methods:** Look for `AddTorrent()` or content-based upload methods
3. **Prototype proof-of-concept:** Test with real Put.io API before committing to roadmap
4. **Challenge constraints:** Question "no file persistence" if it blocks critical feature
5. **Consider temporary files:** If Put.io requires URLs, use temp files with immediate cleanup:
```go
// Only if Put.io API forces this approach
tmpfile, err := os.CreateTemp("", "torrent-*.torrent")
if err != nil {
    return nil, fmt.Errorf("failed to create temp file: %w", err)
}
defer os.Remove(tmpfile.Name()) // Cleanup even on panic
defer tmpfile.Close()

if _, err := tmpfile.Write(torrentBytes); err != nil {
    return nil, fmt.Errorf("failed to write temp file: %w", err)
}

// Upload to Put.io via file URL or multipart upload
```

6. **Investigate Put.io Files API:** Check if Files.Upload() can be used for .torrent files
7. **Consult Put.io support:** Ask if direct torrent content upload endpoint exists

**Warning signs:**
- go-putio library has no method accepting []byte for transfers
- Put.io API documentation only shows URL-based examples
- Official clients use file uploads for .torrent handling
- Research phase didn't verify Put.io API compatibility

**Phase to address:**
**IMMEDIATE — Before Phase 1** — This is a feasibility blocker. Must research and resolve before proceeding with implementation.

---

### Pitfall 5: Backward Compatibility Broken by Control Flow Bug

**What goes wrong:**
When adding MetaInfo handling, the nil torrent variable from the if/else logic causes the existing magnet link code path to break. The `json.Marshal` at line 250 tries to marshal a nil torrent when MetaInfo is empty but before FileName is processed.

**Why it happens:**
The current code structure has `var torrent *transfer.Transfer` declared, then only assigns it in the `if MetaInfo == ""` block. If MetaInfo is populated but the new code panics/errors, torrent remains nil. The marshal code doesn't check for nil before encoding.

**Consequences:**
- Magnet links stop working after deploying .torrent support
- "Success" response returned but with null torrent data
- Sonarr/Radarr think upload succeeded but have no transfer ID
- Breaking change violates v1.1 requirement "existing magnet link support must continue working"

**Prevention:**
1. **Restructure control flow to handle all cases:**
```go
var torrent *transfer.Transfer
var err error

if req.Arguments.MetaInfo != "" {
    torrent, err = h.handleTorrentFileUpload(ctx, req.Arguments.MetaInfo, h.label)
} else if req.Arguments.FileName != "" {
    torrent, err = h.dc.AddTransfer(ctx, req.Arguments.FileName, h.label)
} else {
    return nil, fmt.Errorf("either metainfo or filename is required")
}

if err != nil {
    return nil, fmt.Errorf("failed to add torrent: %w", err)
}

if torrent == nil {
    return nil, fmt.Errorf("torrent creation returned nil without error")
}
```

2. **Add nil check before marshal:**
```go
if torrent == nil {
    logger.Error("torrent is nil after creation")
    return nil, fmt.Errorf("internal error: torrent creation failed")
}
```

3. **Write backward compatibility test:** Test magnet links still work with new code deployed
4. **Test empty MetaInfo field:** Ensure empty string MetaInfo doesn't break magnet path
5. **Use table-driven tests:** Test all combinations of MetaInfo/FileName populated/empty

**Warning signs:**
- Existing integration tests start failing
- JSON responses contain `"torrents": [null]`
- Logs show "received torrent add" but no "transfer added" log
- Sonarr/Radarr show "Unknown error" for both magnet and .torrent uploads

**Phase to address:**
Phase 1 (Base64 Decoding Implementation) — Fix control flow before adding new features

---

### Pitfall 6: Large .torrent Files Cause Memory Exhaustion

**What goes wrong:**
Base64 decoding loads entire .torrent file into memory. For large torrents (1000+ files, 100MB+ .torrent file), base64 string in JSON request is 133MB+, decoded bytes are 100MB+, all held in RAM during request processing.

**Why it happens:**
HTTP handler decodes entire JSON request body into memory (line 134). MetaInfo string fully loaded. Base64 decode creates second copy. Bencode validation creates third copy. Under load with concurrent requests, memory usage spikes.

**Consequences:**
- OOM kills in Docker containers with memory limits
- Slow request processing under concurrent load
- Connection timeouts as GC struggles
- 5 parallel downloads + 1 large torrent upload = 500MB+ memory spike
- Docker healthchecks fail during memory pressure

**Prevention:**
1. **Set request body size limit:**
```go
r.Body = http.MaxBytesReader(w, r.Body, 200*1024*1024) // 200MB max
```

2. **Monitor memory in metrics:**
```go
// In handleTorrentAdd
memBefore := runtime.MemStats{}
runtime.ReadMemStats(&memBefore)

decoded, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)

memAfter := runtime.MemStats{}
runtime.ReadMemStats(&memAfter)
logger.Debug("torrent decode memory", "allocated_mb", (memAfter.Alloc-memBefore.Alloc)/1024/1024)
```

3. **Document size limits:** In API docs, warn about .torrent file size limits
4. **Add observability:** Track `torrent_file_size_bytes` histogram
5. **Consider streaming (future):** For very large torrents, stream decode in chunks (complex, defer to later)
6. **Release memory explicitly:**
```go
decoded, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
if err != nil {
    return nil, err
}
defer func() { decoded = nil }() // Help GC
```

7. **Load test with realistic sizes:** Test with 50MB+ .torrent files under concurrent load

**Warning signs:**
- Docker container OOM kills after deploying v1.1
- Memory usage spikes correlate with .torrent uploads
- Request timeouts on large .torrent files but not small ones
- Works in testing but fails in production under load
- Healthcheck failures during torrent uploads

**Phase to address:**
Phase 2 (Put.io Integration) — Add memory monitoring and limits alongside upload logic

---

### Pitfall 7: Silent Failures Without Observability

**What goes wrong:**
When .torrent upload fails, existing magnet link code path executes silently, or error is logged but metrics don't distinguish between magnet and .torrent failures. Operators can't tell if .torrent support is broken because magnet metrics look healthy.

**Why it happens:**
No metrics track `torrent_type` (magnet vs file). Error logs say "failed to add transfer" without context. OpenTelemetry spans don't differentiate request types. Grafana dashboards show transfer success/failure but can't filter by torrent type.

**Consequences:**
- .torrent support breaks in production, no alerts fire
- "Everything looks fine" in dashboards while .torrent users suffer
- Can't measure .torrent adoption rate
- Can't debug which failure mode is occurring (decode vs bencode vs Put.io rejection)
- No visibility into magnet vs .torrent success rates

**Prevention:**
1. **Add torrent_type attribute to all metrics:**
```go
// In handleTorrentAdd
torrentType := "magnet"
if req.Arguments.MetaInfo != "" {
    torrentType = "file"
}

ctx = logctx.With(ctx, "torrent_type", torrentType)
logger := logctx.LoggerFromContext(ctx)

// Add to OTEL span
span := trace.SpanFromContext(ctx)
span.SetAttributes(attribute.String("torrent.type", torrentType))
```

2. **Create separate metrics for .torrent handling:**
```go
torrentFileDecodeErrors := metric.Must(meter).NewInt64Counter("torrent.file.decode.errors")
torrentFileBencodeErrors := metric.Must(meter).NewInt64Counter("torrent.file.bencode.errors")
torrentFileSizeBytes := metric.Must(meter).NewInt64Histogram("torrent.file.size.bytes")
```

3. **Update Grafana dashboard:** Add panels showing:
   - Transfer success rate by torrent_type
   - .torrent decode failure rate
   - .torrent file size distribution
   - Magnet vs .torrent usage ratio

4. **Add debug logs for happy path:**
```go
if req.Arguments.MetaInfo != "" {
    logger.Debug("processing torrent file", "size_base64", len(req.Arguments.MetaInfo))
    // decode
    logger.Debug("decoded torrent file", "size_bytes", len(decoded))
    // validate
    logger.Debug("validated torrent bencode")
    // upload
    logger.Info("uploaded torrent file to Put.io", "transfer_id", transfer.ID)
} else {
    logger.Debug("processing magnet link", "length", len(req.Arguments.FileName))
    logger.Info("added magnet link to Put.io", "transfer_id", transfer.ID)
}
```

5. **Explicit error log attributes:**
```go
logger.Error("failed to decode torrent file base64",
    "error", err,
    "metainfo_length", len(req.Arguments.MetaInfo),
    "torrent_type", "file",
)
```

**Warning signs:**
- Can't answer "How many .torrent uploads succeeded?"
- Dashboard shows healthy transfers but users report .torrent failures
- No way to alert on .torrent-specific errors
- Debugging requires correlation across multiple log statements
- Can't measure v1.1 feature adoption

**Phase to address:**
Phase 1 (Base64 Decoding Implementation) — Add observability immediately with new code, not as afterthought

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Skip bencode validation | Faster implementation | Impossible debugging, poor error messages | Never — validation cost is tiny vs debugging cost |
| No separate error types | Less code to write | Can't distinguish failure modes in metrics/alerts | Never — use wrapped errors with context |
| Defer observability | Ship feature faster | Can't measure adoption or debug production issues | Never — observability is part of feature definition |
| Assume StdEncoding | Avoid testing variants | Silent failures if Sonarr uses different encoding | Only if verified with real webhook payloads |
| Parse torrent metadata | Rich validation | Memory overhead, complex dependencies | Only if size limits fail and need whitelist by tracker |
| Use temporary files | Workaround Put.io API | Violates architecture constraint, disk I/O overhead | Only if Put.io API has no content upload endpoint |

## Integration Gotchas

Common mistakes when connecting to external services.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Put.io Transfers.Add() | Assume accepts torrent content | Verify API accepts URL-only, may need file hosting |
| Sonarr/Radarr webhook | Assume MetaInfo field is always populated | Handle all combinations of MetaInfo/FileName fields |
| Base64 decoding | Use StdEncoding without verification | Capture real webhook, test which encoding variant used |
| Put.io error responses | Return generic "transfer failed" | Parse error response, return specific message (quota vs invalid torrent) |
| Bencode validation | Rely on Put.io to reject invalid torrents | Validate locally, return clear error before API call |

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Loading full .torrent into memory | OOM kills, slow GC | Set request size limits, monitor memory | First 50MB+ torrent under concurrent load |
| No base64 decode timeout | Hung requests | Use request context with timeout | Malformed base64 causes decode to hang |
| Synchronous torrent upload | Webhook timeout | Return 202 Accepted, process async (future) | First slow Put.io API response under load |
| Unbounded concurrent uploads | Memory and connection exhaustion | Reuse existing semaphore for upload concurrency | First 10 simultaneous .torrent webhooks |

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| No MetaInfo size limit | Memory exhaustion DoS | Set max request body size (200MB) and reject larger |
| Decode without validation | Malicious bencode crashes parser | Validate bencode structure before uploading |
| Log full torrent content | Sensitive tracker URLs in logs | Log only size/hash, not content |
| No rate limiting on webhook | Flood attack via webhook endpoint | Add rate limiting to transmission RPC endpoint |
| Torrent content in error messages | Leak tracker URLs in error responses | Return generic errors, log details server-side |

## UX Pitfalls

Common user experience mistakes in this domain.

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Generic "invalid torrent" error | User doesn't know if problem is their .torrent or our bug | Return specific errors: "base64 decode failed" vs "bencode invalid" vs "Put.io rejected" |
| No indication of .torrent vs magnet | User can't tell if .torrent support is working | Log torrent type at Info level, visible in Sonarr/Radarr |
| Silent fallback to magnet | User thinks .torrent uploaded but magnet link used instead | Error if MetaInfo present but can't process, don't fall back silently |
| No feedback on large uploads | User waits, wonders if it's stuck | Log "processing large torrent file (XMB)" at Info level |
| Timeout without status | Webhook times out, user sees generic error | Return 202 Accepted immediately, process async (future enhancement) |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **Base64 decoding:** Did you verify which encoding variant (Std/Raw/URL) Sonarr uses?
- [ ] **Bencode validation:** Did you parse and validate structure, not just decode base64?
- [ ] **Nil checks:** Did you add nil check before marshaling torrent response?
- [ ] **Both fields present:** Did you test MetaInfo + FileName both populated?
- [ ] **Put.io API verification:** Did you confirm Put.io accepts torrent content, not just URLs?
- [ ] **Backward compatibility:** Did you test magnet links still work after changes?
- [ ] **Error specificity:** Do errors distinguish decode/bencode/upload failure modes?
- [ ] **Observability:** Do metrics track torrent_type separately for magnet vs file?
- [ ] **Memory limits:** Did you set max request body size and test large torrents?
- [ ] **Load testing:** Did you test concurrent .torrent uploads under memory limits?
- [ ] **Grafana dashboard:** Can operators see .torrent success rate separately from magnets?
- [ ] **Documentation:** Did you document size limits and error conditions?

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Wrong base64 encoding variant | LOW | Change to correct variant, redeploy, no data loss |
| Skipped bencode validation | MEDIUM | Add validation, redeploy, investigate Put.io rejections |
| Broken backward compatibility | HIGH | Emergency hotfix to restore magnet support, separate .torrent fix |
| Put.io API incompatibility | HIGH | Either accept temp file constraint or defer v1.1 until alternative found |
| Memory exhaustion OOM | MEDIUM | Add request size limits, increase memory limits, redeploy |
| No observability | LOW | Add metrics, redeploy, can't recover historical data but future is observable |
| Nil torrent panic | MEDIUM | Add nil checks, redeploy with panic recovery already in place from v1 |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Base64 encoding variant | Phase 0: Research (IMMEDIATE) | Test with real Sonarr webhook, all variants decode successfully |
| Bencode validation skipped | Phase 1: Decode & Validate | Test with malformed bencode, specific error returned |
| MetaInfo/FileName precedence | Phase 1: Decode & Validate | Test all 4 combinations, MetaInfo takes priority |
| Put.io API compatibility | Phase 0: Research (IMMEDIATE) | Prove torrent content upload works or document temp file requirement |
| Backward compatibility broken | Phase 1: Decode & Validate | Existing magnet link tests pass unchanged |
| Memory exhaustion | Phase 2: Integration | Load test with 50MB torrent + 5 concurrent downloads, no OOM |
| Silent failures | Phase 1: Decode & Validate | Grafana shows separate success rates for magnet vs .torrent |

## Sources

**Base64 and Transmission RPC:**
- [Transmission RPC Specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md) - Official RPC spec
- [Transmission Forum: Adding .torrent via metainfo](https://forum.transmissionbt.com/viewtopic.php?t=8023) - Community discussion on MetaInfo field
- [Transmission Forum: Base64 encoding](https://forum.transmissionbt.com/viewtopic.php?t=9289) - Encoding issues with MetaInfo
- [Go base64 package documentation](https://pkg.go.dev/encoding/base64) - Official Go base64 encodings

**Bencode and Torrent Validation:**
- [GeekDashboard: Invalid Bencoding](https://www.geekdashboard.com/unable-to-load-torrent-is-not-valid-bencoding/) - Common bencode errors
- [torrentcheck](https://github.com/Network-BEncode-inside/torrentcheck) - Torrent validation tool
- [zeebo/bencode](https://zeebo.github.io/bencode/) - Go bencode library
- [jackpal/bencode-go](https://github.com/jackpal/bencode-go) - Popular Go bencode implementation

**Memory Management:**
- [qBittorrent Issue #21063](https://github.com/qbittorrent/qBittorrent/issues/21063) - Large torrent memory usage
- [libtorrent tuning reference](https://www.libtorrent.org/tuning-ref.html) - Memory management patterns
- [anacrolix/torrent/bencode](https://pkg.go.dev/github.com/anacrolix/torrent/bencode) - Go torrent library with memory limits

**API Backward Compatibility:**
- [Zalando API Guidelines: Compatibility](https://github.com/zalando/restful-api-guidelines/blob/main/chapters/compatibility.adoc) - Industry standards
- [Google AIP-180](https://google.aip.dev/180) - Backwards compatibility patterns
- [Zuplo: API Backward Compatibility](https://zuplo.com/learning-center/api-versioning-backward-compatibility-best-practices) - Best practices

**Put.io Integration:**
- [go-putio package](https://pkg.go.dev/github.com/putdotio/go-putio) - Official Go client API reference
- [Put.io Help Center](http://help.put.io/en/articles/726124-how-do-i-make-put-io-fetch-a-torrent-for-me-right-now) - Transfer creation documentation

**Observability:**
- [Spacelift: Observability Best Practices 2026](https://spacelift.io/blog/observability-best-practices) - Current observability patterns
- [Data Observability Tools 2026](https://www.ovaledge.com/blog/data-observability-tools/) - Silent failure detection

---
*Pitfalls research for: Adding .torrent file support to Transmission API proxy*
*Researched: 2026-02-01*
