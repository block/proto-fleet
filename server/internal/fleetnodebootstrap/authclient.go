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
		token := b.tokenSource()
		if token == "" {
			return &failingStreamingClientConn{
				StreamingClientConn: conn,
				err:                 connect.NewError(connect.CodeUnauthenticated, errors.New("no session token available")),
			}
		}
		conn.RequestHeader().Set("Authorization", "Bearer "+token)
		return conn
	}
}

// failingStreamingClientConn wraps a real conn but forces Send/Receive to
// surface a fixed error. Used by WrapStreamingClient when the token source
// returns empty so streaming callers fail fast like the unary path,
// instead of opening an unauthenticated stream that errors later.
type failingStreamingClientConn struct {
	connect.StreamingClientConn
	err error
}

func (c *failingStreamingClientConn) Send(any) error    { return c.err }
func (c *failingStreamingClientConn) Receive(any) error { return c.err }

func (b *bearerAuth) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
