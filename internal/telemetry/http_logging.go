package telemetry

import (
	"net/http"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

// wrapResponseWriter creates a new responseWriter with status defaulted to 200 OK.
func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader captures the status code and delegates to the underlying ResponseWriter.
func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return // Prevent multiple WriteHeader calls
	}

	rw.status = code
	rw.wroteHeader = true

	rw.ResponseWriter.WriteHeader(code)
}

// Write captures implicit 200 OK if WriteHeader was not called.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}

	return rw.ResponseWriter.Write(b)
}

// HTTPLogging middleware logs HTTP requests with appropriate level based on status code.
// Requirements: HTTP-01, HTTP-02, HTTP-03, HTTP-04, HTTP-05, HTTP-06.
func HTTPLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := logctx.LoggerFromContext(ctx)
		start := time.Now()

		// Wrap response writer to capture status
		wrapped := wrapResponseWriter(w)

		// Execute handler
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)
		status := wrapped.status
		requestID := GetRequestID(ctx)

		// Build log attributes (HTTP-01, HTTP-02, HTTP-06)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"request_id", requestID,
		}

		// Select log level based on status code (HTTP-03, HTTP-04, HTTP-05)
		switch {
		case status >= 500:
			// HTTP-03: 5xx at ERROR level
			logger.ErrorContext(ctx, "http request completed", attrs...)
		case status >= 400:
			// HTTP-04: 4xx at WARN level
			logger.WarnContext(ctx, "http request completed", attrs...)
		default:
			// HTTP-05: 2xx at INFO level
			logger.InfoContext(ctx, "http request completed", attrs...)
		}
	})
}
