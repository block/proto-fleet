package interceptors

import (
	"context"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// NewRequestTelemetryInterceptor classifies fleetd's existing HTTP server span
// before admission/authentication can reject a unary request. Fixed resource
// names let native APM statistics separate API traffic from node heartbeats,
// health probes and long-lived streams without relying on indexed-span samples.
func NewRequestTelemetryInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
			middleware.ClassifyUnaryRequest(ctx, request.Spec().Procedure)
			return next(ctx, request)
		}
	})
}
