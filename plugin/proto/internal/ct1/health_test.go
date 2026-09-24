package ct1

import (
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
		want   sdk.ModuleState
	}{
		{"explicit rail fault", func(b []Board) []Board { v := false; b[0].Health.HashrailOK = &v; return b }, sdk.ModuleAttention},
		{"explicit clock fault", func(b []Board) []Board { v := false; b[0].Health.RefclksOK = &v; return b }, sdk.ModuleAttention},
		{"idle clocks off are not a fault", func(b []Board) []Board {
			for i := range b {
				v := false
				b[i].Health.RefclksOK = &v
				b[i].MiningState = miningStateIdle
			}
			return b
		}, sdk.ModuleSleeping},
		{"hashing", func(b []Board) []Board { return b }, sdk.ModuleHashing},
		{"one error affects one module", func(b []Board) []Board { b[0].Health.Errors = []Issue{{Cause: "BOARD_OVER_TEMPERATURE"}}; return b }, sdk.ModuleAttention},
		{"warnings count", func(b []Board) []Board { b[1].Health.Warnings = []Issue{{Cause: "ASIC_THERMAL_THROTTLING"}}; return b }, sdk.ModuleAttention},
		{"explicit error without details", func(b []Board) []Board { b[0].MiningState = miningStateError; return b }, sdk.ModuleAttention},
		{"disconnected", func(b []Board) []Board { b[0].MiningState = "MINING_STATE_DISCONNECTED"; b[0].Health = nil; return b }, sdk.ModuleOffline},
		{"missing health", func(b []Board) []Board { b[0].Health = nil; return b }, sdk.ModuleUnknown},
		{"stale errors are not active", func(b []Board) []Board {
			b[0].Health.Timestamp = now.Add(-10 * time.Minute)
			b[0].Health.Errors = []Issue{{Cause: "fault"}}
			return b
		}, sdk.ModuleUnknown},
		{"future readings unknown", func(b []Board) []Board { b[0].Health.Timestamp = now.Add(time.Hour); return b }, sdk.ModuleUnknown},
		{"missing member", func(b []Board) []Board { return b[:2] }, sdk.ModuleUnknown},
		{"all idle", func(b []Board) []Board {
			for i := range b {
				b[i].MiningState = miningStateIdle
			}
			return b
		}, sdk.ModuleSleeping},
		{"partially stopped", func(b []Board) []Board { b[0].MiningState = miningStateIdle; return b }, sdk.ModuleAttention},
		{"startup is transitional", func(b []Board) []Board { b[0].MiningState = "MINING_STATE_STARTUP"; return b }, sdk.ModuleUnknown},
		{"all isolated", func(b []Board) []Board {
			for i := range b {
				b[i].Isolated = true
			}
			return b
		}, sdk.ModuleIsolated},
		{"isolation does not hide fault", func(b []Board) []Board {
			b[0].Isolated = true
			b[0].Health.Errors = []Issue{{Cause: "fault"}}
			return b
		}, sdk.ModuleAttention},
		{"PSU warning", func(b []Board) []Board {
			b[0].Health.PSUThermal = &PSUThermal{ThermalStateValid: true, OverTemperatureWarning: true}
			return b
		}, sdk.ModuleAttention},
		{"historical shutdown ignored", func(b []Board) []Board { b[0].Health.LastShutdownError = &Issue{Cause: "fault"}; return b }, sdk.ModuleHashing},
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
	require.Equal(t, sdk.ModuleHashing, got.Tanks[0].Modules[0].State)
	require.Equal(t, oldest, got.Tanks[0].Modules[0].ObservedAt)
}

func TestModuleObservationEvidence(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		board    Board
		state    sdk.ModuleState
		observed time.Time
	}{
		{"current error with stale health", Board{MiningState: miningStateError, Health: &Health{Timestamp: now.Add(-time.Hour)}}, sdk.ModuleAttention, now},
		{"disconnected without health", Board{MiningState: "MINING_STATE_DISCONNECTED"}, sdk.ModuleOffline, now},
		{"source warning", Board{MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now.Add(-119 * time.Second), Warnings: []Issue{{Cause: "fault"}}}}, sdk.ModuleAttention, now.Add(-119 * time.Second)},
		{"unknown has no evidence", Board{}, sdk.ModuleUnknown, time.Time{}},
		{"small clock skew cannot extend expiry", Board{MiningState: "MINING_STATE_MINING", Health: &Health{Timestamp: now.Add(time.Second)}}, sdk.ModuleHashing, now},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.observed, moduleObservedAt([]Board{tc.board}, tc.state, now)) })
	}
}
