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

func TestEquipmentObservationsPreservePresenceAndSourceTimes(t *testing.T) {
	now := time.Now().UTC()
	var plant plantResponse
	require.NoError(t, json.Unmarshal([]byte(`{"timestamp":"`+now.Add(-3*time.Minute).Format(time.RFC3339Nano)+`","coolingFans":[{"componentId":"fan-1","speedPercent":0,"powerW":0,"alarmActive":false},{"componentId":"fan-2"}]}`), &plant))
	fans := plant.fanObservations(now)
	require.Len(t, fans, 2)
	require.Equal(t, now.Add(-3*time.Minute), fans[0].ObservedAt)
	require.Equal(t, 0.0, *fans[0].SpeedPercent)
	require.Equal(t, 0.0, *fans[0].PowerW)
	require.NotNil(t, fans[0].AlarmActive)
	require.Nil(t, fans[1].SpeedPercent)
	require.Nil(t, fans[1].AlarmActive)
	plant.Timestamp = time.Time{}
	require.Empty(t, plant.fanObservations(now))
	plant.Timestamp = now.Add(time.Minute)
	require.Empty(t, plant.fanObservations(now))
}

func TestControlObservationDoesNotInventStateOrCapabilities(t *testing.T) {
	now := time.Now().UTC()
	var wire controlsResponse
	require.NoError(t, json.Unmarshal([]byte(`{"supported":true,"controls":[{"name":"dry_cooler","stateKnown":true,"enabled":false},{"name":"coolant_pump","stateKnown":false,"enabled":true}],"setpoints":[{"name":"coolant_pump_speed_hz","value":0,"unit":"Hz"}],"mainCircuitBreakerSupported":true}`), &wire))
	got := wire.observation(now)
	require.NotNil(t, got)
	require.Equal(t, now, got.ObservedAt)
	require.Equal(t, false, *got.Controls[0].Enabled)
	require.Nil(t, got.Controls[1].Enabled)
	require.Nil(t, got.MainCircuitBreakerClosed)
	require.Nil(t, got.AlarmMuteSupported)
	require.Equal(t, 0.0, *got.Setpoints[0].Value)
	require.Nil(t, (controlsResponse{}).observation(now))
}

func TestCollectEquipmentSupplementFailuresKeepInventory(t *testing.T) {
	for _, tc := range []struct {
		name, fans, controls   string
		failFans, failControls bool
	}{
		{name: "populated", fans: `"coolingFans":[{"componentId":"fan-1","speedPercent":0}]`, controls: `{"supported":true,"controls":[{"name":"dry_cooler","stateKnown":true}]}`},
		{name: "controls fail", fans: `"coolingFans":[{"componentId":"fan-1","powerW":5}]`, failControls: true},
		{name: "fans fail", controls: `{"supported":false}`, failFans: true},
		{name: "both fail", failFans: true, failControls: true},
		{name: "malformed telemetry", fans: `"coolingFans":[{"componentId":"fan-1"}],"plant":false`, controls: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/container/map":
					_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[]}]}}`))
				case "/api/v2/mining/status":
					_, _ = w.Write([]byte(`{}`))
				case "/api/v2/telemetry/container":
					if tc.failFans {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					_, _ = fmt.Fprintf(w, `{"timestamp":%q,%s}`, now.Format(time.RFC3339Nano), tc.fans)
				case "/api/v2/power/container/controls":
					if tc.failControls {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					_, _ = w.Write([]byte(tc.controls))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			require.NoError(t, err)
			port, err := strconv.ParseInt(u.Port(), 10, 32)
			require.NoError(t, err)
			c, err := NewClient(sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: "http"}, sdk.UsernamePassword{})
			require.NoError(t, err)
			got, err := c.Collect(t.Context(), "ct1")
			require.NoError(t, err)
			require.Len(t, got.Container.Tanks, 1)
			if tc.failFans || tc.name == "malformed telemetry" {
				require.Empty(t, got.Container.Fans)
			} else {
				require.Len(t, got.Container.Fans, 1)
				require.Equal(t, now, got.Container.Fans[0].ObservedAt)
			}
			if tc.failControls || tc.controls == `{}` {
				require.Nil(t, got.Container.Controls)
			} else {
				require.NotNil(t, got.Container.Controls)
			}
		})
	}
}

func TestControlObservationRetainsCapabilityOnlyResponses(t *testing.T) {
	for _, body := range []string{`{"mainCircuitBreakerClosed":false}`, `{"alarmResetSupported":false}`, `{"alarmMuteSupported":true}`, `{"dryCoolerFanReverseSupported":false}`} {
		t.Run(body, func(t *testing.T) {
			var wire controlsResponse
			require.NoError(t, json.Unmarshal([]byte(body), &wire))
			require.NotNil(t, wire.observation(time.Now().UTC()))
		})
	}
}
