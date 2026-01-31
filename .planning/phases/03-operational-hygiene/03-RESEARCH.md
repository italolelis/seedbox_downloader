# Phase 3: Operational Hygiene - Research

**Researched:** 2026-01-31
**Domain:** Go application startup validation, database connection management, structured logging
**Confidence:** HIGH

## Summary

This phase focuses on operational hygiene through startup validation and observability improvements. The research covers four main areas: database connectivity validation with retries, connection pool configuration, telemetry status logging, and handling commented-out recovery code.

**Standard approach for Go applications:**
- Use `db.PingContext()` with timeout immediately after `sql.Open()` to validate database connectivity at startup (fail-fast pattern)
- Configure connection pool limits via `SetMaxOpenConns()` and `SetMaxIdleConns()` to prevent resource exhaustion
- Use exponential backoff for retries (the project already has `cenkalti/backoff/v5` dependency)
- Log startup validation events at appropriate levels (Info for status, Debug for details, Error for failures)

**Primary recommendation:** Implement database validation with retry-then-exit pattern using existing backoff library, configure connection pool limits via environment variables with sensible defaults, and log telemetry status at Info level when disabled.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| database/sql | stdlib | Database connection pooling | Go's standard database abstraction |
| log/slog | stdlib (1.21+) | Structured logging | Official structured logging since Go 1.21 |
| cenkalti/backoff/v5 | v5.0.3 | Retry with exponential backoff | Industry standard for retry logic, already in project |
| kelseyhightower/envconfig | v1.4.0 | Environment variable parsing | Already in project, standard for config management |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| context | stdlib | Timeout management | Always use with database operations (PingContext, QueryContext) |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| cenkalti/backoff/v5 | Manual retry loop | Custom solution more error-prone, backoff handles edge cases |
| kelseyhightower/envconfig | sethvargo/go-envconfig | Different tag syntax, but kelseyhightower already in use |

**Installation:**
```bash
# All dependencies already in go.mod
go mod download
```

## Architecture Patterns

### Recommended Startup Validation Order

```
1. Parse configuration (env vars) → continue with defaults on invalid values
2. Initialize logger
3. Validate DATABASE (critical) → retry 3x with backoff, then EXIT on failure
4. Check telemetry status (optional) → log Info if disabled, silent if enabled
5. Continue with service initialization
```

**Rationale:** Fail-fast on critical dependencies (database), continue with optional features (telemetry).

### Pattern 1: Database Validation with Retry-Then-Exit

**What:** Validate database connectivity at startup with exponential backoff retries, exit immediately if all retries fail.

**When to use:** For critical dependencies that must be available before application starts.

**Example:**
```go
// Source: https://pkg.go.dev/github.com/cenkalti/backoff/v5
// Source: https://pkg.go.dev/database/sql
func validateDatabaseConnection(ctx context.Context, db *sql.DB) error {
    operation := func() error {
        ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()

        if err := db.PingContext(ctx); err != nil {
            return fmt.Errorf("database ping failed: %w", err)
        }
        return nil
    }

    b := backoff.NewExponentialBackOff()
    b.MaxElapsedTime = 30 * time.Second // Total time before giving up

    notify := func(err error, d time.Duration) {
        slog.Debug("retrying database connection",
            "error", err,
            "retry_after", d)
    }

    if err := backoff.RetryNotify(operation, b, notify); err != nil {
        return fmt.Errorf("failed to connect to database after retries: %w", err)
    }

    slog.Debug("database connection validated")
    return nil
}
```

**Backoff defaults:**
- Initial interval: 500ms
- Multiplier: 1.5x
- Max interval: 60s
- Randomization: 0.5 (jitter to prevent thundering herd)

### Pattern 2: Connection Pool Configuration with Environment Variables

**What:** Configure connection pool limits via environment variables with struct tag defaults.

**When to use:** Always configure pool limits for production applications to prevent resource exhaustion.

**Example:**
```go
// Source: https://go.dev/doc/database/manage-connections
// Source: https://github.com/kelseyhightower/envconfig
type config struct {
    DBPath          string `envconfig:"DB_PATH" default:"downloads.db"`
    DBMaxOpenConns  int    `envconfig:"DB_MAX_OPEN_CONNS" default:"25"`
    DBMaxIdleConns  int    `envconfig:"DB_MAX_IDLE_CONNS" default:"25"`
    DBConnMaxLife   time.Duration `envconfig:"DB_CONN_MAX_LIFE" default:"5m"`
}

func configureConnectionPool(db *sql.DB, cfg *config) {
    // Validate configuration (MaxIdleConns <= MaxOpenConns)
    maxIdle := cfg.DBMaxIdleConns
    if maxIdle > cfg.DBMaxOpenConns {
        slog.Warn("invalid connection pool config",
            "max_idle", maxIdle,
            "max_open", cfg.DBMaxOpenConns,
            "action", "setting max_idle to max_open")
        maxIdle = cfg.DBMaxOpenConns
    }

    db.SetMaxOpenConns(cfg.DBMaxOpenConns)
    db.SetMaxIdleConns(maxIdle)
    db.SetConnMaxLifetime(cfg.DBConnMaxLife)

    slog.Debug("connection pool configured",
        "max_open", cfg.DBMaxOpenConns,
        "max_idle", maxIdle,
        "max_lifetime", cfg.DBConnMaxLife)
}
```

**Recommended defaults:**
- Small applications: `MaxOpenConns=25, MaxIdleConns=25`
- High-throughput services: `MaxOpenConns=50, MaxIdleConns=50`
- SQLite (single-writer): `MaxOpenConns=1` (to prevent SQLITE_BUSY errors)

### Pattern 3: Telemetry Status Logging

**What:** Log at Info level when telemetry is disabled, silent when enabled.

**When to use:** For optional observability features that operators need visibility into.

**Example:**
```go
// Source: https://pkg.go.dev/log/slog
func checkTelemetryStatus(cfg *config) {
    if cfg.Telemetry.OTELAddress == "" {
        slog.Info("telemetry disabled - metrics and traces will not be collected")
    }
    // Silent when enabled (normal case)
}
```

### Pattern 4: Handling Commented-Out Code

**What:** Decision matrix for commented-out code blocks.

**When to use:** When encountering large commented-out code blocks during refactoring.

**Decision tree:**
```
1. Is this code needed for current requirements?
   NO → Remove it (version control preserves history)

2. Is this temporary debugging/development code?
   YES → Remove it

3. Is this incomplete feature work?
   YES → Either:
      a) Complete it (if in scope)
      b) Remove it (defer to future work)
      c) Add TODO comment explaining why it's incomplete

4. Is this code being actively modified?
   YES → Temporarily acceptable during development
   NO → Remove it
```

**For this phase:** The commented-out recovery code (lines 96-122 in transfer.go) should be evaluated:
- If needed: Implement it properly
- If not needed: Remove it
- If uncertain: Document decision in code comment and create issue for future evaluation

### Anti-Patterns to Avoid

- **Skipping PingContext() after sql.Open()**: sql.Open() doesn't establish connection, just validates DSN. Errors only appear on first query.
- **Unlimited connection pool**: Default `MaxOpenConns=0` (unlimited) can exhaust database resources under load.
- **Using Ping() instead of PingContext()**: Can block indefinitely. Always use context with timeout.
- **Retrying forever**: Set max elapsed time for retries to avoid infinite loops during startup.
- **MaxIdleConns > MaxOpenConns**: Go will silently reduce MaxIdleConns to MaxOpenConns, but configuration is confusing.

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Retry with backoff | Manual sleep loop with exponential calculation | cenkalti/backoff/v5 | Already in project, handles jitter, max elapsed time, and edge cases |
| Environment variable parsing | Manual os.Getenv() with type conversion | kelseyhightower/envconfig | Already in project, handles defaults, type conversion, validation |
| Database timeouts | Manual goroutine with timer | context.WithTimeout() | Standard library, integrates with database/sql *Context methods |
| Connection pool management | Custom connection wrapper | database/sql built-in pooling | Handles connection lifecycle, idle management, and concurrency |

**Key insight:** Go's database/sql package has sophisticated connection pooling built-in. Custom solutions invariably miss edge cases like connection lifetime management, proper cleanup during panics, and race conditions.

## Common Pitfalls

### Pitfall 1: Not Calling PingContext() After sql.Open()

**What goes wrong:** Application starts successfully but fails on first database query. Error messages are unclear because they happen during business logic, not startup.

**Why it happens:** `sql.Open()` only validates DSN format without establishing connection. Developers assume it tests connectivity.

**How to avoid:** Always call `db.PingContext()` with timeout immediately after `sql.Open()`. Use fail-fast pattern at startup.

**Warning signs:**
- Database errors appearing in application logs after startup
- First request fails, subsequent requests work
- "Connection refused" errors during normal operation

### Pitfall 2: Using Query() for Non-SELECT Operations

**What goes wrong:** Connection pool exhaustion. Connections leak because rows aren't properly closed.

**Why it happens:** `db.Query()` returns rows that must be closed. Using it for INSERT/UPDATE/DELETE without closing rows leaks connections.

**How to avoid:**
- Use `db.Exec()` for INSERT/UPDATE/DELETE
- Use `db.Query()` only for SELECT
- Always defer `rows.Close()` immediately after checking error

**Warning signs:**
- "Too many connections" errors
- Application slows down over time
- Database shows idle connections from application

### Pitfall 3: Default Connection Pool in Production

**What goes wrong:**
- Unlimited connections exhaust database resources under load
- Only 2 idle connections (default) causes connection churn under burst traffic

**Why it happens:** Developers don't realize database/sql defaults aren't production-ready. Default `MaxOpenConns=0` (unlimited), `MaxIdleConns=2`.

**How to avoid:** Always explicitly configure:
```go
db.SetMaxOpenConns(25)  // Limit concurrent connections
db.SetMaxIdleConns(25)  // Keep connections warm
db.SetConnMaxLifetime(5*time.Minute)  // Refresh stale connections
```

**Warning signs:**
- Database "max connections exceeded" errors
- High connection establishment rate in metrics
- Database showing many short-lived connections

### Pitfall 4: Not Using Context for Database Operations

**What goes wrong:** Operations block indefinitely. No way to cancel long-running queries. Application hangs during shutdown.

**Why it happens:** Using `Ping()`, `Query()`, `Exec()` instead of `PingContext()`, `QueryContext()`, `ExecContext()`.

**How to avoid:** Always use *Context variants with appropriate timeout:
```go
ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
defer cancel()
err := db.PingContext(ctx)
```

**Warning signs:**
- Application hangs during shutdown
- Requests timeout but database operations continue
- No way to cancel slow queries

### Pitfall 5: Commented-Out Code Without Explanation

**What goes wrong:** Team doesn't know if code should be removed, implemented, or why it exists. Creates maintenance burden and confusion.

**Why it happens:** Developer comments out code "temporarily" but never removes or documents it. Common during debugging or when deferring work.

**How to avoid:**
- Remove commented code (version control preserves history)
- If keeping temporarily, add TODO comment with explanation and date
- During code review, question any commented-out code

**Warning signs:**
- Large blocks of commented code (>10 lines)
- No explanation comment above commented block
- Commented code older than 1 month

### Pitfall 6: Logging Telemetry Disabled as Warning

**What goes wrong:** Operators think something is wrong when telemetry is intentionally disabled. Warning level implies action needed.

**Why it happens:** Developers treat all "not enabled" states as warnings without considering operational context.

**How to avoid:** Use log levels appropriately:
- **Info**: Informational status (telemetry disabled is a choice, not a problem)
- **Warn**: Something unexpected that might need attention
- **Error**: Actual problems requiring action

**Warning signs:**
- Warning logs for expected states
- Operators investigating "warnings" that are intentional configurations
- Alert fatigue from non-actionable warnings

## Code Examples

Verified patterns from official sources:

### Complete Startup Validation

```go
// Source: https://go.dev/doc/database/manage-connections
// Source: https://pkg.go.dev/github.com/cenkalti/backoff/v5
// Source: https://betterstack.com/community/guides/logging/logging-in-go/

func initializeServices(ctx context.Context, cfg *config, tel *telemetry.Telemetry) (*services, error) {
    logger := slog.Default()

    // 1. Initialize database
    database, err := sqlite.InitDB(cfg.DBPath)
    if err != nil {
        return nil, fmt.Errorf("failed to initialize database: %w", err)
    }

    // 2. Configure connection pool BEFORE validation
    configureConnectionPool(database, cfg)

    // 3. Validate database connection with retries
    if err := validateDatabaseWithRetries(ctx, database, logger); err != nil {
        return nil, fmt.Errorf("database validation failed: %w", err)
    }

    // 4. Check telemetry status (after critical dependencies)
    checkTelemetryStatus(cfg, logger)

    // Continue with other service initialization...
    return &services{database: database}, nil
}

func validateDatabaseWithRetries(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
    operation := func() error {
        // Use timeout for each ping attempt
        pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()

        logger.Debug("validating database connection")

        if err := db.PingContext(pingCtx); err != nil {
            return fmt.Errorf("database ping failed: %w", err)
        }

        logger.Debug("database connection validated")
        return nil
    }

    // Configure exponential backoff
    b := backoff.NewExponentialBackOff()
    b.MaxElapsedTime = 30 * time.Second  // Give up after 30 seconds total

    // Log retry attempts at Debug level
    notify := func(err error, d time.Duration) {
        logger.Debug("retrying database connection",
            "error", err.Error(),
            "retry_after", d.String())
    }

    // Retry with notification
    if err := backoff.RetryNotify(operation, b, notify); err != nil {
        logger.Error("failed to connect to database after retries",
            "error", err,
            "max_elapsed_time", b.MaxElapsedTime)
        return err
    }

    return nil
}

func configureConnectionPool(db *sql.DB, cfg *config) {
    logger := slog.Default()

    // Validate MaxIdleConns <= MaxOpenConns
    maxIdle := cfg.DBMaxIdleConns
    if maxIdle > cfg.DBMaxOpenConns {
        logger.Warn("invalid connection pool configuration",
            "max_idle_conns", maxIdle,
            "max_open_conns", cfg.DBMaxOpenConns,
            "action", "reducing max_idle_conns to match max_open_conns")
        maxIdle = cfg.DBMaxOpenConns
    }

    db.SetMaxOpenConns(cfg.DBMaxOpenConns)
    db.SetMaxIdleConns(maxIdle)
    db.SetConnMaxLifetime(cfg.DBConnMaxLife)

    logger.Debug("connection pool configured",
        "max_open_conns", cfg.DBMaxOpenConns,
        "max_idle_conns", maxIdle,
        "conn_max_lifetime", cfg.DBConnMaxLife.String())
}

func checkTelemetryStatus(cfg *config, logger *slog.Logger) {
    // Only log when disabled (silent when enabled)
    if cfg.Telemetry.OTELAddress == "" {
        logger.Info("telemetry disabled - metrics and traces will not be collected")
    }
}
```

### Environment Variable Configuration

```go
// Source: https://github.com/kelseyhightower/envconfig

type config struct {
    // ... existing fields ...

    // Database configuration with defaults
    DBPath         string        `envconfig:"DB_PATH" default:"downloads.db"`
    DBMaxOpenConns int           `envconfig:"DB_MAX_OPEN_CONNS" default:"25"`
    DBMaxIdleConns int           `envconfig:"DB_MAX_IDLE_CONNS" default:"25"`
    DBConnMaxLife  time.Duration `envconfig:"DB_CONN_MAX_LIFE" default:"5m"`

    Telemetry struct {
        Enabled     bool   `split_words:"true" default:"true"`
        OTELAddress string `split_words:"true" default:""`  // Empty = disabled
        ServiceName string `split_words:"true" default:"seedbox_downloader"`
    }
}

func initializeConfig() (*config, *slog.Logger, error) {
    var cfg config

    // envconfig.Process handles:
    // - Reading environment variables
    // - Applying defaults from struct tags
    // - Type conversion (int, duration, bool, etc.)
    if err := envconfig.Process("", &cfg); err != nil {
        return nil, nil, fmt.Errorf("failed to load environment variables: %w", err)
    }

    // ... logger setup ...

    return &cfg, logger, nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| sql.Open() only | sql.Open() + PingContext() | Long-standing best practice | Fail-fast behavior at startup |
| Unlimited connections | Explicit SetMaxOpenConns() | Best practice since database/sql introduction | Prevents resource exhaustion |
| log/Printf | log/slog | Go 1.21 (2023) | Structured logging is now standard |
| Manual retry loops | backoff libraries | Community standard since ~2015 | Handles jitter, max elapsed time, edge cases |
| Ping() | PingContext() | Context added in Go 1.8 (2017) | Timeout control, cancellation support |

**Deprecated/outdated:**
- **golang.org/x/exp/slog**: Experimental package superseded by log/slog in Go 1.21. Use stdlib version.
- **SetMaxIdleConnsPerHost for http.Client**: Different from database connection pooling. Not relevant here.

## Open Questions

Things that couldn't be fully resolved:

1. **SQLite-specific connection pool limits**
   - What we know: SQLite has single-writer limitation. Some sources recommend `MaxOpenConns=1` for SQLite.
   - What's unclear: Project uses SQLite but current code doesn't set connection limits. Is write contention an issue?
   - Recommendation: Start with conservative defaults (`MaxOpenConns=5, MaxIdleConns=5`). SQLite can handle multiple readers. Monitor for SQLITE_BUSY errors.

2. **Commented-out recovery code decision**
   - What we know: Lines 96-122 in transfer.go contain commented-out transfer recovery logic
   - What's unclear: Why was it commented out? Is it incomplete, buggy, or no longer needed?
   - Recommendation: Review with context from git history and either implement, remove, or document decision

3. **Backoff retry count vs elapsed time**
   - What we know: Can configure by retry count (`WithMaxTries`) or elapsed time (`WithMaxElapsedTime`)
   - What's unclear: Which is better for database startup validation?
   - Recommendation: Use `MaxElapsedTime=30s` (time-based). More predictable startup behavior. Database either responds within 30s or something is seriously wrong.

## Sources

### Primary (HIGH confidence)
- [database/sql package - Go Packages](https://pkg.go.dev/database/sql) - Connection pool configuration, Ping methods
- [Managing connections - Go Official Docs](https://go.dev/doc/database/manage-connections) - Best practices for connection management
- [Opening a database handle - Go Official Docs](https://go.dev/doc/database/open-handle) - sql.Open() and PingContext() patterns
- [cenkalti/backoff/v5 - Go Packages](https://pkg.go.dev/github.com/cenkalti/backoff/v5) - Retry with exponential backoff
- [log/slog package - Go Packages](https://pkg.go.dev/log/slog) - Structured logging log levels
- [kelseyhightower/envconfig - GitHub](https://github.com/kelseyhightower/envconfig) - Environment variable parsing with defaults

### Secondary (MEDIUM confidence)
- [Configuring sql.DB for better performance – Alex Edwards](https://www.alexedwards.net/blog/configuring-sqldb) - Connection pool defaults and recommendations
- [Managing database timeouts and cancellations in Go – Alex Edwards](https://www.alexedwards.net/blog/how-to-manage-database-timeouts-and-cancellations-in-go) - Context usage with database operations
- [Logging in Go with Slog: The Ultimate Guide | Better Stack](https://betterstack.com/community/guides/logging/logging-in-go/) - slog best practices and patterns
- [Common Pitfalls When Using database/sql in Go - SolarWinds](https://www.solarwinds.com/blog/common-pitfalls-when-using-database-sql-in-go) - Connection pool mistakes
- [Understanding Go and Databases at Scale: Connection Pooling | KOHO Tech](https://koho.dev/understanding-go-and-databases-at-scale-connection-pooling-f301e56fa73) - Connection pool configuration at scale
- [Go Wiki: Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) - Commented code and TODO patterns
- [Structured Logging with slog - Go Blog](https://go.dev/blog/slog) - Official slog announcement and patterns

### Tertiary (LOW confidence)
- Various Medium articles on connection pooling - General patterns match official docs, but not authoritative

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - All libraries are either stdlib or already in project (go.mod verified)
- Architecture: HIGH - Patterns verified from official Go documentation and package docs
- Pitfalls: HIGH - Common issues documented in official Go wiki and authoritative community sources
- Code examples: HIGH - All examples based on official documentation patterns

**Research date:** 2026-01-31
**Valid until:** 2026-03-31 (60 days - stable domain, stdlib doesn't change frequently)

**Notes:**
- All dependencies already present in go.mod (no new dependencies required)
- Patterns align with existing codebase patterns (slog, envconfig already in use)
- Context.md decisions incorporated (retry with backoff, env var defaults, Info level for telemetry)
