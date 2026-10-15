package device

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestStatusRefreshesUptimeAfterExternalReboot(t *testing.T) {
	var response atomic.Pointer[string]
	initialResponse := `{"system-info":{"os":{"version":"1.0.0"},"uptime_seconds":7200}}`
	response.Store(&initialResponse)
	var systemReads atomic.Int32
	dev := newUptimeTestDevice(t, func(w http.ResponseWriter, _ *http.Request) {
		systemReads.Add(1)
		_, _ = w.Write([]byte(*response.Load()))
	})

	initial, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.NotNil(t, initial.UptimeSeconds)
	require.Equal(t, uint64(7200), *initial.UptimeSeconds)
	require.False(t, initial.UptimeObservedAt.IsZero())

	// The rig reboots outside Fleet. Ordinary polls must retain the original
	// uptime timestamp until the system endpoint is actually read again.
	rebootedResponse := `{"system-info":{"os":{"version":"2.0.0"},"uptime_seconds":0}}`
	response.Store(&rebootedResponse)
	cached, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, initial.UptimeSeconds, cached.UptimeSeconds)
	require.Equal(t, initial.UptimeObservedAt, cached.UptimeObservedAt)
	require.Equal(t, "1.0.0", cached.FirmwareVersion)
	require.Equal(t, int32(1), systemReads.Load())

	dev.lastFirmwareCheckAt = time.Now().Add(-firmwareRefreshInterval)
	refreshed, err := dev.Status(t.Context())
	require.NoError(t, err)
	require.NotNil(t, refreshed.UptimeSeconds)
	require.Zero(t, *refreshed.UptimeSeconds)
	require.True(t, refreshed.UptimeObservedAt.After(initial.UptimeObservedAt))
	require.Equal(t, "2.0.0", refreshed.FirmwareVersion)
	require.Equal(t, int32(2), systemReads.Load())
}

func TestStatusClearsUptimeWhenSystemProbeHasNoEvidence(t *testing.T) {
	for _, tt := range []struct {
		name       string
		statusCode int
		response   string
	}{
		{name: "missing uptime", statusCode: http.StatusOK, response: `{"system-info":{"os":{"version":"2.0.0"}}}`},
		{name: "invalid uptime", statusCode: http.StatusOK, response: `{"system-info":{"uptime_seconds":-1}}`},
		{name: "failed request", statusCode: http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var systemReads atomic.Int32
			dev := newUptimeTestDevice(t, func(w http.ResponseWriter, _ *http.Request) {
				if systemReads.Add(1) == 1 {
					_, _ = w.Write([]byte(`{"system-info":{"os":{"version":"1.0.0"},"uptime_seconds":100}}`))
					return
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			})
			initial, err := dev.Status(t.Context())
			require.NoError(t, err)
			require.NotNil(t, initial.UptimeSeconds)

			dev.lastFirmwareCheckAt = time.Now().Add(-firmwareRefreshInterval)
			for range 2 {
				metrics, err := dev.Status(t.Context())
				require.NoError(t, err)
				require.Nil(t, metrics.UptimeSeconds)
				require.Zero(t, metrics.UptimeObservedAt)
			}
			require.Equal(t, int32(2), systemReads.Load(), "failed or missing evidence is also throttled")
		})
	}
}

func newUptimeTestDevice(t *testing.T, systemHandler http.HandlerFunc) *Device {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pairing/info":
			_, _ = w.Write([]byte(`{"cb_sn":"proto-serial","mac":"aa:bb:cc:dd:ee:ff"}`))
		case "/api/v1/auth/login":
			_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"r"}`))
		case "/api/v1/mining":
			_, _ = w.Write([]byte(`{"mining-status":{"status":"Mining"}}`))
		case "/api/v1/pools":
			_, _ = w.Write([]byte(`{"pools":[{"id":0,"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`))
		case "/api/v1/telemetry":
			_, _ = w.Write([]byte(`{}`))
		case "/api/v1/system/status":
			_, _ = w.Write([]byte(`{"default_password_active":false}`))
		case "/api/v1/system":
			systemHandler(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	host, portString, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	port, err := strconv.ParseInt(portString, 10, 32)
	require.NoError(t, err)
	dev, err := New(t.Context(), "uptime-miner", sdk.DeviceInfo{
		Host: host, Port: int32(port), URLScheme: "http", FirmwareVersion: "stale",
	}, sdk.UsernamePassword{Username: "admin", Password: "proto"}, SetStatusTTL(0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dev.Close(context.Background()) })
	return dev
}
