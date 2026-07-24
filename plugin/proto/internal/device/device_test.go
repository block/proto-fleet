package device

import (
	"context"
	"encoding/json"
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

func TestStatusThrottlesDefaultPasswordProbe(t *testing.T) {
	var systemStatusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"r"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"mining-status":{"status":"Mining"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pools":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pools":[{"id":0,"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/telemetry":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
			systemStatusCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"default_password_active":true}`))
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

	dev, err := New("device-default-password", sdk.DeviceInfo{
		Host:            host,
		Port:            int32(port),
		URLScheme:       "http",
		FirmwareVersion: "1.0.0",
	}, sdk.UsernamePassword{Username: "admin", Password: "proto"}, SetStatusTTL(0*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })
	require.Equal(t, 1, systemStatusCalls, "constructor verification should read the flag once")

	for range 3 {
		metrics, err := dev.Status(context.Background())
		require.NoError(t, err)
		require.NotNil(t, metrics.DefaultPasswordActive)
		assert.True(t, *metrics.DefaultPasswordActive)
	}
	assert.Equal(t, 1, systemStatusCalls, "default-password flag should be cached between throttle intervals")
}

func TestUpdateMinerPasswordClearsDefaultPasswordStatusCache(t *testing.T) {
	defaultPasswordActive := true
	var systemStatusCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"r"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/auth/change-password":
			defaultPasswordActive = false
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"mining-status":{"status":"Mining"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pools":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pools":[{"id":0,"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/telemetry":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
			systemStatusCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"default_password_active":%t}`, defaultPasswordActive)
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

	dev, err := New("device-default-password-change", sdk.DeviceInfo{
		Host:            host,
		Port:            int32(port),
		URLScheme:       "http",
		FirmwareVersion: "1.0.0",
	}, sdk.UsernamePassword{Username: "admin", Password: "proto"}, SetStatusTTL(0*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })
	require.Equal(t, 1, systemStatusCalls, "constructor verification should read the flag once")

	metrics, err := dev.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, metrics.DefaultPasswordActive)
	require.True(t, *metrics.DefaultPasswordActive)
	require.Equal(t, 1, systemStatusCalls, "status should use cached default-password flag")

	require.NoError(t, dev.UpdateMinerPassword(context.Background(), "proto", "new-password"))

	metrics, err = dev.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, metrics.DefaultPasswordActive)
	assert.False(t, *metrics.DefaultPasswordActive, "successful password update should clear stale default-password status")
	assert.Equal(t, 1, systemStatusCalls, "post-change status should not need an immediate extra probe")
}

func TestDevice_CurtailFullWrapsDispatchFailureAsTransient(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusInternalServerError)

	err := dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailTransient, sdkErr.Code)
	assert.Contains(t, err.Error(), "transient curtail failure")
}

func TestDevice_UncurtailWrapsDispatchFailureAsTransient(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusInternalServerError)

	err := dev.Uncurtail(context.Background(), sdk.UncurtailRequest{})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailTransient, sdkErr.Code)
	assert.Contains(t, err.Error(), "transient curtail failure")
}

func TestDevice_CurtailAuthFailureReturnsAuthenticationFailed(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusUnauthorized)

	err := dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeAuthenticationFailed, sdkErr.Code)
	assert.ErrorIs(t, err, sdkErr.Err)
}

func TestDevice_UncurtailAuthFailureReturnsAuthenticationFailed(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusUnauthorized)

	err := dev.Uncurtail(context.Background(), sdk.UncurtailRequest{})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeAuthenticationFailed, sdkErr.Code)
	assert.ErrorIs(t, err, sdkErr.Err)
}

func TestDevice_DescribeDeviceAdvertisesFullAndEfficiencyCurtailment(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusOK)

	_, caps, err := dev.DescribeDevice(context.Background())

	require.NoError(t, err)
	assert.True(t, caps[sdk.CapabilityCurtailFull])
	assert.True(t, caps[sdk.CapabilityCurtailEfficiency])
}

func TestDevice_CurtailUnsupportedLevelReturnsCapabilityNotSupported(t *testing.T) {
	dev := newMiningControlTestDevice(t, http.StatusOK)

	err := dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevel(99)})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailCapabilityNotSupported, sdkErr.Code)
}

func TestDevice_CurtailFullStopsAndUncurtailStartsMining(t *testing.T) {
	var stopped, started bool
	dev := newMiningControlTestDeviceWithCallback(t, http.StatusOK, func(path string) {
		switch path {
		case "/api/v1/mining/stop":
			stopped = true
		case "/api/v1/mining/start":
			started = true
		}
	})

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.True(t, stopped)

	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))
	require.True(t, started)
}

func TestDevice_CurtailFullOnInactiveMinerDoesNotStartOnUncurtail(t *testing.T) {
	var stopCount int
	var started bool
	dev := newMiningControlTestDeviceWithMiningState(t, http.StatusOK, "Stopped", func(path string) {
		switch path {
		case "/api/v1/mining/stop":
			stopCount++
		case "/api/v1/mining/start":
			started = true
		}
	})

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.Equal(t, 1, stopCount)

	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))
	require.False(t, started)
	require.Equal(t, 2, stopCount)
}

func TestDevice_CurtailFullOnPoollessMinerRestoresIdleWithoutStarting(t *testing.T) {
	var stopCount int
	var started bool
	dev := newMiningControlTestDeviceWithPools(t, http.StatusOK, "NoPools", `{"pools":[]}`, func(path string) {
		switch path {
		case "/api/v1/mining/stop":
			stopCount++
		case "/api/v1/mining/start":
			started = true
		}
	})

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.Equal(t, 1, stopCount)

	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))
	require.False(t, started)
	require.Equal(t, 2, stopCount)
}

func TestDevice_CurtailFullMarksStopRequestAsCurtailment(t *testing.T) {
	var curtailmentHeader string
	dev := newMiningControlTestDeviceWithRequestCallback(t, http.StatusOK, "Mining", func(r *http.Request) {
		if r.URL.Path == "/api/v1/mining/stop" {
			curtailmentHeader = r.Header.Get("X-Proto-Fleet-Curtailment")
		}
	})

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.Equal(t, "full", curtailmentHeader)
}

func TestDevice_CurtailFullRefreshesStatusBeforeSnapshot(t *testing.T) {
	var miningState = "Mining"
	var stopped, started bool
	dev := newMiningControlTestDeviceWithDynamicMiningState(t, http.StatusOK, &miningState, func(path string) {
		switch path {
		case "/api/v1/mining/stop":
			stopped = true
		case "/api/v1/mining/start":
			started = true
		}
	})

	_, err := dev.Status(context.Background())
	require.NoError(t, err)
	miningState = "Stopped"

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.True(t, stopped)

	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))
	require.False(t, started)
}

func TestDevice_UncurtailAfterFullDoesNotRestorePowerTarget(t *testing.T) {
	var requests []powerTargetRequest
	var stopped, started bool
	dev := newFullEfficiencyCurtailmentTestDevice(t, "Mining", targetResponse{
		PowerTargetWatts:        2800,
		PowerTargetMinWatts:     1200,
		PowerTargetMaxWatts:     3200,
		DefaultPowerTargetWatts: 1800,
		PerformanceMode:         "MaximumHashrate",
	}, &requests, func(path string) {
		switch path {
		case "/api/v1/mining/stop":
			stopped = true
		case "/api/v1/mining/start":
			started = true
		}
	})

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))

	require.True(t, stopped)
	require.True(t, started)
	assert.Empty(t, requests)
}

func TestDevice_CurtailEfficiencySnapshotsAndSetsEfficiency(t *testing.T) {
	var requests []powerTargetRequest
	dev := newPowerTargetTestDevice(t, http.StatusOK, targetResponse{
		PowerTargetWatts:        2800,
		PowerTargetMinWatts:     1200,
		PowerTargetMaxWatts:     3200,
		DefaultPowerTargetWatts: 1800,
		PerformanceMode:         "MaximumHashrate",
	}, &requests)

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelEfficiency}))

	require.Len(t, requests, 1)
	require.NotNil(t, requests[0].PowerTargetWatts)
	assert.Equal(t, 1800, *requests[0].PowerTargetWatts)
	assert.Equal(t, "Efficiency", requests[0].PerformanceMode)
}

func TestDevice_UncurtailAfterEfficiencyRestoresSnapshot(t *testing.T) {
	var requests []powerTargetRequest
	dev := newPowerTargetTestDevice(t, http.StatusOK, targetResponse{
		PowerTargetWatts:        2800,
		PowerTargetMinWatts:     1200,
		PowerTargetMaxWatts:     3200,
		DefaultPowerTargetWatts: 1800,
		PerformanceMode:         "MaximumHashrate",
	}, &requests)

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelEfficiency}))
	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))

	require.Len(t, requests, 2)
	require.NotNil(t, requests[1].PowerTargetWatts)
	assert.Equal(t, 2800, *requests[1].PowerTargetWatts)
	assert.Equal(t, "MaximumHashrate", requests[1].PerformanceMode)
}

func TestDevice_CurtailFullThenEfficiencyUncurtailRestoresTargetAndMining(t *testing.T) {
	var requests []powerTargetRequest
	var stopped, started bool
	dev := newFullEfficiencyCurtailmentTestDevice(t, "Mining", targetResponse{
		PowerTargetWatts:        2800,
		PowerTargetMinWatts:     1200,
		PowerTargetMaxWatts:     3200,
		DefaultPowerTargetWatts: 1800,
		PerformanceMode:         "MaximumHashrate",
	}, &requests, func(path string) {
		switch path {
		case "/api/v1/mining/stop":
			stopped = true
		case "/api/v1/mining/start":
			started = true
		}
	})

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelEfficiency}))
	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))

	require.True(t, stopped)
	require.True(t, started)
	require.Len(t, requests, 2)
	assert.Equal(t, "Efficiency", requests[0].PerformanceMode)
	require.NotNil(t, requests[1].PowerTargetWatts)
	assert.Equal(t, 2800, *requests[1].PowerTargetWatts)
	assert.Equal(t, "MaximumHashrate", requests[1].PerformanceMode)
}

func TestDevice_CurtailEfficiencyThenFullUncurtailRestoresTargetAndMining(t *testing.T) {
	var requests []powerTargetRequest
	var stopped, started bool
	dev := newFullEfficiencyCurtailmentTestDevice(t, "Mining", targetResponse{
		PowerTargetWatts:        2800,
		PowerTargetMinWatts:     1200,
		PowerTargetMaxWatts:     3200,
		DefaultPowerTargetWatts: 1800,
		PerformanceMode:         "MaximumHashrate",
	}, &requests, func(path string) {
		switch path {
		case "/api/v1/mining/stop":
			stopped = true
		case "/api/v1/mining/start":
			started = true
		}
	})

	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelEfficiency}))
	require.NoError(t, dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	require.NoError(t, dev.Uncurtail(context.Background(), sdk.UncurtailRequest{}))

	require.True(t, stopped)
	require.True(t, started)
	require.Len(t, requests, 2)
	assert.Equal(t, "Efficiency", requests[0].PerformanceMode)
	require.NotNil(t, requests[1].PowerTargetWatts)
	assert.Equal(t, 2800, *requests[1].PowerTargetWatts)
	assert.Equal(t, "MaximumHashrate", requests[1].PerformanceMode)
}

func TestDevice_CurtailEfficiencyMissingTargetReturnsTransient(t *testing.T) {
	dev := newPowerTargetTestDevice(t, http.StatusNoContent, targetResponse{}, nil)

	err := dev.Curtail(context.Background(), sdk.CurtailRequest{Level: sdk.CurtailLevelEfficiency})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailTransient, sdkErr.Code)
	require.Error(t, sdkErr.Err)
	assert.Contains(t, sdkErr.Err.Error(), "power target not available")
}

const configuredPoolsJSON = `{"pools":[{"id":0,"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`

func newMiningControlTestDevice(t *testing.T, miningControlStatus int) *Device {
	t.Helper()
	return newMiningControlTestDeviceWithCallback(t, miningControlStatus, nil)
}

func newMiningControlTestDeviceWithCallback(t *testing.T, miningControlStatus int, onControl func(path string)) *Device {
	return newMiningControlTestDeviceWithMiningState(t, miningControlStatus, "Mining", onControl)
}

func newMiningControlTestDeviceWithMiningState(t *testing.T, miningControlStatus int, miningState string, onControl func(path string)) *Device {
	t.Helper()
	return newMiningControlTestDeviceWithDynamicMiningState(t, miningControlStatus, &miningState, onControl, SetStatusTTL(0*time.Second))
}

func newMiningControlTestDeviceWithDynamicMiningState(t *testing.T, miningControlStatus int, miningState *string, onControl func(path string), opts ...DeviceOption) *Device {
	t.Helper()
	return newMiningControlTestDeviceWithDynamicStateAndPools(
		t,
		miningControlStatus,
		miningState,
		configuredPoolsJSON,
		requestPathCallback(onControl),
		opts...,
	)
}

func newMiningControlTestDeviceWithPools(t *testing.T, miningControlStatus int, miningState, poolsJSON string, onControl func(path string)) *Device {
	t.Helper()
	return newMiningControlTestDeviceWithDynamicStateAndPools(
		t,
		miningControlStatus,
		&miningState,
		poolsJSON,
		requestPathCallback(onControl),
		SetStatusTTL(0*time.Second),
	)
}

func newMiningControlTestDeviceWithRequestCallback(t *testing.T, miningControlStatus int, miningState string, onControl func(*http.Request)) *Device {
	t.Helper()
	return newMiningControlTestDeviceWithDynamicStateAndPools(
		t,
		miningControlStatus,
		&miningState,
		configuredPoolsJSON,
		onControl,
		SetStatusTTL(0*time.Second),
	)
}

func requestPathCallback(callback func(path string)) func(*http.Request) {
	if callback == nil {
		return nil
	}
	return func(r *http.Request) {
		callback(r.URL.Path)
	}
}

func newMiningControlTestDeviceWithDynamicStateAndPools(t *testing.T, miningControlStatus int, miningState *string, poolsJSON string, onControl func(*http.Request), opts ...DeviceOption) *Device {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"r"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"mining-status":{"status":%q}}`, *miningState)))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pools":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(poolsJSON))
		case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/mining/start" || r.URL.Path == "/api/v1/mining/stop"):
			if onControl != nil {
				onControl(r)
			}
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
	}, sdk.UsernamePassword{Username: "admin", Password: "proto"}, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })

	return dev
}

type targetResponse struct {
	PowerTargetWatts        int    `json:"power_target_watts"`
	PowerTargetMinWatts     int    `json:"power_target_min_watts"`
	PowerTargetMaxWatts     int    `json:"power_target_max_watts"`
	DefaultPowerTargetWatts int    `json:"default_power_target_watts"`
	PerformanceMode         string `json:"performance_mode"`
}

type powerTargetRequest struct {
	PowerTargetWatts *int   `json:"power_target_watts,omitempty"`
	PerformanceMode  string `json:"performance_mode,omitempty"`
}

func newPowerTargetTestDevice(t *testing.T, targetStatus int, target targetResponse, requests *[]powerTargetRequest) *Device {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"r"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pools":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pools":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining/target":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(targetStatus)
			if targetStatus != http.StatusNoContent {
				if err := json.NewEncoder(w).Encode(target); err != nil {
					t.Errorf("encode target response: %v", err)
				}
			}
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/mining/target":
			var req powerTargetRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode target request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if requests != nil {
				*requests = append(*requests, req)
			}
			w.WriteHeader(http.StatusOK)
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
	}, sdk.UsernamePassword{Username: "admin", Password: "proto"}, SetStatusTTL(0*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })

	return dev
}

func newFullEfficiencyCurtailmentTestDevice(
	t *testing.T,
	miningState string,
	target targetResponse,
	requests *[]powerTargetRequest,
	onControl func(path string),
) *Device {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"r"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"mining-status":{"status":%q}}`, miningState)))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pools":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pools":[{"id":0,"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`))
		case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/mining/start" || r.URL.Path == "/api/v1/mining/stop"):
			if onControl != nil {
				onControl(r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mining/target":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(target); err != nil {
				t.Errorf("encode target response: %v", err)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/mining/target":
			var req powerTargetRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode target request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if requests != nil {
				*requests = append(*requests, req)
			}
			w.WriteHeader(http.StatusOK)
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
	}, sdk.UsernamePassword{Username: "admin", Password: "proto"}, SetStatusTTL(0*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })

	return dev
}
