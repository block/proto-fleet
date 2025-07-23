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

	// Create HTTP client with proper H2C support
	var httpClient *http.Client

	switch connectionInfo.Protocol {
	case networking.ProtocolHTTP:
		httpClient = &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			},
		}
	case networking.ProtocolHTTPS:
		httpClient = http.DefaultClient
	case networking.ProtocolTCP:
		return empty, fmt.Errorf("protocol %s not supported", connectionInfo.Protocol)
	}

	// Create client using the provided constructor
	client := ctor(httpClient, connectionInfo.GetURL().String(), allOpts...)

	return client, nil
}
