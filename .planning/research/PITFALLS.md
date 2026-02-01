# Pitfalls Research

**Domain:** Improving logging in production Go services (24/7 operation with existing slog + OpenTelemetry)
**Researched:** 2026-02-01
**Confidence:** HIGH

## Critical Pitfalls

### Pitfall 1: Breaking Log Schema for Existing Consumers

**What goes wrong:**
Changing log field names, removing fields, or restructuring JSON log output breaks dashboards, alerts, and log aggregation pipelines that depend on the current schema. Dashboard queries fail silently, alerts stop firing, and log parsers break with obscure errors.

**Why it happens:**
Teams focus on "improving" the logging code without recognizing that log output is an API contract. The temptation to rename `request_id` to `requestId` or consolidate `user_id` and `userId` into a single convention feels like "cleanup" but creates a breaking change downstream.

**How to avoid:**
- Treat log field names as a stable API contract
- Use additive changes only: add new fields, never remove or rename existing fields
- Maintain a log schema registry documenting all field names and types
- Test log output against existing dashboard queries before deploying
- Use deprecation periods: add new field, keep old field for 2-4 weeks, then remove old field

**Warning signs:**
- Dashboards showing "No data" after deployment
- Alert channels going silent
- Log aggregation tools reporting parsing errors
- Support team asking "where did the logs go?"

**Phase to address:**
Phase 1 (Log Schema Audit) - Document existing log consumers and establish schema contract

---

### Pitfall 2: The `!BADKEY` Footgun with slog

**What goes wrong:**
Using alternating key-value pairs `logger.Info("msg", "key", value)` instead of `slog.Attr` helpers creates silent corruption. If you provide an odd number of arguments, slog doesn't fail - it creates broken log entries with `"!BADKEY": "value"` instead of the intended key-value pair. This corruption can go undetected for weeks until someone needs those specific log fields during an incident.

**Why it happens:**
The loose key-value API is convenient and shorter to type than `slog.String("key", value)`. Developers copy this pattern from existing code without understanding the type safety tradeoff. When refactoring removes one argument, the count becomes odd and the silent failure begins.

**How to avoid:**
- **Enforce `sloglint` in CI/CD with `attr-only: true`** - prevents the footgun company-wide
- Use only strongly-typed helpers: `slog.String()`, `slog.Int()`, `slog.Any()`
- Never use the loose `key, value, key, value` syntax
- Add pre-commit hooks that reject commits containing loose key-value pairs

**Warning signs:**
- Log entries containing `"!BADKEY": "something"` in production logs
- Missing expected log fields during incident investigation
- Log queries returning fewer results than expected

**Phase to address:**
Phase 2 (Code Audit) - Scan codebase for loose key-value pairs and convert to slog.Attr helpers

---

### Pitfall 3: Missing Trace Context in Logs (Context-Awareness Gap)

**What goes wrong:**
Using `logger.Info()` instead of `logger.InfoContext(ctx)` means logs lack trace IDs and span IDs, breaking correlation between logs and distributed traces. During production incidents, you can't link log messages to the OpenTelemetry traces, making it nearly impossible to reconstruct what happened across the request lifecycle.

**Why it happens:**
Developers forget to pass context through function calls, or use the simpler non-context logging methods. The code compiles and logs appear normal, so the problem goes unnoticed until an incident requires trace correlation. Context propagation discipline is hard to maintain across a codebase.

**How to avoid:**
- **Always use context-aware logging methods**: `InfoContext(ctx)`, `WarnContext(ctx)`, `ErrorContext(ctx)`
- Thread context through all function signatures (accept `context.Context` as first parameter)
- Use linters to enforce context-aware logging (ban non-context log methods)
- Verify logs contain `trace_id` and `span_id` fields in integration tests
- Use `otelslog` bridge to automatically inject trace context into logs

**Warning signs:**
- Logs missing `trace_id` or `span_id` fields
- Inability to filter logs by trace ID during incident response
- Logs showing activity but corresponding traces are empty
- Debug sessions requiring manual timestamp correlation instead of trace linking

**Phase to address:**
Phase 2 (Context Propagation Audit) - Verify all logging calls use InfoContext and all functions accept context

---

### Pitfall 4: Log Cardinality Explosion Driving Cost and Performance Degradation

**What goes wrong:**
Adding high-cardinality fields (unique request IDs, timestamps with milliseconds, user-specific tokens, UUIDs) to every log entry creates millions of unique log entries. This drives up log storage costs exponentially (from $200/month to $4,000/month), slows down log queries, and overwhelms observability platforms causing ingestion throttling and dashboard performance degradation.

**Why it happens:**
The goal is "better observability" so teams add every possible detail to logs. Each additional field seems harmless in isolation: "let's add request_id", "let's add correlation_id", "let's add session_token". Combined with high log volume (INFO level on hot paths), cardinality explodes. In cloud-native environments, cardinality can jump from 20,000 unique metrics in monoliths to 800 million in microservices deployments.

**How to avoid:**
- **Limit high-cardinality fields to ERROR and WARN levels only**
- Use sampling: log 1% of INFO messages on hot paths, 100% of WARN/ERROR
- Set up cardinality monitoring alerts (warn when unique field combinations exceed thresholds)
- Use fixed log levels per component (HTTP handler at INFO, DB queries at DEBUG)
- Aggregate before shipping: summarize metrics instead of logging every event
- Implement log dropping rules for low-value high-volume logs

**Warning signs:**
- Log platform bill increasing 20%+ month-over-month without traffic increase
- Query timeouts in log dashboards
- Ingestion lag or dropped logs
- Observability platform sending "approaching quota" warnings
- Log storage growing faster than request volume

**Phase to address:**
Phase 3 (Cardinality Analysis) - Audit log fields for cardinality, implement sampling strategy

---

### Pitfall 5: Context Propagation Failure in Goroutines

**What goes wrong:**
Starting goroutines without propagating context means logs from background operations lack trace correlation and context cancellation signals are ignored. When parent request completes, goroutines continue running with orphaned context, leaking goroutines and logging with disconnected trace IDs. In a production incident, a 24/7 service reached 50,847 goroutines, 47GB memory, and 32-second response times.

**Why it happens:**
Developers launch goroutines with `go func() {}` and either forget to pass context or use `context.Background()` thinking it's safer for "background work". Async operations feel independent, so context propagation seems unnecessary. The problem is subtle: the service works fine, goroutines complete eventually, but during high load or when parent contexts cancel quickly, goroutines accumulate.

**How to avoid:**
- **Always pass context to goroutines**: `go func(ctx context.Context) { ... }(ctx)`
- For operations that must outlive request: use `context.WithoutCancel(ctx)` (Go 1.21+) to preserve trace context while preventing cancellation propagation
- Check `ctx.Done()` in goroutine loops and exit gracefully when context cancels
- Use `context.WithTimeout()` for goroutines that might hang
- Monitor goroutine count in production: alert when `runtime.NumGoroutine()` exceeds baseline

**Warning signs:**
- Goroutine count steadily increasing over hours/days
- Memory usage growing without corresponding traffic increase
- Logs from background operations missing trace IDs
- Operations continuing after request timeout/cancellation
- Background task logs appearing without corresponding request logs

**Phase to address:**
Phase 2 (Context Propagation Audit) - Review all goroutine launches for context propagation

---

### Pitfall 6: Performance Degradation from Synchronous Logging on Hot Paths

**What goes wrong:**
Adding detailed logging to hot paths (request handlers, tight loops, high-frequency operations) causes measurable throughput degradation. Benchmarks show 20% throughput reduction when adding structured logging (191k to 152k req/sec). At scale, this means needing extra servers to handle the same load, directly increasing infrastructure costs and potentially causing latency spikes during traffic peaks.

**Why it happens:**
The goal is better observability, so logging is added to "see what's happening". Logging feels cheap because individual calls are microseconds. The team doesn't run load tests to measure aggregate impact. Production traffic reveals the problem: hot paths executing 10,000 times per second with 5 log statements each means 50,000 log writes per second consuming significant CPU, memory allocations, and I/O.

**How to avoid:**
- **Use DEBUG level for hot path logs, INFO only for lifecycle events**
- Implement sampling: log 1 in every 100 iterations on tight loops
- Use async logging handlers with buffering for high-throughput scenarios
- Profile before and after adding logging: measure req/sec, p99 latency, CPU usage
- Reserve ERROR/WARN for actual problems, not normal operation
- Consider metrics instead of logs for high-frequency events (use counters, not log lines)

**Warning signs:**
- P99 latency increasing after logging improvements deployed
- CPU usage increase disproportionate to traffic increase
- Throughput regression in load tests
- Increased allocation rates visible in pprof
- Services requiring more instances to handle same load

**Phase to address:**
Phase 3 (Performance Testing) - Load test with new logging, benchmark hot paths, implement sampling

---

### Pitfall 7: Missing AddSource Performance Cost Understanding

**What goes wrong:**
Enabling `AddSource: true` in slog handlers to include file name and line numbers in every log entry adds measurable overhead by calling `runtime.Caller()` for every log statement. In high-throughput services, this causes CPU usage increases and throughput degradation. The source information is helpful during development but rarely needed in production where traces and correlation IDs provide better context.

**Why it happens:**
Source information (file and line number) is useful for quickly finding log statements in code during development. Teams enable `AddSource` globally thinking it's harmless. The `runtime.Caller()` cost seems small per-call but accumulates significantly at scale. When logging 10,000 times per second, the repeated stack walking becomes measurable overhead.

**How to avoid:**
- **Disable `AddSource` in production**, enable only in development/staging
- Use environment variables to control AddSource based on deployment environment
- If source info is needed, include it only for ERROR level logs
- Use trace IDs and span names for context instead of file/line numbers
- Profile with and without AddSource to measure actual impact on your workload

**Warning signs:**
- `runtime.Caller()` appearing high in CPU profiles
- Increased CPU usage after enabling source information
- Logs becoming noticeably slower to write
- pprof showing time spent in runtime package during logging

**Phase to address:**
Phase 3 (Performance Testing) - Measure AddSource impact, disable in production config

---

### Pitfall 8: No Graceful Log Level Change Mechanism (Requires Redeploy for Debugging)

**What goes wrong:**
When production incidents occur requiring DEBUG logs to diagnose, the service requires a redeploy to change log level from INFO to DEBUG. This adds 5-15 minutes to incident response time, during which the issue may resolve itself (making reproduction impossible) or cause continued user impact. Without dynamic log level control, teams are forced to choose between permanently verbose logging (high cost) or blind debugging (slow incident response).

**Why it happens:**
Log level is configured via environment variable or config file at startup, with no runtime update mechanism. This is simplest to implement - read config once, never change it. The limitation isn't discovered until a production incident when "just enable DEBUG logging" becomes "deploy a new version with DEBUG enabled, wait 10 minutes, then hope the problem reproduces".

**How to avoid:**
- **Implement dynamic log level control via HTTP endpoint or signal handling**
- Use `/admin/loglevel` endpoint with POST to change level without restart
- Support `kill -SIGUSR1 <pid>` to toggle DEBUG mode on/off
- Use feature flags or remote config to change log level for specific modules/paths
- Monitor log level changes: log when level changes and who requested it
- Implement temporary DEBUG mode: auto-revert to INFO after 15 minutes

**Warning signs:**
- Incident response requiring service restarts to enable debugging
- Complaints from ops team about needing redeploys for diagnostics
- Repeated "we need more logging in this area" after incidents
- DEBUG logging permanently enabled because changing it requires redeployment

**Phase to address:**
Phase 4 (Dynamic Controls) - Add runtime log level change endpoint with authentication

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Using loose key-value pairs instead of slog.Attr | Less typing, more concise code | Silent `!BADKEY` corruption when argument count is odd | **Never** - use sloglint to enforce |
| Skipping context propagation in goroutines | Simpler function signatures | Logs lack trace correlation, goroutine leaks | **Never** - context discipline is essential |
| Using `context.Background()` in async operations | Avoids context threading complexity | Loses trace correlation, no cancellation propagation | Only when using `context.WithoutCancel(ctx)` to preserve trace while decoupling lifecycle |
| Logging at INFO on hot paths | Complete visibility into operations | Performance degradation, high cardinality costs | Only with sampling (log 1% of operations) |
| Enabling AddSource in production | Helpful file/line info in logs | CPU overhead from runtime.Caller() | Only for ERROR level logs in low-traffic services |
| Renaming log fields for consistency | Cleaner, more consistent schema | Breaks existing dashboards and alerts | Only with deprecation period (keep both fields for 4+ weeks) |
| Using `context.TODO()` placeholder | Unblocks development without context plumbing | Logs missing trace IDs, linters complain | Only temporarily during migration, never commit |

## Integration Gotchas

Common mistakes when connecting logs to external services.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| OpenTelemetry traces | Using `slog.Info()` instead of `slog.InfoContext(ctx)` | Always use context-aware logging methods to inject trace context automatically |
| Log aggregation platforms | Changing field names without versioning | Treat log schema as API contract, use additive changes only |
| Dashboards and alerts | Assuming logs are only for humans | Document log fields consumed by dashboards, test changes against queries |
| Discord webhooks | Logging webhook failures at ERROR level | Use WARN for webhook failures (external service, not application error) |
| Database query logging | Logging full SQL with parameter values | Redact sensitive parameters, log query patterns not full text |
| HTTP request logging | Logging request/response bodies | Log metadata only (method, path, status, duration), bodies at DEBUG with size limits |

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Logging every operation at INFO | Service works fine initially | Use DEBUG for detailed traces, INFO for lifecycle only | 100+ req/sec or 10k+ log entries/hour |
| High-cardinality fields everywhere | Logs are detailed and searchable | Limit UUIDs, timestamps, unique IDs to ERROR/WARN only | 1M+ unique field combinations, log bill >$1k/month |
| Synchronous log writes on hot paths | Simple, reliable logging | Use async handlers with buffering, implement sampling | 1000+ req/sec, p99 latency >100ms |
| AddSource enabled globally | Helpful file/line info | Disable in production, enable per-environment | 10k+ log entries/sec, CPU >50% |
| No log level sampling | Complete visibility | Implement probabilistic sampling (1% at INFO, 100% at ERROR) | 100k+ log entries/min, ingestion throttling |
| Logging in tight loops | Visibility into iterations | Move to metrics (counters), log every Nth iteration | Loop >1000 iterations/sec |

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| Logging authentication tokens | Credentials leak into log aggregation platforms, accessible to many engineers | Implement `LogValuer` interface to redact sensitive fields automatically |
| Including raw request bodies | PII leakage (passwords, SSNs, credit cards) | Log metadata only, bodies only at DEBUG with explicit redaction |
| Logging database connection strings | Credentials exposed in logs | Use `[REDACTED]` placeholder, log only host/database name |
| User IDs in INFO logs without consent | GDPR/privacy violations | Hash user IDs, log only at WARN/ERROR when necessary for debugging |
| Exception stack traces with local variables | Secrets in environment variables visible in traces | Sanitize stack traces, avoid logging full exception details |
| Trace IDs without rate limiting | Trace ID enumeration attacks | Use random UUIDs, implement rate limiting on trace queries |

## UX Pitfalls

Common user experience mistakes in this domain (operations/debugging perspective).

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Inconsistent log levels | Operators can't filter effectively | Establish level contract: DEBUG=trace, INFO=lifecycle, WARN=unusual, ERROR=failure |
| Missing correlation IDs | Impossible to trace request across components | Include `trace_id`, `span_id`, `request_id` in every log entry |
| No structured context | Grep-driven debugging, slow incident response | Use structured fields consistently, avoid string interpolation |
| Overly verbose DEBUG logs | Can't find important information | Reserve DEBUG for detailed traces, not normal operations |
| Error logs without actionable info | Engineer must add more logging and redeploy | Include error cause, context (what was attempted), suggested remediation |
| No log timestamps in structured output | Can't correlate with metrics | Always include RFC3339 timestamp in UTC |
| Mixing stdout and stderr | Logs interleaved unpredictably | Use stdout for all logs, configure handler to emit JSON to stdout |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **Context propagation:** Often missing in goroutines — verify `go func(ctx context.Context)` pattern and `InfoContext()` usage
- [ ] **Trace correlation:** Often missing trace_id/span_id fields — verify otelslog bridge is configured and logs include trace context
- [ ] **Log schema documentation:** Often undocumented — verify .planning/schemas/logs.md documents all field names and types
- [ ] **Consumer impact testing:** Often skipped — verify dashboards, alerts, and log queries still work with new schema
- [ ] **Performance testing:** Often assumed negligible — verify load tests with new logging, measure throughput and latency impact
- [ ] **Dynamic log level control:** Often hardcoded at startup — verify runtime change mechanism (HTTP endpoint or signal)
- [ ] **Log sampling on hot paths:** Often "we'll add it later" — verify sampling strategy implemented for high-frequency operations
- [ ] **Sensitive data redaction:** Often "we'll be careful" — verify LogValuer implementations for credentials, tokens, PII

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Breaking log schema | MEDIUM | Deploy new version with both old and new fields, wait 4 weeks for consumers to migrate, remove old fields in subsequent release |
| `!BADKEY` corruption in logs | LOW | Convert loose key-value pairs to slog.Attr helpers, deploy new version, corrupted logs remain but stop accumulating |
| Missing trace context | MEDIUM | Add context parameters to functions, change Info() to InfoContext(), deploy new version — old logs lack correlation forever |
| Cardinality explosion | HIGH | Implement sampling immediately (emergency hotfix), analyze high-cardinality fields, remove from INFO logs, move to ERROR only |
| Context propagation failure in goroutines | MEDIUM | Add context parameters to goroutine functions, verify Done() checks, deploy new version — monitor goroutine count decrease |
| Performance degradation | LOW | Change log level from INFO to DEBUG for hot paths, deploy hotfix, implement sampling in next release |
| No dynamic log level control | HIGH | Implement /admin/loglevel endpoint, requires code change and deployment — can't fix retroactively |
| Log consumer dashboards broken | LOW | Revert log format changes immediately, plan deprecation strategy with both old and new fields |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Breaking log schema for consumers | Phase 1: Log Schema Audit | Document existing consumers, run dashboard queries against test logs |
| `!BADKEY` footgun with slog | Phase 2: Code Audit | Run sloglint with attr-only: true, zero violations in CI |
| Missing trace context in logs | Phase 2: Context Propagation Audit | grep for `\.Info\(` and `\.Error\(` (non-context variants), verify trace_id in logs |
| Log cardinality explosion | Phase 3: Cardinality Analysis | Monitor unique field combinations, verify sampling on hot paths |
| Context propagation failure in goroutines | Phase 2: Context Propagation Audit | Review all `go func()` launches, verify context parameter |
| Performance degradation from logging | Phase 3: Performance Testing | Load test before/after, benchmark hot paths, measure req/sec delta |
| Missing AddSource performance cost | Phase 3: Performance Testing | Profile with/without AddSource, verify disabled in prod config |
| No graceful log level change | Phase 4: Dynamic Controls | Test /admin/loglevel endpoint, verify signal handling |

## Sources

### High Confidence (Official Documentation & Recent 2025-2026 Articles)

**slog Best Practices & Pitfalls:**
- [Logging in Go with Slog: A Practitioner's Guide](https://www.dash0.com/guides/logging-in-go-with-slog)
- [Logging in Go with Slog: The Ultimate Guide](https://betterstack.com/community/guides/logging/logging-in-go/)
- [Structured Logging with slog](https://go.dev/blog/slog)
- [OpenTelemetry Slog Integration](https://uptrace.dev/guides/opentelemetry-slog)

**Production Logging Best Practices:**
- [Logging Best Practices: 12 Dos and Don'ts](https://betterstack.com/community/guides/logging/logging-best-practices/)
- [9 Logging Best Practices](https://www.dash0.com/guides/logging-best-practices)
- [How to Set Up Structured Logging in Go with OpenTelemetry](https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view)

**Context Propagation & Goroutine Issues:**
- [Propagating an Inappropriate Context in Go: Pitfalls and Solutions](https://medium.com/@marcianojosepaulo/propagating-an-inappropriate-context-in-go-pitfalls-and-solutions-531b6cc692ad)
- [Mastering Goroutines in Go: Common Pitfalls](https://dev.to/mx_tech/mastering-goroutines-in-go-common-pitfalls-and-how-to-avoid-them-3j25)
- [Goroutines and OpenTelemetry: Avoiding Common Pitfalls](https://medium.com/@veyselsahin/goroutines-and-opentelemetry-avoiding-common-pitfalls-538688f10b68)

**Cardinality & Cost Management:**
- [Understanding High Cardinality in Observability](https://www.observeinc.com/blog/understanding-high-cardinality-in-observability)
- [How to Wrangle Metric Data Explosions](https://chronosphere.io/learn/wrangle-metric-data-explosions-with-chronosphere-profiler/)
- [Cardinality Metrics for Monitoring and Observability](https://www.splunk.com/en_us/blog/devops/high-cardinality-monitoring-is-a-must-have-for-microservices-and-containers.html)

**Goroutine & Memory Leaks (Recent 2025-2026):**
- [Understanding and Debugging Goroutine Leaks in Go Web Servers](https://leapcell.io/blog/understanding-and-debugging-goroutine-leaks-in-go-web-servers) (Oct 2025)
- [Finding and Fixing a 50,000 Goroutine Leak](https://skoredin.pro/blog/golang/goroutine-leak-debugging) (Dec 2025)
- [Detecting Goroutine Leaks via the Go Garbage Collector](https://medium.com/@aman.kohli1/detecting-goroutine-leaks-via-the-go-garbage-collector-deep-dive-180128dd81cc) (Jan 2026)
- [Go Memory Leak: How One Line Drained Memory](https://www.harness.io/blog/the-silent-leak) (Oct 2025)

**Incident Response & Debugging:**
- [Debugging Microservices: How Trace IDs Saved My Production Incident](https://dev.to/nishantmodak/debugging-microservices-like-a-pro-how-trace-ids-saved-my-production-incident-7o6)
- [Debugging Production Issues: Complete Enterprise Methodology](https://logit.io/blog/post/debugging-production-issues-enterprise-guide/)

**Migration & Schema Evolution:**
- [Deep Dive and Migration Guide to Go 1.21+ slog](https://leapcell.io/blog/deep-dive-and-migration-guide-to-go-1-21-s-structured-logging-with-slog)
- [A Beginner's Guide to JSON Logging](https://betterstack.com/community/guides/logging/json-logging/)
- [Runtime Log Level Change using Golang](https://dev.to/aryanmehrotra/remote-runtime-log-level-change-using-golang-gofr-54d8)

---
*Pitfalls research for: Logging improvements in production Go service*
*Researched: 2026-02-01*
