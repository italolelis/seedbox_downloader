# Phase 2: Resource Leak Prevention - Research

**Researched:** 2026-01-31
**Domain:** Go goroutine lifecycle management, ticker cleanup, panic recovery
**Confidence:** HIGH

## Summary

This phase prevents resource leaks by ensuring proper ticker cleanup in long-running goroutines. The codebase has three goroutines with `time.Ticker` instances that need cleanup on all exit paths:
- TransferOrchestrator.ProduceTransfers() - polling for new transfers
- Downloader.WatchForImported() - polling for import completion
- Downloader.WatchForSeeding() - polling for seeding status
- main.go notification loop - processing download events

The standard Go pattern is `defer ticker.Stop()` immediately after creating the ticker. While Go 1.23+ automatically garbage collects unreferenced tickers, explicit `Stop()` calls remain best practice for deterministic resource cleanup. For panic recovery, the pattern is to defer a recovery function at goroutine start that logs with stack traces and optionally restarts the goroutine with clean state. Exit scenarios should be logged with structured context to aid debugging.

The codebase already uses `log/slog` for structured logging and follows consistent patterns with `logctx.LoggerFromContext(ctx)`. Goroutines use context cancellation via `<-ctx.Done()` for graceful shutdown.

**Primary recommendation:** Add `defer ticker.Stop()` immediately after `time.NewTicker()` in all goroutines, wrap goroutine bodies with deferred panic recovery, and log all exit scenarios (context cancellation, normal completion, panics) with structured fields.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| time | stdlib (go 1.23) | Ticker lifecycle management | Standard library, ticker.Stop() releases resources |
| context | stdlib | Goroutine cancellation | Standard pattern for signaling goroutine shutdown |
| log/slog | stdlib (go 1.23) | Structured logging | Already used throughout codebase for consistent logging |
| runtime/debug | stdlib | Stack traces for panics | Standard library for capturing stack traces in panic recovery |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| github.com/italolelis/seedbox_downloader/internal/logctx | internal | Context-aware logging | Retrieve logger from context: `logctx.LoggerFromContext(ctx)` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| defer ticker.Stop() | Rely on Go 1.23+ GC | GC handles unreferenced tickers automatically, but explicit Stop() provides deterministic cleanup and documents intent |
| Manual panic recovery | errgroup with wrapper | errgroup pattern good for task-based concurrency, but these are long-running monitoring loops that need in-place recovery |
| Context-aware ticker library | github.com/tkennon/ticker | Adds dependency for minimal benefit - standard select with ctx.Done() is clear and sufficient |

**Installation:**
No new dependencies required - everything needed is in stdlib.

## Architecture Patterns

### Pattern 1: Ticker Cleanup with Defer
**What:** Ensure ticker.Stop() is called on all exit paths
**When to use:** Every goroutine that creates a time.Ticker
**Example:**
```go
// Source: https://pkg.go.dev/time and https://dev.to/serifcolakel/go-concurrency-mastery-preventing-goroutine-leaks-with-context-timeout-cancellation-best-1lg0
go func() {
    ticker := time.NewTicker(pollingInterval)
    defer ticker.Stop() // Cleanup on all exit paths

    for {
        select {
        case <-ctx.Done():
            logger.Info("shutting down")
            return
        case <-ticker.C:
            // Do work
        }
    }
}()
```

**Key points:**
- `defer ticker.Stop()` must be immediately after `ticker := time.NewTicker()`
- Works for all exit paths: return, context cancellation, panic (if recovered)
- Calling Stop() is safe even if ticker already stopped
- Stop() does NOT close the ticker.C channel (by design - prevents spurious ticks on concurrent readers)

### Pattern 2: Panic Recovery in Goroutines
**What:** Recover from panics to prevent crash and enable cleanup
**When to use:** Long-running goroutines that should survive unexpected panics
**Example:**
```go
// Source: https://www.dolthub.com/blog/2026-01-09-golang-panic-recovery/ and https://go.dev/blog/defer-panic-and-recover
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Error("goroutine panic recovered",
                "panic", r,
                "stack", string(debug.Stack()))
            // Optional: Restart goroutine with clean state
        }
    }()

    ticker := time.NewTicker(interval)
    defer ticker.Stop() // Executes even after panic

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Work that might panic
        }
    }
}()
```

**Key points:**
- Panic recovery MUST be in deferred function in the same goroutine
- Panics do NOT cross goroutine boundaries
- Deferred functions execute during panic unwinding (cleanup happens)
- Log panic with stack trace using `debug.Stack()`
- Recovery converts panic into controlled shutdown or restart

### Pattern 3: Exit Scenario Logging
**What:** Log all goroutine exit scenarios with structured context
**When to use:** All long-running goroutines for observability
**Example:**
```go
// Source: Existing codebase patterns from Phase 1 and https://betterstack.com/community/guides/logging/golang-contextual-logging/
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Error("goroutine panic",
                "operation", "watch_transfers",
                "panic", r,
                "stack", string(debug.Stack()))
        }
    }()

    ticker := time.NewTicker(pollingInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            logger.Info("goroutine shutdown",
                "operation", "watch_transfers",
                "reason", "context_cancelled")
            return
        case <-ticker.C:
            if err := doWork(ctx); err != nil {
                logger.Error("work failed",
                    "operation", "watch_transfers",
                    "err", err)
                continue
            }
        }
    }
}()
```

**Key points:**
- Use structured fields: "operation", "reason", "err", "panic", "stack"
- Consistent with Phase 1 logging patterns
- "operation" field identifies which goroutine/function
- "reason" field explains why goroutine exited
- Context cancellation is expected exit, log at Info level
- Panics are unexpected, log at Error level with stack trace

### Pattern 4: Multiple Tickers Cleanup
**What:** When goroutine has multiple tickers, ensure all are stopped
**When to use:** Goroutines with multiple time.Ticker instances
**Example:**
```go
go func() {
    ticker1 := time.NewTicker(interval1)
    ticker2 := time.NewTicker(interval2)

    defer func() {
        ticker1.Stop()
        ticker2.Stop()
    }()

    // Or use named cleanup function for clarity:
    // cleanupTickers := func() {
    //     ticker1.Stop()
    //     ticker2.Stop()
    // }
    // defer cleanupTickers()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker1.C:
            // Handle ticker1
        case <-ticker2.C:
            // Handle ticker2
        }
    }
}()
```

**Note:** Current codebase has only single tickers per goroutine, but pattern documented for completeness.

### Pattern 5: Goroutine Restart After Panic
**What:** Restart goroutine with clean state after panic recovery
**When to use:** Critical long-running services that must remain operational
**Example:**
```go
// Source: https://groups.google.com/g/golang-nuts/c/iSZcILsry1U and https://www.dolthub.com/blog/2026-01-09-golang-panic-recovery/
func startWatcher(ctx context.Context, name string, interval time.Duration, workFn func(context.Context) error) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                logger.Error("watcher panic, restarting",
                    "watcher", name,
                    "panic", r,
                    "stack", string(debug.Stack()))

                // Restart with clean state
                time.Sleep(time.Second) // Brief delay before restart
                startWatcher(ctx, name, interval, workFn)
            }
        }()

        ticker := time.NewTicker(interval)
        defer ticker.Stop()

        logger.Info("watcher started", "watcher", name)

        for {
            select {
            case <-ctx.Done():
                logger.Info("watcher shutdown", "watcher", name)
                return
            case <-ticker.C:
                if err := workFn(ctx); err != nil {
                    logger.Error("watcher work failed",
                        "watcher", name,
                        "err", err)
                }
            }
        }
    }()
}
```

**Key points:**
- Recursive call to restart function spawns new goroutine
- Brief sleep before restart prevents tight panic loops
- New goroutine has clean stack and ticker instance
- Log restart event for observability
- Only restart if not context cancelled (check ctx.Err())

### Anti-Patterns to Avoid
- **Manual ticker cleanup in select cases:** Don't call `ticker.Stop()` in ctx.Done case - use defer instead to cover all exit paths
- **Missing panic recovery in long-running goroutines:** Unrecovered panics crash the entire program
- **Restarting without checking context:** Don't restart if context is cancelled (service is shutting down)
- **Reusing ticker after Stop():** Create new ticker for restart, don't reuse stopped ticker
- **Suppressing panic without logging:** Always log panics with stack traces for debugging

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Stack traces for panics | String formatting of panic value | runtime/debug.Stack() | Captures full call stack, formatted for readability |
| Goroutine cleanup coordination | Manual channel orchestration | defer statements | Executes on all exit paths including panic |
| Restart backoff timing | Custom sleep logic | Simple time.Sleep(duration) + exponential backoff if needed | For single restart after panic, simple is sufficient |
| Detecting context cancellation in panic recovery | Parsing error messages | errors.Is(ctx.Err(), context.Canceled) | Type-safe, works with wrapped errors |

**Key insight:** Go's defer mechanism guarantees cleanup execution even during panics. The runtime/debug package provides production-ready stack traces. Don't build custom panic handling infrastructure - use these built-in primitives.

## Common Pitfalls

### Pitfall 1: Ticker Cleanup Only in ctx.Done Case
**What goes wrong:** Ticker cleanup placed only in `case <-ctx.Done():` branch, not as defer
**Why it happens:** Developer thinks "I'll stop the ticker when context is cancelled"
**How to avoid:** Always use `defer ticker.Stop()` immediately after `time.NewTicker()` to cover ALL exit paths (return, panic, break from nested loops)
**Warning signs:**
- `ticker.Stop()` appears inside select case block
- No defer statement after ticker creation
- Explicit cleanup logic in multiple exit paths

**Example - WRONG:**
```go
go func() {
    ticker := time.NewTicker(interval)

    for {
        select {
        case <-ctx.Done():
            ticker.Stop() // WRONG: Only handles this exit path
            return
        case <-ticker.C:
            doWork()
        }
    }
}()
```

**Example - CORRECT:**
```go
go func() {
    ticker := time.NewTicker(interval)
    defer ticker.Stop() // RIGHT: Handles all exit paths

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            doWork()
        }
    }
}()
```

### Pitfall 2: Panic Recovery in Wrong Goroutine
**What goes wrong:** Panic recovery in parent goroutine instead of spawned goroutine
**Why it happens:** Misunderstanding of goroutine isolation - panics don't cross boundaries
**How to avoid:** Always place `defer func() { recover() }()` at the start of the spawned goroutine, not in the spawning function
**Warning signs:**
- Recovery logic in function that calls `go func()` instead of inside the `go func()`
- Expectation that parent can catch child goroutine panics

**Example - WRONG:**
```go
func startWorker(ctx context.Context) {
    defer func() {
        if r := recover(); r != nil {
            // WRONG: This only catches panics in startWorker, not in the goroutine
        }
    }()

    go func() {
        // Panics here are NOT caught by parent's recovery
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        // ...
    }()
}
```

**Example - CORRECT:**
```go
func startWorker(ctx context.Context) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                // RIGHT: Recovery in the same goroutine
            }
        }()

        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        // ...
    }()
}
```

### Pitfall 3: Defer Order Confusion
**What goes wrong:** Ticker cleanup deferred after panic recovery, causing cleanup to not execute during panic
**Why it happens:** Misunderstanding LIFO (Last In First Out) defer execution order
**How to avoid:** Order matters - defer panic recovery FIRST, then defer ticker cleanup
**Warning signs:**
- Ticker cleanup happens before panic recovery can execute
- Cleanup not happening during panics

**Correct order:**
```go
go func() {
    // Defer 2 (executes FIRST during unwind)
    defer func() {
        if r := recover(); r != nil {
            logger.Error("panic", "panic", r)
        }
    }()

    // Defer 1 (executes SECOND during unwind)
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    // Work...
}()
```

**Why this order:** During panic, defers execute LIFO. Ticker cleanup (defer 1) runs first, THEN panic recovery (defer 2). This ensures ticker is stopped before we log/restart.

### Pitfall 4: Restarting During Shutdown
**What goes wrong:** Goroutine restarts after panic even though service is shutting down
**Why it happens:** Restart logic doesn't check if context is cancelled
**How to avoid:** Before restarting, check `ctx.Err()` - don't restart if context is cancelled
**Warning signs:**
- Goroutines restarting during application shutdown
- Restart loops continuing after SIGTERM/SIGINT
- Logs show repeated restarts followed by immediate shutdown

**Example - WRONG:**
```go
defer func() {
    if r := recover(); r != nil {
        logger.Error("panic, restarting", "panic", r)
        startWatcher(ctx, name, interval, workFn) // WRONG: Always restarts
    }
}()
```

**Example - CORRECT:**
```go
defer func() {
    if r := recover(); r != nil {
        logger.Error("panic detected", "panic", r)

        if ctx.Err() != nil {
            logger.Info("context cancelled, not restarting")
            return
        }

        logger.Info("restarting watcher")
        time.Sleep(time.Second) // Backoff
        startWatcher(ctx, name, interval, workFn)
    }
}()
```

### Pitfall 5: Missing Exit Logging
**What goes wrong:** Goroutine exits silently, making debugging difficult
**Why it happens:** Developer focuses on error cases, forgets normal shutdown is also important
**How to avoid:** Log ALL exit scenarios with structured context: normal shutdown, errors, panics
**Warning signs:**
- No log message when goroutine exits normally
- Difficulty determining if goroutine stopped or is stuck
- Missing "operation" context in exit logs

**Example - WRONG:**
```go
go func() {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return // WRONG: Silent exit
        case <-ticker.C:
            doWork()
        }
    }
}()
```

**Example - CORRECT:**
```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Error("panic",
                "operation", "watch_transfers",
                "panic", r,
                "stack", string(debug.Stack()))
        }
    }()

    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            logger.Info("shutdown",
                "operation", "watch_transfers",
                "reason", "context_cancelled")
            return
        case <-ticker.C:
            if err := doWork(); err != nil {
                logger.Error("work failed",
                    "operation", "watch_transfers",
                    "err", err)
            }
        }
    }
}()
```

## Code Examples

Verified patterns from official sources:

### Complete Goroutine with All Patterns
```go
// Source: Combination of https://go.dev/blog/defer-panic-and-recover,
// https://www.dolthub.com/blog/2026-01-09-golang-panic-recovery/,
// and https://dev.to/serifcolakel/go-concurrency-mastery-preventing-goroutine-leaks-with-context-timeout-cancellation-best-1lg0
func (o *TransferOrchestrator) ProduceTransfers(ctx context.Context) {
    logger := logctx.LoggerFromContext(ctx)

    go func() {
        // Panic recovery (executes last during unwind)
        defer func() {
            if r := recover(); r != nil {
                logger.Error("transfer orchestrator panic",
                    "operation", "produce_transfers",
                    "panic", r,
                    "stack", string(debug.Stack()))

                // Optional: Restart with clean state
                if ctx.Err() == nil {
                    logger.Info("restarting transfer orchestrator")
                    time.Sleep(time.Second)
                    o.ProduceTransfers(ctx)
                }
            }
        }()

        // Resource cleanup (executes first during unwind)
        ticker := time.NewTicker(o.pollingInterval)
        defer ticker.Stop()

        logger.Info("transfer orchestrator started",
            "operation", "produce_transfers",
            "polling_interval", o.pollingInterval)

        for {
            select {
            case <-ctx.Done():
                logger.Info("transfer orchestrator shutdown",
                    "operation", "produce_transfers",
                    "reason", "context_cancelled")
                return
            case <-ticker.C:
                if err := o.watchTransfers(ctx); err != nil {
                    logger.Error("failed to watch transfers",
                        "operation", "produce_transfers",
                        "err", err)
                }
            }
        }
    }()
}
```

### Downloader Watch Loop Pattern
```go
// Source: Existing codebase pattern enhanced with verified cleanup patterns
func (d *Downloader) WatchForImported(ctx context.Context, t *transfer.Transfer, pollingInterval time.Duration) {
    logger := logctx.LoggerFromContext(ctx)

    go func() {
        defer func() {
            if r := recover(); r != nil {
                logger.Error("watch imported panic",
                    "operation", "watch_imported",
                    "transfer_id", t.ID,
                    "panic", r,
                    "stack", string(debug.Stack()))
            }
        }()

        ticker := time.NewTicker(pollingInterval)
        defer ticker.Stop()

        logger.Info("watching for imported transfers",
            "operation", "watch_imported",
            "transfer_id", t.ID,
            "polling_interval", pollingInterval)

        for {
            select {
            case <-ctx.Done():
                logger.Info("watch imported shutdown",
                    "operation", "watch_imported",
                    "transfer_id", t.ID,
                    "reason", "context_cancelled")
                return
            case <-ticker.C:
                imported, err := d.checkForImported(ctx, t)
                if err != nil {
                    logger.Error("failed to check for imported transfer",
                        "operation", "watch_imported",
                        "transfer_id", t.ID,
                        "err", err)
                    continue
                }

                if imported {
                    logger.Info("transfer imported, stopping watch",
                        "operation", "watch_imported",
                        "transfer_id", t.ID)
                    d.OnTransferImported <- t
                    return
                }
            }
        }
    }()
}
```

### Main Loop Notification Pattern
```go
// Source: Existing main.go pattern enhanced with cleanup patterns
func setupNotificationForDownloader(
    ctx context.Context,
    repo storage.DownloadRepository,
    downloader *downloader.Downloader,
    cfg *config,
) {
    logger := logctx.LoggerFromContext(ctx).WithGroup("notification")
    var notif notifier.Notifier
    if cfg.DiscordWebhookURL != "" {
        notif = &notifier.DiscordNotifier{WebhookURL: cfg.DiscordWebhookURL}
    }

    go func() {
        defer func() {
            if r := recover(); r != nil {
                logger.Error("notification loop panic",
                    "operation", "notification_loop",
                    "panic", r,
                    "stack", string(debug.Stack()))
            }
        }()

        logger.Info("notification loop started", "operation", "notification_loop")

        for {
            select {
            case <-ctx.Done():
                logger.Info("notification loop shutdown",
                    "operation", "notification_loop",
                    "reason", "context_cancelled")
                return
            case t := <-downloader.OnTransferDownloadError:
                // Handle error...
            case t := <-downloader.OnTransferDownloadFinished:
                // Handle finished...
            case t := <-downloader.OnTransferImported:
                // Handle imported...
            }
        }
    }()
}
```

**Note:** The notification loop doesn't use a ticker (it's event-driven), so no ticker cleanup needed. Included here to show panic recovery pattern applies to all long-running goroutines.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Manual ticker cleanup in exit cases | defer ticker.Stop() | Best practice since Go 1.0 | Guarantees cleanup on all exit paths including panic |
| Required ticker.Stop() for GC | Optional with Go 1.23+ GC improvements | Go 1.23 (August 2024) | Stop() still recommended for explicit control and documentation |
| Suppressing panics in goroutines | Log with stack trace, optionally restart | Ongoing best practice | Improves observability and resilience |
| String-based logging | Structured logging with log/slog | Standard since Go 1.21 | Better querying, filtering, analysis |

**Deprecated/outdated:**
- **No ticker cleanup:** Pre-Go-1.23 required explicit Stop() for GC. Post-1.23 GC handles it, but explicit Stop() remains best practice for deterministic cleanup.
- **Silent goroutine exits:** Modern practice is comprehensive logging of all exit scenarios for observability.
- **Panic without recovery in services:** Long-running services should recover and optionally restart rather than crash on unexpected panics.

## Open Questions

Things that couldn't be fully resolved:

1. **Should all goroutines restart after panic or just some?**
   - What we know: Context allows Claude's discretion on restart logic
   - What's unclear: Whether TransferOrchestrator and Downloader watch loops should restart or exit cleanly after panic
   - Recommendation: Start without automatic restart. Add restart logic if observability shows panics are recoverable and service availability is impacted. Most panics indicate bugs that should be fixed, not restarted around.

2. **What backoff strategy for goroutine restart?**
   - What we know: Context specifies clean state restart but doesn't mandate backoff strategy
   - What's unclear: Whether simple time.Sleep(1*time.Second) is sufficient or if exponential backoff is needed
   - Recommendation: If implementing restart, start with simple 1-second delay. If restart loops become an issue (tight panic loops), add exponential backoff with max attempts. For now, likely don't need restart at all - panics should be investigated and fixed.

3. **Should panic recovery include telemetry metrics?**
   - What we know: Codebase has telemetry infrastructure (internal/telemetry)
   - What's unclear: Whether panic recoveries should increment metrics counters for alerting
   - Recommendation: Phase focuses on resource cleanup. If panics occur in practice, add metrics in separate observability enhancement. For now, logging with stack traces is sufficient for debugging.

## Sources

### Primary (HIGH confidence)
- https://pkg.go.dev/time - Official Go time.Ticker documentation, Stop() method
- https://go.dev/blog/defer-panic-and-recover - Official Go blog on defer/panic/recover patterns
- https://www.dolthub.com/blog/2026-01-09-golang-panic-recovery/ - Recent (January 2026) panic recovery patterns with errgroup
- Existing codebase: internal/downloader/downloader.go, internal/transfer/transfer.go, cmd/seedbox_downloader/main.go, .planning/phases/01-critical-safety/01-RESEARCH.md

### Secondary (MEDIUM confidence)
- [Go Concurrency Mastery: Preventing Goroutine Leaks](https://dev.to/serifcolakel/go-concurrency-mastery-preventing-goroutine-leaks-with-context-timeout-cancellation-best-1lg0) - Ticker cleanup and context cancellation patterns (2025)
- [Contextual Logging in Go with Slog](https://betterstack.com/community/guides/logging/golang-contextual-logging/) - Structured logging patterns for goroutine lifecycle (2025)
- [A Guide to Graceful Shutdown in Go with Goroutines and Context](https://medium.com/@karthianandhanit/a-guide-to-graceful-shutdown-in-go-with-goroutines-and-context-1ebe3654cac8) - Context cancellation patterns

### Tertiary (LOW confidence)
- [Restarting a panicked goroutine - golang-nuts](https://groups.google.com/g/golang-nuts/c/iSZcILsry1U) - Community discussion on restart patterns
- [Context Control in Go](https://zenhorace.dev/blog/context-control-go/) - Context cancellation patterns
- [proposal: time: context-aware time Ticker and Timer · Issue #45571](https://github.com/golang/go/issues/45571) - Proposed but not implemented context-aware tickers

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - All libraries are Go stdlib, patterns verified in official docs
- Architecture: HIGH - Patterns verified in official Go blog, recent 2026 sources, and existing codebase
- Pitfalls: HIGH - Verified through official docs and common community mistakes

**Research date:** 2026-01-31
**Valid until:** 2026-04-30 (90 days - Go stdlib is stable, patterns are fundamental)
