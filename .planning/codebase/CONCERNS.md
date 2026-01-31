# Codebase Concerns

**Analysis Date:** 2026-01-31

## Tech Debt

**Goroutine lifecycle management without cleanup:**
- Issue: Multiple goroutines spawned for file download watching, transfer orchestration, and notification handling without guaranteed cleanup paths. Tickers are stopped only in success paths, not when context cancels.
- Files: `cmd/seedbox_downloader/main.go` (lines 279-328), `internal/downloader/downloader.go` (lines 183-215, 217-250), `internal/transfer/transfer.go` (lines 126-141)
- Impact: Long-running applications may leak goroutines and resources if download operations hang or if graceful shutdown doesn't complete. Ticker resources may not be properly cleaned up when context times out.
- Fix approach: Ensure all goroutines with tickers call `ticker.Stop()` before exiting, either on success or context cancellation. Use context cancellation as the primary cleanup signal in all watch loops.

**Unused telemetry system initialization:**
- Issue: Telemetry system has comprehensive metric initialization in `initializeMetrics()` but metrics are never recorded outside of instrumentation wrappers. The noop.MeterProvider fallback is always used unless OTEL_ADDRESS is explicitly set.
- Files: `internal/telemetry/telemetry.go` (lines 62-120), `cmd/seedbox_downloader/main.go` (lines 155-166)
- Impact: Telemetry is disabled by default even though infrastructure is initialized. Operators must explicitly set OTEL_ADDRESS environment variable, otherwise metrics collection is silently disabled with no warnings.
- Fix approach: Log a warning when telemetry is disabled, or change default behavior to enable basic metrics even without external OTEL collector. Document the requirement clearly in startup logs.

**Incomplete commented-out initialization code:**
- Issue: Large block of commented code in `ProduceTransfers()` that checks unfinished transfers and imports at startup.
- Files: `internal/transfer/transfer.go` (lines 96-122)
- Impact: This logic suggests the system once had startup recovery but it's been disabled without explanation. If restarted, previous incomplete downloads won't be resumed, they'll need manual intervention or re-adding.
- Fix approach: Either remove the commented code entirely with a git history explanation, or implement proper startup recovery with error handling.

**Response body error handling in deluge client:**
- Issue: In `GrabFile()` method, `resp.Body.Close()` is called after `client.Do(req)` error, but `resp` could be nil if the request fails.
- Files: `internal/dc/deluge/client.go` (line 212)
- Impact: Potential nil pointer dereference if HTTP request fails with a network error before response is received.
- Fix approach: Check if `resp != nil` before calling `Close()`.

**Database connection not properly initialized on startup:**
- Issue: `InitDB()` creates database file but doesn't ping the connection to verify it's usable. No connection pooling configuration.
- Files: `internal/storage/sqlite/init.go` (lines 11-29)
- Impact: Database initialization could silently fail with permission errors or filesystem issues that only manifest during first query. Single database connection with default pooling may create contention under parallel downloads.
- Fix approach: Add `db.Ping()` after opening, set reasonable connection pool limits with `db.SetMaxOpenConns()` and `db.SetMaxIdleConns()`.

## Known Bugs

**Potential nil pointer in GrabFile error path:**
- Symptoms: Application crashes with nil pointer dereference when downloading files from Deluge under certain network conditions
- Files: `internal/dc/deluge/client.go` (line 212)
- Trigger: When HTTP client request fails before response is received
- Workaround: Check if `resp` is not nil before calling `resp.Body.Close()`

**Discord notifier silently ignores HTTP response status:**
- Symptoms: Failed Discord notifications are not detected; webhook failures are silently swallowed
- Files: `internal/notifier/discord.go` (line 36)
- Trigger: When Discord webhook returns non-2xx status code
- Workaround: Add explicit status code check and error logging before returning nil

## Security Considerations

**TLS certificate verification can be disabled globally:**
- Risk: Insecure TLS configuration bypasses certificate verification for all requests to Deluge, making the application vulnerable to man-in-the-middle attacks
- Files: `internal/dc/deluge/client.go` (lines 65-71, 201-206), `cmd/seedbox_downloader/main.go` (line 335)
- Current mitigation: Passed as parameter from config, not hardcoded
- Recommendations: Only allow insecure TLS when explicitly set via environment variable, add warnings when insecure mode is active. Consider separating TLS config from general client creation. Log a warning at startup if `Insecure` mode is enabled.

**Basic authentication credentials stored in plaintext in config:**
- Risk: Deluge and Transmission credentials are stored as environment variables and log context contains passwords in error scenarios
- Files: `cmd/seedbox_downloader/main.go` (lines 36, 54, 76), `internal/dc/deluge/client.go` (line 93)
- Current mitigation: Environment variables are standard practice, but logging could leak credentials
- Recommendations: Never log passwords or auth tokens. Redact sensitive values in error messages. Consider using separate auth flow that doesn't require storing credentials.

**Discord webhook URL visible in logs:**
- Risk: Discord webhook URL could be exposed in error logs or debug output, allowing attackers to spam the webhook
- Files: `cmd/seedbox_downloader/main.go` (line 48), `internal/notifier/discord.go` (line 19)
- Current mitigation: Not logged by default, but webhook URL is stored in config
- Recommendations: Never log webhook URLs. Treat as secret-level configuration. Consider rotating webhook keys regularly.

## Performance Bottlenecks

**Sequential file download within parallel transfer downloads:**
- Problem: While multiple transfers are downloaded in parallel (via errgroup), files within each transfer that need to be checked for import are checked sequentially through ARR services
- Files: `internal/downloader/downloader.go` (lines 252-280)
- Cause: Nested loop checking every file against every ARR service (Sonarr + Radarr)
- Improvement path: Cache ARR import check results, parallelize ARR checks within a transfer, or implement batch import checking.

**Polling-based transfer detection:**
- Problem: Transfer orchestrator polls download client every `PollingInterval` (default 10 minutes) which adds latency to detecting new transfers
- Files: `internal/transfer/transfer.go` (lines 124-141)
- Cause: No webhook support from Deluge/Putio, forced to poll
- Improvement path: If Deluge/Putio support webhooks, implement push-based detection. Consider adaptive polling that increases frequency when transfers are pending.

**No transfer resume on incomplete downloads:**
- Problem: If downloader crashes mid-transfer, the entire transfer must be re-downloaded from scratch instead of resuming from last downloaded byte
- Files: `internal/downloader/downloader.go` (lines 105-149)
- Cause: No HTTP range request support or partial file tracking
- Improvement path: Implement partial file tracking in database and range request support in GrabFile, resume downloads from last byte instead of restarting.

**Database operations in hot path without prepared statements:**
- Problem: ClaimTransfer builds SQL query dynamically in every call with string formatting
- Files: `internal/storage/sqlite/download_repository.go` (lines 62-69)
- Cause: Using `db.Exec` with inline parameters instead of prepared statements
- Improvement path: Use prepared statements for frequently-called queries to improve query planning and reduce parsing overhead.

## Fragile Areas

**Transfer state machine is implicit and documented only in comments:**
- Files: `internal/transfer/transfer.go`, `internal/downloader/downloader.go`
- Why fragile: Status transitions (pending → downloading → downloaded → imported → seeding → removed) are scattered across multiple components. No centralized validation of valid status transitions. Comment at line 96-122 of transfer.go hints at missing logic.
- Safe modification: Document all valid status transitions. Create explicit state transition validation. Test all status paths in unit tests.
- Test coverage: No test coverage for state machine transitions. Status transitions are only implicitly tested through integration.

**Hard-coded assumption that Transmission handler only works with Putio:**
- Files: `cmd/seedbox_downloader/main.go` (lines 356-361)
- Why fragile: Handler explicitly checks for `*putio.Client` and returns error if Deluge is used. This constraint is not obvious from config naming.
- Safe modification: Either extend handler to support all clients or make it clear in config that Transmission handler requires Putio. Add explicit validation at config load time.
- Test coverage: No tests covering client type validation.

**Notification system assumes nil notifier is safe to use:**
- Files: `cmd/seedbox_downloader/main.go` (line 274-277, 296-325)
- Why fragile: If Discord webhook URL is empty, `notif` is nil, but code doesn't check before calling `notif.Notify()`. Will panic.
- Safe modification: Implement no-op notifier pattern instead of nil. Add nil checks before all notif calls.
- Test coverage: No tests for notification flow.

## Scaling Limits

**Single-threaded database access:**
- Current capacity: SQLite supports limited concurrent writes (one writer at a time)
- Limit: With multiple instances or high concurrency, database write lock contention becomes bottleneck
- Scaling path: Migrate to PostgreSQL for true concurrent write support, or implement write queue to serialize database updates.

**Hard-coded MAX_PARALLEL limit of 5:**
- Current capacity: Default 5 concurrent downloads per instance
- Limit: CPU-bound or network-bound workloads may be over/under-utilized
- Scaling path: Make MAX_PARALLEL dynamic based on system resources (CPU count, available memory). Add metrics to monitor queue depth and adjust automatically.

**Unbuffered channels for transfer events:**
- Current capacity: Single-element channel buffers may cause goroutine blocking if event handlers fall behind
- Limit: If downloading takes longer than polling interval, event channel fills and send blocks
- Scaling path: Use buffered channels sized to expected queue depth, or implement non-blocking send with dropped event logging.

**No rate limiting on polling:**
- Current capacity: All polling loops run independently with no coordination
- Limit: As system scales, many concurrent polling requests could overload downstream services
- Scaling path: Implement centralized rate limiter or backoff strategy for polling based on error rates.

## Dependencies at Risk

**Deprecated go-chi/chi import:**
- Risk: Code imports `"github.com/go-chi/chi"` (v4 style) but also imports `"github.com/go-chi/chi/v5"` in other files
- Impact: Version mismatch could cause dependency conflicts
- Migration plan: Standardize on `github.com/go-chi/chi/v5` across all files. Update any v4 API usage to v5.

## Test Coverage Gaps

**No tests for main initialization and wiring:**
- What's not tested: Config loading, service initialization, server startup, graceful shutdown
- Files: `cmd/seedbox_downloader/main.go`
- Risk: Refactoring initialization code could silently break application startup without failing tests
- Priority: High - initialization is critical path

**No tests for state machine transitions:**
- What's not tested: Transfer status transitions (pending → downloading → downloaded → imported), error recovery flows
- Files: `internal/transfer/transfer.go`, `internal/downloader/downloader.go`
- Risk: Status updates could be applied incorrectly without detection (e.g., updating imported status twice)
- Priority: High - core business logic

**No tests for concurrent download handling:**
- What's not tested: Race conditions in parallel file downloads, semaphore correctness, goroutine cleanup
- Files: `internal/downloader/downloader.go` (lines 118-142)
- Risk: Race conditions and deadlocks could manifest only under production load
- Priority: High - concurrency critical for performance

**No tests for database repository:**
- What's not tested: ClaimTransfer atomicity, UpdateTransferStatus correctness, GetDownloads consistency
- Files: `internal/storage/sqlite/download_repository.go`
- Risk: Data corruption or lost updates under concurrent access
- Priority: High - data integrity critical

**No tests for notification flow:**
- What's not tested: Discord notifier error handling, nil notifier safety, notification failure recovery
- Files: `cmd/seedbox_downloader/main.go` (notification loop), `internal/notifier/discord.go`
- Risk: Notification failures could go unnoticed or cause application crashes
- Priority: Medium - feature completeness

**No integration tests:**
- What's not tested: End-to-end flows (discovery → download → import), error recovery between components
- Files: All components interact in main loop
- Risk: Component integration issues only discovered in production
- Priority: Medium - system-level validation

---

*Concerns audit: 2026-01-31*
