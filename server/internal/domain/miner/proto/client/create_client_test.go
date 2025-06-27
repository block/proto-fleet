package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestCreateClient(t *testing.T) {
	tests := []struct {
		name        string
		ctor        func(connect.HTTPClient, string, ...connect.ClientOption) miner_data_apiconnect.MinerDataApiClient
		httpClient  connect.HTTPClient
		ip          string
		port        string
		expectError bool
	}{
		{
			name:        "valid parameters",
			ctor:        miner_data_apiconnect.NewMinerDataApiClient,
			ip:          "localhost",
			port:        "8080",
			expectError: false,
		},
		{
			name:        "nil constructor",
			ctor:        nil,
			ip:          "localhost",
			port:        "8080",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connectionInfo, err := networking.NewConnectionInfo(tt.ip, tt.port, networking.ProtocolHTTPS)
			require.NoError(t, err)
			client, err := CreateClient(tt.ctor, *connectionInfo)

			if tt.expectError {
				require.Error(t, err, "expected an error but got none")
				return
			}

			require.NoError(t, err, "expected no error but got one")
			require.NotNil(t, client, "expected client to be created but got nil")
		})
	}
}

func TestContextWithAuth(t *testing.T) {
	t.Run("no panic", func(t *testing.T) {
		authCtx := ContextWithAuth(t.Context(), secrets.NewText("test-token"))
		require.NotNil(t, authCtx, "expected context with auth token to be created")
	})
}

func TestNewFallbackClient(t *testing.T) {
	t.Run("creates client with HTTP/2 transport", func(t *testing.T) {
		client := NewFallbackClient()
		require.NotNil(t, client, "expected client to be created")
		require.NotNil(t, client.Transport, "expected transport to be set")

		// Verify it's our fallback client
		fallbackTransport, ok := client.Transport.(*fallbackClient)
		require.True(t, ok, "expected transport to be fallbackClient")
		require.NotNil(t, fallbackTransport.http2Transport, "expected HTTP/2 transport to be set")

		// Verify HTTP/2 transport is configured
		require.IsType(t, &http2.Transport{}, fallbackTransport.http2Transport, "expected HTTP/2 transport")
	})
}

// TestFallbackClientIntegration tests the actual NewFallbackClient against real local services
func TestFallbackClientIntegration(t *testing.T) {
	t.Run("HTTP server succeeds - tests h2c functionality", func(t *testing.T) {
		// Start HTTP server that supports h2c
		httpPort := findFreePort(t)
		httpServer := startRealHTTPServer(t, httpPort)
		defer httpServer.Close()

		// Test the real fallback client with HTTP request (h2c)
		client := NewFallbackClient()
		url := fmt.Sprintf("http://localhost:%d/test", httpPort)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		req = req.WithContext(ctx)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		defer resp.Body.Close()
	})

	t.Run("HTTPS request falls back to HTTP - tests fallback logic", func(t *testing.T) {
		// Start only HTTP server (h2c)
		httpPort := findFreePort(t)
		httpServer := startRealHTTPServer(t, httpPort)
		defer httpServer.Close()

		// Test the real fallback client with HTTPS URL that should fallback to HTTP
		// This tests the core fallback functionality
		client := NewFallbackClient()
		url := fmt.Sprintf("https://localhost:%d/test", httpPort)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		req = req.WithContext(ctx)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		defer resp.Body.Close()
	})

	t.Run("both HTTPS and HTTP fail - connection refused", func(t *testing.T) {
		// Use a port that's guaranteed to be closed
		closedPort := findFreePort(t)

		// Test the real fallback client
		client := NewFallbackClient()
		url := fmt.Sprintf("https://localhost:%d/test", closedPort)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		req = req.WithContext(ctx)

		resp, err := client.Do(req)
		require.Error(t, err)
		if resp != nil {
			defer resp.Body.Close()
		}
		require.Nil(t, resp)
		require.Contains(t, err.Error(), "failed to connect to gRPC server")
	})

	t.Run("HTTP request fails - connection refused", func(t *testing.T) {
		// Use a port that's guaranteed to be closed
		closedPort := findFreePort(t)

		// Test the real fallback client
		client := NewFallbackClient()
		url := fmt.Sprintf("http://localhost:%d/test", closedPort)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		req = req.WithContext(ctx)

		resp, err := client.Do(req)
		require.Error(t, err)
		if resp != nil {
			defer resp.Body.Close()
		}
		require.Nil(t, resp)
		require.Contains(t, err.Error(), "failed to connect to gRPC server")
	})
}

// Helper function to find a free port
func findFreePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "localhost:0") //nolint:gosec // Test function binding to localhost only
	require.NoError(t, err)
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "expected TCP address")
	return addr.Port
}

// Helper function to start a real HTTP server
func startRealHTTPServer(t *testing.T, port int) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("HTTP OK"))
		if err != nil {
			t.Logf("Failed to write response: %v", err)
		}
	})

	// Create HTTP/2 server that supports h2c (HTTP/2 over cleartext)
	h2s := &http2.Server{}
	server := &http.Server{
		Addr:              fmt.Sprintf("localhost:%d", port),
		Handler:           h2c.NewHandler(mux, h2s), // h2c handler for HTTP/2 over cleartext
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server in goroutine
	go func() {
		listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port)) //nolint:gosec // Test server binding to localhost only
		if err != nil {
			t.Logf("Failed to create listener: %v", err)
			return
		}
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Logf("HTTP server error: %v", err)
		}
	}()

	// Wait for server to start
	waitForServer(t, fmt.Sprintf("localhost:%d", port), false)
	return server
}

// Helper function to wait for server to be ready
func waitForServer(t *testing.T, addr string, isHTTPS bool) {
	scheme := "http"
	if isHTTPS {
		scheme = "https"
	}

	for range 50 { // Wait up to 5 seconds
		client := &http.Client{
			Timeout: 100 * time.Millisecond,
		}
		if isHTTPS {
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // Skip verification for test certificates
				},
			}
		}

		resp, err := client.Get(fmt.Sprintf("%s://%s/test", scheme, addr))
		if err == nil {
			defer resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Server at %s did not start within timeout", addr)
}
