# Project Research Summary

**Project:** .torrent File Content Support for Transmission API Proxy
**Domain:** Webhook-triggered torrent transfer proxy
**Researched:** 2026-02-01
**Confidence:** HIGH

## Executive Summary

This project extends an existing Transmission RPC webhook that proxies torrent transfers to Put.io. Currently it only supports magnet links via the `filename` parameter. The goal is to add support for .torrent file content sent as base64-encoded data in the `metainfo` parameter, enabling compatibility with .torrent-only trackers (specifically amigos-share).

The recommended approach is straightforward: decode base64 MetaInfo, validate the torrent content, and upload to Put.io using `Files.Upload()` instead of `Transfers.Add()`. The Put.io SDK automatically detects .torrent files and creates transfers when the filename ends in `.torrent`. Implementation requires no new dependencies beyond Go standard library (`encoding/base64`, `bytes`), though adding bencode validation (via `jackpal/bencode-go` or `zeebo/bencode`) is strongly recommended to distinguish decoding failures from corrupt torrents. The entire flow operates in-memory with no file persistence, adhering to project constraints.

The primary risk is incorrect base64 variant selection (StdEncoding vs RawStdEncoding vs URLEncoding) causing silent failures. Prevention requires testing with actual Sonarr/Radarr webhooks to verify which encoding they use. Secondary risks include memory exhaustion from large .torrent files (mitigated by request size limits) and breaking backward compatibility with magnet links (prevented by proper control flow with explicit MetaInfo vs FileName precedence). The architecture uses type assertion to access Put.io-specific `UploadTorrent()` method without polluting the `TransferClient` interface, maintaining clean separation between implementations.

## Key Findings

### Recommended Stack

The implementation requires zero new dependencies for core functionality, leveraging Go standard library and the existing Put.io SDK. Base64 decoding uses `encoding/base64.StdEncoding` (RFC 4648 compliant), bytes are wrapped in `bytes.NewReader` for the io.Reader interface, and `github.com/putdotio/go-putio` v1.7.2's `Files.Upload()` method handles the upload. The SDK automatically detects .torrent files by extension and populates the Transfer field in the Upload response. Optional but strongly recommended: add bencode validation with `jackpal/bencode-go` or `zeebo/bencode` to provide specific error messages distinguishing invalid base64 from corrupt torrent structure.

**Core technologies:**
- Go stdlib `encoding/base64`: Decode base64-encoded .torrent content — built-in, RFC 4648 compliant, zero dependencies
- Go stdlib `bytes`: Convert decoded bytes to io.Reader — built-in, efficient for in-memory operations
- `github.com/putdotio/go-putio` v1.7.2: Upload .torrent content via Files.Upload() — already in use, supports auto-detection of torrent files

**Optional validation:**
- `jackpal/bencode-go` or `zeebo/bencode`: Parse torrent structure for validation — enables specific error messages, prevents blind uploads

### Expected Features

The feature set focuses on core .torrent support with strong observability to distinguish magnet vs file handling.

**Must have (table stakes):**
- Parse base64-encoded MetaInfo field from Transmission RPC requests — Transmission spec requires either filename or metainfo
- Upload .torrent content to Put.io — core milestone requirement
- Return Transmission-compatible success/error responses — Sonarr/Radarr expect standard response format
- Support both FileName (magnet) and MetaInfo (.torrent) in same endpoint — maintain backward compatibility
- Explicit error handling for invalid base64, corrupt torrents, upload failures — no silent failures (project core value)

**Should have (operational visibility):**
- Log torrent type explicitly (magnet vs file) — critical for debugging tracker-specific issues
- OpenTelemetry metric for torrent type distribution — track magnet vs .torrent usage patterns
- Log .torrent file size (decoded bytes) — identify bloated .torrent files from certain trackers
- Log Put.io file ID after upload — improves traceability for debugging transfer failures

**Defer (v2+):**
- Watch folder for .torrent files — not how Sonarr/Radarr work, requires new architecture
- Direct .torrent file upload API — adds new API surface, not needed for milestone goal
- Deluge .torrent support — webhook API is Put.io only, Deluge not used in webhook path
- Cache decoded .torrent content — Sonarr/Radarr send each .torrent once, unnecessary complexity

### Architecture Approach

The architecture maintains clean separation between HTTP concerns (handler layer), domain operations (client adapter layer), and external service APIs (Put.io SDK). The handler validates input and transforms base64 to bytes before calling business logic. For .torrent files, a new `UploadTorrent(ctx, torrentData, downloadDir)` method is added to the Put.io client concrete type (NOT the TransferClient interface) to avoid forcing Deluge client to implement unsupported methods. Type assertion is used in the handler to access this Put.io-specific method, failing fast for non-Put.io clients. The entire data flow is in-memory: request → base64 decode → bytes.NewReader → Files.Upload → network, with no intermediate file persistence.

**Major components:**
1. **handleTorrentAdd (transmission.go)** — Validates mutually exclusive FileName/MetaInfo fields, decodes base64, routes to appropriate client method
2. **Put.io Client.UploadTorrent** — Wraps torrent bytes in io.Reader, calls Files.Upload with .torrent filename, extracts Transfer from Upload response
3. **Put.io SDK Files.Upload** — Creates multipart request, auto-detects .torrent extension, returns Upload struct with Transfer field

**Key patterns:**
- **Handler layer validation:** HTTP handlers validate and transform input before calling business logic (base64 decoding is HTTP concern)
- **Type assertion for implementation-specific features:** Use type assertion to access methods not on interface when feature only supported by subset of implementations
- **SDK auto-detection:** Put.io SDK automatically creates transfers when uploading files with .torrent extension
- **Bytes-in-memory:** Process file content entirely in memory without disk writes (explicit project constraint: no file persistence)

### Critical Pitfalls

1. **Base64 Encoding Variant Mismatch** — Go provides four base64 variants (StdEncoding, RawStdEncoding, URLEncoding, RawURLEncoding). The Transmission spec doesn't specify which variant. Using the wrong one causes "invalid or corrupt torrent file" errors even when content is valid. Prevention: capture actual Sonarr/Radarr webhook requests and test which encoding variant they use. Standard Transmission implementations typically use StdEncoding with padding.

2. **Bencode Validation Skipped Before Upload** — Decoded base64 data uploaded to Put.io without validating it's actually valid bencoded torrent content. Put.io rejects the upload with generic "invalid transfer" error, making debugging impossible. Prevention: validate bencode structure after decoding with `bencode.Unmarshal()`, return specific "invalid bencode" error vs "invalid base64" vs "Put.io rejected upload".

3. **MetaInfo vs FileName Field Precedence Undefined** — Transmission spec says "either filename or metainfo must be included" but doesn't specify behavior when both are sent. Current code silently ignores MetaInfo when present, causing nil torrent and panic. Prevention: define explicit precedence (MetaInfo takes priority over FileName, matching Transmission behavior), handle all four field combination cases explicitly.

4. **Backward Compatibility Broken by Control Flow Bug** — When adding MetaInfo handling, nil torrent variable from if/else logic causes existing magnet link code path to break. Prevention: restructure control flow to handle all cases (MetaInfo, FileName, both, neither), add nil check before marshaling torrent response, test magnet links still work with new code deployed.

5. **Silent Failures Without Observability** — No metrics track torrent_type (magnet vs file), making it impossible to tell if .torrent support is broken while magnet metrics look healthy. Prevention: add torrent_type attribute to all metrics and logs immediately with new code, create separate metrics for .torrent-specific failures (decode errors, bencode errors), update Grafana dashboard to show success rate by type.

## Implications for Roadmap

Based on research, suggested phase structure prioritizes foundation (client layer), integration (handler routing), and observability as a first-class concern, not an afterthought.

### Phase 1: Client Layer Foundation
**Rationale:** Build bottom-up starting with Put.io client method before handler integration. Allows independent testing of upload logic without HTTP complexity. Validates that Put.io SDK's Files.Upload() actually works for .torrent content before committing to architecture.

**Delivers:** `UploadTorrent(ctx, torrentData, downloadDir)` method in Put.io client that wraps bytes in io.Reader, calls Files.Upload with .torrent filename, extracts Transfer from response, and maps to domain model.

**Addresses:**
- Upload .torrent content to Put.io (table stakes)
- Return Transfer ID from Put.io response (table stakes)
- Error wrapping with context (operational visibility)

**Avoids:**
- **Pitfall 4 (Put.io API compatibility):** Proves Files.Upload() works for .torrent content before building handler layer
- **Pitfall 6 (memory exhaustion):** Establishes baseline memory usage patterns for testing

**Implementation notes:**
- Reuse existing `findDirectoryID` helper
- Add error handling for Upload.Transfer == nil
- No changes to TransferClient interface
- Unit testable with mock Put.io SDK

### Phase 2: Handler Layer Integration
**Rationale:** Now that client method works, integrate with HTTP handler. This phase handles all the validation, routing, and backward compatibility concerns. Must get control flow right before adding observability, otherwise metrics will be inconsistent.

**Delivers:** Modified `handleTorrentAdd` that validates FileName XOR MetaInfo fields, decodes base64, routes MetaInfo → UploadTorrent vs FileName → AddTransfer, maintains backward compatibility with magnet links.

**Addresses:**
- Parse base64-encoded MetaInfo field (table stakes)
- Support both FileName and MetaInfo in same endpoint (table stakes)
- Explicit error handling for invalid base64, corrupt torrents (table stakes)
- Return Transmission-compatible responses (table stakes)

**Avoids:**
- **Pitfall 1 (base64 variant mismatch):** Test with real Sonarr/Radarr webhooks to verify StdEncoding works
- **Pitfall 2 (bencode validation skipped):** Add bencode.Unmarshal() validation between decode and upload
- **Pitfall 3 (field precedence undefined):** MetaInfo takes explicit priority over FileName, all four cases handled
- **Pitfall 5 (backward compatibility broken):** Restructure if/else to handle all cases, test magnet links unchanged

**Implementation notes:**
- Add `encoding/base64` import
- Add type assertion to `*putio.Client` for UploadTorrent access
- Add bencode validation (optional but strongly recommended)
- Return specific errors: "invalid base64" vs "invalid bencode" vs "upload failed"

### Phase 3: Observability & Instrumentation
**Rationale:** With functional implementation complete, add comprehensive observability to enable production debugging and measure .torrent adoption. This is not an afterthought but a first-class feature requirement (project core value: no silent failures).

**Delivers:** OpenTelemetry metrics, structured logging, and Grafana dashboard updates to distinguish magnet vs .torrent handling.

**Addresses:**
- Log torrent type explicitly (operational visibility)
- OpenTelemetry metric for torrent type distribution (operational visibility)
- Log .torrent file size (operational visibility)
- Log Put.io file ID after upload (operational visibility)

**Avoids:**
- **Pitfall 7 (silent failures without observability):** Metrics track torrent_type separately, errors categorized by type and reason, Grafana shows success rate by type

**Implementation notes:**
- Add counter metric: `torrent_add_total{type="magnet"|"file"}`
- Add error metric: `torrent_add_errors_total{type="...", reason="..."}`
- Add histogram: `torrent_file_size_bytes`
- Update Grafana dashboard with panels for type distribution, success rate by type, decode failure rate
- Add OTEL span attributes: `torrent.type`, `torrent.size_bytes`

### Phase Ordering Rationale

- **Bottom-up implementation:** Client layer before handler ensures each component is independently testable
- **Validation with integration:** Handler phase combines routing and validation, preventing partial implementations
- **Observability as feature:** Separate phase ensures metrics aren't skipped as "nice to have"
- **Dependency order:** Handler depends on client method existing, observability depends on routing logic
- **Risk mitigation:** Each phase addresses specific pitfalls, preventing compounding errors

### Research Flags

**Phases with standard patterns (skip research-phase):**
- **Phase 1 (Client Layer):** Well-documented Put.io SDK, Files.Upload() method verified from source code
- **Phase 2 (Handler Layer):** Standard HTTP validation patterns, base64 decoding is Go stdlib
- **Phase 3 (Observability):** Existing telemetry infrastructure in place, add to established patterns

**No phases require deeper research.** All critical integration points verified from official sources (Put.io SDK source code, Transmission RPC spec, Go stdlib docs). Base64 variant testing with real Sonarr/Radarr webhooks is implementation verification, not research.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All components verified from official sources: Go stdlib docs, Put.io SDK source code at v1.7.2 line 215, zero new dependencies required |
| Features | HIGH | Transmission RPC spec explicitly defines MetaInfo field format, Sonarr/Radarr behavior documented in community wikis, feature set validated against real-world usage |
| Architecture | HIGH | Existing codebase provides clear patterns, Put.io SDK method signatures verified from source, type assertion pattern is standard Go practice |
| Pitfalls | HIGH | All pitfalls identified from official documentation (Transmission RPC spec, Put.io SDK behavior), community forums (Transmission forum on MetaInfo usage), and industry best practices (backward compatibility, observability) |

**Overall confidence:** HIGH

All critical paths verified from authoritative sources. Put.io SDK behavior confirmed from actual source code (`/Users/italovietro/go/pkg/mod/github.com/putdotio/go-putio@v1.7.2/files.go:215`). Transmission RPC spec is official protocol documentation. Base64 decoding is Go standard library with stable API since Go 1.0.

### Gaps to Address

**Base64 encoding variant verification (minor):** Transmission spec doesn't explicitly state which base64 variant (Std/Raw/URL). Research indicates StdEncoding is standard, but must verify with actual Sonarr/Radarr webhook during implementation. This is implementation verification, not a research gap.

**Put.io error response format (minor):** Research shows Put.io returns generic errors when upload fails, but exact error response structure for invalid .torrent content not documented. During implementation, test with malformed .torrent to capture actual error response format for proper error handling.

**Large .torrent memory profile (minor):** Research identifies memory exhaustion risk for 50MB+ .torrent files, but exact memory multiplier (base64 string → decoded bytes → reader) not profiled. Add memory instrumentation during Phase 3 to measure actual overhead under load.

**Bencode validation library choice (low priority):** Research identifies two options (`jackpal/bencode-go`, `zeebo/bencode`) but doesn't benchmark performance or API ergonomics. Choose during implementation based on simplest API for validation-only use case (don't need full torrent parsing).

## Sources

### PRIMARY (HIGH confidence)

**Official Documentation:**
- [Transmission RPC Specification](https://github.com/transmission/transmission/blob/main/docs/rpc-spec.md) — MetaInfo field format, torrent-add method, response format
- [Go encoding/base64 package](https://pkg.go.dev/encoding/base64) — StdEncoding, RawStdEncoding variants, DecodeString API
- [Go bytes package](https://pkg.go.dev/bytes) — NewReader method for io.Reader conversion
- [github.com/putdotio/go-putio package](https://pkg.go.dev/github.com/putdotio/go-putio/putio) — FilesService.Upload() documentation, Upload type definition

**Source Code:**
- `/Users/italovietro/go/pkg/mod/github.com/putdotio/go-putio@v1.7.2/files.go:215` — Files.Upload() method implementation showing torrent auto-detection
- `/Users/italovietro/projects/seedbox_downloader/internal/http/rest/transmission.go` — Current handleTorrentAdd implementation (lines 230-260)
- `/Users/italovietro/projects/seedbox_downloader/internal/dc/putio/client.go` — Current Put.io client showing AddTransfer pattern
- `/Users/italovietro/projects/seedbox_downloader/.planning/PROJECT.md` — Project constraints (no file persistence line 96)

### SECONDARY (MEDIUM confidence)

**Community Documentation:**
- [Sonarr Download Clients](https://buildarr.github.io/plugins/sonarr/configuration/download-clients/) — Sonarr behavior with download clients
- [Sonarr Settings](https://wiki.servarr.com/sonarr/settings) — Official Servarr wiki on download client configuration
- [Transmission Forum: MetaInfo Usage](https://forum.transmissionbt.com/viewtopic.php?t=8023) — Community discussion confirming MetaInfo field usage
- [Transmission Forum: Base64 Encoding](https://forum.transmissionbt.com/viewtopic.php?t=9289) — Community discussion on encoding issues

**Architecture Patterns:**
- [OpenTelemetry Metrics](https://opentelemetry.io/docs/concepts/signals/metrics/) — Official OTel metrics concepts
- [Go REST API Architecture](https://medium.com/@janishar.ali/how-to-architecture-good-go-backend-rest-api-services-14cc4730c05b) — Layer responsibility patterns
- [Clean Architecture in Go](https://depshub.com/blog/clean-architecture-in-go/) — Interface segregation patterns

### TERTIARY (LOW confidence, referenced for context)

**Bencode Libraries:**
- [jackpal/bencode-go](https://github.com/jackpal/bencode-go) — Popular Go bencode implementation
- [zeebo/bencode](https://zeebo.github.io/bencode/) — Alternative Go bencode library
- [anacrolix/torrent/bencode](https://pkg.go.dev/github.com/anacrolix/torrent/bencode) — Full-featured torrent library (overkill for validation-only)

**Error Handling Best Practices:**
- [Zalando API Guidelines: Compatibility](https://github.com/zalando/restful-api-guidelines/blob/main/chapters/compatibility.adoc) — Industry standards for backward compatibility
- [Google AIP-180](https://google.aip.dev/180) — Backwards compatibility patterns
- [Spacelift: Observability Best Practices 2026](https://spacelift.io/blog/observability-best-practices) — Current observability patterns

---
*Research completed: 2026-02-01*
*Ready for roadmap: yes*
