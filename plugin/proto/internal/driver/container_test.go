package driver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// A real HTTP boundary covers classification, credential-free pairing on current
// Frigate, and reconstruction by a fresh driver after the plugin restarts.
func TestContainerLifecycle(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/system/info":
			_, _ = w.Write([]byte(`{"info":{"id":"controller-a","product":"Frigate","version":"v1"}}`))
		case "/api/v2/container/map":
			_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1}]}]}}`))
		case "/api/v2/mining/status":
			_, _ = w.Write([]byte(`{"hashrate":100,"mining":true}`))
		case "/api/v2/telemetry/container":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"timestamp": now, "plant": map[string]any{"totalPowerW": 3200}}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	driver, err := New(port)
	require.NoError(t, err)
	info, err := driver.DiscoverDevice(t.Context(), u.Hostname(), u.Port())
	require.NoError(t, err)
	require.Equal(t, sdk.DeviceClassContainer, info.DeviceClass)
	info, err = driver.PairDevice(t.Context(), info, sdk.SecretBundle{})
	require.NoError(t, err)
	restarted, err := New(port)
	require.NoError(t, err)
	result, err := restarted.NewDevice(t.Context(), "container-1", info, sdk.SecretBundle{})
	require.NoError(t, err)
	metrics, err := result.Device.Status(t.Context())
	require.NoError(t, err)
	require.Equal(t, "container-1", metrics.DeviceID)
	require.Equal(t, "v1", metrics.FirmwareVersion)
	require.Equal(t, sdk.ModuleUnknown, metrics.Container.Tanks[0].Modules[0].State)
	caps := restarted.GetCapabilitiesForModel(t.Context(), info.Manufacturer, info.Model)
	require.True(t, caps[sdk.CapabilityRealtimeTelemetry])
	require.False(t, caps[sdk.CapabilityMiningStop])
	require.False(t, caps[sdk.CapabilityBasicAuth])
}
