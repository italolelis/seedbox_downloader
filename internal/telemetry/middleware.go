package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewHTTPMiddleware creates HTTP middleware using the standard otelhttp package.
// This provides automatic HTTP instrumentation following OpenTelemetry semantic conventions.
func NewHTTPMiddleware(serviceName string) func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware(serviceName)
}

// NewHTTPHandler wraps an HTTP handler with OpenTelemetry instrumentation.
// This is an alternative to middleware for wrapping individual handlers.
func NewHTTPHandler(handler http.Handler, operation string) http.Handler {
	return otelhttp.NewHandler(handler, operation)
}
