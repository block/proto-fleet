package fleetnodebootstrap

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
)

// tokenSource is invoked per-call so a daemon that mutates its own
// state.SessionToken via Refresh picks up the new value on the next
// request without rebuilding the client.
func NewAuthenticatedGatewayClient(serverURL string, tokenSource func() string) fleetnodegatewayv1connect.FleetNodeGatewayServiceClient {
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(
		newGatewayHTTPClient(),
		serverURL,
		connect.WithInterceptors(bearerInterceptor(tokenSource)),
	)
}

func bearerInterceptor(tokenSource func() string) connect.Interceptor {
	return &bearerAuth{tokenSource: tokenSource}
}

type bearerAuth struct {
	tokenSource func() string
}

func (b *bearerAuth) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		token := b.tokenSource()
		if token == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("no session token available"))
		}
		req.Header().Set("Authorization", "Bearer "+token)
		return next(ctx, req)
	}
}

func (b *bearerAuth) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		if token := b.tokenSource(); token != "" {
			conn.RequestHeader().Set("Authorization", "Bearer "+token)
		}
		return conn
	}
}

func (b *bearerAuth) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
