# Phase 3: Operational Hygiene - Context

**Gathered:** 2026-01-31
**Status:** Ready for planning

<domain>
## Phase Boundary

Add startup validation and improve observability. Specifically:
- Log warning when telemetry is disabled (OTEL_ADDRESS not set)
- Validate database connectivity at startup with db.Ping()
- Configure connection pool limits (SetMaxOpenConns, SetMaxIdleConns)
- Remove or implement commented-out recovery code in transfer.go (lines 96-122)

This phase does NOT add new functionality or change runtime behavior — it improves startup reliability and code quality.

</domain>

<decisions>
## Implementation Decisions

### Startup validation behavior
- **Telemetry check:** Warn and continue if disabled
  - Log at Info level (telemetry is optional, not critical)
  - Silent when enabled (normal case)
  - Only log when disabled
- **Database check:** Retry then exit on failure
  - Database is critical, refuse to start if validation fails after retries
  - Retry db.Ping() 3 times with exponential backoff
  - Exit immediately after exhausting retries
- **Validation order:** Database first (critical), then telemetry (optional)
  - Fail-fast: exit on first critical failure (database)
  - Don't continue checking if database validation fails
- **Logging strategy:** Debug level for each validation, Info level at start if helpful
  - Logs should tell a story and be useful at each level
  - Each check logs individually at Debug level
  - Summary/status at Info level if it adds context

### Database validation approach
- **Retry configuration:** 3 retries with exponential backoff
  - Consistent with Phase 1 HTTP retry pattern
  - Use existing backoff library (cenkalti/backoff/v5)
  - Exponential intervals (not fixed delay)
- **Connection pool limits:** Configurable via environment variables
  - Read DB_MAX_OPEN_CONNS and DB_MAX_IDLE_CONNS from env
  - Set sensible defaults in env var tags (fallbacks if not specified)
  - If invalid values provided, use fallback defaults (don't exit)
- **Configuration failure handling:** Use fallback defaults
  - Invalid env values for pool limits: log warning, use defaults, continue
  - Defaults should be set in env var struct tags

### Telemetry logging
- **Log level:** Info (not Warning)
  - Telemetry disabled is informational, not a warning condition
  - Only log when disabled (silent when enabled)
- **Message content:** Fact + impact
  - Include what's affected: "Telemetry disabled - metrics and traces will not be collected"
  - Don't include instructions (how to enable) - keep message focused
- **Check timing:** After database validation
  - Validate critical dependencies (database) first
  - Then check optional observability (telemetry)

### Claude's Discretion
- Exact log message wording (following existing patterns)
- Specific default values for connection pool limits (max open conns, max idle conns)
- How to handle commented-out recovery code (lines 96-122): implement, remove, or document decision
- Retry backoff timing specifics (use backoff library defaults or tune for startup)

</decisions>

<specifics>
## Specific Ideas

- Use existing backoff library (cenkalti/backoff/v5) for db.Ping() retries - consistent with Phase 1
- Debug-level logs for granular validation steps, Info-level for summary/status
- Environment variable defaults set in struct tags (common Go pattern)
- Fail-fast philosophy: exit on first critical failure (database), don't waste time checking remaining items

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope (startup validation and code cleanup only, no new features).

</deferred>

---

*Phase: 03-operational-hygiene*
*Context gathered: 2026-01-31*
