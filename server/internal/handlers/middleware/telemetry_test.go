package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// serveTracedRequest runs one request carrying a W3C traceparent header through
// the middleware against a fresh recorder, swapping the otel globals the
// middleware reads (reset to no-ops via t.Cleanup).
func serveTracedRequest(t *testing.T, trust bool) sdktrace.ReadOnlySpan {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		// Reset to no-ops: restoring the pre-test defaults would re-install delegators already bound to this test's provider.
		otel.SetTracerProvider(noop.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	})

	handler := middleware.TelemetryMiddleware{TrustIncomingTraces: trust}.Wrap(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Traceparent", "00-"+clientTraceID+"-"+clientSpanID+"-01")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	return spans[0]
}

const (
	clientTraceID = "0af7651916cd43dd8448eb211c80319c"
	clientSpanID  = "b7ad6b7169203331"
)

func TestTelemetryMiddlewareUntrustedStartsNewTrace(t *testing.T) {
	span := serveTracedRequest(t, false)

	require.NotEqual(t, clientTraceID, span.SpanContext().TraceID().String())
	require.False(t, span.Parent().IsValid())
	// Public-endpoint mode keeps the client context as a link, not a parent.
	require.Len(t, span.Links(), 1)
	require.Equal(t, clientTraceID, span.Links()[0].SpanContext.TraceID().String())
}

func TestTelemetryMiddlewareTrustedParentsToClientTrace(t *testing.T) {
	span := serveTracedRequest(t, true)

	require.Equal(t, clientTraceID, span.SpanContext().TraceID().String())
	require.Equal(t, clientSpanID, span.Parent().SpanID().String())
}
