package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"golang.org/x/net/http2"
)

type clientFunc[T any] func(connect.HTTPClient, string, ...connect.ClientOption) T

// CreateClient is a generic function for creating Connect clients with standardized interceptors
func CreateClient[T any](
	ctor clientFunc[T],
	connectionInfo networking.ConnectionInfo,
	opts ...connect.ClientOption,
) (T, error) {
	var empty T

	// Validate inputs
	if ctor == nil {
		return empty, fmt.Errorf("constructor function cannot be nil")
	}

	// Create standard interceptors that replace current client logic
	clientInterceptors := []connect.Interceptor{
		interceptors.NewAuthInterceptor(),         // Bearer token injection
		interceptors.NewErrorMappingInterceptor(), // Error handling and mapping
		interceptors.NewRetryInterceptor(),        // Retry logic with exponential backoff
		interceptors.NewLoggingInterceptor(),      // Request/response logging
	}

	// Combine with user-provided options
	allOpts := append([]connect.ClientOption{
		connect.WithInterceptors(clientInterceptors...),
		connect.WithGRPC(),
	}, opts...)

	fallbackClient := NewFallbackClient()
	// Create client using the provided constructor
	client := ctor(fallbackClient, connectionInfo.GetURL().String(), allOpts...)

	return client, nil
}

type fallbackClient struct {
	http2Transport *http2.Transport
}

func NewFallbackClient() *http.Client {
	// Create HTTP/2 transport for gRPC/Connect
	http2Transport := &http2.Transport{
		AllowHTTP: true, // Allow HTTP/2 over plain HTTP (h2c)
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}

	return &http.Client{
		Transport: &fallbackClient{
			http2Transport: http2Transport,
		},
	}
}

func (f *fallbackClient) RoundTrip(req *http.Request) (*http.Response, error) {
	// gRPC/Connect requires HTTP/2. The only fallback is from HTTPS to HTTP (not HTTP/2 to HTTP/1.1)

	if req.URL.Scheme == "https" {
		// Try HTTP/2 over HTTPS
		resp, err := f.http2Transport.RoundTrip(req)
		if err == nil {
			return resp, nil
		}

		// If HTTPS fails, fallback to HTTP/2 over plain HTTP (h2c)
		httpReq := req.Clone(req.Context())
		httpReq.URL.Scheme = "http"
		resp, err = f.http2Transport.RoundTrip(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to gRPC server at %s (tried HTTPS/HTTP2 and HTTP/HTTP2): %w", req.URL.String(), err)
		}
		return resp, nil
	}

	// For HTTP connections, use HTTP/2 (h2c)
	resp, err := f.http2Transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server at %s over HTTP/2: %w", req.URL.String(), err)
	}
	return resp, nil
}
