package logctx

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceHandler is an slog.Handler wrapper that automatically injects trace_id and span_id
// from OpenTelemetry span context into log records.
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler creates a new TraceHandler that wraps the provided handler.
// Panics if the provided handler is nil.
func NewTraceHandler(h slog.Handler) *TraceHandler {
	if h == nil {
		panic("logctx: NewTraceHandler called with nil handler")
	}
	return &TraceHandler{inner: h}
}

// Enabled reports whether the handler handles records at the given level.
// Delegates to the inner handler.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle processes the log record by injecting trace context if present,
// then delegates to the inner handler.
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	// Extract span from context
	span := trace.SpanFromContext(ctx)
	spanCtx := span.SpanContext()

	// Only add trace fields if span context is valid
	if spanCtx.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}

	// Delegate to inner handler
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new TraceHandler whose inner handler includes the given attributes.
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new TraceHandler whose inner handler starts a group with the given name.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}
