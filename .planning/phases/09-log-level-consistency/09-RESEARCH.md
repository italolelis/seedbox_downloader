# Phase 9: Log Level Consistency - Research

**Researched:** 2026-02-08
**Domain:** Log level selection and consistency in Go applications using slog
**Confidence:** HIGH

## Summary

Log level consistency ensures that log messages are categorized by severity appropriately across all application components, making it possible to filter logs effectively in production and reduce noise while maintaining visibility into important events. This phase focuses on auditing and correcting log levels throughout the codebase to match established conventions.

The standard approach follows the well-established log level hierarchy: ERROR for failed operations requiring attention, WARN for recovered or degraded conditions, INFO for significant business events and lifecycle milestones, and DEBUG for detailed diagnostic information. The key insight for this phase is that repetitive operations (polling ticks, per-file progress) should be at DEBUG level, while only meaningful state changes (transfer discovered, download started, batch completed) should be at INFO level. This "silent when nothing happens" principle drastically reduces log noise in production.

Based on codebase analysis, several logging statements need level adjustment. The transfer orchestrator currently logs "watching transfers" and "active transfers" at INFO during every polling tick, even when no new transfers are found. Per-file download progress is correctly at DEBUG, but some per-file operations log at INFO. The goal is: INFO when work happens, DEBUG for routine checks, WARN for recovered errors, ERROR for failures.

**Primary recommendation:** Audit all logging calls and apply consistent level rules: (1) Lifecycle events at INFO, (2) Transfer-level operations at INFO, (3) Per-file operations at DEBUG, (4) Polling without results at DEBUG, (5) Retries/recovery at WARN, (6) Failures at ERROR.

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| log/slog | stdlib (Go 1.21+) | Structured logging with levels | Standard library, already in use, provides DEBUG/INFO/WARN/ERROR levels with integer values allowing custom intermediate levels |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| logctx | internal | Context-aware logger extraction | Already in use for trace correlation, provides consistent logger access |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Manual level audit | Linting rules for log levels | Linters can't understand semantic intent; manual audit needed |
| Per-message level adjustment | Global log filtering | Filtering hides messages but doesn't fix semantic incorrectness |

**Installation:**
No additional dependencies required - uses existing stdlib slog package.

## Architecture Patterns

### Recommended Log Level Assignment

```
Level Hierarchy (slog integer values):
  DEBUG (-4) - Detailed diagnostic info, OFF in production
  INFO   (0) - Significant events, business milestones
  WARN   (4) - Recovered errors, degraded conditions
  ERROR  (8) - Failed operations requiring attention
```

### Pattern 1: Decision Tree for Level Selection

**What:** Systematic approach to choosing the right log level for any message.
**When to use:** When adding new logging or auditing existing logs.
**Decision Flow:**

```
Is the application unable to complete the requested operation?
  YES -> ERROR
  NO  -> Is something unusual or potentially problematic but handled?
         YES -> WARN
         NO  -> Is this a significant business event or lifecycle milestone?
               YES -> INFO
               NO  -> DEBUG
```

### Pattern 2: Polling Loop Logging (Silent When Idle)

**What:** Log polling activity only when meaningful work occurs, not every tick.
**When to use:** Any polling loop that checks for work periodically.

**Example:**
```go
// Source: Best practice from https://oneuptime.com/blog/post/2026-01-30-logging-level-guidelines/view

func (o *TransferOrchestrator) watchTransfers(ctx context.Context) error {
    logger := logctx.LoggerFromContext(ctx)

    // DEBUG: Routine check - only visible when DEBUG enabled
    logger.DebugContext(ctx, "polling for transfers", "label", o.label)

    transfers, err := o.dc.GetTaggedTorrents(ctx, o.label)
    if err != nil {
        return fmt.Errorf("failed to get tagged torrents: %w", err)
    }

    // Only log at INFO when there are transfers to process
    if len(transfers) > 0 {
        logger.InfoContext(ctx, "transfers found", "count", len(transfers))
    } else {
        // DEBUG: Nothing found - silent in production
        logger.DebugContext(ctx, "no transfers found")
    }

    for _, transfer := range transfers {
        // ... process transfers
    }

    return nil
}
```

### Pattern 3: Multi-File Operations (Transfer-Level vs File-Level)

**What:** Log batch operations at INFO, individual item operations at DEBUG.
**When to use:** Downloads with multiple files, batch processing.

**Example:**
```go
// Source: https://betterstack.com/community/guides/logging/log-levels-explained/

// Transfer level - INFO (significant business event)
logger.InfoContext(ctx, "starting download",
    "transfer_id", transfer.ID,
    "transfer_name", transfer.Name,
    "file_count", len(transfer.Files),
    "total_size", humanize.Bytes(uint64(transfer.Size)))

// File level - DEBUG (implementation detail)
for _, file := range transfer.Files {
    logger.DebugContext(ctx, "downloading file",
        "transfer_id", transfer.ID,
        "file_path", file.Path,
        "file_size", humanize.Bytes(uint64(file.Size)))

    // ... download file ...

    logger.DebugContext(ctx, "file downloaded",
        "transfer_id", transfer.ID,
        "file_path", file.Path)
}

// Transfer completion - INFO (significant milestone)
logger.InfoContext(ctx, "download completed",
    "transfer_id", transfer.ID,
    "transfer_name", transfer.Name,
    "files_downloaded", downloadedCount,
    "duration_ms", duration.Milliseconds())
```

### Pattern 4: Error Classification (ERROR vs WARN)

**What:** Use ERROR for failures, WARN for recovered/handled issues.
**When to use:** Any error handling code path.

**Example:**
```go
// Source: https://edgedelta.com/company/blog/log-debug-vs-info-vs-warn-vs-error-and-fatal

// WARN: Retry succeeded - system recovered
func retryableOperation(ctx context.Context) error {
    logger := logctx.LoggerFromContext(ctx)

    var lastErr error
    for attempt := 1; attempt <= maxRetries; attempt++ {
        err := doOperation(ctx)
        if err == nil {
            if attempt > 1 {
                // WARN: We had to retry, but succeeded
                logger.WarnContext(ctx, "operation succeeded after retry",
                    "attempts", attempt,
                    "last_error", lastErr.Error())
            }
            return nil
        }
        lastErr = err
        logger.DebugContext(ctx, "operation failed, retrying",
            "attempt", attempt,
            "max_retries", maxRetries,
            "err", err)
    }

    // ERROR: All retries exhausted - operation failed
    logger.ErrorContext(ctx, "operation failed after retries",
        "attempts", maxRetries,
        "err", lastErr)
    return lastErr
}

// ERROR: Unrecoverable failure
func criticalOperation(ctx context.Context) error {
    logger := logctx.LoggerFromContext(ctx)

    if err := doSomethingCritical(ctx); err != nil {
        logger.ErrorContext(ctx, "critical operation failed",
            "operation", "do_something_critical",
            "err", err)
        return err
    }
    return nil
}
```

### Anti-Patterns to Avoid

- **INFO flooding:** Logging every polling tick at INFO creates noise; use DEBUG for routine checks
- **ERROR for validation:** Business validation failures should be DEBUG/WARN, not ERROR
- **DEBUG in production:** Keep DEBUG disabled in production; only enable temporarily for troubleshooting
- **Per-file INFO:** Don't log each file in a multi-file download at INFO; use DEBUG for files, INFO for transfer
- **Silent errors:** Never swallow errors without logging; at minimum log at DEBUG
- **Inconsistent levels:** Same event type logging at different levels in different components

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Log level filtering | Custom per-message filters | slog Handler level filtering | Built into slog, consistent across codebase |
| Conditional logging | `if logLevel >= DEBUG` checks | `logger.Enabled(ctx, slog.LevelDebug)` | Official API, handles handler chain correctly |
| Level-specific formatting | Custom formatters per level | Standard JSONHandler | Consistent output, integrates with log aggregation |

**Key insight:** Log level selection is about semantic meaning, not technical filtering. Choose the level that accurately represents the event's significance.

## Common Pitfalls

### Pitfall 1: Logging Polling Ticks at INFO

**What goes wrong:** Every 10-minute polling interval generates INFO logs even when nothing happens, creating hundreds of noisy entries per day that obscure meaningful events.

**Why it happens:** Developer logs "checking for transfers" at INFO because it seems important, not realizing it runs continuously.

**How to avoid:**
- Log "polling for X" at DEBUG level
- Log "found X" at INFO only when count > 0
- Log "no X found" at DEBUG level

**Warning signs:**
- Logs dominated by repetitive "checking" or "polling" messages
- Grep for actual events (downloads, errors) buried in noise
- Log volume directly proportional to polling interval setting

**Current codebase locations:**
- `internal/transfer/transfer.go:140` - "watching transfers" at INFO on every tick
- `internal/transfer/transfer.go:147` - "active transfers" at INFO even when 0

### Pitfall 2: Per-File Logging at INFO for Multi-File Torrents

**What goes wrong:** A 500-file torrent generates 500 INFO-level logs for individual files, making it impossible to see transfer-level events.

**Why it happens:** Each file download feels significant during development; multiplied across many files creates explosion.

**How to avoid:**
- Transfer start/complete: INFO
- Per-file start/complete: DEBUG
- Per-file errors: ERROR (individual files matter for errors)

**Warning signs:**
- Large torrents produce hundreds of INFO lines
- Transfer completion buried in per-file messages
- Log aggregation dashboards show file-level metrics dominating

**Current codebase locations:**
- `internal/downloader/downloader.go:179` - "downloaded and saved file" at INFO for each file
- `internal/downloader/downloader.go:325` - "downloading file" at INFO for each file

### Pitfall 3: Using ERROR for Expected Conditions

**What goes wrong:** "File already downloaded" logged at ERROR triggers alerts and clutters error monitoring, even though it's expected behavior.

**Why it happens:** Error handling code path automatically uses ERROR without considering whether condition is exceptional.

**How to avoid:**
- Already downloaded/claimed: DEBUG (expected skip)
- Validation failure: DEBUG/WARN (expected input issue)
- Retry needed: WARN (handled but notable)
- Operation failed: ERROR (requires attention)

**Warning signs:**
- Error monitoring alerts for non-errors
- ERROR count high despite system working correctly
- Developers ignoring ERROR logs due to noise

**Current codebase:** Generally good - "already downloaded" is at DEBUG level (line 129), but should verify all error paths.

### Pitfall 4: Inconsistent Levels Across Components

**What goes wrong:** Same event type (authentication success) logged at INFO in one client and DEBUG in another, creating inconsistent log output.

**Why it happens:** Different developers or different times; no central guidelines followed.

**How to avoid:**
- Document level guidelines for event categories
- Review logs holistically across components
- Use consistent patterns (lifecycle = INFO, routine = DEBUG)

**Warning signs:**
- Same filter showing different events from different components
- Log analysis requires checking multiple levels
- New developers confused about which level to use

**Current codebase locations:**
- Deluge: "authenticated with deluge" at DEBUG (line 147)
- Put.io: "authenticated with Put.io" at INFO (line 144)
- Should be consistent: authentication success = INFO

### Pitfall 5: Missing WARN Level Usage

**What goes wrong:** Events that are concerning but handled go straight to ERROR or disappear at DEBUG, losing the middle ground of "notable but not critical."

**Why it happens:** Developers default to ERROR/INFO binary; WARN feels uncertain.

**How to avoid:**
- Retries that eventually succeed: WARN
- Degraded performance: WARN
- Approaching limits: WARN
- Deprecated usage: WARN

**Warning signs:**
- Very few WARN entries in logs
- ERROR mixed with recoverable issues
- No early warning before ERROR conditions

**Current codebase:** Only 1 WARN usage found ("transfer download error"). Good candidate for WARN: retry situations, slow operations, unexpected but handled states.

## Code Examples

Verified patterns from official sources and current codebase:

### Log Level Audit - Transfer Orchestrator

Current (needs change):
```go
// internal/transfer/transfer.go - current implementation
func (o *TransferOrchestrator) watchTransfers(ctx context.Context) error {
    logger := logctx.LoggerFromContext(ctx)

    // PROBLEM: Logs at INFO on every polling tick, even when nothing found
    logger.InfoContext(ctx, "watching transfers", "label", o.label)

    transfers, err := o.dc.GetTaggedTorrents(ctx, o.label)
    if err != nil {
        return fmt.Errorf("failed to get tagged torrents: %w", err)
    }

    // PROBLEM: Logs transfer count at INFO even when 0
    logger.InfoContext(ctx, "active transfers", "transfer_count", len(transfers))
    // ...
}
```

Corrected:
```go
// Source: Pattern from https://oneuptime.com/blog/post/2026-01-30-logging-level-guidelines/view
func (o *TransferOrchestrator) watchTransfers(ctx context.Context) error {
    logger := logctx.LoggerFromContext(ctx)

    // DEBUG: Routine polling check
    logger.DebugContext(ctx, "polling for transfers", "label", o.label)

    transfers, err := o.dc.GetTaggedTorrents(ctx, o.label)
    if err != nil {
        return fmt.Errorf("failed to get tagged torrents: %w", err)
    }

    // INFO only when work found; DEBUG when idle
    if len(transfers) > 0 {
        logger.InfoContext(ctx, "transfers found", "count", len(transfers))
    } else {
        logger.DebugContext(ctx, "no transfers found")
    }
    // ...
}
```

### Log Level Audit - Downloader (Multi-File)

Current (needs change):
```go
// internal/downloader/downloader.go - current implementation

// Per-file logging at INFO (should be DEBUG for files)
func (d *Downloader) writeFile(ctx context.Context, ...) error {
    logger := logctx.LoggerFromContext(ctx)

    // PROBLEM: Logs at INFO for every file
    logger.InfoContext(ctx, "downloading file", "file_path", targetPath, ...)
    // ...
}

func (d *Downloader) DownloadFile(ctx context.Context, ...) error {
    // ...
    // PROBLEM: Logs at INFO for every file
    logger.InfoContext(ctx, "downloaded and saved file", "target", targetPath)
    // ...
}
```

Corrected:
```go
// Source: Pattern from https://betterstack.com/community/guides/logging/log-levels-explained/

// Per-file at DEBUG level
func (d *Downloader) writeFile(ctx context.Context, ...) error {
    logger := logctx.LoggerFromContext(ctx)

    // DEBUG: Per-file detail
    logger.DebugContext(ctx, "downloading file", "file_path", targetPath, ...)
    // ...
}

func (d *Downloader) DownloadFile(ctx context.Context, ...) error {
    // ...
    // DEBUG: Per-file completion
    logger.DebugContext(ctx, "file downloaded", "target", targetPath)
    // ...
}

// Transfer-level stays at INFO
func (d *Downloader) DownloadTransfer(ctx context.Context, transfer *transfer.Transfer) (int, error) {
    logger := logctx.LoggerFromContext(ctx)

    // INFO: Transfer-level start (already correct via calling code)
    // After completion:
    if downloadedFiles > 0 {
        // INFO: Transfer-level completion (correct level)
        logger.InfoContext(ctx, "transfer download completed",
            "transfer_id", transfer.ID,
            "files_downloaded", downloadedFiles)
    }
    // ...
}
```

### Log Level Audit - Client Authentication Consistency

Current (inconsistent):
```go
// internal/dc/deluge/client.go
func (c *Client) Authenticate(ctx context.Context) error {
    // ...
    // DEBUG for success (inconsistent with Put.io)
    logger.DebugContext(ctx, "authenticated with deluge")
    return nil
}

// internal/dc/putio/client.go
func (c *Client) Authenticate(ctx context.Context) error {
    // ...
    // INFO for success
    logger.InfoContext(ctx, "authenticated with Put.io", "user", user.Username)
    return nil
}
```

Corrected (consistent INFO for lifecycle events):
```go
// Source: Phase 8 pattern - lifecycle events at INFO

// Both clients should use INFO for authentication success (lifecycle event)
// internal/dc/deluge/client.go
func (c *Client) Authenticate(ctx context.Context) error {
    // ...
    logger.InfoContext(ctx, "authenticated with deluge", "username", c.Username)
    return nil
}

// internal/dc/putio/client.go - already correct
func (c *Client) Authenticate(ctx context.Context) error {
    // ...
    logger.InfoContext(ctx, "authenticated with Put.io", "user", user.Username)
    return nil
}
```

### Level Classification Reference

```go
// Source: Composite from multiple sources

// =============================================================================
// ERROR: Failed operations requiring attention
// =============================================================================
logger.ErrorContext(ctx, "failed to download transfer", "err", err)
logger.ErrorContext(ctx, "database connection failed", "err", err)
logger.ErrorContext(ctx, "panic recovered", "panic", r, "stack", stack)

// =============================================================================
// WARN: Recovered/handled issues, degraded conditions
// =============================================================================
logger.WarnContext(ctx, "operation succeeded after retry", "attempts", n, "last_error", err)
logger.WarnContext(ctx, "transfer download error", "transfer_id", id) // recoverable
logger.WarnContext(ctx, "slow operation detected", "duration_ms", dur)

// =============================================================================
// INFO: Significant business events, lifecycle milestones
// =============================================================================
logger.InfoContext(ctx, "service ready", "bind_address", addr)
logger.InfoContext(ctx, "transfer discovered", "transfer_id", id, "name", name)
logger.InfoContext(ctx, "download completed", "transfer_id", id, "files", count)
logger.InfoContext(ctx, "authenticated with client", "client_type", typ)

// =============================================================================
// DEBUG: Implementation details, routine operations
// =============================================================================
logger.DebugContext(ctx, "polling for transfers")
logger.DebugContext(ctx, "no transfers found")
logger.DebugContext(ctx, "downloading file", "path", path)
logger.DebugContext(ctx, "file downloaded", "path", path)
logger.DebugContext(ctx, "download progress", "percent", pct)
logger.DebugContext(ctx, "skipping transfer, already claimed")
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| All INFO | Level-appropriate logging | 2023+ (observability focus) | Meaningful filtering, reduced noise |
| Every operation INFO | Significant events INFO only | 2024+ (production experience) | 10x reduction in log volume |
| No WARN usage | WARN for recovered issues | 2023+ (monitoring integration) | Early warning signals |
| Per-item INFO in batches | Batch-level INFO, item-level DEBUG | 2024+ (multi-file downloads) | Usable logs for large operations |
| Polling INFO | Polling DEBUG, results INFO | 2025+ (service mesh era) | Silent idle, visible activity |

**Deprecated/outdated:**
- **Verbose INFO:** Logging every operation at INFO considered anti-pattern
- **Binary ERROR/INFO:** Missing WARN loses nuance
- **Per-file INFO:** Unusable for large batches

## Open Questions

Things that couldn't be fully resolved:

1. **What constitutes "slow operation" worthy of WARN?**
   - What we know: Slow operations indicate potential issues
   - What's unclear: Threshold values specific to this application
   - Recommendation: Start with 10x expected duration (e.g., download timeout, API timeout), tune based on observation

2. **Should skipped transfers log at INFO or DEBUG?**
   - What we know: "Already downloaded" is DEBUG. "Not available" and "not downloadable" are filtering conditions.
   - What's unclear: Operator may want visibility into why transfers skipped
   - Recommendation: Keep at DEBUG; operators can enable DEBUG if investigating skip reasons

3. **Should "no transfers found" log at all?**
   - What we know: Silent when idle is goal. But complete silence makes it hard to verify polling is working.
   - What's unclear: Whether DEBUG is too verbose even for DEBUG level
   - Recommendation: Log at DEBUG level; provides verification when DEBUG enabled without production noise

## Sources

### Primary (HIGH confidence)
- [Go slog package documentation](https://pkg.go.dev/log/slog) - Official level definitions (DEBUG=-4, INFO=0, WARN=4, ERROR=8)
- [Structured Logging with slog - The Go Programming Language](https://go.dev/blog/slog) - Official slog blog post
- [How to Create Log Level Guidelines](https://oneuptime.com/blog/post/2026-01-30-logging-level-guidelines/view) - 2026 comprehensive level guidelines

### Secondary (MEDIUM confidence)
- [Log Levels Explained and How to Use Them | Better Stack Community](https://betterstack.com/community/guides/logging/log-levels-explained/) - Decision tree and examples
- [Log Debug vs. Info vs. Warn vs. Error](https://edgedelta.com/company/blog/log-debug-vs-info-vs-warn-vs-error-and-fatal) - Level comparison
- [Logging Levels: What They Are & How to Choose Them - Sematext](https://sematext.com/blog/logging-levels/) - Practical guidelines
- [9 Logging Best Practices You Should Know - Dash0](https://www.dash0.com/guides/logging-best-practices) - Batch and multi-file patterns

### Tertiary (LOW confidence)
- [Logging Best Practices - Medium](https://brennonloveless.medium.com/logging-best-practices-82da864c6f22) - General guidance
- Current codebase analysis - Identified specific locations needing changes

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Uses stdlib slog already in project, no new dependencies
- Architecture: HIGH - Log level semantics well-established across industry
- Pitfalls: HIGH - Based on actual codebase analysis and documented anti-patterns
- Code examples: HIGH - Derived from current codebase with specific line references

**Research date:** 2026-02-08
**Valid until:** 2026-08-08 (180 days - stable domain, conventions rarely change)

## Codebase Audit Summary

### Files Requiring Level Changes

| File | Line | Current Level | Should Be | Message/Context |
|------|------|---------------|-----------|-----------------|
| `internal/transfer/transfer.go` | 140 | INFO | DEBUG | "watching transfers" (every polling tick) |
| `internal/transfer/transfer.go` | 147 | INFO | Conditional | "active transfers" (INFO if >0, else DEBUG) |
| `internal/downloader/downloader.go` | 179 | INFO | DEBUG | "downloaded and saved file" (per-file) |
| `internal/downloader/downloader.go` | 325 | INFO | DEBUG | "downloading file" (per-file) |
| `internal/dc/deluge/client.go` | 147 | DEBUG | INFO | "authenticated with deluge" (lifecycle) |

### Files Correctly Using Levels (No Changes Needed)

| File | Pattern | Assessment |
|------|---------|------------|
| `cmd/seedbox_downloader/main.go` | Lifecycle at INFO, errors at ERROR | Correct (Phase 8) |
| `internal/downloader/downloader.go:330-336` | Progress at DEBUG | Correct |
| `internal/downloader/downloader.go:129` | "already downloaded" at DEBUG | Correct |
| `internal/transfer/transfer.go:153,161,170` | Skip conditions at DEBUG | Correct |
| `internal/dc/putio/client.go:144` | Authentication at INFO | Correct |

### Files Needing Additional Logging

| File | Location | Missing Log | Level |
|------|----------|-------------|-------|
| `internal/downloader/downloader.go` | DownloadTransfer start | Transfer-level "starting download" | INFO |
