package proto

import (
	"bytes"
	"context"
	"crypto/tls"
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

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			port:    2121,
			scheme:  "http",
			wantErr: false,
		},
		{
			name:    "valid HTTPS client",
			host:    "192.168.1.100",
			port:    2121,
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
			port:    2121,
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
			assert.NotNil(t, client.dataClient, "Data client should be set")
			assert.NotNil(t, client.commandClient, "Command client should be set")
			assert.NotNil(t, client.systemClient, "System client should be set")
			assert.NotNil(t, client.pairingClient, "Pairing client should be set")

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

// TestTLSVerificationConfiguration tests TLS verification environment variable handling
func TestTLSVerificationConfiguration(t *testing.T) {
	// Reset clients before each test
	resetClients()

	tests := []struct {
		name          string
		skipTLSVerify string
		insecureTLS   string
		expectedSkip  bool
	}{
		{
			name:          "default - verification enabled",
			skipTLSVerify: "",
			insecureTLS:   "",
			expectedSkip:  false,
		},
		{
			name:          "SKIP_TLS_VERIFY=true",
			skipTLSVerify: "true",
			insecureTLS:   "",
			expectedSkip:  true,
		},
		{
			name:          "SKIP_TLS_VERIFY=TRUE (case insensitive)",
			skipTLSVerify: "TRUE",
			insecureTLS:   "",
			expectedSkip:  true,
		},
		{
			name:          "INSECURE_TLS=true",
			skipTLSVerify: "",
			insecureTLS:   "true",
			expectedSkip:  true,
		},
		{
			name:          "SKIP_TLS_VERIFY=false",
			skipTLSVerify: "false",
			insecureTLS:   "",
			expectedSkip:  false,
		},
		{
			name:          "both set to true",
			skipTLSVerify: "true",
			insecureTLS:   "true",
			expectedSkip:  true,
		},
		{
			name:          "invalid values",
			skipTLSVerify: "invalid",
			insecureTLS:   "invalid",
			expectedSkip:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset clients for each test
			resetClients()

			// Set environment variables
			if tt.skipTLSVerify != "" {
				t.Setenv("SKIP_TLS_VERIFY", tt.skipTLSVerify)
			}
			if tt.insecureTLS != "" {
				t.Setenv("INSECURE_TLS", tt.insecureTLS)
			}

			// Test the function directly
			result := shouldSkipTLSVerification()
			assert.Equal(t, tt.expectedSkip, result, "shouldSkipTLSVerification() result")

			// Test that HTTPS client respects the setting
			client := createHTTPSClient()
			transport, ok := client.Transport.(*http.Transport)
			require.True(t, ok, "Transport should be *http.Transport")
			assert.Equal(t, tt.expectedSkip, transport.TLSClientConfig.InsecureSkipVerify, "TLS InsecureSkipVerify setting")
		})
	}
}

// TestCredentialManagement tests credential setting and usage
func TestCredentialManagement(t *testing.T) {
	client, err := NewClient("localhost", 2121, "http")
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

// TestAuthTokenContextHandling tests auth token context operations
func TestAuthTokenContextHandling(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{
			name:     "valid token",
			token:    "test-token-123",
			expected: "test-token-123",
		},
		{
			name:     "empty token",
			token:    "",
			expected: "",
		},
		{
			name:     "JWT token",
			token:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.signature",
			expected: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test adding token to context
			ctx := t.Context()
			ctxWithToken := withAuthToken(ctx, tt.token)

			// Test extracting token from context
			extractedToken := getAuthTokenFromContext(ctxWithToken)
			assert.Equal(t, tt.expected, extractedToken, "Extracted token should match expected")

			// Test with context without token
			emptyToken := getAuthTokenFromContext(t.Context())
			assert.Empty(t, emptyToken, "Context without token should return empty string")

			// Test with context without token (second check)
			emptyToken2 := getAuthTokenFromContext(t.Context())
			assert.Empty(t, emptyToken2, "Context without token should return empty string")
		})
	}
}

// TestWithAuthMethod tests the client's withAuth method
func TestWithAuthMethod(t *testing.T) {
	client, err := NewClient("localhost", 2121, "http")
	require.NoError(t, err, "Failed to create client")

	t.Run("with bearer token", func(t *testing.T) {
		token := sdk.BearerToken{Token: "test-token"}
		err := client.SetCredentials(token)
		require.NoError(t, err, "SetCredentials should not return error")

		ctx := t.Context()
		authCtx := client.withAuth(ctx)

		extractedToken := getAuthTokenFromContext(authCtx)
		assert.Equal(t, token.Token, extractedToken, "Token should be extracted from context")
	})

	t.Run("without bearer token", func(t *testing.T) {
		err := client.SetCredentials(sdk.BearerToken{Token: ""})
		require.NoError(t, err, "SetCredentials should not return error")

		ctx := t.Context()
		authCtx := client.withAuth(ctx)

		extractedToken := getAuthTokenFromContext(authCtx)
		assert.Empty(t, extractedToken, "Token should be empty")
	})
}

// TestAuthInterceptor tests the auth interceptor functionality
func TestAuthInterceptor(t *testing.T) {
	interceptor := newAuthInterceptor()
	require.NotNil(t, interceptor, "Interceptor should be created")

	// Test that it implements connect.Interceptor interface
	_, ok := interceptor.(connect.Interceptor)
	assert.True(t, ok, "Interceptor should implement connect.Interceptor")

	// Test that WrapUnary method exists and returns a function
	mockNext := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, nil
	}

	wrappedFunc := interceptor.WrapUnary(mockNext)
	assert.NotNil(t, wrappedFunc, "Wrapped function should be returned")

	// Test WrapStreamingClient method exists
	mockStreamingNext := func(ctx context.Context, _ connect.Spec) connect.StreamingClientConn {
		return nil
	}

	wrappedStreamingFunc := interceptor.WrapStreamingClient(mockStreamingNext)
	assert.NotNil(t, wrappedStreamingFunc, "Wrapped streaming function should be returned")
}

// TestTimeToAPITimestamp tests timestamp conversion functionality
func TestTimeToAPITimestamp(t *testing.T) {
	tests := []struct {
		name      string
		input     time.Time
		wantNil   bool
		wantSecs  uint64
		wantNanos uint32
	}{
		{
			name:    "zero time",
			input:   time.Time{},
			wantNil: true,
		},
		{
			name:      "epoch time",
			input:     time.Unix(0, 0),
			wantNil:   false,
			wantSecs:  0,
			wantNanos: 0,
		},
		{
			name:      "specific time",
			input:     time.Unix(1234567890, 123456789),
			wantNil:   false,
			wantSecs:  1234567890,
			wantNanos: 123456789,
		},
		{
			name:      "negative time",
			input:     time.Unix(-1, 0),
			wantNil:   false,
			wantSecs:  0, // Should be clamped to 0
			wantNanos: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := timeToAPITimestamp(tt.input)

			if tt.wantNil {
				assert.Nil(t, result, "Result should be nil")
				return
			}

			require.NotNil(t, result, "Result should not be nil")
			assert.Equal(t, tt.wantSecs, result.Seconds, "Seconds should match")
			assert.Equal(t, tt.wantNanos, result.Nanos, "Nanos should match")
		})
	}
}

// TestClientSingletonBehavior tests that HTTP clients are properly shared
func TestClientSingletonBehavior(t *testing.T) {
	// Reset clients
	resetClients()

	// Create multiple clients with same scheme
	client1, err1 := NewClient("host1", 2121, "http")
	client2, err2 := NewClient("host2", 2121, "http")

	require.NoError(t, err1, "Failed to create client1")
	require.NoError(t, err2, "Failed to create client2")

	// They should share the same underlying HTTP client
	assert.Same(t, client1.httpClient, client2.httpClient, "HTTP clients should be shared (singleton)")

	// Test with HTTPS
	client3, err3 := NewClient("host3", 2121, "https")
	client4, err4 := NewClient("host4", 2121, "https")

	require.NoError(t, err3, "Failed to create HTTPS client3")
	require.NoError(t, err4, "Failed to create HTTPS client4")

	// HTTPS clients should share the same underlying HTTP client
	assert.Same(t, client3.httpClient, client4.httpClient, "HTTPS clients should be shared (singleton)")

	// But HTTP and HTTPS clients should be different
	assert.NotSame(t, client1.httpClient, client3.httpClient, "HTTP and HTTPS clients should be different")
}

// TestClientRuntimeEnvChange tests runtime environment variable changes with client reset
// This aligns with the server's create_client_test.go TestCreateClientRuntimeEnvChange
func TestClientRuntimeEnvChange(t *testing.T) {
	// Reset clients to ensure we start fresh
	resetClients()

	// Start with TLS verification enabled
	t.Setenv("SKIP_TLS_VERIFY", "false")

	// Create first client with TLS verification enabled
	client1, err1 := NewClient("localhost", 8443, "https")

	require.NoError(t, err1, "Failed to create first client")
	require.NotNil(t, client1, "First client should be created")
	// Verify TLS verification is enabled
	transport, ok := client1.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.False(t, transport.TLSClientConfig.InsecureSkipVerify,
		"TLS verification should be enabled initially")

	// Change environment variable at runtime
	t.Setenv("SKIP_TLS_VERIFY", "true")

	// Reset clients to force recreation with new environment
	resetClients()

	// Create second client with TLS verification disabled
	client2, err2 := NewClient("localhost", 8443, "https")

	require.NoError(t, err2, "Failed to create second client")
	require.NotNil(t, client2, "Second client should be created")
	// Verify TLS verification is now disabled
	transport, ok = client2.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify,
		"TLS verification should be disabled after environment change")

	// Verify both clients were created (errors are expected for connection, not creation)
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
			client, err := NewClient("localhost", 2121, tt.scheme)

			// Should either return an error or create a client that will fail on actual use
			// The current implementation doesn't validate schemes upfront, but this test
			// documents the expected behavior for future improvements
			require.NoError(t, err, "NewClient should not error on unsupported scheme")
			require.NotNil(t, client, "Client should be created even with unsupported scheme")
			_ = client.Close()
		})
	}
}

// TestClientCreationWithInsecureTLS tests client creation with TLS verification disabled
// This mirrors the server's TestCreateClientWithInsecureTLS
func TestClientCreationWithInsecureTLS(t *testing.T) {
	// Reset clients to ensure we start fresh
	resetClients()

	t.Setenv("SKIP_TLS_VERIFY", "true")

	// Test that the client can be created with HTTPS protocol
	// when TLS verification is disabled
	client, err := NewClient("localhost", 8443, "https")

	require.NoError(t, err, "Failed to create client with insecure TLS")
	require.NotNil(t, client, "Client should be created")

	// Verify TLS verification is disabled
	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify,
		"TLS verification should be disabled when SKIP_TLS_VERIFY=true")

	// Clean up
	_ = client.Close()
}

// TestClientCreationWithoutInsecureTLS tests client creation with TLS verification enabled
// This mirrors the server's TestCreateClientWithoutInsecureTLS
func TestClientCreationWithoutInsecureTLS(t *testing.T) {
	// Reset clients to ensure we start fresh
	resetClients()

	// Explicitly set TLS verification to enabled (default behavior)
	t.Setenv("SKIP_TLS_VERIFY", "false")

	client, err := NewClient("localhost", 8443, "https")

	require.NoError(t, err, "Failed to create client with secure TLS")
	require.NotNil(t, client, "Client should be created")

	// Verify TLS verification is enabled
	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	require.NotNil(t, transport.TLSClientConfig, "TLS config should be set")
	assert.False(t, transport.TLSClientConfig.InsecureSkipVerify,
		"TLS verification should be enabled by default")

	// Clean up
	_ = client.Close()
}

// newTestClient creates a Client pointed at the given httptest.Server.
// The singleton HTTP/2 transport is replaced with a plain HTTP/1.1 client so
// that the client works with httptest.Server (which only speaks HTTP/1.1).
// webUIBaseURL is also pointed at the test server since tests use a random port.
func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), decimalBase, int32Bits)
	require.NoError(t, err)
	client, err := NewClient(u.Hostname(), int32(port), "http")
	require.NoError(t, err)
	client.httpClient = &http.Client{}
	// In production webUIBaseURL uses the standard port (80/443); override here so
	// loginWithPassword and ChangePassword hit the same test server handler.
	client.webUIBaseURL = server.URL
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
	httpClient = nil
	httpsClient = nil
}

// mockDataClient implements miner_data_apiconnect.MinerDataApiClient for testing
type mockDataClient struct {
	miningStatusResponse *miner_data_api.MiningStatusResponse
	miningStatusError    error
	poolsResponse        *miner_data_api.PoolsResponse
	poolsError           error
	softwareInfoResponse *miner_data_api.SoftwareInfoResponse
	softwareInfoError    error
	coolingModeResponse  *miner_data_api.CoolingModeResponse
	coolingModeError     error
}

func (m *mockDataClient) GetMiningStatus(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.MiningStatusResponse], error) {
	if m.miningStatusError != nil {
		return nil, m.miningStatusError
	}
	return connect.NewResponse(m.miningStatusResponse), nil
}

func (m *mockDataClient) GetPools(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PoolsResponse], error) {
	if m.poolsError != nil {
		return nil, m.poolsError
	}
	return connect.NewResponse(m.poolsResponse), nil
}

func (m *mockDataClient) GetSoftwareInfo(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.SoftwareInfoResponse], error) {
	if m.softwareInfoError != nil {
		return nil, m.softwareInfoError
	}
	return connect.NewResponse(m.softwareInfoResponse), nil
}

func (m *mockDataClient) GetCoolingMode(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.CoolingModeResponse], error) {
	if m.coolingModeError != nil {
		return nil, m.coolingModeError
	}
	if m.coolingModeResponse != nil {
		return connect.NewResponse(m.coolingModeResponse), nil
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetPowerTarget(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PowerTargetResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetHardwareInfo(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.HardwareInfoResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetHashboardStatus(_ context.Context, _ *connect.Request[miner_data_api.HashboardStatusRequest]) (*connect.Response[miner_data_api.HashboardStatusResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetAsicStatus(_ context.Context, _ *connect.Request[miner_data_api.AsicStatusRequest]) (*connect.Response[miner_data_api.AsicStatusResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetTimeSeriesData(_ context.Context, _ *connect.Request[miner_data_api.TimeSeriesDataRequest]) (*connect.Response[miner_data_api.TimeSeriesDataResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetUnifiedTimeSeriesData(_ context.Context, _ *connect.Request[miner_data_api.UnifiedTimeSeriesDataRequest]) (*connect.Response[miner_data_api.UnifiedTimeSeriesDataResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetErrors(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.ErrorsResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetPsuStatusList(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PsuStatusListResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetPsuInfoList(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PsuInfoListResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetTelemetryValues(_ context.Context, _ *connect.Request[miner_data_api.GetTelemetryValuesRequest]) (*connect.Response[miner_data_api.GetTelemetryValuesResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockDataClient) GetAsicMetadata(_ context.Context, _ *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.GetAsicMetadataResponse], error) {
	return nil, fmt.Errorf("not implemented")
}

// TestGetStatusPoolStateOverride tests that the actual pool list is the source of truth
// for determining NeedsMiningPool status, overriding the firmware-reported MiningState.
func TestGetStatusPoolStateOverride(t *testing.T) {
	tests := []struct {
		name          string
		miningState   miner_data_api.MiningState
		pools         []*miner_data_api.Pool
		expectedState sdk.HealthStatus
	}{
		{
			name:        "firmware reports NO_POOLS but pools are configured",
			miningState: miner_data_api.MiningState_MINING_STATE_NO_POOLS,
			pools: []*miner_data_api.Pool{
				{Url: "stratum+tcp://pool.example.com:3333"},
			},
			expectedState: sdk.HealthHealthyInactive,
		},
		{
			name:          "firmware reports MINING but no pools configured",
			miningState:   miner_data_api.MiningState_MINING_STATE_MINING,
			pools:         []*miner_data_api.Pool{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:        "firmware reports MINING but all pools have empty URLs",
			miningState: miner_data_api.MiningState_MINING_STATE_MINING,
			pools: []*miner_data_api.Pool{
				{Url: ""},
				{Url: ""},
			},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:          "firmware reports NO_POOLS and no pools configured",
			miningState:   miner_data_api.MiningState_MINING_STATE_NO_POOLS,
			pools:         []*miner_data_api.Pool{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:        "firmware reports MINING and pools are configured",
			miningState: miner_data_api.MiningState_MINING_STATE_MINING,
			pools: []*miner_data_api.Pool{
				{Url: "stratum+tcp://pool.example.com:3333"},
			},
			expectedState: sdk.HealthHealthyActive,
		},
		{
			name:          "firmware reports STOPPED but no pools",
			miningState:   miner_data_api.MiningState_MINING_STATE_STOPPED,
			pools:         []*miner_data_api.Pool{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
		{
			name:          "firmware reports DEGRADED_MINING but no pools",
			miningState:   miner_data_api.MiningState_MINING_STATE_DEGRADED_MINING,
			pools:         []*miner_data_api.Pool{},
			expectedState: sdk.HealthNeedsMiningPool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDataClient{
				miningStatusResponse: &miner_data_api.MiningStatusResponse{
					State: tt.miningState,
				},
				poolsResponse: &miner_data_api.PoolsResponse{
					Pools: tt.pools,
				},
				softwareInfoResponse: &miner_data_api.SoftwareInfoResponse{},
			}

			client := &Client{
				dataClient: mockClient,
			}

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
		apiMode     miner_data_api.CoolingMode
		expectedSDK sdk.CoolingMode
	}{
		{"auto maps to air cooled", miner_data_api.CoolingMode_COOLING_MODE_AUTO, sdk.CoolingModeAirCooled},
		{"off maps to immersion cooled", miner_data_api.CoolingMode_COOLING_MODE_OFF, sdk.CoolingModeImmersionCooled},
		{"manual maps to manual", miner_data_api.CoolingMode_COOLING_MODE_MANUAL, sdk.CoolingModeManual},
		{"unknown maps to unspecified", miner_data_api.CoolingMode_COOLING_MODE_UNKNOWN, sdk.CoolingModeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockDataClient{
				coolingModeResponse: &miner_data_api.CoolingModeResponse{
					Mode: tt.apiMode,
				},
			}

			client := &Client{
				dataClient: mockClient,
			}

			mode, err := client.GetCoolingMode(t.Context())

			require.NoError(t, err)
			assert.Equal(t, tt.expectedSDK, mode)
		})
	}
}

func TestGetCoolingMode_Error(t *testing.T) {
	mockClient := &mockDataClient{
		coolingModeError: fmt.Errorf("connection refused"),
	}

	client := &Client{
		dataClient: mockClient,
	}

	mode, err := client.GetCoolingMode(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get cooling mode")
	assert.Equal(t, sdk.CoolingModeUnspecified, mode)
}

// TestGetStatusPoolCheckError tests behavior when pool check fails
func TestGetStatusPoolCheckError(t *testing.T) {
	mockClient := &mockDataClient{
		miningStatusResponse: &miner_data_api.MiningStatusResponse{
			State: miner_data_api.MiningState_MINING_STATE_MINING,
		},
		poolsError:           fmt.Errorf("connection refused"),
		softwareInfoResponse: &miner_data_api.SoftwareInfoResponse{},
	}

	client := &Client{
		dataClient: mockClient,
	}

	status, err := client.GetStatus(t.Context())

	require.NoError(t, err, "GetStatus should not fail when pool check fails")
	assert.Equal(t, sdk.HealthHealthyActive, status.State,
		"Should fall back to firmware-reported state when pool check fails")
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
