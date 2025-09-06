package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// InstrumentedFunc represents a function that can be instrumented.
type InstrumentedFunc func(ctx context.Context) error

// InstrumentOperation instruments a generic operation with telemetry.
func (t *Telemetry) InstrumentOperation(ctx context.Context, operationName, component string, fn InstrumentedFunc) error {
	if t == nil || t.tracer == nil {
		return fn(ctx)
	}

	start := time.Now()
	ctx, span := t.tracer.Start(ctx, operationName)

	defer span.End()

	span.SetAttributes(
		attribute.String("component", component),
		attribute.String("operation", operationName),
	)

	err := fn(ctx)
	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"

		span.SetAttributes(
			attribute.Bool("error", true),
			attribute.String("error.message", err.Error()),
		)
		span.SetStatus(codes.Error, err.Error())
	}

	span.SetAttributes(
		attribute.String("status", status),
		attribute.Float64("duration_seconds", duration.Seconds()),
	)

	return err
}

// InstrumentDBOperation instruments database operations.
func (t *Telemetry) InstrumentDBOperation(ctx context.Context, operation string, fn InstrumentedFunc) error {
	if t == nil {
		return fn(ctx)
	}

	start := time.Now()
	err := t.InstrumentOperation(ctx, "db_"+operation, "database", fn)
	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
	}

	t.RecordDBOperation(operation, status, duration)

	return err
}

// InstrumentClientOperation instruments download client operations.
func (t *Telemetry) InstrumentClientOperation(ctx context.Context, client, operation string, fn InstrumentedFunc) error {
	if t == nil {
		return fn(ctx)
	}

	err := t.InstrumentOperation(ctx, "client_"+operation, "download_client", func(ctx context.Context) error {
		ctx, span := t.tracer.Start(ctx, "client_"+operation)
		defer span.End()

		span.SetAttributes(
			attribute.String("client.type", client),
			attribute.String("client.operation", operation),
		)

		return fn(ctx)
	})

	status := "success"
	if err != nil {
		status = "error"
	}

	t.RecordClientOperation(client, operation, status)

	return err
}

// InstrumentDownload instruments download operations.
func (t *Telemetry) InstrumentDownload(ctx context.Context, transferID, transferName string, fn InstrumentedFunc) error {
	if t == nil {
		return fn(ctx)
	}

	start := time.Now()

	t.IncrementActiveDownloads()
	defer t.DecrementActiveDownloads()

	err := t.InstrumentOperation(ctx, "download", "downloader", func(ctx context.Context) error {
		ctx, span := t.tracer.Start(ctx, "download")
		defer span.End()

		span.SetAttributes(
			attribute.String("transfer.id", transferID),
			attribute.String("transfer.name", transferName),
		)

		return fn(ctx)
	})

	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
	}

	t.RecordDownload(status, duration)

	return err
}

// InstrumentTransfer instruments transfer operations.
func (t *Telemetry) InstrumentTransfer(ctx context.Context, operation string, fn InstrumentedFunc) error {
	if t == nil {
		return fn(ctx)
	}

	t.IncrementActiveTransfers()
	defer t.DecrementActiveTransfers()

	err := t.InstrumentOperation(ctx, "transfer_"+operation, "transfer", fn)

	status := "success"
	if err != nil {
		status = "error"
	}

	t.RecordTransfer(operation, status)

	return err
}
