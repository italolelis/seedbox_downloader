package putio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/putdotio/go-putio"
)

// Put.io answers a burst of API calls with 429 Too Many Requests. A transfer
// with hundreds of small files, downloaded several at a time, asks for a
// download URL per file fast enough to trigger it. Read calls are therefore
// retried with backoff, bounded so a sustained limit fails the call (and the
// transfer is retried on the next poll) instead of stalling a worker for long.
const (
	defaultRetryMaxTries        = 5
	defaultRetryInitialInterval = time.Second
	defaultRetryMaxInterval     = 30 * time.Second
	defaultRetryMaxElapsed      = 2 * time.Minute
)

type retryPolicy struct {
	maxTries        uint
	initialInterval time.Duration
	maxInterval     time.Duration
	maxElapsed      time.Duration
}

func defaultRetryPolicy() retryPolicy {
	return retryPolicy{
		maxTries:        defaultRetryMaxTries,
		initialInterval: defaultRetryInitialInterval,
		maxInterval:     defaultRetryMaxInterval,
		maxElapsed:      defaultRetryMaxElapsed,
	}
}

// WithRetry sets how many times a failed Put.io read is tried in total and the
// first wait between tries, which then grows exponentially. A Retry-After header
// on a 429 takes precedence over the computed wait. Intended for tests, which
// cannot afford production's waits.
func WithRetry(maxTries uint, initialInterval time.Duration) Option {
	return func(c *Client) {
		c.retry.maxTries = maxTries
		c.retry.initialInterval = initialInterval
		c.retry.maxInterval = max(initialInterval, c.retry.maxInterval)
	}
}

// withAPIRetry runs a Put.io read, retrying the failures that say nothing about
// the resource itself: rate limits, server errors and network errors. Anything
// else -- a 404, a 403, a cancelled context -- is returned on the first try,
// unchanged, so the caller can classify it.
func withAPIRetry[T any](ctx context.Context, c *Client, op func() (T, error)) (T, error) {
	logger := logctx.LoggerFromContext(ctx)

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = c.retry.initialInterval
	b.MaxInterval = c.retry.maxInterval

	return backoff.Retry(ctx, func() (T, error) {
		v, err := op()
		if err == nil {
			return v, nil
		}

		if !isTransient(ctx, err) {
			return v, backoff.Permanent(err)
		}

		// Both are wrapped so the caller still sees the Put.io error once the
		// tries run out, while Retry still finds the server's requested wait.
		if wait, ok := retryAfter(err); ok {
			return v, fmt.Errorf("%w: %w", err, &backoff.RetryAfterError{Duration: wait})
		}

		return v, err
	},
		backoff.WithBackOff(b),
		backoff.WithMaxTries(c.retry.maxTries),
		backoff.WithMaxElapsedTime(c.retry.maxElapsed),
		backoff.WithNotify(func(err error, wait time.Duration) {
			logger.WarnContext(ctx, "Put.io API call failed, retrying", "wait", wait, "err", err)
		}),
	)
}

// apiStatus is the HTTP status of a failed Put.io API call, when the failure was
// a response at all rather than a network or context error.
func apiStatus(err error) (int, bool) {
	var apiErr *putio.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response == nil {
		return 0, false
	}

	return apiErr.Response.StatusCode, true
}

// isGone reports whether Put.io said the resource does not exist. This is the
// only evidence that a transfer's files are missing: a rate limit, an outage or
// a cancelled call says nothing about whether the files are still there.
func isGone(err error) bool {
	status, ok := apiStatus(err)

	return ok && (status == http.StatusNotFound || status == http.StatusGone)
}

// isCancelled reports whether err is the caller giving up -- shutdown, or a
// sibling download failing and cancelling the group -- rather than Put.io.
func isCancelled(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled)
}

func isTransient(ctx context.Context, err error) bool {
	if isCancelled(ctx, err) {
		return false
	}

	status, ok := apiStatus(err)
	if !ok {
		// No response: a dropped connection or a timeout on a live context.
		return true
	}

	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// retryAfter reads the wait a 429 or 503 asked for, in either form RFC 9110
// allows: delay-seconds or an HTTP date.
func retryAfter(err error) (time.Duration, bool) {
	var apiErr *putio.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response == nil {
		return 0, false
	}

	value := strings.TrimSpace(apiErr.Response.Header.Get("Retry-After"))
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}

	if at, err := http.ParseTime(value); err == nil {
		return max(time.Until(at), 0), true
	}

	return 0, false
}
