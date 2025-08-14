package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto/client/interceptors"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"golang.org/x/net/http2"
)

type clientFunc[T any] func(connect.HTTPClient, string, ...connect.ClientOption) T

func createHTTPClient() *http.Client {
	transport := &http2.Transport{
		// Enable HTTP/2 over cleartext connections
		AllowHTTP: true,

		// Custom dialer that bypasses TLS for HTTP connections
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}
			return dialer.DialContext(ctx, network, addr)
		},

		// Timeouts to prevent hanging connections
		ReadIdleTimeout:  30 * time.Second,
		PingTimeout:      15 * time.Second,
		WriteByteTimeout: 10 * time.Second,

		// Connection management
		StrictMaxConcurrentStreams: true, // Enforce stream limits
		CountError: func(errType string) {
			slog.Debug("HTTP/2 transport error", "type", errType)
		},
	}

	return &http.Client{
		Transport: transport,
	}
}

// shouldSkipTLSVerification checks if TLS certificate verification should be skipped
// based on environment variables. This is useful for testing scenarios.
func shouldSkipTLSVerification() bool {
	skipVerify := strings.ToLower(os.Getenv("SKIP_TLS_VERIFY"))
	insecureTLS := strings.ToLower(os.Getenv("INSECURE_TLS"))

	return skipVerify == "true" || insecureTLS == "true"
}

func createHTTPSClient() *http.Client {
	// Use http.DefaultClient's transport as base for maximum compatibility
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		slog.Warn("http.DefaultTransport is not *http.Transport, falling back to new transport")
		defaultTransport = &http.Transport{}
	}
	clonedTransport := defaultTransport.Clone()

	clonedTransport.MaxIdleConns = 2000       // Global idle connection pool
	clonedTransport.MaxIdleConnsPerHost = 100 // Per-host idle connections
	clonedTransport.MaxConnsPerHost = 200     // Max connections per host
	clonedTransport.IdleConnTimeout = 90 * time.Second

	// Connection reuse and management
	clonedTransport.DisableKeepAlives = false  // Enable connection reuse
	clonedTransport.DisableCompression = false // Enable gzip compression

	// Timeouts to prevent hanging connections and resource leaks
	clonedTransport.TLSHandshakeTimeout = 10 * time.Second
	clonedTransport.ResponseHeaderTimeout = 30 * time.Second
	clonedTransport.ExpectContinueTimeout = 1 * time.Second

	// TCP keep-alive settings to detect dead connections
	clonedTransport.DialContext = (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}).DialContext

	// Enable HTTP/2 for better multiplexing
	clonedTransport.ForceAttemptHTTP2 = true

	// Configure TLS based on environment variables
	if shouldSkipTLSVerification() {
		slog.Debug("TLS certificate verification disabled via environment variable")
		clonedTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // #nosec G402 -- this is only enabled in test runs via environment variable
		}
	} else {
		clonedTransport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12, // Minimum TLS 1.2
			// Let Go choose the cipher suites (secure defaults)
		}
	}

	if err := http2.ConfigureTransport(clonedTransport); err != nil {
		slog.Warn("Failed to configure HTTP/2 for HTTPS client", "error", err)
	}

	return &http.Client{
		Transport: clonedTransport,
	}
}

var (
	httpClientOnce  sync.Once
	httpsClientOnce sync.Once
	httpClient      *http.Client
	httpsClient     *http.Client
)

// getHTTPClient returns the HTTP client, creating it lazily if needed
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		httpClient = createHTTPClient()
	})
	return httpClient
}

// getHTTPSClient returns the HTTPS client, creating it lazily if needed
func getHTTPSClient() *http.Client {
	httpsClientOnce.Do(func() {
		httpsClient = createHTTPSClient()
	})
	return httpsClient
}

// ResetClients resets the client singletons, forcing them to be recreated on next use.
// This is useful for testing when environment variables need to be changed at runtime.
// Note: This function should only be used in tests.
func ResetClients() {
	httpClientOnce = sync.Once{}
	httpsClientOnce = sync.Once{}
	httpClient = nil
	httpsClient = nil
}

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

	// Log connection details for debugging
	url := connectionInfo.GetURL()
	if url == nil {
		return empty, fmt.Errorf("invalid connection info: URL is nil")
	}
	slog.Debug("Creating client", "url", url.String(), "protocol", connectionInfo.Protocol.String())

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

	// Select appropriate HTTP client based on protocol
	var selectedClient *http.Client

	switch connectionInfo.Protocol {
	case networking.ProtocolHTTP:
		selectedClient = getHTTPClient() // HTTP/2 over cleartext (h2c) client
		slog.Debug("Using HTTP client for connection")
	case networking.ProtocolHTTPS:
		selectedClient = getHTTPSClient() // TLS secure client
		slog.Debug("Using HTTPS client for connection")
	case networking.ProtocolTCP:
		return empty, fmt.Errorf("protocol %s not supported", connectionInfo.Protocol)
	default:
		return empty, fmt.Errorf("unsupported protocol: %s", connectionInfo.Protocol)
	}

	if selectedClient == nil {
		return empty, fmt.Errorf("failed to create HTTP client for protocol %s", connectionInfo.Protocol)
	}

	// Create client using the provided constructor
	client := ctor(selectedClient, url.String(), allOpts...)

	return client, nil
}
