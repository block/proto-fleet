package ct1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestCollectBulkMapAuthenticatesAndCountsModulesOnce(t *testing.T) {
	calls := map[string]int{}
	now := time.Now().UTC()
	boards := []Slot{}
	for i := range 3 {
		boards = append(boards, Slot{Occupied: true, Hashboard: &Board{MACAddress: fmt.Sprintf("00:00:00:00:00:%02d", i), ModuleID: "module-a", MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now, Errors: []Issue{{Cause: "fault"}}}}})
	}
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{{ID: 1, Slots: boards}}}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/auth/login" {
			var login map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&login))
			require.Equal(t, "password", login["password"])
			_, _ = w.Write([]byte(`{"token":"test-session"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-session" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v2/container/map":
			require.NoError(t, json.NewEncoder(w).Encode(wire))
		case "/api/v2/telemetry/container":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"timestamp": now, "plant": map[string]any{"facilityPowerW": 3500, "pue": 1.1}}))
		case "/api/v2/power/container/controls":
			_, _ = w.Write([]byte(`{"supported":false}`))
		case "/api/v2/mining/status":
			_, _ = w.Write([]byte(`{"hashrate":100,"power":3000,"mining":true}`))
		default:
			t.Errorf("unexpected per-component request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 32)
	require.NoError(t, err)
	c, err := NewClient(sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)
	metrics, err := c.Collect(t.Context(), "controller-1")
	require.NoError(t, err)
	require.Len(t, metrics.Container.Tanks, 1)
	require.Len(t, metrics.Container.Tanks[0].Modules, 1)
	require.Equal(t, sdk.ModuleAttention, metrics.Container.Tanks[0].Modules[0].State)
	require.Equal(t, 100e12, metrics.HashrateHS.Value)
	require.Equal(t, 3500.0, metrics.PowerW.Value)
	require.Equal(t, 1.1, *metrics.Container.Plant.PUE)
	require.Equal(t, 1, calls["/api/v2/auth/login"])
	require.Equal(t, 2, calls["/api/v2/container/map"])
	require.Equal(t, 1, calls["/api/v2/mining/status"])
}

func TestSnapshotRejectsDuplicateMembership(t *testing.T) {
	board := &Board{MACAddress: "a"}
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{{ID: 1, Slots: []Slot{{Occupied: true, Hashboard: board}, {Occupied: true, Hashboard: board}}}}}}}}
	_, err := snapshot(wire, time.Now())
	require.ErrorContains(t, err, "duplicate hashboard")
}

func TestCollectDerivesEfficiencyFromControllerTotals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mining  string
		plant   map[string]any
		stale   bool
		wantJTH *float64
	}{
		{name: "mining power", mining: `{"hashrate":100,"power":3000}`, wantJTH: new(30.0)},
		{name: "facility power takes precedence", mining: `{"hashrate":100,"power":3000}`, plant: map[string]any{"facilityPowerW": 3500, "totalPowerW": 3200}, wantJTH: new(35.0)},
		{name: "plant total power", mining: `{"hashrate":100}`, plant: map[string]any{"totalPowerW": 3200}, wantJTH: new(32.0)},
		{name: "stale plant uses mining power", mining: `{"hashrate":100,"power":3000}`, plant: map[string]any{"facilityPowerW": 3500}, stale: true, wantJTH: new(30.0)},
		{name: "missing power", mining: `{"hashrate":100}`},
		{name: "missing hashrate", mining: `{"power":3000}`},
		{name: "stopped", mining: `{"hashrate":0,"power":3000}`},
		{name: "negative hashrate", mining: `{"hashrate":-100,"power":3000}`},
		{name: "negative power", mining: `{"hashrate":100,"power":-3000}`},
		{name: "measured zero power", mining: `{"hashrate":100,"power":0}`, wantJTH: new(0.0)},
		{name: "overflowing hashrate conversion", mining: `{"hashrate":1e308,"power":3000}`},
		{name: "overflowing ratio", mining: `{"hashrate":1e-300,"power":1e308}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/container/map":
					_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[]}]}}`))
				case "/api/v2/mining/status":
					_, _ = w.Write([]byte(tc.mining))
				case "/api/v2/telemetry/container":
					observedAt := time.Now().UTC()
					if tc.stale {
						observedAt = observedAt.Add(-5 * time.Minute)
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"timestamp": observedAt, "plant": tc.plant}))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			require.NoError(t, err)
			port, err := strconv.ParseInt(u.Port(), 10, 32)
			require.NoError(t, err)
			client, err := NewClient(sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
			require.NoError(t, err)
			metrics, err := client.Collect(t.Context(), "ct1")
			require.NoError(t, err)
			if tc.wantJTH == nil {
				require.Nil(t, metrics.EfficiencyJH)
			} else {
				require.NotNil(t, metrics.EfficiencyJH)
				require.InDelta(t, *tc.wantJTH, metrics.EfficiencyJH.Value*1e12, 1e-9)
				require.Equal(t, sdk.MetricKindGauge, metrics.EfficiencyJH.Kind)
			}
		})
	}
}
