package bootstrap

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
)

// tokenSource is invoked per-call so a daemon that mutates its own
// state.SessionToken via Refresh picks up the new value on the next
// request without rebuilding the client.
func NewAuthenticatedGatewayClient(serverURL string, tokenSource func() string) (fleetnodegatewayv1connect.FleetNodeGatewayServiceClient, error) {
	httpClient, err := newGatewayHTTPClient(serverURL)
	if err != nil {
		return nil, err
	}
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(
		httpClient,
		serverURL,
		connect.WithInterceptors(bearerInterceptor(tokenSource)),
	), nil
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
		token := b.tokenSource()
		if token == "" {
			return &failingStreamingClientConn{
				spec: spec,
				err:  connect.NewError(connect.CodeUnauthenticated, errors.New("no session token available")),
			}
		}
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+token)
		return conn
	}
}

// failingStreamingClientConn is a stub StreamingClientConn that surfaces
// the configured error from every operation that could open or progress
// the underlying HTTP request. The real conn is never constructed, so a
// stream open with no session token does not hit the network at all.
type failingStreamingClientConn struct {
	spec connect.Spec
	err  error
}

func (c *failingStreamingClientConn) Spec() connect.Spec           { return c.spec }
func (c *failingStreamingClientConn) Peer() connect.Peer           { return connect.Peer{} }
func (c *failingStreamingClientConn) Send(any) error               { return c.err }
func (c *failingStreamingClientConn) RequestHeader() http.Header   { return http.Header{} }
func (c *failingStreamingClientConn) CloseRequest() error          { return c.err }
func (c *failingStreamingClientConn) Receive(any) error            { return c.err }
func (c *failingStreamingClientConn) ResponseHeader() http.Header  { return http.Header{} }
func (c *failingStreamingClientConn) ResponseTrailer() http.Header { return http.Header{} }
func (c *failingStreamingClientConn) CloseResponse() error         { return c.err }

func (b *bearerAuth) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
