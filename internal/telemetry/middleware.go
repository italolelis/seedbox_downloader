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
