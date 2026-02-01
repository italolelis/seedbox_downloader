# Feature Research: Logging Improvements

**Domain:** Production Go service observability (24/7 operations)
**Researched:** 2026-02-01
**Confidence:** HIGH

## Feature Landscape

### Table Stakes (Users Expect These)

Features operators assume exist in a production service. Missing these = service is hard to operate.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Structured JSON logging | Standard for production systems, machine-parseable, integrates with log aggregators (Datadog, Grafana, etc.) | LOW | **Already implemented** via slog with JSONHandler |
| Consistent log levels | Operators filter by severity; inconsistent levels create noise and hide signals | MEDIUM | Partial - needs audit of existing log.Info/Debug/Warn/Error usage |
| Lifecycle logging | Operators need to see: startup sequence, readiness, shutdown gracefully | MEDIUM | Partial - main.go has basic startup, needs clear component initialization order |
| Error context | Every error needs: what failed, why, what was being attempted | LOW | Present but inconsistent - some errors have transfer_id, some don't |
| Request correlation | Follow a single entity (torrent/transfer) through entire pipeline | HIGH | Missing - no request_id or correlation_id for torrent flow tracking |
| Trace context in logs | Logs should include trace_id/span_id from OpenTelemetry spans for cross-system correlation | MEDIUM | Missing - existing OTel integration lacks slog bridge |
| Log level configurability | Change verbosity at runtime or via env var without restart | LOW | **Already implemented** via LOG_LEVEL env var and slog.LevelVar |
| Operation logging | Key operations (download start, import complete, cleanup) must be visible at INFO | LOW | Partial - exists but mixed with noise |
| Error stack traces | Critical errors (panics) need stack traces; regular errors don't | LOW | **Already implemented** for panics via debug.Stack() |

### Differentiators (Competitive Advantage)

Features that make logs **excellent** for operators. Not required, but transform debugging experience.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Transfer lifecycle narrative | Single grep for transfer_id shows complete story: discovered → claimed → downloading → imported → cleaned up | MEDIUM | Requires consistent transfer_id logging across all components |
| Startup sequence reporting | Clear phases: "Config loaded → Database validated → Client authenticated → Server listening → Ready" | LOW | Makes troubleshooting startup failures trivial |
| Operation grouping | Logs use consistent "operation" field (e.g., "produce_transfers", "watch_imported") to group related log lines | LOW | Partially exists in panic handlers, needs expansion |
| Progress indicators | Long operations (file downloads, polling) log periodic progress | LOW | Partially exists - download progress at DEBUG level |
| State transitions | Explicit logs when transfers change state (queued → downloading → downloaded → imported) | LOW | Exists via channels, could be more explicit |
| Resource utilization | Log active download count, goroutine count at intervals | MEDIUM | Metrics exist via OTel, could add periodic INFO logs |
| Silent operations visibility | Periodic "heartbeat" log shows service is alive even when idle | LOW | No polling activity logged at INFO when no transfers found |
| Component readiness | Each component logs when ready: "TransferOrchestrator ready", "Downloader ready", "API server listening" | LOW | Missing explicit readiness logs |
| Dependency health checks | Log success/failure of external dependencies: Deluge/Put.io, Sonarr/Radarr, Database | MEDIUM | Auth logs exist, ongoing health unclear |
| Log sampling for high-volume | Debug logs that fire per-file should be sampled to avoid noise | MEDIUM | Currently no sampling - could flood at DEBUG level |

### Anti-Features (Commonly Requested, Often Problematic)

Features that seem good but create problems in production.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Log every file download at INFO | "I want to see progress" | Generates 100+ logs per transfer for multi-file torrents, drowns signal in noise | Log transfer-level events at INFO (start, complete), per-file at DEBUG |
| Log every polling tick | "I want to know service is working" | Creates noise every 10 minutes even when nothing happens, obscures real events | Log "watching transfers" at INFO only when transfers found; heartbeat log every hour when idle |
| Duplicate logs in multiple formats | "I want human-readable AND JSON" | Doubles log volume, complicates aggregation, slows I/O | Use JSON in production (betterstack.com best practice), text locally via LOG_LEVEL |
| Log request/response bodies | "I want to debug API issues" | Exposes credentials (API keys, tokens), massive log volume, PII concerns | Log operation metadata (method, status, duration) not payloads; use sampling for debugging |
| WARN level for expected conditions | "I want to highlight important info" | Triggers false alerts, creates alarm fatigue, WARN should mean action needed | Use INFO for normal operations, WARN only for conditions requiring investigation |
| Pass loggers in context.Context | "I want logger available everywhere" | Creates implicit runtime dependency, tight coupling, not compiler-enforced (Dave Cheney anti-pattern) | Inject logger as explicit dependency, use logctx only for enrichment |
| log.Fatal in libraries/components | "I want to stop on error" | Prevents graceful shutdown, no cleanup, same as panic (Dave Cheney warning) | Return errors, let main() decide to exit |

## Feature Dependencies

```
Trace Context in Logs
    └──requires──> OpenTelemetry spans (already exists)
    └──requires──> slog handler wrapper (otelslog bridge)

Request Correlation
    └──requires──> Trace Context in Logs
    └──enhances──> Transfer lifecycle narrative

Transfer Lifecycle Narrative
    └──requires──> Consistent log levels
    └──requires──> Transfer_id in all logs
    └──enhances──> Operation grouping

Startup Sequence Reporting
    └──requires──> Component readiness
    └──conflicts──> Silent operations (startup should be explicit)

Log Sampling
    └──requires──> Consistent log levels (only sample DEBUG)
    └──conflicts──> Transfer lifecycle narrative (don't sample correlation logs)
```

### Dependency Notes

- **Trace Context requires OpenTelemetry spans:** Already exists, just need bridge handler (go.opentelemetry.io/contrib/bridges/otelslog)
- **Request Correlation enhances Transfer lifecycle narrative:** Adding trace_id/span_id to logs makes grep-ability even better
- **Transfer Lifecycle Narrative requires consistent transfer_id:** Must ensure every log from discovery → cleanup includes transfer_id
- **Startup Sequence conflicts with Silent operations:** Startup should be explicit and verbose (INFO level), normal operations should be quiet unless action required
- **Log Sampling conflicts with lifecycle narrative:** Don't sample logs that have transfer_id correlation - operators need complete narrative

## MVP Definition

### Launch With (v1.2 - This Milestone)

Minimum viable improvement to make logs tell the application's story.

- [x] **Consistent log levels audit** - Review all existing logs, ensure INFO = lifecycle events, DEBUG = details, WARN/ERROR = problems
- [x] **Startup sequence narrative** - Clear initialization order: config → telemetry → database → client auth → server → ready
- [x] **Transfer lifecycle logging** - Ensure transfer_id appears in every log from discovery → cleanup
- [x] **Component readiness logs** - Each major component logs "ready" at INFO level after initialization
- [x] **Trace context in logs** - Add OpenTelemetry trace_id/span_id to logs via otelslog bridge
- [x] **Operation field consistency** - Add "operation" field to all logs (already in panic handlers, expand)
- [x] **Remove noise** - Eliminate redundant/confusing logs that don't add value

### Add After Validation (v1.3+)

Features to add once core logging improvements are validated.

- [ ] **Periodic heartbeat logs** - Log "idle, watching for transfers" every hour when no activity (trigger: first week of operation)
- [ ] **Resource utilization logs** - Periodic INFO log with active downloads, goroutines, memory (trigger: production monitoring gaps identified)
- [ ] **Dependency health checks** - Periodic validation of Sonarr/Radarr/seedbox connectivity (trigger: operational need)
- [ ] **State transition logs** - Explicit "transfer state changed: downloading → downloaded" logs (trigger: state machine debugging)

### Future Consideration (v2+)

Features to defer until production experience guides priorities.

- [ ] **Log sampling** - Sample per-file DEBUG logs to reduce volume (why defer: wait to see if DEBUG volume is actually problematic)
- [ ] **Dynamic log levels** - Change log level at runtime via HTTP API or signal (why defer: no operational need yet, adds complexity)
- [ ] **Structured error taxonomy** - Error codes/types for programmatic alerting (why defer: need production experience to identify error patterns)

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Consistent log levels audit | HIGH | LOW | P1 |
| Startup sequence narrative | HIGH | LOW | P1 |
| Transfer lifecycle logging | HIGH | MEDIUM | P1 |
| Trace context in logs | HIGH | MEDIUM | P1 |
| Component readiness logs | MEDIUM | LOW | P1 |
| Operation field consistency | MEDIUM | LOW | P1 |
| Remove noise | HIGH | LOW | P1 |
| Periodic heartbeat logs | MEDIUM | LOW | P2 |
| Resource utilization logs | MEDIUM | MEDIUM | P2 |
| Dependency health checks | MEDIUM | HIGH | P2 |
| State transition logs | LOW | MEDIUM | P2 |
| Log sampling | LOW | MEDIUM | P3 |
| Dynamic log levels | LOW | HIGH | P3 |
| Structured error taxonomy | MEDIUM | HIGH | P3 |

**Priority key:**
- P1: Must have for milestone - makes logs tell the story
- P2: Should have - adds operational value but not critical for narrative
- P3: Nice to have - defer until production need identified

## Current State Analysis

Based on codebase examination:

**What's good:**
- ✓ Structured JSON logging via slog (log/slog package)
- ✓ Log level configuration via env var (LOG_LEVEL)
- ✓ Transfer_id in most logs (downloader.go, transfer.go)
- ✓ Panic recovery with stack traces (all goroutines)
- ✓ Context-aware logging (logctx.LoggerFromContext)
- ✓ OpenTelemetry metrics instrumentation exists

**What needs improvement:**
- ✗ No trace_id/span_id in logs (OTel exists but not bridged to slog)
- ✗ Inconsistent log levels (mixing INFO/DEBUG for similar events)
- ✗ Startup sequence is implicit, not narrative ("starting..." then jumps to "waiting for downloads")
- ✗ No explicit component readiness logs
- ✗ "operation" field only in panic logs, not consistent
- ✗ Some logs lack transfer_id context (notification loop, main.go)
- ✗ Polling activity logged at INFO even when no transfers found (noise)

## Observability Patterns from Research

### Production Best Practices (2026)

1. **Use JSON format in production** - TextHandler for development, JSONHandler for production (Better Stack, go.dev)
2. **Add contextual information** - Common attributes (trace_id, transfer_id) should appear in all related logs (go.dev blog)
3. **Include stack traces for errors** - Only for unexpected failures, not business errors (Better Stack)
4. **Maintain consistency** - Standard format across application (Better Stack)
5. **Test before production** - Verify logs contain necessary info, not too verbose (Better Stack)

### Lifecycle Logging Patterns

1. **Component ordering** - Shutdown in reverse order from startup (Kubernetes graceful shutdown)
2. **Readiness indicators** - Each component explicitly logs ready state (go-lifecycle pattern)
3. **Health checks** - Validate dependencies on startup with retry (github.com/g4s8/go-lifecycle)
4. **Graceful shutdown** - Log shutdown phases: stopping accepting work → draining → cleanup → exit (fx.Lifecycle pattern)

### Trace Correlation Patterns

1. **OpenTelemetry bridge** - Use otelslog to inject trace_id/span_id automatically (go.opentelemetry.io/contrib/bridges/otelslog)
2. **Context propagation** - Use InfoContext(ctx, ...) not Info(...) to preserve trace context (OpenTelemetry docs)
3. **Unified naming** - Consistent field names (trace_id, span_id) across all logs (oneuptime.com guide)
4. **Cross-system correlation** - Logs + traces combined enable full request lifecycle (OpenTelemetry concepts)

### Anti-Patterns to Avoid

1. **Package-level loggers** - Creates tight coupling, breaks dependency injection (Dave Cheney)
2. **Logger in context** - Implicit runtime dependency, not compiler-enforced (Dave Cheney)
3. **Excessive WARN level** - WARN should mean action needed, not "interesting info" (Dave Cheney)
4. **log.Fatal in libraries** - Prevents graceful shutdown, equivalent to panic (Dave Cheney)
5. **Excessive verbosity** - High signal-to-noise ratio obscures problems (Better Stack, Datadog)
6. **Unsampled high-volume** - Per-file logs at INFO create noise (Better Stack sampling guidance)

## Competitor Analysis

While this is an internal service, comparable production systems (monitoring agents, data pipelines, long-running workers) demonstrate these patterns:

| Pattern | Datadog Agent | Prometheus Exporter | This Service |
|---------|---------------|---------------------|--------------|
| Startup narrative | Yes - component init sequence logged | Yes - config → validation → ready | Partial - needs explicit phases |
| Trace correlation | Yes - trace_id in all logs | N/A (metrics-only) | No - missing otelslog bridge |
| Component readiness | Yes - "datadog-agent is ready" | Yes - "exporter started successfully" | No - implicit readiness |
| Heartbeat logs | Yes - periodic health at INFO | Yes - scrape success logged | No - silent when idle |
| Operation grouping | Yes - consistent operation tags | Yes - collector component tags | Partial - only in panic handlers |

## Sources

**Go slog Best Practices:**
- [Logging in Go with Slog: The Ultimate Guide | Better Stack Community](https://betterstack.com/community/guides/logging/logging-in-go/)
- [Structured Logging with slog - The Go Programming Language](https://go.dev/blog/slog)
- [Logging in Go with Slog: A Practitioner's Guide · Dash0](https://www.dash0.com/guides/logging-in-go-with-slog)
- [Effective Logging in Go: Best Practices and Implementation Guide - DEV Community](https://dev.to/fazal_mansuri_/effective-logging-in-go-best-practices-and-implementation-guide-23hp)

**Lifecycle & Graceful Shutdown:**
- [How to shutdown a Go application gracefully | Josemy's blog](https://josemyduarte.github.io/2023-04-24-golang-lifecycle/)
- [How to Implement Graceful Shutdown in Go for Kubernetes](https://oneuptime.com/blog/post/2026-01-07-go-graceful-shutdown-kubernetes/view)
- [lifecycle package - github.com/g4s8/go-lifecycle](https://pkg.go.dev/github.com/g4s8/go-lifecycle)

**Trace Context & Correlation:**
- [How to Set Up Structured Logging in Go with OpenTelemetry](https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view)
- [OpenTelemetry Slog [otelslog]: Golang Bridge Setup & Examples | Uptrace](https://uptrace.dev/guides/opentelemetry-slog)
- [Context propagation | OpenTelemetry](https://opentelemetry.io/docs/concepts/context-propagation/)
- [Traces | OpenTelemetry](https://opentelemetry.io/docs/concepts/signals/traces/)

**Anti-Patterns:**
- [The package level logger anti pattern | Dave Cheney](https://dave.cheney.net/2017/01/23/the-package-level-logger-anti-pattern)
- [Logging Best Practices: 12 Dos and Don'ts | Better Stack Community](https://betterstack.com/community/guides/logging/logging-best-practices/)

**Signal-to-Noise Ratio:**
- [How to Optimize Log Volume and Reduce Noise at Scale | Datadog](https://www.datadoghq.com/knowledge-center/log-optimization/)
- [Monitoring: Turning Noise into Signal](https://accu.org/journals/overload/26/144/oldwood_2488/)

---
*Feature research for: Logging Improvements (v1.2)*
*Researched: 2026-02-01*
*Confidence: HIGH (verified with authoritative sources + codebase analysis)*
