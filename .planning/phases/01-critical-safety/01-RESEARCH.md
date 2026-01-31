# Phase 1: Critical Safety - Research

**Researched:** 2026-01-31
**Domain:** Go error handling, HTTP client safety, retry strategies
**Confidence:** HIGH

## Summary

This phase fixes two critical bugs that cause crashes and silent failures in HTTP operations:
1. Nil pointer dereference when HTTP request fails before receiving a response
2. Silent failures in Discord webhook notifications (missing HTTP status code validation)

The standard approach in Go is to check errors before accessing response objects, and validate HTTP status codes after successful responses. The codebase already uses structured logging with `log/slog`, has `cenkalti/backoff/v5` for retries, and follows consistent error wrapping patterns with `fmt.Errorf(...%w)`.

For retry strategies, Go provides error types to categorize network failures: `context.DeadlineExceeded` for timeouts, `context.Canceled` for explicit cancellations, and `net.DNSError` for DNS failures. HTTP status codes follow a clear pattern: 4xx errors are client errors (don't retry), 5xx errors are server errors (may retry with backoff), with exceptions for 429 (rate limiting - retry with backoff) and specific 5xx codes where retry is inappropriate per user decisions.

**Primary recommendation:** Fix nil checks first (BUG-01), add status code validation (BUG-02), then add structured error categorization and selective retry logic using existing `backoff/v5` library.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| log/slog | stdlib (go 1.23) | Structured logging | Go's official structured logging (since 1.21), used throughout codebase |
| net/http | stdlib | HTTP client | Go's standard HTTP library, no wrapper needed for basic operations |
| errors | stdlib | Error handling | Standard error wrapping/unwrapping with `errors.Is`, `errors.As` |
| context | stdlib | Timeout/cancellation | Provides standard error types: `context.DeadlineExceeded`, `context.Canceled` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| github.com/cenkalti/backoff/v5 | v5.0.3 | Exponential backoff | Already in go.mod, use for retrying network operations |
| github.com/italolelis/seedbox_downloader/internal/logctx | internal | Context-aware logging | Retrieve logger from context: `logctx.LoggerFromContext(ctx)` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| cenkalti/backoff | hashicorp/go-retryablehttp | More opinionated HTTP wrapper, adds dependency complexity for minimal benefit |
| log/slog | third-party loggers | Codebase already standardized on slog, no reason to change |
| Manual retry logic | avast/retry-go or sethvargo/go-retry | Current backoff library sufficient, no need to add more dependencies |

**Installation:**
No new dependencies required - everything needed is already in go.mod or stdlib.

## Architecture Patterns

### Error Checking Pattern (HTTP Client)
**What:** Standard pattern for safe HTTP response handling
**When to use:** Every HTTP request in the codebase
**Example:**
```go
// Source: https://pkg.go.dev/net/http
resp, err := client.Do(req)
if err != nil {
    logger.Error("failed to send request", "url", url, "err", err)
    return fmt.Errorf("failed to send request: %w", err)
}
defer resp.Body.Close()

// Now safe to access resp.StatusCode and resp.Body
if resp.StatusCode != http.StatusOK {
    logger.Error("non-OK response", "url", url, "status_code", resp.StatusCode)
    return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}
```

### Structured Logging Pattern
**What:** Consistent logging with key-value pairs following codebase conventions
**When to use:** All error logging and operational logging
**Example:**
```go
// Source: internal/dc/deluge/client.go
logger := logctx.LoggerFromContext(ctx)

// Error with context
logger.Error("failed to download file",
    "url", url,
    "status_code", resp.StatusCode,
    "err", err)

// Success with context
logger.Info("downloaded file",
    "target", targetPath,
    "size", humanize.Bytes(uint64(totalBytes)))
```

### Error Wrapping Pattern
**What:** Add context to errors while preserving original error for inspection
**When to use:** When propagating errors up the call stack
**Example:**
```go
// Source: .planning/codebase/CONVENTIONS.md
if err != nil {
    return fmt.Errorf("failed to grab file: %w", err)
}

// Later, caller can check:
if errors.Is(err, context.DeadlineExceeded) {
    // Handle timeout specifically
}
```

### Retry with Exponential Backoff Pattern
**What:** Retry operations with increasing delays using backoff/v5
**When to use:** Network operations that fail with retryable errors (timeouts, connection refused, DNS failures)
**Example:**
```go
// Source: https://pkg.go.dev/github.com/cenkalti/backoff/v5
operation := func(ctx context.Context) error {
    resp, err := client.Do(req)
    if err != nil {
        // Check if error is retryable
        if errors.Is(err, context.DeadlineExceeded) ||
           errors.Is(err, context.Canceled) ||
           isNetworkError(err) {
            return err // Retry
        }
        return backoff.Permanent(err) // Don't retry
    }
    defer resp.Body.Close()

    // Check status code
    if resp.StatusCode == 429 || resp.StatusCode == 503 {
        return fmt.Errorf("server busy: %d", resp.StatusCode) // Retry
    }
    if resp.StatusCode >= 400 && resp.StatusCode < 500 {
        return backoff.Permanent(fmt.Errorf("client error: %d", resp.StatusCode)) // Don't retry
    }
    if resp.StatusCode >= 500 {
        return backoff.Permanent(fmt.Errorf("server error: %d", resp.StatusCode)) // Don't retry per decisions
    }

    return nil // Success
}

// Execute with retry
err := backoff.Retry(ctx, operation,
    backoff.WithBackOff(backoff.NewExponentialBackOff()),
    backoff.WithMaxTries(3))
```

### Anti-Patterns to Avoid
- **Accessing resp.Body before checking err**: Always check `if err != nil` before accessing any response fields
- **Using deprecated net.Error.Temporary()**: This method is deprecated; use explicit error type checking instead
- **Retrying 5xx errors**: Per user decisions, do NOT retry 5xx server errors
- **Calling defer resp.Body.Close() before error check**: Only safe after confirming resp is non-nil

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Retry with backoff | Custom retry loop with time.Sleep | github.com/cenkalti/backoff/v5 | Already in go.mod, handles jitter, max elapsed time, max tries, context cancellation |
| Error categorization | String matching on error messages | errors.Is() and errors.As() | Works with wrapped errors, type-safe, standard Go practice |
| Timeout detection | Type assertions on net.Error | errors.Is(err, context.DeadlineExceeded) | Modern Go idiom, works with wrapped errors |
| Structured logging | String concatenation | log/slog with key-value pairs | Already used consistently in codebase, supports filtering/querying |

**Key insight:** Go's stdlib and the existing backoff library cover all retry and error categorization needs. Custom solutions would be more complex and error-prone than using these well-tested tools.

## Common Pitfalls

### Pitfall 1: Nil Response Body Access
**What goes wrong:** Calling `resp.Body.Close()` when `resp` is nil causes panic
**Why it happens:** HTTP requests can fail before getting a response (network unreachable, DNS failure, connection refused). In these cases, `err != nil` but `resp == nil`.
**How to avoid:** Always check error before accessing response. Only use `defer resp.Body.Close()` after confirming no error.
**Warning signs:**
- `defer resp.Body.Close()` appears before error check
- Code assumes response exists whenever Do() or Get() is called

### Pitfall 2: Ignoring HTTP Status Codes
**What goes wrong:** Requests that return 4xx or 5xx status codes are treated as successful
**Why it happens:** `http.Client` only returns errors for network/protocol failures, not HTTP-level errors (non-2xx status codes)
**How to avoid:** Always check `resp.StatusCode` after confirming successful request (err == nil)
**Warning signs:**
- Missing status code validation after HTTP requests
- Assuming `err == nil` means the operation succeeded

### Pitfall 3: Over-Retrying Client Errors
**What goes wrong:** Retrying 400/401/403/404 errors wastes resources and may trigger rate limiting
**Why it happens:** Assuming all failed requests should be retried
**How to avoid:** Categorize errors - only retry network errors and specific status codes (429, 503 per decisions)
**Warning signs:**
- Retry logic doesn't check error types or status codes
- All errors trigger the same retry behavior

### Pitfall 4: Using Deprecated Error Methods
**What goes wrong:** Code uses `net.Error.Temporary()` which is deprecated and ambiguous
**Why it happens:** Old examples and pre-Go-1.13 patterns still in documentation
**How to avoid:** Use `errors.Is(err, context.DeadlineExceeded)` and explicit error type checking
**Warning signs:**
- Type assertions to `net.Error` interface
- Calls to `.Temporary()` method

### Pitfall 5: Missing Structured Context in Error Logs
**What goes wrong:** Error logs lack context needed to debug issues (URL, operation, IDs)
**Why it happens:** Using simple string messages without structured fields
**How to avoid:** Follow existing codebase pattern: include operation context as key-value pairs
**Warning signs:**
- `logger.Error("error", "err", err)` without operation context
- Missing URL, transfer ID, file path, or other relevant identifiers

## Code Examples

Verified patterns from official sources:

### Safe HTTP Request Pattern
```go
// Source: https://pkg.go.dev/net/http and existing codebase
resp, err := client.Do(req)
if err != nil {
    logger.Error("HTTP request failed",
        "url", url,
        "method", req.Method,
        "err", err)
    return fmt.Errorf("HTTP request failed: %w", err)
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
    body, _ := io.ReadAll(resp.Body)
    logger.Error("non-OK response",
        "url", url,
        "status_code", resp.StatusCode,
        "body", string(body))
    return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
}

// Now safe to process response body
```

### Error Categorization for Retry
```go
// Source: https://pkg.go.dev/errors, context, and community best practices
func isRetryableError(err error) bool {
    // Context timeouts and cancellations
    if errors.Is(err, context.DeadlineExceeded) {
        return true
    }
    if errors.Is(err, context.Canceled) {
        return true
    }

    // DNS errors
    var dnsErr *net.DNSError
    if errors.As(err, &dnsErr) {
        return true
    }

    // Connection refused (temporary network issue)
    if errors.Is(err, syscall.ECONNREFUSED) {
        return true
    }

    return false
}

func isRetryableStatusCode(statusCode int) bool {
    switch statusCode {
    case 429: // Too Many Requests
        return true
    case 503: // Service Unavailable (user decision allows this)
        return true
    default:
        return false
    }
}
```

### Retry Operation with Backoff
```go
// Source: https://pkg.go.dev/github.com/cenkalti/backoff/v5
func downloadWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
    var resp *http.Response

    operation := func(ctx context.Context) error {
        var err error
        resp, err = client.Do(req)

        if err != nil {
            if isRetryableError(err) {
                return err // Retry
            }
            return backoff.Permanent(err) // Don't retry
        }

        if isRetryableStatusCode(resp.StatusCode) {
            resp.Body.Close()
            return fmt.Errorf("retryable status: %d", resp.StatusCode)
        }

        if resp.StatusCode >= 400 {
            resp.Body.Close()
            return backoff.Permanent(fmt.Errorf("HTTP error: %d", resp.StatusCode))
        }

        return nil // Success
    }

    err := backoff.Retry(ctx, operation,
        backoff.WithBackOff(backoff.NewExponentialBackOff()),
        backoff.WithMaxTries(3),
        backoff.WithNotify(func(err error, duration time.Duration) {
            logger := logctx.LoggerFromContext(ctx)
            logger.Warn("retrying request",
                "url", req.URL.String(),
                "error", err,
                "retry_after", duration)
        }))

    if err != nil {
        return nil, err
    }

    return resp, nil
}
```

### Discord Notifier with Status Validation
```go
// Source: Existing codebase pattern from internal/svc/arr/arr.go
func (d *DiscordNotifier) Notify(content string) error {
    if d.WebhookURL == "" {
        return fmt.Errorf("webhook URL is not set")
    }

    payload := map[string]string{"content": content}
    body, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("failed to marshal payload: %w", err)
    }

    resp, err := http.Post(d.WebhookURL, "application/json", bytes.NewBuffer(body))
    if err != nil {
        return fmt.Errorf("failed to send request: %w", err)
    }
    defer resp.Body.Close()

    // BUG-02 FIX: Check status code
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return fmt.Errorf("webhook failed with status %d", resp.StatusCode)
    }

    return nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| net.Error.Temporary() | errors.Is() with specific errors | Deprecated in Go 1.18+ | Temporary() is ambiguous; use explicit error type checking |
| os.IsTimeout(err) | errors.Is(err, context.DeadlineExceeded) | Recommended since Go 1.13 | Better semantics, works with wrapped errors |
| Type assertion for errors | errors.As() | Standard since Go 1.13 | Works with wrapped error chains |
| String matching on errors | errors.Is() for sentinel errors | Standard since Go 1.13 | Type-safe, works with wrapping |
| HTTP wrappers for retries | Direct http.Client with backoff/v5 | Ongoing | Less abstraction, more control, backoff library handles complexity |

**Deprecated/outdated:**
- **net.Error.Temporary()**: Deprecated due to ambiguous semantics. Use explicit error categorization.
- **os.IsTimeout()**: Still works but `errors.Is(err, context.DeadlineExceeded)` is clearer.
- **Direct error equality (err == target)**: Prefer `errors.Is()` which handles wrapped errors.

## Open Questions

Things that couldn't be fully resolved:

1. **Which specific operations should retry vs fail-fast?**
   - What we know: Context allows Claude's discretion on which operations retry
   - What's unclear: Whether operations like Deluge authentication should retry or fail immediately
   - Recommendation: Start with retry on network errors only (DNS, connection refused, timeout). Don't retry authentication failures (likely 401/403). Let verification testing reveal if other operations need different behavior.

2. **Optimal retry count and backoff timing**
   - What we know: Context allows Claude's discretion on retry attempts and backoff timing
   - What's unclear: Whether 3 retries is sufficient or if different operations need different limits
   - Recommendation: Use backoff/v5 defaults (initial: 500ms, max interval: 60s, max elapsed: 15min) with max 3 tries. Monitor in practice and adjust if needed.

3. **Should rate limiting (429) honor Retry-After header?**
   - What we know: Context doesn't specify Retry-After header handling
   - What's unclear: Whether to parse and respect Retry-After from 429 responses
   - Recommendation: Basic implementation ignores Retry-After. If rate limiting becomes an issue, add Retry-After parsing as enhancement in later phase.

## Sources

### Primary (HIGH confidence)
- https://pkg.go.dev/net/http - Official Go HTTP documentation, response handling patterns
- https://pkg.go.dev/errors - Standard error wrapping/unwrapping with errors.Is and errors.As
- https://pkg.go.dev/context - Standard timeout/cancellation errors
- https://pkg.go.dev/github.com/cenkalti/backoff/v5 - Retry with exponential backoff (already in go.mod)
- Existing codebase: internal/dc/deluge/client.go, internal/svc/arr/arr.go, .planning/codebase/CONVENTIONS.md

### Secondary (MEDIUM confidence)
- [Error handling and Go - The Go Programming Language](https://go.dev/blog/error-handling-and-go) - Official blog post on error patterns
- [Go Gotcha: Closing a Nil HTTP Response Body](https://medium.com/@KeithAlpichi/go-gotcha-closing-a-nil-http-response-body-with-defer-9b7a3eb30e8c) - Community article on nil response pitfall
- [Which HTTP Error Status Codes Should Not Be Retried? | Baeldung](https://www.baeldung.com/cs/http-error-status-codes-retry) - HTTP status code retry guidelines
- [How to Implement Retry Logic in Go with Exponential Backoff](https://oneuptime.com/blog/post/2026-01-07-go-retry-exponential-backoff/view) - Recent 2026 guide on retry patterns

### Tertiary (LOW confidence)
- [net: deprecate Temporary error status · Issue #45729 · golang/go](https://github.com/golang/go/issues/45729) - Context on Temporary() deprecation
- [net/http: Client does not wrap context errors · Issue #50856 · golang/go](https://github.com/golang/go/issues/50856) - Known issues with error wrapping

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - All libraries verified in go.mod and stdlib docs
- Architecture: HIGH - Patterns verified in existing codebase and official documentation
- Pitfalls: HIGH - Verified through official docs and real bug examples (BUG-01, BUG-02)

**Research date:** 2026-01-31
**Valid until:** 2026-03-31 (60 days - Go stdlib is stable, backoff/v5 is mature)
