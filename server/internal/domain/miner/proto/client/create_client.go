package client

import (
	"fmt"
	"net/http"
	"net/url"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client/interceptors"
)

type clientFunc[T any] func(connect.HTTPClient, string, ...connect.ClientOption) T

// CreateClient is a generic function for creating Connect clients with standardized interceptors
func CreateClient[T any](
	ctor clientFunc[T],
	addr string,
	opts ...connect.ClientOption,
) (T, error) {
	var empty T

	// Validate inputs
	if ctor == nil {
		return empty, fmt.Errorf("constructor function cannot be nil")
	}
	if addr == "" {
		return empty, fmt.Errorf("address cannot be empty")
	}

	// Parse and validate address
	parsedURL, err := url.Parse(addr)
	if err != nil {
		return empty, fmt.Errorf("invalid address format: %w", err)
	}

	// Ensure scheme is set (default to https)
	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "https"
		addr = parsedURL.String()
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
	client := ctor(fallbackClient, addr, allOpts...)

	return client, nil
}

type fallbackClient struct {
	http.RoundTripper
}

func NewFallbackClient() *http.Client {
	return &http.Client{
		Transport: &fallbackClient{
			RoundTripper: http.DefaultTransport,
		},
	}
}

func (f *fallbackClient) RoundTrip(req *http.Request) (*http.Response, error) {
	// Attempt the request using the configured RoundTripper
	resp, err := f.RoundTripper.RoundTrip(req)
	if err != nil && req.URL.Scheme == "https" {
		// If it fails and the scheme is HTTPS, fallback to HTTP
		req.URL.Scheme = "http"
		resp, err = f.RoundTripper.RoundTrip(req)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to %s: %w", req.URL.String(), err)
		}
	}
	return resp, nil
}
