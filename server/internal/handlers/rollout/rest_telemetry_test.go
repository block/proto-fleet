package rollout

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

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestRESTTelemetryClassifiesOneHTTPSpanBeforeDecodingAndRPCDispatch(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, procedure string
		handlerError, admissionError        error
		status, dispatches, handlerCalls    int
	}{
		{
			name: "REST success", method: http.MethodGet, path: "/api/v1/rollouts",
			procedure: rolloutv1connect.RolloutServiceListRolloutsProcedure,
			status:    http.StatusOK, dispatches: 1, handlerCalls: 1,
		},
		{
			name: "REST server failure", method: http.MethodGet, path: "/api/v1/rollouts",
			procedure:    rolloutv1connect.RolloutServiceListRolloutsProcedure,
			handlerError: connect.NewError(connect.CodeInternal, nil),
			status:       http.StatusInternalServerError, dispatches: 1, handlerCalls: 1,
		},
		{
			name: "REST admission failure", method: http.MethodGet, path: "/api/v1/rollouts",
			procedure:      rolloutv1connect.RolloutServiceListRolloutsProcedure,
			admissionError: connect.NewError(connect.CodeUnavailable, nil),
			status:         http.StatusServiceUnavailable, dispatches: 1,
		},
		{
			name: "REST authentication failure", method: http.MethodGet, path: "/api/v1/rollouts",
			procedure:      rolloutv1connect.RolloutServiceListRolloutsProcedure,
			admissionError: connect.NewError(connect.CodeUnauthenticated, nil),
			status:         http.StatusUnauthorized, dispatches: 1,
		},
		{
			name: "REST invalid query", method: http.MethodGet, path: "/api/v1/rollouts?unexpected=1",
			procedure: rolloutv1connect.RolloutServiceListRolloutsProcedure,
			status:    http.StatusBadRequest,
		},
		{
			name: "REST invalid path ID", method: http.MethodGet, path: "/api/v1/rollouts/not-an-id",
			procedure: rolloutv1connect.RolloutServiceGetRolloutProcedure,
			status:    http.StatusBadRequest,
		},
		{
			name: "REST malformed JSON", method: http.MethodPost, path: "/api/v1/release-channels", body: "{",
			procedure: rolloutv1connect.RolloutServiceCreateReleaseChannelProcedure,
			status:    http.StatusBadRequest,
		},
		{
			name: "direct RPC success", method: http.MethodPost, path: rolloutv1connect.RolloutServiceListRolloutsProcedure, body: "{}",
			procedure: rolloutv1connect.RolloutServiceListRolloutsProcedure,
			status:    http.StatusOK, dispatches: 1, handlerCalls: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				require.NoError(t, provider.Shutdown(context.Background()))
				otel.SetTracerProvider(noop.NewTracerProvider())
			})

			dispatches, handlerCalls := 0, 0
			rpc := connect.NewUnaryHandler(rolloutv1connect.RolloutServiceListRolloutsProcedure,
				func(context.Context, *connect.Request[pb.ListRolloutsRequest]) (*connect.Response[pb.ListRolloutsResponse], error) {
					handlerCalls++
					return connect.NewResponse(&pb.ListRolloutsResponse{}), tc.handlerError
				}, connect.WithInterceptors(interceptors.NewRequestTelemetryInterceptor(), connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
					return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
						dispatches++
						if tc.admissionError != nil {
							return nil, tc.admissionError
						}
						return next(ctx, request)
					}
				})))
			mux := http.NewServeMux()
			mux.Handle(rolloutv1connect.RolloutServiceListRolloutsProcedure, rpc)
			RegisterRESTRoutes(mux, rpc)
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			middleware.TelemetryMiddleware{}.Wrap(mux).ServeHTTP(response, request)

			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, tc.dispatches, dispatches)
			require.Equal(t, tc.handlerCalls, handlerCalls)
			spans := recorder.Ended()
			require.Len(t, spans, 1, "REST and RPC classification must update one existing HTTP span")
			require.Equal(t, trace.SpanKindServer, spans[0].SpanKind())
			require.Equal(t, tc.status >= 500, spans[0].Status().Code == codes.Error)
			attrs := attribute.NewSet(spans[0].Attributes()...)
			resource, ok := attrs.Value("resource.name")
			require.True(t, ok)
			require.Equal(t, "fleet.api.unary", resource.AsString())
			route, ok := attrs.Value("http.route")
			require.True(t, ok)
			require.Equal(t, tc.procedure, route.AsString(), "only the registered procedure may enter telemetry attributes")
		})
	}
}
