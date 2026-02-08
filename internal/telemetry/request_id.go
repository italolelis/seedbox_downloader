package telemetry

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type ctxKey string

const (
	requestIDKey    ctxKey = "request_id"
	RequestIDHeader        = "X-Request-ID"
)

// RequestID middleware generates a unique request_id for each request.
// If an X-Request-ID header is present (upstream propagation), it is reused.
// The request_id is stored in the context and set as a response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for existing request_id (upstream propagation)
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set response header for client visibility
		w.Header().Set(RequestIDHeader, requestID)

		// Add to context for logging
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request_id from context.
// Returns empty string if not found.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
