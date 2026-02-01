# Project Research Summary

**Project:** Seedbox Downloader Logging Improvements
**Domain:** Production Go service observability (24/7 event-driven operations)
**Researched:** 2026-02-01
**Confidence:** HIGH

## Executive Summary

The seedbox downloader is a production Go service with existing structured logging (slog) and OpenTelemetry tracing infrastructure. The research reveals a mature foundation that needs strategic enhancements rather than wholesale replacement. The primary opportunity is bridging the gap between existing OpenTelemetry traces and slog output using the official otelslog bridge, which will automatically inject trace_id and span_id into all logs for correlation.

The recommended approach is incremental enhancement through four phases: (1) add the otelslog bridge for automatic trace correlation, (2) audit and fix context propagation to eliminate logs missing trace IDs, (3) standardize log levels and add structured lifecycle logging, and (4) implement dynamic log level controls for production debugging. This approach preserves the existing investment in slog and OpenTelemetry while addressing the critical gaps: missing trace correlation, inconsistent log levels, and inadequate lifecycle visibility.

Key risks center on performance degradation from excessive logging, log cardinality explosion driving costs, and breaking existing log consumers (dashboards, alerts). Mitigation requires careful log level discipline (INFO for lifecycle only, DEBUG for details), sampling high-frequency operations, and treating log field names as an API contract with deprecation periods for any schema changes.

## Key Findings

### Recommended Stack

The research strongly validates the existing technology choices and recommends a single strategic addition: the official OpenTelemetry slog bridge. The current stack (slog for structured logging, OpenTelemetry v1.38.0 for tracing, Chi router for HTTP) is production-ready and follows 2026 best practices.

**Core technologies:**
- **go.opentelemetry.io/contrib/bridges/otelslog** (v0.14.0+): Automatic trace context injection — Official OTel bridge that adds trace_id/span_id to all log entries with <1% overhead. This is the single most valuable addition, enabling correlation between logs and distributed traces without code changes.
- **log/slog** (Go 1.23 stdlib): Structured JSON logging — Already in use, no changes needed. Standard library choice ensures long-term stability and zero external dependencies for core logging.
- **OpenTelemetry** (v1.38.0): Distributed tracing — Already in use, just needs log integration via otelslog bridge. Existing HTTP middleware and instrumented clients provide trace context that logs currently don't capture.

**Optional enhancements:**
- **github.com/go-chi/httplog**: HTTP request logging middleware for Chi router — Zero dependencies, built-in request ID generation, automatic log level by status code (5xx=error, 4xx=warn, 2xx=info).
- **github.com/golang-cz/devslog**: Pretty console output for local development — Should NOT be used in production, only for improving local debugging experience.

**Anti-recommendations (what NOT to use):**
- Custom trace context handlers — Use official otelslog bridge instead
- Third-party community bridges (e.g., github.com/go-slog/otelslog) — Prefer official go.opentelemetry.io package
- TextHandler in production — Use JSONHandler for machine-parseable logs
- Multiple logger instances — Single logger with handler composition is clearer

### Expected Features

The research identifies production observability patterns from comparable 24/7 services (monitoring agents, data pipelines, long-running workers). The feature landscape separates table stakes (required for operability) from differentiators (transforming debugging experience).

**Must have (table stakes):**
- **Structured JSON logging** — Already implemented via slog with JSONHandler
- **Consistent log levels** — Needs audit; operators filter by severity and inconsistency creates noise
- **Lifecycle logging** — Partial; needs clear component initialization sequence
- **Error context** — Present but inconsistent; needs transfer_id in all related logs
- **Trace context in logs** — Missing; requires otelslog bridge for trace_id/span_id injection
- **Log level configurability** — Already implemented via LOG_LEVEL env var
- **Operation logging** — Partial; key operations visible at INFO but mixed with noise
- **Error stack traces** — Already implemented for panics

**Should have (competitive):**
- **Transfer lifecycle narrative** — Single grep for transfer_id shows complete story (discovered → claimed → downloading → imported → cleaned up)
- **Startup sequence reporting** — Clear phases: "Config loaded → Database validated → Client authenticated → Server listening → Ready"
- **Operation grouping** — Consistent "operation" field to group related log lines
- **Component readiness** — Each component logs when ready: "TransferOrchestrator ready", "Downloader ready"
- **State transitions** — Explicit logs when transfers change state
- **HTTP request logging** — Currently missing; no visibility into API usage patterns

**Defer (v2+):**
- **Log sampling** — Wait to see if DEBUG volume is actually problematic in production
- **Dynamic log levels via API** — No operational need yet; adds complexity
- **Structured error taxonomy** — Need production experience to identify error patterns first
- **Dependency health checks** — Periodic validation of external services (add when operational gaps identified)
- **Resource utilization logs** — Periodic reporting of active downloads, goroutines (add when monitoring gaps identified)

**Anti-features (avoid these):**
- **Log every file download at INFO** — Creates 100+ logs per transfer, drowns signal in noise; use transfer-level events at INFO, per-file at DEBUG
- **Log every polling tick** — Creates noise every 10 minutes; log only when transfers found
- **Duplicate logs in multiple formats** — Doubles volume; use JSON in production, text locally
- **Log request/response bodies** — Exposes credentials and creates massive volume; log metadata only
- **WARN level for expected conditions** — Triggers false alerts; use INFO for normal operations

### Architecture Approach

The existing architecture is event-driven with goroutine-based pipeline stages. The logging architecture must overlay this without disrupting the proven patterns. The key insight is that context propagation already exists (via logctx package) but isn't fully leveraged for trace correlation.

**Major components and logging enhancements:**
1. **OpenTelemetry Bridge Integration** — Wrap existing slog handler with otelslog bridge; zero changes to existing logging calls, automatic trace_id/span_id injection using context already passed to InfoContext()
2. **Component-Scoped Loggers** — Change from `.WithGroup("component")` (nested JSON) to `.With(slog.String("component", "name"))` (flat attributes) for easier filtering in log aggregation
3. **Pipeline Flow Tracing** — Add consistent attributes (transfer_id, transfer_name, operation, component) to enable tracing torrents through entire pipeline: Webhook → Orchestrator → Downloader → Import Monitor → Cleanup
4. **HTTP Request Logging Middleware** — Add go-chi/httplog before existing telemetry middleware for request/response logging with automatic request ID generation
5. **Lifecycle Event Logging** — Structure startup/shutdown as distinct phases with clear component initialization order and readiness indicators
6. **Goroutine Lifecycle Logging** — Already well-implemented with panic recovery and shutdown logging; maintain existing patterns

**Data flow:**
```
[HTTP Request] → [HTTP Middleware logs: method, path, status]
    → [Handler logs: operation="torrent-add"]
    → [Client logs: component="putio", operation="add_transfer"]
    → [Pipeline logs: transfer_id, operation="discover|claim|download|import|cleanup"]
```

**Integration points:**
- Existing logctx package (preserve) — Works perfectly with new patterns
- Existing OpenTelemetry instrumentation (preserve) — Logs will automatically correlate via otelslog
- Existing HTTP middleware (enhance) — Add httplog middleware before existing telemetry middleware
- Existing goroutine patterns (preserve) — Already have good panic recovery and context propagation

### Critical Pitfalls

Research identified eight critical pitfalls from recent production incidents (2025-2026) and authoritative observability sources. These represent real failure modes, not theoretical concerns.

1. **Breaking Log Schema for Existing Consumers** — Changing log field names or structure breaks dashboards and alerts downstream. Treat log field names as API contract; use additive changes only with 2-4 week deprecation periods.

2. **The !BADKEY Footgun with slog** — Using alternating key-value pairs `logger.Info("msg", "key", value)` instead of slog.Attr helpers creates silent corruption when argument count is odd. Enforce `sloglint` with `attr-only: true` in CI/CD to prevent company-wide.

3. **Missing Trace Context in Logs** — Using `logger.Info()` instead of `logger.InfoContext(ctx)` means logs lack trace IDs. Always use context-aware methods; verify logs contain trace_id/span_id fields in integration tests.

4. **Log Cardinality Explosion** — High-cardinality fields (UUIDs, timestamps with milliseconds) drive exponential cost increases (from $200/month to $4,000/month). Limit high-cardinality fields to ERROR/WARN only; implement sampling for INFO logs.

5. **Context Propagation Failure in Goroutines** — Starting goroutines without propagating context loses trace correlation and prevents cancellation. In one incident, this caused 50,847 goroutines, 47GB memory, 32-second response times. Always pass context: `go func(ctx context.Context) { ... }(ctx)`.

6. **Performance Degradation from Synchronous Logging** — Detailed logging on hot paths causes measurable throughput reduction (20% in benchmarks). Use DEBUG level for hot paths, INFO only for lifecycle; implement sampling for high-frequency operations.

7. **Missing AddSource Performance Cost** — Enabling `AddSource: true` globally adds runtime.Caller() overhead on every log call. Disable in production; enable only in development or for ERROR level logs.

8. **No Graceful Log Level Change Mechanism** — Hardcoded log levels require 5-15 minute redeploy for DEBUG during incidents. Implement dynamic control via HTTP endpoint or signal handling with authentication.

## Implications for Roadmap

Based on the research, the implementation should follow a risk-managed progression from low-risk infrastructure changes to higher-risk refactoring. The phases build on existing patterns rather than replacing them.

### Phase 1: OpenTelemetry Bridge Integration
**Rationale:** Lowest-risk, highest-value enhancement. Wraps existing logger without any code changes to logging calls. Immediately enables trace correlation across entire system.

**Delivers:** Automatic trace_id/span_id injection in all logs, enabling correlation with distributed traces for incident response.

**Addresses:**
- Missing trace context in logs (table stakes feature)
- Request correlation (differentiator feature)

**Avoids:**
- Missing trace context in logs pitfall (Critical #3)
- Context propagation failures (Critical #5 — foundation for fixing)

**Implementation:** Add otelslog bridge in main.go initializeConfig(), wrap existing JSONHandler, verify trace_id appears in logs.

### Phase 2: Context Propagation Audit
**Rationale:** Essential foundation for trace correlation to work. Must verify all logging calls use context-aware methods and all goroutines receive context. Builds on Phase 1 bridge infrastructure.

**Delivers:** All logs include trace context; no broken correlation chains; proper goroutine lifecycle management.

**Addresses:**
- Consistent logging across all components
- Goroutine lifecycle logging (already partially done, needs verification)

**Avoids:**
- Missing trace context pitfall (Critical #3)
- Context propagation failure in goroutines pitfall (Critical #5)
- !BADKEY footgun pitfall (Critical #2 — audit finds these)

**Implementation:** Grep for non-context log calls (.Info, .Error without Context suffix), verify all go func() receives context, run sloglint in CI.

### Phase 3: Lifecycle & Component Logging
**Rationale:** With trace correlation working (Phases 1-2), now enhance the content of logs. Changes isolated to startup/shutdown sequences and component initialization — lower risk than touching pipeline logic.

**Delivers:** Clear startup narrative, component readiness indicators, consistent operation fields, HTTP request logging.

**Addresses:**
- Startup sequence reporting (differentiator)
- Component readiness (differentiator)
- Operation grouping (differentiator)
- HTTP request logging (should-have)

**Avoids:**
- Breaking log schema pitfall (Critical #1 — additive changes only)
- Performance degradation pitfall (Critical #6 — log levels discipline)

**Implementation:** Add lifecycle phases to main.go run(), add component-scoped loggers, add httplog middleware, standardize operation field usage.

### Phase 4: Pipeline Flow Enhancement
**Rationale:** Most invasive changes touching component logic. With solid foundation (Phases 1-3), now enhance pipeline-specific logging. Requires careful testing to avoid breaking existing behavior.

**Delivers:** Transfer lifecycle narrative (grep transfer_id shows complete story), state transition visibility, consistent transfer context across pipeline stages.

**Addresses:**
- Transfer lifecycle narrative (differentiator)
- State transitions (should-have)
- Operation logging consistency (table stakes)

**Avoids:**
- Log cardinality explosion pitfall (Critical #4 — control transfer_id usage)
- Performance degradation pitfall (Critical #6 — sampling in hot paths)

**Implementation:** Create transfer-scoped loggers with consistent attributes, add operation field to all pipeline stages, audit and reduce noisy DEBUG logs.

### Phase Ordering Rationale

- **Phase 1 first** because it's infrastructure wrapping with zero behavior changes — lowest risk, enables everything else
- **Phase 2 second** because it verifies the context foundation needed for Phase 1 to work correctly
- **Phase 3 third** because lifecycle changes are isolated to main.go and startup/shutdown — lower risk than pipeline changes
- **Phase 4 last** because it touches component logic and requires careful testing — highest risk, but built on solid foundation

**Deferred to post-implementation:**
- Dynamic log level control (Phase 5) — useful but not critical; no production need yet
- Log sampling implementation (Phase 6) — wait to see if volume is actually problematic
- Dependency health checks (Phase 7) — add when operational gaps identified

### Research Flags

**Phases likely needing deeper research during planning:**
- **Phase 2 (Context Propagation Audit):** May discover complex context threading issues requiring architectural decisions
- **Phase 4 (Pipeline Flow Enhancement):** Transfer state machine may reveal edge cases needing domain research

**Phases with standard patterns (skip research-phase):**
- **Phase 1 (OpenTelemetry Bridge):** Well-documented official pattern, no unknowns
- **Phase 3 (Lifecycle Logging):** Standard startup/shutdown patterns, heavily documented

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Official OpenTelemetry documentation, slog is stdlib, multiple authoritative guides (Better Stack, Uptrace, go.dev blog) consulted |
| Features | HIGH | Analyzed comparable production services (Datadog Agent, Prometheus exporters), verified with 2026 best practices guides and real operational needs |
| Architecture | HIGH | Patterns validated against official OpenTelemetry architecture docs, context propagation patterns from multiple recent guides, existing codebase analysis confirms applicability |
| Pitfalls | HIGH | All eight pitfalls sourced from recent production incidents (2025-2026), official warning documentation, and authoritative observability sources |

**Overall confidence:** HIGH

The research is grounded in official documentation (go.dev blog, OpenTelemetry specs, slog package docs), authoritative guides from observability platforms (Better Stack, Uptrace, Dash0, SigNoz), and recent production incident reports (2025-2026). All recommendations align with current Go best practices as of January 2026.

### Gaps to Address

While confidence is high, these areas need attention during implementation:

- **Log consumer inventory:** Need to identify all dashboards, alerts, and log queries consuming current log output before making schema changes. Address during Phase 1 planning by documenting existing consumers and testing changes against queries.

- **Actual cardinality impact:** Research provides thresholds (1M+ unique combinations problematic) but need to measure actual cardinality in this service's workload. Address during Phase 4 by monitoring unique field combinations and implementing sampling if needed.

- **Performance baseline:** Benchmarks from research (20% throughput reduction) are general guidance, not specific to this codebase. Address during Phase 3/4 by load testing before and after changes to measure actual impact.

- **Existing log level discipline:** Current state analysis shows inconsistent levels but needs comprehensive audit to understand scope. Address early in Phase 2 by grepping for all log calls and categorizing by level/context.

## Sources

### Primary (HIGH confidence)

**Official Documentation:**
- [Structured Logging with slog - The Go Programming Language](https://go.dev/blog/slog) — Official introduction and best practices
- [log/slog package documentation](https://pkg.go.dev/log/slog) — Standard library reference
- [OpenTelemetry Slog Bridge Package](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog) — Official API documentation for otelslog bridge
- [OpenTelemetry Logging Specification](https://opentelemetry.io/docs/specs/otel/logs/) — Standard specification for log integration
- [Context propagation | OpenTelemetry](https://opentelemetry.io/docs/concepts/context-propagation/) — Official context patterns

**Authoritative Guides (2025-2026):**
- [Logging in Go with Slog: The Ultimate Guide | Better Stack](https://betterstack.com/community/guides/logging/logging-in-go/) — Comprehensive best practices
- [Logging in Go with Slog: A Practitioner's Guide | Dash0](https://www.dash0.com/guides/logging-in-go-with-slog) — Production patterns
- [How to Set Up Structured Logging in Go with OpenTelemetry | OneUpTime](https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view) — January 2026 integration guide
- [OpenTelemetry Slog Integration | Uptrace](https://uptrace.dev/guides/opentelemetry-slog) — Practical setup guide
- [Complete Guide to Logging in Golang with slog | SigNoz](https://signoz.io/guides/golang-slog/) — Comprehensive tutorial

### Secondary (MEDIUM confidence)

**Best Practices & Patterns:**
- [Logging Best Practices: 12 Dos and Don'ts | Better Stack](https://betterstack.com/community/guides/logging/logging-best-practices/) — General logging guidance
- [9 Logging Best Practices | Dash0](https://www.dash0.com/guides/logging-best-practices) — Production logging patterns
- [How to shutdown a Go application gracefully | Josemy's blog](https://josemyduarte.github.io/2023-04-24-golang-lifecycle/) — Lifecycle management patterns
- [The package level logger anti pattern | Dave Cheney](https://dave.cheney.net/2017/01/23/the-package-level-logger-anti-pattern) — Logger dependency injection guidance

**Production Incidents & Case Studies:**
- [Finding and Fixing a 50,000 Goroutine Leak](https://skoredin.pro/blog/golang/goroutine-leak-debugging) (Dec 2025) — Real incident demonstrating context propagation failures
- [Understanding and Debugging Goroutine Leaks in Go Web Servers](https://leapcell.io/blog/understanding-and-debugging-goroutine-leaks-in-go-web-servers) (Oct 2025) — Production debugging patterns
- [Detecting Goroutine Leaks via the Go Garbage Collector](https://medium.com/@aman.kohli1/detecting-goroutine-leaks-via-the-go-garbage-collector-deep-dive-180128dd81cc) (Jan 2026) — Diagnostic techniques

### Tertiary (LOW confidence)

**Cost & Cardinality Management:**
- [Understanding High Cardinality in Observability](https://www.observeinc.com/blog/understanding-high-cardinality-in-observability) — Cost impact patterns
- [How to Wrangle Metric Data Explosions](https://chronosphere.io/learn/wrangle-metric-data-explosions-with-chronosphere-profiler/) — Cardinality management strategies
- [How to Optimize Log Volume and Reduce Noise at Scale | Datadog](https://www.datadoghq.com/knowledge-center/log-optimization/) — Volume optimization guidance

**Middleware & Tooling:**
- [go-chi/httplog](https://github.com/go-chi/httplog) — HTTP logging middleware for Chi
- [slog-multi Package](https://pkg.go.dev/github.com/samber/slog-multi) — Advanced handler composition patterns
- [go-lifecycle Package](https://pkg.go.dev/github.com/g4s8/go-lifecycle) — Lifecycle management library

---
*Research completed: 2026-02-01*
*Ready for roadmap: yes*
