package ct1

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestModuleHealth(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	healthy := func() Board { return Board{MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now}} }
	cases := []struct {
		name   string
		mutate func([]Board) []Board
		want   sdk.MinerStatus
	}{
		{"explicit rail fault", func(b []Board) []Board { v := false; b[0].Health.HashrailOK = &v; return b }, sdk.MinerStatusError},
		{"explicit clock fault", func(b []Board) []Board { v := false; b[0].Health.RefclksOK = &v; return b }, sdk.MinerStatusError},
		{"idle clocks off are not a fault", func(b []Board) []Board {
			for i := range b {
				v := false
				b[i].Health.RefclksOK = &v
				b[i].MiningState = miningStateIdle
			}
			return b
		}, sdk.MinerStatusInactive},
		{"hashing", func(b []Board) []Board { return b }, sdk.MinerStatusActive},
		{"one error affects one module", func(b []Board) []Board { b[0].Health.Errors = []Issue{{Cause: "BOARD_OVER_TEMPERATURE"}}; return b }, sdk.MinerStatusError},
		{"warnings preserve operation", func(b []Board) []Board { b[1].Health.Warnings = []Issue{{Cause: "ASIC_THERMAL_THROTTLING"}}; return b }, sdk.MinerStatusActive},
		{"explicit error without details", func(b []Board) []Board { b[0].MiningState = miningStateError; return b }, sdk.MinerStatusError},
		{"disconnected", func(b []Board) []Board { b[0].MiningState = "MINING_STATE_DISCONNECTED"; b[0].Health = nil; return b }, sdk.MinerStatusActive},
		{"missing health", func(b []Board) []Board { b[0].Health = nil; return b }, sdk.MinerStatusActive},
		{"stale errors are not active", func(b []Board) []Board {
			b[0].Health.Timestamp = now.Add(-10 * time.Minute)
			b[0].Health.Errors = []Issue{{Cause: "fault"}}
			return b
		}, sdk.MinerStatusActive},
		{"future health does not replace current operation", func(b []Board) []Board { b[0].Health.Timestamp = now.Add(time.Hour); return b }, sdk.MinerStatusActive},
		{"missing member", func(b []Board) []Board { return b[:2] }, sdk.MinerStatusUnknown},
		{"all idle", func(b []Board) []Board {
			for i := range b {
				b[i].MiningState = miningStateIdle
			}
			return b
		}, sdk.MinerStatusInactive},
		{"partially stopped", func(b []Board) []Board { b[0].MiningState = miningStateIdle; return b }, sdk.MinerStatusActive},
		{"startup is transitional", func(b []Board) []Board { b[0].MiningState = "MINING_STATE_STARTUP"; return b }, sdk.MinerStatusActive},
		{"all isolated", func(b []Board) []Board {
			for i := range b {
				b[i].Isolated = true
			}
			return b
		}, sdk.MinerStatusActive},
		{"isolation does not hide fault", func(b []Board) []Board {
			b[0].Isolated = true
			b[0].Health.Errors = []Issue{{Cause: "BOARD_OVER_TEMPERATURE"}}
			return b
		}, sdk.MinerStatusError},
		{"PSU warning", func(b []Board) []Board {
			b[0].Health.PSUThermal = &PSUThermal{ThermalStateValid: true, OverTemperatureWarning: true}
			return b
		}, sdk.MinerStatusActive},
		{"historical shutdown ignored", func(b []Board) []Board { b[0].Health.LastShutdownError = &Issue{Cause: "fault"}; return b }, sdk.MinerStatusActive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boards := tc.mutate([]Board{healthy(), healthy(), healthy()})
			require.Equal(t, tc.want, moduleState(boards, now))
		})
	}
}

func TestSnapshotPreservesOldestRequiredSourceObservation(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 2, 0, 0, time.UTC)
	oldest := now.Add(-119 * time.Second)
	slots := []Slot{}
	for i, stamp := range []time.Time{now, oldest, now.Add(-time.Second)} {
		slots = append(slots, Slot{Occupied: true, Hashboard: &Board{MACAddress: string(rune('a' + i)), MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: stamp}}})
	}
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{{ID: 1, Slots: slots}}}}}}
	got, err := snapshot(wire, now)
	require.NoError(t, err)
	require.Equal(t, now, got.ObservedAt)
	require.Equal(t, sdk.MinerStatusActive, got.Tanks[0].Modules[0].State)
	require.Equal(t, now, got.Tanks[0].Modules[0].ObservedAt, "current mining state is independent of sampled health age")
}

func TestModuleObservationEvidence(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		board    Board
		state    sdk.MinerStatus
		observed time.Time
	}{
		{"current error with stale health", Board{MiningState: miningStateError, Health: &Health{Timestamp: now.Add(-time.Hour)}}, sdk.MinerStatusError, now},
		{"disconnected without health", Board{MiningState: "MINING_STATE_DISCONNECTED"}, sdk.MinerStatusOffline, now},
		{"source warning", Board{MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now.Add(-119 * time.Second), Warnings: []Issue{{Cause: "fault"}}}}, sdk.MinerStatusActive, now},
		{"unknown has no evidence", Board{}, sdk.MinerStatusUnknown, time.Time{}},
		{"small clock skew cannot extend expiry", Board{MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now.Add(time.Second)}}, sdk.MinerStatusActive, now},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.observed, moduleObservedAt([]Board{tc.board}, tc.state, now)) })
	}
}

func TestSnapshotRequiresEveryOccupiedBoardToConfirmModuleIdentity(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		identity  string
		invalid   bool
		wantID    string
		wantState sdk.MinerStatus
	}{
		{"matching", "module-a", false, "module-a", sdk.MinerStatusActive},
		{"missing", "", false, "module-a", sdk.MinerStatusActive},
		{"blank", " \t", false, "module-a", sdk.MinerStatusActive},
		{"conflicting", "module-b", false, "", sdk.MinerStatusUnknown},
		{"invalid board", "module-a", true, "module-a", sdk.MinerStatusUnknown},
	} {
		for changed := range boardsPerModule {
			t.Run(tc.name+string(rune('1'+changed)), func(t *testing.T) {
				slots := make([]Slot, boardsPerModule)
				for i := range slots {
					slots[i] = Slot{Occupied: true, Hashboard: &Board{MACAddress: string(rune('a' + i)), ModuleID: "module-a", MiningState: miningStateMining, Health: &Health{Timestamp: now}}}
				}
				slots[changed].Hashboard.ModuleID = tc.identity
				slots[changed].invalid = tc.invalid
				got, err := snapshot(MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{{ID: 1, Slots: slots}}}}}}, now)
				require.NoError(t, err)
				require.Equal(t, tc.wantID, got.Tanks[0].Modules[0].ReportedID)
				require.Equal(t, tc.name == "matching", got.Tanks[0].Modules[0].IdentityConfirmed)
				require.Equal(t, tc.wantState, got.Tanks[0].Modules[0].State)
			})
		}
	}
}

func TestSnapshotKeepsDegradedMetricsWithoutConfirmingIdentity(t *testing.T) {
	now := time.Now()
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint("unavailable=", unavailable), func(t *testing.T) {
			slots := make([]Slot, boardsPerModule)
			for i := range slots {
				slots[i] = Slot{Occupied: true, Hashboard: &Board{MACAddress: string(rune('a' + i)), ModuleID: "module-a", MiningState: miningStateMining, Health: &Health{Timestamp: now, Hashrate: new(100.0), AverageAsicTemperatureC: new(60.0), PSUElectrical: &PSUElectrical{InputPower: new(3600.0)}}}}
			}
			slots[0].Hashboard.ModuleID = ""
			slots[0].invalid = unavailable
			wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{{ID: 1, Slots: slots}}}}}}
			got, err := snapshot(wire, now)
			require.NoError(t, err)
			module := got.Tanks[0].Modules[0]
			require.Equal(t, "module-a", module.ReportedID)
			require.False(t, module.IdentityConfirmed)
			require.Equal(t, 3600.0, *module.Metrics.PowerW)
			if !unavailable {
				require.Equal(t, 300e12, *module.Metrics.HashrateHS)
				require.Equal(t, 60.0, *module.Metrics.TemperatureC)
			}
			// A contradictory claim, even from an unavailable slot, cannot own metrics.
			slots[0].Hashboard.ModuleID = "replacement"
			got, err = snapshot(wire, now)
			require.NoError(t, err)
			require.Empty(t, got.Tanks[0].Modules[0].ReportedID)
			require.False(t, got.Tanks[0].Modules[0].IdentityConfirmed)
		})
	}
}

func TestSnapshotRejectsDuplicateModuleClaimsFromUnavailableSlots(t *testing.T) {
	now := time.Now()
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{
		{ID: 1, Slots: []Slot{{Occupied: true, Hashboard: &Board{MACAddress: "a", ModuleID: "module-a"}}, {}, {}}},
		{ID: 2, Slots: []Slot{{Occupied: false, Hashboard: &Board{MACAddress: "b", ModuleID: "module-a"}}, {}, {}}},
	}}}}}
	got, err := snapshot(wire, now)
	require.NoError(t, err)
	require.Empty(t, got.Tanks[0].Modules[0].ReportedID)
	require.False(t, got.Tanks[0].Modules[0].IdentityConfirmed)
}

func TestModuleErrorObservationUsesNewestEvidence(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	old := now.Add(-119 * time.Second)
	recent := now.Add(-10 * time.Second)
	fault := func(at time.Time) Board {
		return Board{MiningState: miningStateMining, Health: &Health{Timestamp: at, Errors: []Issue{{Cause: "BOARD_OVER_TEMPERATURE"}}}}
	}
	mixed := fault(recent)
	mixed.Health.PSUThermal = &PSUThermal{ThermalStateValid: true, OverTemperatureFault: true, ThermalStateAgeMs: new(109000.0)}
	for _, tc := range []struct {
		name     string
		boards   []Board
		observed time.Time
	}{
		{"explicit error survives older fault", []Board{fault(old), {MiningState: miningStateError}}, now},
		{"newer fault survives older fault", []Board{fault(old), fault(recent)}, recent},
		{"healthy board does not refresh fault", []Board{fault(old), {MiningState: miningStateMining, Health: &Health{Timestamp: now}}}, old},
		{"stale fault does not supply evidence", []Board{fault(now.Add(-time.Hour)), fault(recent)}, recent},
		{"board fault survives older thermal fault", []Board{mixed}, recent},
		{"clock skew cannot extend expiry", []Board{fault(now.Add(time.Second))}, now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 2 {
				state := moduleState(tc.boards, now)
				require.Equal(t, sdk.MinerStatusError, state)
				require.Equal(t, tc.observed, moduleObservedAt(tc.boards, state, now))
				for i, j := 0, len(tc.boards)-1; i < j; i, j = i+1, j-1 {
					tc.boards[i], tc.boards[j] = tc.boards[j], tc.boards[i]
				}
			}
		})
	}
}
