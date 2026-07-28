package db

import (
	"context"
	"database/sql"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

const dbTracerName = "github.com/block/proto-fleet/server/internal/infrastructure/db"

// NewTracingQuerier wraps next so each query runs in a client span named after the sqlc method under the ctx's existing span; wrap outermost so spans cover retries.
func NewTracingQuerier(next sqlc.Querier) sqlc.Querier {
	return sqlc.NewRetryingQuerier(next, tracingInterceptor{tracer: otel.Tracer(dbTracerName)})
}

// tracingInterceptor uses the QueryRetrier seam as a non-retrying query interceptor, like failoverResetRetrier.
type tracingInterceptor struct {
	tracer trace.Tracer
}

var _ sqlc.QueryRetrier = tracingInterceptor{}

func (t tracingInterceptor) RetryQuery(ctx context.Context, operationName string, fn func() error) error {
	// Trace only inside an existing trace; parentless background polls would otherwise each export their own root trace.
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return fn()
	}
	_, span := t.tracer.Start(ctx, "db."+operationName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.DBSystemPostgreSQL, semconv.DBOperationName(operationName)),
	)
	completed := false
	// Ending via defer keeps a panic recovered upstream from exporting a success-looking span.
	defer func() {
		if !completed {
			span.SetStatus(codes.Error, "query panicked")
		}
		span.End()
	}()

	err := fn()
	completed = true
	// sql.ErrNoRows is an expected outcome for lookups, not a span failure.
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}
