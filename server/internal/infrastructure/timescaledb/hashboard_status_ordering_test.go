package timescaledb

import (
	"encoding/json"
	"testing"
	"time"

	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestHashboardStatusFollowsAcceptedCollectionOrder(t *testing.T) {
	r := newHashboardReviewRig(t)
	poll := func(collected, health, stateAt time.Duration, state sdk.MinerStatus) {
		at := r.base.Add(collected)
		board := reviewBoard("a", "serial-a")
		board.ObservedAt, board.StateObservedAt, board.State = r.base.Add(health), r.base.Add(stateAt), state
		board.MiningState = "MINING_STATE_MINING"
		if state == sdk.MinerStatusError {
			board.HealthJSON = json.RawMessage(`{"errors":[{"cause":"BOARD_OVER_TEMPERATURE"}]}`)
		}
		snapshot := &sdk.ContainerSnapshot{ObservedAt: at, TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, State: state, ObservedAt: board.StateObservedAt, Hashboards: []sdk.ContainerHashboard{board}}}}}}
		require.NoError(t, r.store.StoreDeviceMetrics(t.Context(), modelsV2.DeviceMetrics{DeviceIdentifier: r.deviceID, Timestamp: at, Health: modelsV2.HealthHealthyActive, Container: snapshot}))
	}
	poll(10*time.Second, 5*time.Second, 10*time.Second, sdk.MinerStatusActive)
	poll(11*time.Second, 8*time.Second, 8*time.Second, sdk.MinerStatusError)
	current := r.read(t)[0].Slots[0].Board
	var moduleState, metricState string
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT state FROM container_module WHERE id=$1", r.modules[0]).Scan(&moduleState))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT state FROM hashboard_latest_metrics WHERE hashboard_id=$1", current.HashboardId).Scan(&metricState))
	require.Equal(t, "error", moduleState)
	require.Equal(t, "error", metricState)
	require.Equal(t, "error", current.State, "newer accepted fault must replace active")
	require.Equal(t, r.base.Add(8*time.Second), current.StateObservedAt.AsTime())
	require.Equal(t, r.base.Add(8*time.Second), current.ObservedAt.AsTime())
	// A delayed snapshot cannot replace the accepted fault, even with newer state evidence.
	poll(10*time.Second, 5*time.Second, 12*time.Second, sdk.MinerStatusActive)
	current = r.read(t)[0].Slots[0].Board
	require.Equal(t, "error", current.State)
	require.Equal(t, r.base.Add(8*time.Second), current.StateObservedAt.AsTime())
	// Current operating evidence can change without regressing the health watermark.
	poll(12*time.Second, 7*time.Second, 12*time.Second, sdk.MinerStatusActive)
	current = r.read(t)[0].Slots[0].Board
	require.Equal(t, "active", current.State)
	require.Equal(t, r.base.Add(12*time.Second), current.StateObservedAt.AsTime())
	require.Equal(t, r.base.Add(8*time.Second), current.ObservedAt.AsTime())
	require.Equal(t, r.base.Add(11*time.Second), current.CollectedAt.AsTime())
}

func TestUnknownHashboardRetainsFreshIssues(t *testing.T) {
	r := newHashboardReviewRig(t)
	at := r.base.Add(10 * time.Second)
	board := reviewBoard("a", "serial-a")
	board.State, board.StateObservedAt, board.ObservedAt = sdk.MinerStatusUnknown, time.Time{}, at
	board.HealthJSON = json.RawMessage(`{"errors":[{"cause":"ASIC_ENUMERATION"}]}`)
	issues := &sdk.ContainerModuleIssues{ObservedAt: at, Complete: false, SourceTimes: map[string]time.Time{"hashboard:a": at}, Items: []sdk.ContainerModuleIssue{{Component: "hashboard", ComponentID: "a", Cause: "ASIC_ENUMERATION", Severity: "fault", ObservedAt: at}}}
	snapshot := &sdk.ContainerSnapshot{ObservedAt: at, TopologyComplete: true, Tanks: []sdk.ContainerTank{{Position: 1, Modules: []sdk.ContainerModule{{Position: 1, State: sdk.MinerStatusUnknown, Issues: issues, Hashboards: []sdk.ContainerHashboard{board}}}}}}
	require.NoError(t, r.store.StoreDeviceMetrics(t.Context(), modelsV2.DeviceMetrics{DeviceIdentifier: r.deviceID, Timestamp: at, Health: modelsV2.HealthHealthyActive, Container: snapshot}))
	current := r.read(t)[0].Slots[0].Board
	require.Equal(t, "unknown", current.State)
	require.Nil(t, current.StateObservedAt)
	require.Len(t, current.Issues, 1)
	require.Equal(t, "ASIC_ENUMERATION", current.Issues[0].Cause)
}
