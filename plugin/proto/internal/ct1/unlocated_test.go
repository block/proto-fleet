package ct1

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestCollectUnlocatedInventory(t *testing.T) {
	for _, mapped := range []bool{true, false} {
		t.Run(fmt.Sprintf("mapped=%t", mapped), func(t *testing.T) {
			now := time.Now().UTC()
			unlocatedSlots := make([]any, 6)
			for i := range unlocatedSlots {
				unlocatedSlots[i] = map[string]any{"occupied": true, "hashboard": map[string]any{
					"serial":      fmt.Sprintf("unlocated-%d", i),
					"location":    map[string]any{"tank": 0, "module": 0},
					"miningState": "MINING_STATE_MINING",
					"health":      map[string]any{"timestamp": now, "hashrate": 999},
				}}
			}
			tanks := []any{map[string]any{"id": 0, "modules": []any{map[string]any{"id": 0, "slots": unlocatedSlots}}}}
			if mapped {
				for ti := 1; ti <= sdk.CT1TankCount; ti++ {
					modules := make([]any, sdk.CT1ModulesPerTank)
					for mi := range modules {
						modules[mi] = pollingCorrectionsModule(now, mi+1, "")
					}
					if ti == 1 {
						modules[0] = pollingCorrectionsModule(now, 1, "mapped", [2]float64{10, 40}, [2]float64{20, 50}, [2]float64{30, 60})
					}
					tanks = append(tanks, map[string]any{"id": ti, "modules": modules})
				}
			}
			var logs bytes.Buffer
			priorLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(priorLogger) })
			got := collectPollingCorrections(t, now, map[string]any{"containerMap": map[string]any{"tanks": tanks}}, map[string]any{"facilityPowerW": 5000})
			require.False(t, got.Container.TopologyComplete)
			require.EqualValues(t, 6, got.Container.Metrics.UnlocatedHashboards)
			require.False(t, got.Container.Metrics.Hashrate.Complete())
			require.False(t, got.Container.Metrics.Temperature.Complete())
			require.Nil(t, got.EfficiencyJH, "unlocated inventory prevents complete container efficiency")
			require.Equal(t, sdk.HealthWarning, got.Health)
			require.NotNil(t, got.PowerW, "independent facility telemetry still updates")
			require.Equal(t, 5000.0, got.PowerW.Value)
			require.Contains(t, logs.String(), `"msg":"CT1 inventory contains unlocated hashboards"`)
			require.Contains(t, logs.String(), `"hashboards":6`)
			if !mapped {
				require.Empty(t, got.Container.Tanks)
				require.NotNil(t, got.HashrateHS)
				require.Equal(t, 6*999e12, got.HashrateHS.Value, "unlocated readings contribute only at container level")
				return
			}
			require.Len(t, got.Container.Tanks, 6)
			for i, tank := range got.Container.Tanks {
				require.EqualValues(t, i+1, tank.Position)
				require.True(t, tank.TopologyComplete)
			}
			module := got.Container.Tanks[0].Modules[0]
			require.Equal(t, sdk.ModuleHashing, module.State)
			require.Equal(t, now, module.ObservedAt)
			require.Equal(t, "mapped", module.ReportedID)
			require.Equal(t, 60e12, *module.Metrics.HashrateHS)
			require.Equal(t, 60e12+6*999e12, got.HashrateHS.Value, "container includes mapped and unlocated boards once")
		})
	}
}

func TestSnapshotUnlocatedIdentityConflicts(t *testing.T) {
	for _, unlocatedFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("unlocated_first=%t", unlocatedFirst), func(t *testing.T) {
			// The claim is outside the three physical slots a mapped module has.
			// Inventory group size must not truncate duplicate-identity checks.
			unlocated := Tank{ID: 0, Modules: []Module{{ID: 0, Slots: make([]Slot, 6)}}}
			unlocated.Modules[0].Slots[5] = Slot{Occupied: true, Hashboard: &Board{Serial: "duplicate"}}
			mapped := Tank{ID: 1, Modules: []Module{{ID: 1, Slots: []Slot{{Occupied: true, Hashboard: &Board{Serial: "duplicate"}}}}}}
			tanks := []Tank{mapped, unlocated}
			if unlocatedFirst {
				tanks = []Tank{unlocated, mapped}
			}
			got, err := snapshot(MapResponse{ContainerMap: &ContainerMap{Tanks: tanks}}, time.Now())
			require.NoError(t, err)
			require.Len(t, got.Tanks, 1)
			require.Equal(t, sdk.HashboardIdentityConflict, got.Tanks[0].Modules[0].Hashboards[0].UnavailableReason)
			require.False(t, got.TopologyComplete)
		})
	}
}

func TestSnapshotInvalidTopologyWithUnlocatedInventory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tanks []Tank
		err   string
	}{
		{"negative tank", []Tank{{ID: -1}}, "invalid or duplicate tank -1"},
		{"duplicate tank", []Tank{{ID: 1}, {ID: 1}}, "invalid or duplicate tank 1"},
		{"duplicate unlocated tank", []Tank{{ID: 0}}, "invalid or duplicate tank 0"},
		{"zero mapped module", []Tank{{ID: 1, Modules: []Module{{ID: 0}}}}, "invalid or duplicate module 0 in tank 1"},
		{"negative mapped module", []Tank{{ID: 1, Modules: []Module{{ID: -1}}}}, "invalid or duplicate module -1 in tank 1"},
		{"duplicate mapped module", []Tank{{ID: 1, Modules: []Module{{ID: 1}, {ID: 1}}}}, "invalid or duplicate module 1 in tank 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tanks := append([]Tank{{ID: 0, Modules: []Module{{ID: 0}}}}, tc.tanks...)
			_, err := snapshot(MapResponse{ContainerMap: &ContainerMap{Tanks: tanks}}, time.Now())
			require.EqualError(t, err, tc.err)
		})
	}
	for _, tc := range []struct {
		name    string
		modules []Module
	}{
		{"negative unlocated module", []Module{{ID: -1}}},
		{"positive unlocated module", []Module{{ID: 1}}},
		{"duplicate unlocated module", []Module{{ID: 0}, {ID: 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := snapshot(MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 0, Modules: tc.modules}}}}, time.Now())
			require.ErrorContains(t, err, "invalid or duplicate module")
		})
	}
}

func TestStoppedUnlocatedContainerStaysInactive(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			mapped := ""
			if mixed {
				mapped = `,{"id":1,"modules":[{"id":1,"slots":[]}]}`
			}
			client := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/container/map":
					_, _ = fmt.Fprintf(w, `{"containerMap":{"tanks":[{"id":0,"modules":[{"id":0,"slots":[{"occupied":true,"hashboard":{"serial":"unlocated"}}]}]}%s]}}`, mapped)
				case "/api/v2/mining/status":
					_, _ = w.Write([]byte(`{"mining":false}`))
				case "/api/v2/telemetry/container":
					_, _ = fmt.Fprintf(w, `{"timestamp":%q,"plant":{"alarmActive":true}}`, time.Now().UTC().Format(time.RFC3339Nano))
				case "/api/v2/power/container/controls":
					_, _ = w.Write([]byte(`{"supported":false}`))
				default:
					http.NotFound(w, r)
				}
			}))
			got, err := client.Collect(t.Context(), "controller")
			require.NoError(t, err)
			require.Equal(t, sdk.HealthHealthyInactive, got.Health)
			require.EqualValues(t, 1, got.Container.Metrics.UnlocatedHashboards)
		})
	}
}

func TestUnlocatedModuleIdentityDoesNotClaimPhysicalLocation(t *testing.T) {
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{
		{ID: 0, Modules: []Module{{ID: 0, Slots: []Slot{{Hashboard: &Board{Serial: "unlocated", ModuleID: "module-a"}}}}}},
		{ID: 1, Modules: []Module{{ID: 1, Slots: []Slot{{Hashboard: &Board{Serial: "mapped", ModuleID: "module-a"}}}}}},
	}}}
	ids := collectTopologyIdentities(wire)
	require.True(t, ids.uniqueModule(1, 1, "module-a"))
	require.Equal(t, 1, ids.boards["serial:unlocated"], "unlocated board identity still participates in conflict checks")
}
