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
