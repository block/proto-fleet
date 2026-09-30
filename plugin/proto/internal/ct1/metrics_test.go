package ct1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModuleMetricsUseSharedPSUOnceAndPreserveMissingData(t *testing.T) {
	now := time.Now().UTC()
	var boards []Board
	for _, age := range []int{30, 10, 20} {
		wire, err := json.Marshal(map[string]any{"health": map[string]any{
			"timestamp": now, "hashrate": 100, "averageAsicTemperatureC": 65,
			"power": 1000, "psuElectrical": map[string]any{"inputPower": 3600, "sampleAgeMs": age},
		}})
		require.NoError(t, err)
		var b Board
		require.NoError(t, json.Unmarshal(wire, &b))
		boards = append(boards, b)
	}
	m := moduleMetrics(boards, now)
	require.Equal(t, 300e12, *m.HashrateHS)
	require.Equal(t, 3600.0, *m.PowerW)
	require.Equal(t, 65.0, *m.TemperatureC)
	require.Equal(t, now.Add(-10*time.Millisecond), m.ObservedAt)

	boards[0].Health.Hashrate = nil
	require.Nil(t, moduleMetrics(boards, now).HashrateHS)
	for i := range boards {
		boards[i].Health.PSUElectrical = nil
	}
	require.Nil(t, moduleMetrics(boards, now).PowerW, "board power is not module input power")
	boards[1].Health.Timestamp = now.Add(-3 * time.Minute)
	missing := moduleMetrics(boards, now)
	require.NotNil(t, missing)
	require.Equal(t, boards[1].Health.Timestamp, missing.ObservedAt)
	require.Nil(t, missing.HashrateHS)
	require.Nil(t, missing.PowerW)
	require.Nil(t, missing.TemperatureC)
}

func TestEmptyModuleMetricsPreserveSourceOrdering(t *testing.T) {
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	value := 100.0
	validAt := now.Add(-5 * time.Second)
	validBoards := make([]Board, boardsPerModule)
	for i := range validBoards {
		validBoards[i].Health = &Health{Timestamp: validAt, Hashrate: &value}
	}
	valid := moduleMetrics(validBoards, now)
	require.NotNil(t, valid.HashrateHS)
	require.Equal(t, validAt, valid.ObservedAt)

	for _, age := range []time.Duration{30 * time.Second, 3 * time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			source := now.Add(-age)
			empty := moduleMetrics([]Board{
				{Health: &Health{Timestamp: source.Add(time.Second)}},
				{Health: &Health{Timestamp: source}},
				{Health: &Health{Timestamp: source.Add(2 * time.Second)}},
			}, now)
			require.NotNil(t, empty)
			require.Nil(t, empty.HashrateHS)
			require.Nil(t, empty.PowerW)
			require.Nil(t, empty.TemperatureC)
			require.Equal(t, source, empty.ObservedAt)
			require.True(t, empty.ObservedAt.Before(valid.ObservedAt), "delayed empty evidence must not supersede valid metrics")
		})
	}
}

func TestEmptyModuleMetricsWithoutSourceTimestampUseCollectionTime(t *testing.T) {
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	for _, boards := range [][]Board{nil, {{}}, {{Health: &Health{}}}} {
		empty := moduleMetrics(boards, now)
		require.NotNil(t, empty)
		require.Equal(t, now, empty.ObservedAt)
		require.Nil(t, empty.HashrateHS)
		require.Nil(t, empty.PowerW)
		require.Nil(t, empty.TemperatureC)
	}
}
