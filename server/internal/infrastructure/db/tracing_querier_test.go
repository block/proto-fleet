package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func newRecordingInterceptor() (*tracetest.SpanRecorder, tracingInterceptor, trace.Tracer) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer("test")
	return recorder, tracingInterceptor{tracer: tracer}, tracer
}

func TestTracingInterceptorNestsSpanUnderRequestSpan(t *testing.T) {
	recorder, interceptor, tracer := newRecordingInterceptor()

	ctx, requestSpan := tracer.Start(context.Background(), "http.request")
	err := interceptor.RetryQuery(ctx, "GetUserById", func() error { return nil })
	require.NoError(t, err)
	requestSpan.End()

	spans := recorder.Ended()
	require.Len(t, spans, 2)

	dbSpan := spans[0]
	require.Equal(t, "db.GetUserById", dbSpan.Name())
	require.Equal(t, trace.SpanKindClient, dbSpan.SpanKind())
	require.Equal(t, requestSpan.SpanContext().SpanID(), dbSpan.Parent().SpanID())
	require.Equal(t, requestSpan.SpanContext().TraceID(), dbSpan.SpanContext().TraceID())
	require.Equal(t, codes.Unset, dbSpan.Status().Code)

	attrs := attribute.NewSet(dbSpan.Attributes()...)
	requireAttr(t, attrs, "db.system", "postgresql")
	requireAttr(t, attrs, "db.operation.name", "GetUserById")
}

func TestTracingInterceptorRecordsQueryError(t *testing.T) {
	recorder, interceptor, tracer := newRecordingInterceptor()
	ctx, requestSpan := tracer.Start(context.Background(), "http.request")
	defer requestSpan.End()

	queryErr := errors.New("connection refused to 10.0.0.5")
	err := interceptor.RetryQuery(ctx, "GetUserById", func() error { return queryErr })
	require.ErrorIs(t, err, queryErr)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, "query failed", spans[0].Status().Description)
	require.Empty(t, spans[0].Events(), "raw error text must not be exported as an event")
}

func TestTracingInterceptorExportsOnlySQLStateForPostgresErrors(t *testing.T) {
	recorder, interceptor, tracer := newRecordingInterceptor()
	ctx, requestSpan := tracer.Start(context.Background(), "http.request")
	defer requestSpan.End()

	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key", Detail: `Key (email)=(pii@example.com) already exists.`}
	err := interceptor.RetryQuery(ctx, "GetUserById", func() error { return fmt.Errorf("insert user: %w", pgErr) })
	require.ErrorIs(t, err, error(pgErr))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, "SQLSTATE 23505", spans[0].Status().Description)
	require.Empty(t, spans[0].Events())
}

func TestTracingInterceptorTreatsNoRowsAsSuccess(t *testing.T) {
	recorder, interceptor, tracer := newRecordingInterceptor()
	ctx, requestSpan := tracer.Start(context.Background(), "http.request")
	defer requestSpan.End()

	err := interceptor.RetryQuery(ctx, "GetUserById", func() error { return sql.ErrNoRows })
	require.ErrorIs(t, err, sql.ErrNoRows)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	require.Empty(t, spans[0].Events())
}

func TestTracingInterceptorSkipsParentlessQueries(t *testing.T) {
	recorder, interceptor, _ := newRecordingInterceptor()

	calls := 0
	queryErr := errors.New("connection refused")
	err := interceptor.RetryQuery(context.Background(), "GetUserById", func() error { calls++; return queryErr })
	require.ErrorIs(t, err, queryErr)
	require.Equal(t, 1, calls)
	require.Empty(t, recorder.Ended())
}

func TestTracingInterceptorMarksPanickedQueries(t *testing.T) {
	recorder, interceptor, tracer := newRecordingInterceptor()
	ctx, requestSpan := tracer.Start(context.Background(), "http.request")
	defer requestSpan.End()

	require.Panics(t, func() {
		_ = interceptor.RetryQuery(ctx, "GetUserById", func() error { panic("scan exploded") })
	})

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, "query panicked", spans[0].Status().Description)
}

// stubQuerier panics on every method except the one overridden below.
type stubQuerier struct {
	sqlc.Querier
}

func (stubQuerier) GetUserById(context.Context, int64) (sqlc.User, error) {
	return sqlc.User{ID: 42}, nil
}

func TestNewTracingQuerierTracesGeneratedMethods(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() {
		// Reset to no-ops: restoring the pre-test defaults would re-install delegators already bound to this test's provider.
		otel.SetTracerProvider(noop.NewTracerProvider())
	})

	ctx, requestSpan := otel.Tracer("test").Start(context.Background(), "http.request")
	defer requestSpan.End()

	q := NewTracingQuerier(stubQuerier{})
	user, err := q.GetUserById(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), user.ID)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "db.GetUserById", spans[0].Name())
}

func requireAttr(t *testing.T, attrs attribute.Set, key attribute.Key, want string) {
	t.Helper()
	got, ok := attrs.Value(key)
	require.True(t, ok, "missing attribute %s", key)
	require.Equal(t, want, got.AsString())
}
