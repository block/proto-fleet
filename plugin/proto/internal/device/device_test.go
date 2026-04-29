package device

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAuthenticationError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		// Nil error
		{
			name:     "nil_error",
			err:      nil,
			expected: false,
		},

		// HTTP status code detection
		{
			name:     "http_401_status",
			err:      fmt.Errorf("request failed with status %d", http.StatusUnauthorized),
			expected: true,
		},
		{
			name:     "wrapped_http_401_status",
			err:      fmt.Errorf("API call failed: %w", fmt.Errorf("status %d: unauthorized", http.StatusUnauthorized)),
			expected: true,
		},
		{
			name:     "http_500_status_not_auth",
			err:      fmt.Errorf("request failed with status %d", http.StatusInternalServerError),
			expected: false,
		},
		{
			name:     "http_403_status_not_auth",
			err:      fmt.Errorf("request failed with status %d", http.StatusForbidden),
			expected: false,
		},

		// String-based detection (serialized errors that crossed gRPC boundary)
		{
			name:     "string_unauthenticated",
			err:      errors.New("rpc error: code=Unknown desc=unauthenticated, missing API key"),
			expected: true,
		},
		{
			name:     "string_missing_api_key",
			err:      errors.New("failed to verify: missing api key - set via set auth key first"),
			expected: true,
		},
		{
			name:     "string_unauthorized",
			err:      errors.New("request failed: unauthorized access"),
			expected: true,
		},
		{
			name:     "string_authentication_failed",
			err:      errors.New("authentication failed: invalid token"),
			expected: true,
		},
		{
			name:     "string_invalid_credentials",
			err:      errors.New("login failed: invalid credentials"),
			expected: true,
		},

		// Negative cases - should NOT be auth errors
		{
			name:     "network_error_connection_refused",
			err:      errors.New("connection refused"),
			expected: false,
		},
		{
			name:     "generic_internal_error",
			err:      errors.New("internal server error"),
			expected: false,
		},
		{
			name:     "timeout_error",
			err:      context.DeadlineExceeded,
			expected: false,
		},
		{
			name:     "io_timeout_error",
			err:      errors.New("i/o timeout"),
			expected: false,
		},
		{
			name:     "device_not_found_error",
			err:      errors.New("device not found"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthenticationError(tt.err)
			assert.Equal(t, tt.expected, result, "isAuthenticationError(%v)", tt.err)
		})
	}
}

func TestIsDefaultPasswordError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil_error",
			err:      nil,
			expected: false,
		},
		{
			name:     "default_password_must_be_changed",
			err:      fmt.Errorf("forbidden: default password must be changed"),
			expected: true,
		},
		{
			name:     "wrapped_default_password",
			err:      fmt.Errorf("API call failed: %w", fmt.Errorf("forbidden: default password must be changed")),
			expected: true,
		},
		{
			name:     "default_password_active_code",
			err:      errors.New("request failed: DEFAULT_PASSWORD_ACTIVE"),
			expected: true,
		},
		{
			name:     "http_403_without_default_password",
			err:      fmt.Errorf("request failed with status %d", http.StatusForbidden),
			expected: false,
		},
		{
			name:     "auth_error_not_default_password",
			err:      errors.New("unauthenticated: missing or invalid credentials"),
			expected: false,
		},
		{
			name:     "generic_error",
			err:      errors.New("connection refused"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDefaultPasswordError(tt.err)
			assert.Equal(t, tt.expected, result, "isDefaultPasswordError(%v)", tt.err)
		})
	}
}

func TestNew_DefaultPasswordActive_UnpairReportsDefaultPassword(t *testing.T) {
	// Firmware gates DELETE /api/v1/pairing/auth-key behind the default-password
	// lockout (see server/fake-proto-rig's matching handler test), so Unpair
	// cannot actually clear Fleet's installed key on a never-rotated device.
	// The constructor still returns a live handle so UpdateMinerPassword —
	// routed through /auth/change-password, which is exempt — remains reachable.
	var clearAuthKeyCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"DEFAULT_PASSWORD_ACTIVE","message":"default password must be changed"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/pairing/auth-key":
			clearAuthKeyCalls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"DEFAULT_PASSWORD_ACTIVE","message":"default password must be changed"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	host, portStr, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	port, err := strconv.ParseInt(portStr, 10, 32)
	require.NoError(t, err)

	deviceInfo := sdk.DeviceInfo{
		Host:      host,
		Port:      int32(port),
		URLScheme: "http",
	}

	dev, err := New("device-locked", deviceInfo, sdk.BearerToken{Token: "test-token"}, SetStatusTTL(0*time.Second))
	require.NoError(t, err, "constructor must succeed under default-password so remediation ops remain reachable")
	require.NotNil(t, dev)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })

	unpairErr := dev.Unpair(context.Background())
	require.Error(t, unpairErr, "Unpair must surface the firmware default-password gate rather than silently succeed")
	assert.True(t, isDefaultPasswordError(unpairErr), "Unpair error should be recognizable as default-password active; got: %v", unpairErr)
	assert.Equal(t, 1, clearAuthKeyCalls, "Unpair should still attempt DELETE /api/v1/pairing/auth-key")
}

func TestDevice_CurtailFullWrapsDispatchFailureAsTransient(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusInternalServerError)

	err := dev.Curtail(context.Background(), sdk.CurtailLevelFull)

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailTransient, sdkErr.Code)
	assert.Contains(t, err.Error(), "transient curtail failure")
}

func TestDevice_UncurtailWrapsDispatchFailureAsTransient(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusInternalServerError)

	err := dev.Uncurtail(context.Background())

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailTransient, sdkErr.Code)
	assert.Contains(t, err.Error(), "transient curtail failure")
}

func TestDevice_CurtailUnsupportedLevelReturnsCapabilityNotSupported(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusOK)

	err := dev.Curtail(context.Background(), sdk.CurtailLevelEfficiency)

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailCapabilityNotSupported, sdkErr.Code)
}

func newMiningControlTestDevice(t *testing.T, miningControlStatus int) *Device {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pools":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pools":[]}`))
		case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/mining/start" || r.URL.Path == "/api/v1/mining/stop"):
			w.WriteHeader(miningControlStatus)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	host, portStr, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	port, err := strconv.ParseInt(portStr, 10, 32)
	require.NoError(t, err)

	dev, err := New("device-curtail", sdk.DeviceInfo{
		Host:      host,
		Port:      int32(port),
		URLScheme: "http",
	}, sdk.BearerToken{Token: "test-token"}, SetStatusTTL(0*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })

	return dev
}
