package proto

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	decimalBase = 10
	int32Bits   = 32
)

// TestClientCreation tests the NewClient function with different configurations
func TestClientCreation(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		port    int32
		scheme  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid HTTP client",
			host:    "192.168.1.100",
			port:    80,
			scheme:  "http",
			wantErr: false,
		},
		{
			name:    "valid HTTPS client",
			host:    "192.168.1.100",
			port:    80,
			scheme:  "https",
			wantErr: false,
		},
		{
			name:    "localhost HTTP",
			host:    "localhost",
			port:    8080,
			scheme:  "http",
			wantErr: false,
		},
		{
			name:    "IPv6 address",
			host:    "::1",
			port:    80,
			scheme:  "http",
			wantErr: false,
		},
		{
			name:    "custom port HTTPS",
			host:    "miner.local",
			port:    8443,
			scheme:  "https",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.host, tt.port, tt.scheme)

			if tt.wantErr {
				assert.Error(t, err, "Expected an error")
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg, "Error message should contain expected text")
				}
				return
			}

			require.NoError(t, err, "Should not return an error")
			require.NotNil(t, client, "Client should not be nil")

			// Verify client properties
			assert.Contains(t, client.baseURL, tt.host, "BaseURL should contain host")
			assert.NotNil(t, client.httpClient, "HTTP client should be set")

			// Test Close method
			err = client.Close()
			assert.NoError(t, err, "Close() should not return error")
		})
	}
}

// TestHTTPClientCreation tests HTTP client creation and configuration
func TestHTTPClientCreation(t *testing.T) {
	// Reset clients to ensure fresh state
	resetClients()

	t.Run("HTTP client creation", func(t *testing.T) {
		client := createHTTPClient()
		require.NotNil(t, client, "HTTP client should be created")
		assert.Equal(t, 30*time.Second, client.Timeout, "Timeout should be 30 seconds")
		assert.NotNil(t, client.Transport, "Transport should be set")
	})

	t.Run("HTTP client singleton behavior", func(t *testing.T) {
		client1 := createHTTPClient()
		client2 := createHTTPClient()
		assert.Same(t, client1, client2, "HTTP client should be singleton")
	})
}

// TestHTTPSClientCreation tests HTTPS client creation and TLS configuration
func TestHTTPSClientCreation(t *testing.T) {
	// Reset clients to ensure fresh state
	resetClients()

	t.Run("HTTPS client creation", func(t *testing.T) {
		client := createHTTPSClient()
		require.NotNil(t, client, "HTTPS client should be created")
		assert.Equal(t, 30*time.Second, client.Timeout, "Timeout should be 30 seconds")

		// Verify transport configuration
		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok, "Transport should be *http.Transport")
		require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
		assert.Equal(t, uint16(tls.VersionTLS12), transport.TLSClientConfig.MinVersion, "Min TLS version should be 1.2")
	})

	t.Run("HTTPS client singleton behavior", func(t *testing.T) {
		client1 := createHTTPSClient()
		client2 := createHTTPSClient()
		assert.Same(t, client1, client2, "HTTPS client should be singleton")
	})
}

// TestTLSVerificationConfiguration verifies Proto HTTPS always skips certificate verification.
func TestTLSVerificationConfiguration(t *testing.T) {
	resetClients()

	client := createHTTPSClient()
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify, "Proto HTTPS should always skip certificate verification")
}

// TestCredentialManagement tests credential setting and usage
func TestCredentialManagement(t *testing.T) {
	client, err := NewClient("localhost", 80, "http")
	require.NoError(t, err, "Failed to create client")

	tests := []struct {
		name  string
		token sdk.BearerToken
	}{
		{
			name:  "valid JWT token",
			token: sdk.BearerToken{Token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.signature"},
		},
		{
			name:  "empty token",
			token: sdk.BearerToken{Token: ""},
		},
		{
			name:  "simple token",
			token: sdk.BearerToken{Token: "simple-token-123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.SetCredentials(tt.token)
			require.NoError(t, err, "SetCredentials() should not return error")
			assert.Equal(t, tt.token.Token, client.bearerToken.Token, "Token should be set correctly")
		})
	}
}

// TestClientSingletonBehavior tests that HTTP clients are properly shared
func TestClientSingletonBehavior(t *testing.T) {
	// Reset clients
	resetClients()

	// Create multiple clients with same scheme
	client1, err1 := NewClient("host1", 80, "http")
	client2, err2 := NewClient("host2", 80, "http")

	require.NoError(t, err1, "Failed to create client1")
	require.NoError(t, err2, "Failed to create client2")

	// They should share the same underlying HTTP client
	assert.Same(t, client1.httpClient, client2.httpClient, "HTTP clients should be shared (singleton)")

	// Test with HTTPS
	client3, err3 := NewClient("host3", 80, "https")
	client4, err4 := NewClient("host4", 80, "https")

	require.NoError(t, err3, "Failed to create HTTPS client3")
	require.NoError(t, err4, "Failed to create HTTPS client4")

	// HTTPS clients should share the same underlying HTTP client
	assert.Same(t, client3.httpClient, client4.httpClient, "HTTPS clients should be shared (singleton)")

	// But HTTP and HTTPS clients should be different
	assert.NotSame(t, client1.httpClient, client3.httpClient, "HTTP and HTTPS clients should be different")
}

// TestClientRuntimeConfigChange verifies environment changes do not alter Proto TLS behavior.
func TestClientRuntimeEnvChange(t *testing.T) {
	resetClients()

	t.Setenv("SKIP_TLS_VERIFY", "false")
	client1, err1 := NewClient("localhost", 8443, "https")
	require.NoError(t, err1, "Failed to create first client")
	require.NotNil(t, client1, "First client should be created")
	transport, ok := client1.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify,
		"Proto HTTPS should skip certificate verification regardless of environment")

	t.Setenv("SKIP_TLS_VERIFY", "true")
	resetClients()

	client2, err2 := NewClient("localhost", 8443, "https")
	require.NoError(t, err2, "Failed to create second client")
	require.NotNil(t, client2, "Second client should be created")
	transport, ok = client2.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify,
		"Proto HTTPS should keep skipping certificate verification after environment changes")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingReadCloser struct {
	reader *bytes.Reader
	io.ReadCloser
	readToEOF   bool
	closeCalled bool
}

func (t *trackingReadCloser) Close() error {
	t.closeCalled = true
	t.readToEOF = t.reader.Len() == 0
	if err := t.ReadCloser.Close(); err != nil {
		return fmt.Errorf("tracking read closer close: %w", err)
	}
	return nil
}

func TestDoGetWithStatus_DrainsBodyOnForbidden(t *testing.T) {
	body := &trackingReadCloser{
		reader: bytes.NewReader([]byte(`{"error":{"message":"DEFAULT_PASSWORD_ACTIVE"}}`)),
	}
	body.ReadCloser = io.NopCloser(body.reader)
	client := &Client{
		baseURL: "http://miner.local",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     make(http.Header),
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
	}

	statusCode, err := client.doGetWithStatus(context.Background(), "/api/v1/system", nil)

	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, statusCode)
	assert.True(t, body.readToEOF, "403 response body should be drained before returning")
	assert.True(t, body.closeCalled, "response body should still be closed")
}

func TestDoGetWithStatus_ForbiddenWithoutDefaultPasswordCodeStaysGeneric(t *testing.T) {
	client := &Client{
		baseURL: "http://miner.local",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"ACCESS_DENIED","message":"Access denied"}}`)),
					Request:    req,
				}, nil
			}),
		},
	}

	statusCode, err := client.doGetWithStatus(context.Background(), "/api/v1/system", nil)

	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, statusCode)
	assert.EqualError(t, err, "forbidden: Access denied")
}

func TestDoGetWithStatus_ForbiddenPlainTextDefaultPasswordPreservesMarker(t *testing.T) {
	client := &Client{
		baseURL: "http://miner.local",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("default password must be changed")),
					Request:    req,
				}, nil
			}),
		},
	}

	statusCode, err := client.doGetWithStatus(context.Background(), "/api/v1/system", nil)

	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, statusCode)
	assert.EqualError(t, err, "forbidden: default password must be changed")
}

func TestDirectWriteEndpoints_ClassifyDefaultPasswordForbidden(t *testing.T) {
	tests := []struct {
		name string
		path string
		call func(*Client) error
	}{
		{
			name: "pair",
			path: "/api/v1/pairing/auth-key",
			call: func(client *Client) error {
				return client.Pair(context.Background(), sdk.APIKey{Key: "test-public-key"})
			},
		},
		{
			name: "clear auth key",
			path: "/api/v1/pairing/auth-key",
			call: func(client *Client) error {
				return client.ClearAuthKey(context.Background())
			},
		},
		{
			name: "set cooling mode",
			path: "/api/v1/cooling",
			call: func(client *Client) error {
				return client.SetCoolingMode(context.Background(), sdk.CoolingModeManual)
			},
		},
		{
			name: "set power target",
			path: "/api/v1/mining/target",
			call: func(client *Client) error {
				return client.SetPowerTarget(context.Background(), 3200, sdk.PerformanceModeEfficiency)
			},
		},
		{
			name: "update pools",
			path: "/api/v1/pools",
			call: func(client *Client) error {
				return client.UpdatePools(context.Background(), []Pool{{
					Priority:   0,
					URL:        "stratum+tcp://pool.example.com:3333",
					WorkerName: "worker.1",
				}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.path, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"DEFAULT_PASSWORD_ACTIVE","message":"default password must be changed"}}`))
			}))
			defer server.Close()

			client := newTestClient(t, server)
			defer func() { _ = client.Close() }()

			err := tt.call(client)

			require.Error(t, err)
			assert.EqualError(t, err, "forbidden: default password must be changed")
		})
	}
}

// TestUnsupportedScheme tests handling of unsupported protocol schemes
// This aligns with the server's protocol validation approach
func TestUnsupportedScheme(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
	}{
		{
			name:   "tcp scheme not supported",
			scheme: "tcp",
		},
		{
			name:   "ftp scheme not supported",
			scheme: "ftp",
		},
		{
			name:   "invalid scheme",
			scheme: "invalid",
		},
		{
			name:   "empty scheme",
			scheme: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient("localhost", 80, tt.scheme)

			// Should either return an error or create a client that will fail on actual use
			// The current implementation doesn't validate schemes upfront, but this test
			// documents the expected behavior for future improvements
			require.NoError(t, err, "NewClient should not error on unsupported scheme")
			require.NotNil(t, client, "Client should be created even with unsupported scheme")
			_ = client.Close()
		})
	}
}

// TestClientCreationWithInsecureTLS verifies HTTPS client creation uses Proto's fixed TLS policy.
func TestClientCreationWithInsecureTLS(t *testing.T) {
	resetClients()
	client, err := NewClient("localhost", 8443, "https")
	require.NoError(t, err, "Failed to create client with insecure TLS")
	require.NotNil(t, client, "Client should be created")
	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify,
		"Proto HTTPS should always disable certificate verification")
	_ = client.Close()
}

// TestClientCreationWithoutInsecureTLS verifies environment values do not re-enable verification.
func TestClientCreationWithoutInsecureTLS(t *testing.T) {
	resetClients()
	t.Setenv("SKIP_TLS_VERIFY", "false")
	client, err := NewClient("localhost", 8443, "https")
	require.NoError(t, err, "Failed to create client with secure TLS")
	require.NotNil(t, client, "Client should be created")
	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify,
		"Proto HTTPS should ignore environment attempts to re-enable verification")
	_ = client.Close()
}

// newTestClient creates a Client pointed at the given httptest.Server.
// The singleton HTTP/2 transport is replaced with a plain HTTP/1.1 client so
// that the client works with httptest.Server (which only speaks HTTP/1.1).
func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), decimalBase, int32Bits)
	require.NoError(t, err)
	client, err := NewClient(u.Hostname(), int32(port), "http")
	require.NoError(t, err)
	client.httpClient = &http.Client{}
	return client
}

// TestLoginWithPassword tests the miner login step used by ChangePassword.
func TestLoginWithPassword(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(w http.ResponseWriter, r *http.Request)
		expectErr   bool
		errContains string
	}{
		{
			name: "correct password returns 200 with token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/auth/login", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"test-refresh"}`))
			},
			expectErr: false,
		},
		{
			name: "wrong password returns 401",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			expectErr:   true,
			errContains: "incorrect current password",
		},
		{
			name: "server error returns 500",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectErr:   true,
			errContains: "login failed with status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			client := newTestClient(t, server)
			defer func() { _ = client.Close() }()

			token, err := client.loginWithPassword(context.Background(), "testpassword")
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Empty(t, token)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "test-token", token)
			}
		})
	}
}

// TestChangePassword tests that ChangePassword uses the web UI flow:
// login first (verifying current password and obtaining a JWT), then
// call change-password with that JWT — no fleet Bearer token used.
func TestChangePassword(t *testing.T) {
	t.Run("wrong password: login fails, change-password not called", func(t *testing.T) {
		loginCalled := false
		changeCalled := false

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/auth/login":
				loginCalled = true
				w.WriteHeader(http.StatusUnauthorized)
			case "/api/v1/auth/change-password":
				changeCalled = true
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		defer func() { _ = client.Close() }()

		err := client.ChangePassword(context.Background(), "wrongpassword", "newpassword")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incorrect current password")
		assert.True(t, loginCalled, "login endpoint should be called")
		assert.False(t, changeCalled, "change-password should not be called after login fails")
	})

	t.Run("correct password: login succeeds, change-password called with web UI JWT", func(t *testing.T) {
		const webUIToken = "web-ui-access-token"
		loginCalled := false
		changeCalled := false
		var changeAuthHeader string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/auth/login":
				loginCalled = true
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"access_token":"` + webUIToken + `","refresh_token":"refresh"}`))
			case "/api/v1/auth/change-password":
				changeCalled = true
				changeAuthHeader = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		defer func() { _ = client.Close() }()

		err := client.ChangePassword(context.Background(), "correctpassword", "newpassword")
		require.NoError(t, err)
		assert.True(t, loginCalled, "login endpoint should be called")
		assert.True(t, changeCalled, "change-password endpoint should be called")
		assert.Equal(t, "Bearer "+webUIToken, changeAuthHeader, "change-password should use the web UI JWT, not the fleet Bearer token")
	})
}

// Helper function to reset client singletons for testing
func resetClients() {
	httpClientOnce = &sync.Once{}
	httpsClientOnce = &sync.Once{}
	sharedHTTPClient = nil
	sharedHTTPSClient = nil
}

// newStatusTestServer creates an httptest.Server that serves JSON responses for
// /api/v1/mining, /api/v1/pools, and /api/v1/system endpoints.
func newStatusTestServer(t *testing.T, miningStatus string, pools []poolData, systemErr bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mining":
			resp := miningStatusResponse{
				MiningStatus: miningStatusInner{Status: miningStatus},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/pools":
			resp := poolsList{Pools: pools}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/system":
			if systemErr {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			resp := systemInfoResponse{}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestGetStatusPoolStateOverride tests that the actual pool list is the source of truth
// for determining NeedsMiningPool status, overriding the firmware-reported MiningState.
func TestGetStatusPoolStateOverride(t *testing.T) {
	tests := []struct {
		name          string
		miningStatus  string
		pools         []poolData
		expectedState sdk.HealthStatus
	}{
		{
			name:         "firmware reports no_pools but pools are configured",
			miningStatus: "no_pools",
			pools: []poolData{
				{URL: "stratum+tcp://pool.example.com:3333"},
			},
			expectedState: sdk.HealthHealthyInactive,
		},
		{
			name:          "firmware reports mining but no pools configured",
			miningStatus:  "mining",
			pools:         []poolData{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:         "firmware reports mining but all pools have empty URLs",
			miningStatus: "mining",
			pools: []poolData{
				{URL: ""},
				{URL: ""},
			},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:          "firmware reports no_pools and no pools configured",
			miningStatus:  "no_pools",
			pools:         []poolData{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:         "firmware reports mining and pools are configured",
			miningStatus: "mining",
			pools: []poolData{
				{URL: "stratum+tcp://pool.example.com:3333"},
			},
			expectedState: sdk.HealthHealthyActive,
		},
		{
			name:          "firmware reports stopped but no pools",
			miningStatus:  "stopped",
			pools:         []poolData{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:          "firmware reports degraded_mining but no pools",
			miningStatus:  "degraded_mining",
			pools:         []poolData{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newStatusTestServer(t, tt.miningStatus, tt.pools, false)
			defer server.Close()

			client := newTestClient(t, server)
			defer func() { _ = client.Close() }()

			status, err := client.GetStatus(t.Context())

			require.NoError(t, err, "GetStatus should not return error")
			assert.Equal(t, tt.expectedState, status.State,
				"State should match expected value based on actual pool configuration")
		})
	}
}

// TestGetCoolingMode tests the API-to-SDK cooling mode mapping
func TestGetCoolingMode(t *testing.T) {
	tests := []struct {
		name        string
		fanMode     string
		expectedSDK sdk.CoolingMode
	}{
		{"auto maps to air cooled", "auto", sdk.CoolingModeAirCooled},
		{"off maps to immersion cooled", "off", sdk.CoolingModeImmersionCooled},
		{"manual maps to manual", "manual", sdk.CoolingModeManual},
		{"unknown maps to unspecified", "unknown", sdk.CoolingModeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/cooling", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				resp := coolingStatusResponse{
					CoolingStatus: coolingStatusInner{FanMode: tt.fanMode},
				}
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			defer func() { _ = client.Close() }()

			mode, err := client.GetCoolingMode(t.Context())

			require.NoError(t, err)
			assert.Equal(t, tt.expectedSDK, mode)
		})
	}
}

func TestGetCoolingMode_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("connection refused"))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	defer func() { _ = client.Close() }()

	mode, err := client.GetCoolingMode(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get cooling mode")
	assert.Equal(t, sdk.CoolingModeUnspecified, mode)
}

// TestGetFirmwareVersion tests the REST-backed firmware version helper.
func TestGetFirmwareVersion(t *testing.T) {
	tests := []struct {
		name            string
		statusCode      int
		responseBody    string
		expectedVersion string
		expectErr       bool
		errContains     string
	}{
		{
			name:            "success with firmware version populated",
			statusCode:      http.StatusOK,
			responseBody:    `{"system-info":{"os":{"version":"1.2.3"}}}`,
			expectedVersion: "1.2.3",
		},
		{
			name:            "missing os section",
			statusCode:      http.StatusOK,
			responseBody:    `{"system-info":{}}`,
			expectedVersion: "",
		},
		{
			name:         "system endpoint returns error",
			statusCode:   http.StatusInternalServerError,
			responseBody: `connection refused`,
			expectErr:    true,
			errContains:  "failed to get system info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/system", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := newTestClient(t, server)
			defer func() { _ = client.Close() }()

			version, err := client.GetFirmwareVersion(t.Context())

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Empty(t, version)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedVersion, version)
			}
		})
	}
}

// TestGetStatusPoolCheckError tests behavior when pool check fails
func TestGetStatusPoolCheckError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mining":
			resp := miningStatusResponse{
				MiningStatus: miningStatusInner{Status: "mining"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/pools":
			// Simulate pool endpoint failure
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("connection refused"))
		case "/api/v1/system":
			resp := systemInfoResponse{}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	defer func() { _ = client.Close() }()

	status, err := client.GetStatus(t.Context())

	require.NoError(t, err, "GetStatus should not fail when pool check fails")
	assert.Equal(t, sdk.HealthHealthyActive, status.State,
		"Should fall back to firmware-reported state when pool check fails")
}

func TestGetStatusNoMiningStatistics(t *testing.T) {
	tests := []struct {
		name          string
		pools         []poolData
		expectedState sdk.HealthStatus
	}{
		{
			name: "configured pools remain inactive when mining stats are unavailable",
			pools: []poolData{
				{URL: "stratum+tcp://pool.example.com:3333"},
			},
			expectedState: sdk.HealthHealthyInactive,
		},
		{
			name:          "missing pools still report needs mining pool when mining stats are unavailable",
			pools:         []poolData{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/mining":
					w.WriteHeader(http.StatusNoContent)
				case "/api/v1/pools":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(poolsList{Pools: tt.pools})
				case "/api/v1/system":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(systemInfoResponse{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := newTestClient(t, server)
			defer func() { _ = client.Close() }()

			status, err := client.GetStatus(t.Context())

			require.NoError(t, err, "GetStatus should not fail for 204 No Content")
			assert.Equal(t, tt.expectedState, status.State)
		})
	}
}

// TestUploadFirmware tests the multipart firmware upload to the MDK REST API.
func TestUploadFirmware(t *testing.T) {
	const testToken = "fleet-bearer-token"
	firmwareContent := []byte("fake-swu-firmware-content-for-test")

	tests := []struct {
		name        string
		handler     func(t *testing.T) http.HandlerFunc
		token       string
		expectErr   bool
		errContains string
	}{
		{
			name: "successful upload",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPut, r.Method)
					assert.Equal(t, "/api/v1/system/update", r.URL.Path)
					assert.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
					assert.True(t, strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data"))

					file, header, err := r.FormFile("file")
					require.NoError(t, err, "should be able to read 'file' field")
					defer file.Close()

					assert.Equal(t, "firmware.swu", header.Filename)
					body, err := io.ReadAll(file)
					require.NoError(t, err)
					assert.Equal(t, firmwareContent, body)

					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"message":"Firmware uploaded successfully"}`))
				}
			},
			token:     testToken,
			expectErr: false,
		},
		{
			name: "unauthorized (401)",
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				}
			},
			token:       testToken,
			expectErr:   true,
			errContains: "invalid token",
		},
		{
			name: "unauthorized (401) with empty body falls back",
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}
			},
			token:       testToken,
			expectErr:   true,
			errContains: "check bearer token",
		},
		{
			name: "update already in progress (409)",
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"error":"update in progress"}`))
				}
			},
			token:       testToken,
			expectErr:   true,
			errContains: "update in progress",
		},
		{
			name: "bad request (400)",
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"unsupported firmware"}`))
				}
			},
			token:       testToken,
			expectErr:   true,
			errContains: "unsupported firmware",
		},
		{
			name: "server error (500)",
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal failure"}`))
				}
			},
			token:       testToken,
			expectErr:   true,
			errContains: "internal failure",
		},
		{
			name: "no bearer token omits auth header",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					assert.Empty(t, r.Header.Get("Authorization"), "no auth header when token is empty")
					w.WriteHeader(http.StatusOK)
				}
			},
			token:     "",
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler(t))
			defer server.Close()

			client := newTestClient(t, server)
			defer func() { _ = client.Close() }()

			if tt.token != "" {
				err := client.SetCredentials(sdk.BearerToken{Token: tt.token})
				require.NoError(t, err)
			}

			firmware := sdk.FirmwareFile{
				Reader:   bytes.NewReader(firmwareContent),
				Filename: "firmware.swu",
				Size:     int64(len(firmwareContent)),
			}

			err := client.UploadFirmware(context.Background(), firmware)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestUploadFirmware_413_ReturnsFailedPrecondition verifies that an HTTP 413 from the rig
// produces a gRPC FailedPrecondition status error (which the server classifies as permanent).
func TestUploadFirmware_413_ReturnsFailedPrecondition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`<html><head><title>413 Request Entity Too Large</title></head></html>`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	defer func() { _ = client.Close() }()

	err := client.SetCredentials(sdk.BearerToken{Token: "test-token"})
	require.NoError(t, err)

	firmware := sdk.FirmwareFile{
		Reader:   bytes.NewReader([]byte("firmware-data")),
		Filename: "firmware.swu",
		Size:     97_000_000,
	}

	err = client.UploadFirmware(context.Background(), firmware)

	require.Error(t, err)
	st, ok := grpcstatus.FromError(err)
	require.True(t, ok, "error should be a gRPC status error")
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "payload too large")
	assert.Contains(t, st.Message(), "97000000")
	assert.Contains(t, st.Message(), "413 Request Entity Too Large")
}

// TestUploadFirmware_ContextCancellation tests that firmware upload respects context cancellation.
func TestUploadFirmware_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate a slow server that responds only after a significant delay
		time.Sleep(10 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	firmware := sdk.FirmwareFile{
		Reader:   bytes.NewReader([]byte("firmware-data")),
		Filename: "firmware.swu",
		Size:     13,
	}

	err := client.UploadFirmware(ctx, firmware)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// TestUploadFirmware_NilReader tests that a nil firmware reader returns a clear error.
func TestUploadFirmware_NilReader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called when reader is nil")
	}))
	defer server.Close()

	client := newTestClient(t, server)
	defer func() { _ = client.Close() }()

	firmware := sdk.FirmwareFile{
		Reader:   nil,
		Filename: "firmware.swu",
		Size:     100,
	}

	err := client.UploadFirmware(context.Background(), firmware)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "firmware reader is required")
}

func TestErrorsResponse_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("wrapped_object", func(t *testing.T) {
		t.Parallel()
		const payload = `{"errors":[{"source":"fan","slot":1,"error_code":"FanSlow","timestamp":1,"message":"slow"}]}`
		var got ErrorsResponse
		require.NoError(t, json.Unmarshal([]byte(payload), &got))
		require.Len(t, got.Errors, 1)
		assert.Equal(t, "fan", got.Errors[0].Source)
		assert.Equal(t, 1, got.Errors[0].Slot)
		assert.Equal(t, "FanSlow", got.Errors[0].ErrorCode)
	})

	t.Run("bare_array", func(t *testing.T) {
		t.Parallel()
		const payload = `[{"source":"rig","slot":0,"error_code":"LowHashRate","timestamp":2,"message":"low"}]`
		var got ErrorsResponse
		require.NoError(t, json.Unmarshal([]byte(payload), &got))
		require.Len(t, got.Errors, 1)
		assert.Equal(t, "rig", got.Errors[0].Source)
		assert.Equal(t, "LowHashRate", got.Errors[0].ErrorCode)
	})

	t.Run("empty_array", func(t *testing.T) {
		t.Parallel()
		var got ErrorsResponse
		require.NoError(t, json.Unmarshal([]byte(`[]`), &got))
		assert.Empty(t, got.Errors)
	})
}

// TestProtoDefaultPasswordContract pins the Proto firmware default-password
// 403 response format that this client parses. If firmware changes its 403
// prose or code name, this test fails here first — rather than downstream
// detection silently breaking.
func TestProtoDefaultPasswordContract(t *testing.T) {
	t.Run("markers match firmware default-password contract", func(t *testing.T) {
		assert.Equal(t, "default password must be changed", defaultPasswordMessageMarker)
		assert.Equal(t, "default_password_active", defaultPasswordCodeMarker)
	})

	t.Run("isDefaultPasswordMessage matches known firmware shapes", func(t *testing.T) {
		cases := []struct {
			name     string
			msg      string
			expected bool
		}{
			{"firmware prose lowercase", "default password must be changed", true},
			{"firmware prose mixed case", "Default Password Must Be Changed", true},
			{"wrapped with prefix", "forbidden: default password must be changed", true},
			{"code as plain-text body", "DEFAULT_PASSWORD_ACTIVE", true},
			{"code lowercase", "default_password_active", true},
			{"gRPC wrapped error", "rpc error: code = PermissionDenied desc = default password must be changed", true},
			{"generic forbidden", "forbidden: access denied", false},
			{"auth error", "unauthenticated: missing or invalid credentials", false},
			{"empty", "", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, isDefaultPasswordMessage(tc.msg))
			})
		}
	})

	t.Run("classifyForbiddenResponse normalizes firmware payloads to the marker", func(t *testing.T) {
		body := []byte(`{"error":{"code":"DEFAULT_PASSWORD_ACTIVE","message":"Default password must be changed"}}`)
		err := classifyForbiddenResponse(body)
		require.Error(t, err)
		assert.Contains(t, err.Error(), defaultPasswordMessageMarker,
			"classifier must emit the marker so downstream detectors can recognize it")
	})

	t.Run("classifyForbiddenResponse preserves plain-text default-password bodies", func(t *testing.T) {
		err := classifyForbiddenResponse([]byte("default password must be changed"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), defaultPasswordMessageMarker)
	})
}
