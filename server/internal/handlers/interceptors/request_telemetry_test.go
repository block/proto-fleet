package interceptors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func requestSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
		otel.SetTracerProvider(noop.NewTracerProvider())
	})
	return recorder
}

func TestRequestTelemetryClassifiesHTTPUnaryResults(t *testing.T) {
	for _, tc := range []struct {
		name      string
		procedure string
		err       error
		status    int
		resource  string
	}{
		{"api success", "/test.v1.APIService/Call", nil, http.StatusOK, "fleet.api.unary"},
		{"api server failure", "/test.v1.APIService/Call", connect.NewError(connect.CodeInternal, nil), http.StatusInternalServerError, "fleet.api.unary"},
		{"api invalid input", "/test.v1.APIService/Call", connect.NewError(connect.CodeInvalidArgument, nil), http.StatusBadRequest, "fleet.api.unary"},
		{"node heartbeat", "/fleetnodegateway.v1.FleetNodeGatewayService/UploadHeartbeat", nil, http.StatusOK, "fleet.node.unary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := requestSpanRecorder(t)
			handler := connect.NewUnaryHandler(tc.procedure,
				func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
					return connect.NewResponse(&emptypb.Empty{}), tc.err
				}, connect.WithInterceptors(NewRequestTelemetryInterceptor()))
			mux := http.NewServeMux()
			mux.Handle(tc.procedure, handler)
			response := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.procedure, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			middleware.TelemetryMiddleware{}.Wrap(mux).ServeHTTP(response, req)

			require.Equal(t, tc.status, response.Code)
			spans := recorder.Ended()
			require.Len(t, spans, 1, "classification must reuse the existing server span")
			require.Equal(t, "POST "+tc.procedure, spans[0].Name())
			attrs := attribute.NewSet(spans[0].Attributes()...)
			resource, ok := attrs.Value("resource.name")
			require.True(t, ok)
			require.Equal(t, tc.resource, resource.AsString())
			route, ok := attrs.Value("http.route")
			require.True(t, ok)
			require.Equal(t, tc.procedure, route.AsString())
			require.Equal(t, tc.status >= 500, spans[0].Status().Code == codes.Error)
		})
	}
}

func TestRequestTelemetryClassifiesBeforeAdmissionRejects(t *testing.T) {
	recorder := requestSpanRecorder(t)
	_, span := otel.Tracer("test").Start(t.Context(), "POST", trace.WithSpanKind(trace.SpanKindServer))
	ctx := trace.ContextWithSpan(t.Context(), span)
	interceptor := NewRequestTelemetryInterceptor()
	response, err := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(connect.CodeUnavailable, nil)
	})(ctx, connect.NewRequest(&emptypb.Empty{}))
	span.End()

	require.Nil(t, response)
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
	attrs := attribute.NewSet(recorder.Ended()[0].Attributes()...)
	resource, ok := attrs.Value("resource.name")
	require.True(t, ok)
	require.Equal(t, "fleet.api.unary", resource.AsString())
}

func TestRequestTelemetryLeavesStreamsAndHealthOutsideUnaryPopulation(t *testing.T) {
	recorder := requestSpanRecorder(t)
	ctx, span := otel.Tracer("test").Start(t.Context(), "POST", trace.WithSpanKind(trace.SpanKindServer))
	called := false
	err := NewRequestTelemetryInterceptor().WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error {
		called = true
		return nil
	})(ctx, nil)
	span.End()
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, "POST", recorder.Ended()[0].Name())

	middleware.TelemetryMiddleware{}.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/active", nil))
	spans := recorder.Ended()
	require.Len(t, spans, 2)
	require.Equal(t, "GET", spans[1].Name())
	attrs := attribute.NewSet(spans[1].Attributes()...)
	_, hasClass := attrs.Value("resource.name")
	require.False(t, hasClass)
}
