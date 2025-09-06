package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// HTTPMiddleware provides HTTP telemetry middleware.
type HTTPMiddleware struct {
	telemetry *Telemetry
}

// NewHTTPMiddleware creates a new HTTP middleware for telemetry.
func NewHTTPMiddleware(telemetry *Telemetry) *HTTPMiddleware {
	return &HTTPMiddleware{
		telemetry: telemetry,
	}
}

// Middleware returns the HTTP middleware function.
func (m *HTTPMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.telemetry == nil {
			next.ServeHTTP(w, r)

			return
		}

		start := time.Now()

		// Increment in-flight requests
		m.telemetry.IncrementHTTPInFlight()
		defer m.telemetry.DecrementHTTPInFlight()

		// Create a span for tracing
		ctx, span := m.telemetry.Tracer().Start(r.Context(), "http_request")
		defer span.End()

		// Add request attributes to span
		span.SetAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.String()),
			attribute.String("http.route", r.URL.Path),
			attribute.String("http.user_agent", r.UserAgent()),
			attribute.String("http.remote_addr", r.RemoteAddr),
		)

		// Create a response writer wrapper to capture status code
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Process the request
		next.ServeHTTP(rw, r.WithContext(ctx))

		// Calculate duration
		duration := time.Since(start)

		// Add response attributes to span
		span.SetAttributes(
			attribute.Int("http.status_code", rw.statusCode),
			attribute.Int64("http.response_size", rw.bytesWritten),
		)

		// Set span status based on HTTP status code
		if rw.statusCode >= http.StatusBadRequest {
			span.SetAttributes(attribute.Bool("error", true))

			if rw.statusCode >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, "HTTP "+strconv.Itoa(rw.statusCode))
			}
		}

		// Record metrics
		statusClass := getStatusClass(rw.statusCode)
		m.telemetry.RecordHTTPRequest(r.Method, r.URL.Path, statusClass, duration)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

// WriteHeader captures the status code.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the number of bytes written.
func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)

	return n, err
}

// getStatusClass returns the status class (2xx, 3xx, 4xx, 5xx) for a given status code.
func getStatusClass(statusCode int) string {
	switch {
	case statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices:
		return "2xx"
	case statusCode >= http.StatusMultipleChoices && statusCode < http.StatusBadRequest:
		return "3xx"
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		return "4xx"
	case statusCode >= http.StatusInternalServerError:
		return "5xx"
	default:
		return "unknown"
	}
}
