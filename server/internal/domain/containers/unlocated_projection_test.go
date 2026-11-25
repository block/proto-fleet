package containers

import (
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestUnlocatedBoardProjectionPreservesInventoryAndFreshEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	health, err := json.Marshal(map[string]any{
		"timestamp": now,
		"errors":    []map[string]string{{"cause": "HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE"}},
		"warnings":  []map[string]string{{"cause": "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT"}},
	})
	require.NoError(t, err)
	board := sdk.ContainerHashboard{
		MACAddress: "02:00:00:00:00:01", Serial: "unlocated-1", State: sdk.MinerStatusError,
		ObservedAt: now, StateObservedAt: now, HashrateHS: new(0.0), TemperatureC: new(0.0),
		MetadataJSON: json.RawMessage(`{"firmwareVersion":"2.3.4"}`), HealthJSON: health,
	}
	data, err := json.Marshal([]sdk.ContainerHashboard{board})
	require.NoError(t, err)
	c := &pb.Container{}
	require.NoError(t, projectUnlocated(c, data, now))
	require.Len(t, c.UnlocatedBoards, 1)
	got := c.UnlocatedBoards[0]
	require.Equal(t, board.MACAddress, got.MacAddress)
	require.Equal(t, board.Serial, got.Serial)
	require.Equal(t, "2.3.4", got.FirmwareVersion)
	require.Equal(t, "error", got.State)
	require.True(t, now.Equal(got.ObservedAt.AsTime()))
	require.False(t, got.Stale)
	require.Equal(t, new(0.0), got.HashrateHs, "a measured zero must remain distinct from missing evidence")
	require.Equal(t, new(0.0), got.TemperatureC)
	require.True(t, got.IssuesKnown)
	require.Equal(t, []string{"board over voltage", "Warning: board over current"}, got.Issues)
}

func TestUnlocatedBoardProjectionWithholdsStaleEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tc := range []struct {
		name           string
		observedAt     time.Time
		containerStale bool
	}{
		{"old board observation", now.Add(-3 * time.Minute), false},
		{"future board observation", now.Add(time.Minute), false},
		{"missing board observation", time.Time{}, false},
		{"stale container inventory", now, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			health, err := json.Marshal(map[string]any{"timestamp": now, "errors": []map[string]string{{"cause": "HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE"}}})
			require.NoError(t, err)
			data, err := json.Marshal([]sdk.ContainerHashboard{{
				MACAddress: "02:00:00:00:00:01", Serial: "unlocated-1", State: sdk.MinerStatusError,
				ObservedAt: tc.observedAt, StateObservedAt: now, HashrateHS: new(100e12), TemperatureC: new(65.0),
				MetadataJSON: json.RawMessage(`{"firmwareVersion":"2.3.4"}`), HealthJSON: health,
			}})
			require.NoError(t, err)
			c := &pb.Container{Stale: tc.containerStale}
			require.NoError(t, projectUnlocated(c, data, now))
			require.Len(t, c.UnlocatedBoards, 1)
			got := c.UnlocatedBoards[0]
			require.Equal(t, "unlocated-1", got.Serial)
			require.Equal(t, "2.3.4", got.FirmwareVersion)
			require.True(t, got.Stale)
			require.Equal(t, "unknown", got.State)
			require.Nil(t, got.HashrateHs)
			require.Nil(t, got.TemperatureC)
			require.False(t, got.IssuesKnown)
			require.Empty(t, got.Issues)
		})
	}
}

func TestUnlocatedBoardProjectionUsesIndependentStateAndHealthTimes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tc := range []struct {
		name        string
		stateAt     time.Time
		healthAt    time.Time
		wantState   string
		issuesKnown bool
	}{
		{"fresh empty health", now, now, "active", true},
		{"old state", now.Add(-3 * time.Minute), now, "unknown", true},
		{"missing state", time.Time{}, now, "unknown", true},
		{"old health", now, now.Add(-3 * time.Minute), "active", false},
		{"future health", now, now.Add(time.Minute), "active", false},
		{"missing health time", now, time.Time{}, "active", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			health, err := json.Marshal(map[string]any{"timestamp": tc.healthAt, "errors": []any{}, "warnings": []any{}})
			require.NoError(t, err)
			data, err := json.Marshal([]sdk.ContainerHashboard{{
				Serial: "unlocated-1", ObservedAt: now, State: sdk.MinerStatusActive, StateObservedAt: tc.stateAt,
				HashrateHS: new(100e12), HealthJSON: health,
			}})
			require.NoError(t, err)
			c := &pb.Container{}
			require.NoError(t, projectUnlocated(c, data, now))
			require.Len(t, c.UnlocatedBoards, 1)
			got := c.UnlocatedBoards[0]
			require.False(t, got.Stale)
			require.Equal(t, tc.wantState, got.State)
			require.Equal(t, tc.issuesKnown, got.IssuesKnown)
			require.Empty(t, got.Issues)
			require.Equal(t, new(100e12), got.HashrateHs)
		})
	}
}
