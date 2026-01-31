# Phase 2: Resource Leak Prevention - Context

**Gathered:** 2026-01-31
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix resource leaks by adding proper ticker cleanup in long-running goroutines. Specifically:
- TransferOrchestrator watch loops
- Downloader watch loops
- main.go notification loop

This phase does NOT add new functionality or change behavior — it prevents resource leaks in existing goroutines by ensuring ticker.Stop() is called on all exit paths.

</domain>

<decisions>
## Implementation Decisions

### Exit path coverage
- **Cleanup triggers:** Context cancellation AND normal loop completion (all expected exit paths)
- **Panic handling:** Add panic recovery to ensure ticker.Stop() runs even if goroutine panics
  - After recovery: Log the panic with stack trace, stop ticker, attempt to restart the goroutine with clean state
- **Exit logging:** Log all exit scenarios (context cancellation, normal completion, errors) with context about why the goroutine stopped
- **Cleanup pattern:** Single defer at goroutine start (`defer ticker.Stop()`) handles all exit paths automatically
  - Works for select statements with multiple channels
  - Context cancellation case (`ctx.Done()`) in select triggers exit, defer executes cleanup

### Cleanup consistency
- **Pattern uniformity:** Identical pattern across all goroutines with tickers
  - Every goroutine uses same cleanup structure (defer at start, same logging, same recovery)
  - Consistent across TransferOrchestrator, Downloader, and notification loops
- **Multiple tickers:** If goroutine has multiple tickers, wrap in single cleanup function
  - Use `defer cleanupTickers()` pattern rather than multiple individual defers
- **Documentation:** No inline comments needed
  - defer ticker.Stop() is standard Go practice, self-documenting
  - Code clarity over comment clutter

### Claude's Discretion
- Exact log message format and structured fields (following existing logging patterns)
- Specific panic recovery implementation details
- Whether to create shared cleanup helper functions vs inline defer statements
- Restart logic details for recovered panics (backoff, state reset, etc.)

</decisions>

<specifics>
## Specific Ideas

- Panic recovery should log with stack trace for debugging
- Restart after panic should use clean state (don't reuse potentially corrupted state)
- Exit logging should use structured fields consistent with Phase 1 (operation, reason, context)

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope (resource leak fixes only, no new features).

</deferred>

---

*Phase: 02-resource-leak-prevention*
*Context gathered: 2026-01-31*
