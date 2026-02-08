package logctx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// TestTraceHandler_NoSpanContext verifies that logs without span context
// do NOT include trace_id or span_id fields.
func TestTraceHandler_NoSpanContext(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{})
	traceHandler := NewTraceHandler(jsonHandler)
	logger := slog.New(traceHandler)

	// Log without any span context
	ctx := context.Background()
	logger.InfoContext(ctx, "test message", "key", "value")

	output := buf.String()

	// Parse JSON to verify structure
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	// Verify trace_id and span_id are NOT present
	if _, exists := logEntry["trace_id"]; exists {
		t.Errorf("trace_id should not be present in logs without span context, got: %v", logEntry["trace_id"])
	}
	if _, exists := logEntry["span_id"]; exists {
		t.Errorf("span_id should not be present in logs without span context, got: %v", logEntry["span_id"])
	}

	// Verify normal fields are present
	if logEntry["msg"] != "test message" {
		t.Errorf("expected msg='test message', got: %v", logEntry["msg"])
	}
	if logEntry["key"] != "value" {
		t.Errorf("expected key='value', got: %v", logEntry["key"])
	}
}

// TestTraceHandler_WithSpanContext verifies that logs with valid span context
// include trace_id and span_id fields.
func TestTraceHandler_WithSpanContext(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{})
	traceHandler := NewTraceHandler(jsonHandler)
	logger := slog.New(traceHandler)

	// Create a trace provider and start a span
	tp := trace.NewNoopTracerProvider()
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	// Log with span context
	logger.InfoContext(ctx, "test message", "key", "value")

	output := buf.String()

	// For NoopTracerProvider, SpanContext is not valid, so we need to use a real span
	// Let's check if the span is valid first
	if !span.SpanContext().IsValid() {
		t.Skip("NoopTracerProvider does not create valid spans, skipping valid span test")
	}

	// Parse JSON to verify structure
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	// Verify trace_id is present
	traceID, exists := logEntry["trace_id"]
	if !exists {
		t.Errorf("trace_id should be present in logs with valid span context")
	} else if traceID == "" {
		t.Errorf("trace_id should not be empty string")
	}

	// Verify span_id is present
	spanID, exists := logEntry["span_id"]
	if !exists {
		t.Errorf("span_id should be present in logs with valid span context")
	} else if spanID == "" {
		t.Errorf("span_id should not be empty string")
	}
}

// mockTracerProvider creates a valid trace and span for testing.
type mockTracerProvider struct {
	trace.TracerProvider
}

type mockTracer struct {
	trace.Tracer
}

type mockSpan struct {
	trace.Span
	spanContext trace.SpanContext
}

func (m *mockSpan) SpanContext() trace.SpanContext {
	return m.spanContext
}

func (m *mockSpan) End(...trace.SpanEndOption) {}

func (m *mockTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	// Create a valid trace ID and span ID
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	span := &mockSpan{spanContext: spanCtx}
	return trace.ContextWithSpan(ctx, span), span
}

func (m *mockTracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return &mockTracer{}
}

// TestTraceHandler_WithValidSpan uses a mock to ensure we test with a valid span.
func TestTraceHandler_WithValidSpan(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{})
	traceHandler := NewTraceHandler(jsonHandler)
	logger := slog.New(traceHandler)

	// Create mock trace provider with valid span
	tp := &mockTracerProvider{}
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	// Log with span context
	logger.InfoContext(ctx, "test message", "key", "value")

	output := buf.String()

	// Parse JSON to verify structure
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	// Verify trace_id is present and matches expected format
	traceID, exists := logEntry["trace_id"]
	if !exists {
		t.Errorf("trace_id should be present in logs with valid span context")
	}
	expectedTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	if traceID != expectedTraceID {
		t.Errorf("expected trace_id='%s', got: %v", expectedTraceID, traceID)
	}

	// Verify span_id is present and matches expected format
	spanID, exists := logEntry["span_id"]
	if !exists {
		t.Errorf("span_id should be present in logs with valid span context")
	}
	expectedSpanID := "00f067aa0ba902b7"
	if spanID != expectedSpanID {
		t.Errorf("expected span_id='%s', got: %v", expectedSpanID, spanID)
	}

	// Verify normal fields are still present
	if logEntry["msg"] != "test message" {
		t.Errorf("expected msg='test message', got: %v", logEntry["msg"])
	}
	if logEntry["key"] != "value" {
		t.Errorf("expected key='value', got: %v", logEntry["key"])
	}
}

// TestTraceHandler_Enabled verifies that Enabled delegates to inner handler.
func TestTraceHandler_Enabled(t *testing.T) {
	jsonHandler := slog.NewJSONHandler(nil, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})
	traceHandler := NewTraceHandler(jsonHandler)

	ctx := context.Background()

	// Should be disabled for Info (below Warn)
	if traceHandler.Enabled(ctx, slog.LevelInfo) {
		t.Errorf("expected Info level to be disabled when handler level is Warn")
	}

	// Should be enabled for Warn
	if !traceHandler.Enabled(ctx, slog.LevelWarn) {
		t.Errorf("expected Warn level to be enabled")
	}

	// Should be enabled for Error (above Warn)
	if !traceHandler.Enabled(ctx, slog.LevelError) {
		t.Errorf("expected Error level to be enabled")
	}
}

// TestTraceHandler_WithAttrs verifies that WithAttrs returns a new TraceHandler.
func TestTraceHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{})
	traceHandler := NewTraceHandler(jsonHandler)

	// Add attributes
	attrs := []slog.Attr{slog.String("component", "test")}
	newHandler := traceHandler.WithAttrs(attrs)

	// Verify it's a TraceHandler
	_, ok := newHandler.(*TraceHandler)
	if !ok {
		t.Errorf("WithAttrs should return *TraceHandler, got: %T", newHandler)
	}

	// Verify attributes are preserved in logs
	logger := slog.New(newHandler)
	logger.InfoContext(context.Background(), "test")

	output := buf.String()
	if !strings.Contains(output, "component") || !strings.Contains(output, "test") {
		t.Errorf("expected attributes to be present in output, got: %s", output)
	}
}

// TestTraceHandler_WithGroup verifies that WithGroup returns a new TraceHandler.
func TestTraceHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{})
	traceHandler := NewTraceHandler(jsonHandler)

	// Add group
	newHandler := traceHandler.WithGroup("mygroup")

	// Verify it's a TraceHandler
	_, ok := newHandler.(*TraceHandler)
	if !ok {
		t.Errorf("WithGroup should return *TraceHandler, got: %T", newHandler)
	}

	// Verify group is preserved in logs
	logger := slog.New(newHandler)
	logger.InfoContext(context.Background(), "test", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "mygroup") {
		t.Errorf("expected group to be present in output, got: %s", output)
	}
}

// TestTraceHandler_NilHandler verifies that NewTraceHandler panics with nil handler.
func TestTraceHandler_NilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewTraceHandler with nil handler should panic")
		}
	}()

	NewTraceHandler(nil)
}
