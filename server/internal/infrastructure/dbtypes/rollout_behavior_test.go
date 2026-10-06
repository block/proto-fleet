package dbtypes

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRolloutBehaviorSnapshotRoundTrip(t *testing.T) {
	zero, drop, coverage, errors := 0.0, 15.0, 90.0, int32(0)
	want := RolloutBehaviorSnapshot{
		Method: "batched", OrderBy: "random", BatchSize: 5,
		ReviewAfterEachBatch: true, AutoContinue: true, StabilizationSeconds: 600,
		MaxHashrateDropPercent: &drop, MaxTempIncreaseC: &zero,
		MaxNewErrors: &errors, MinSampleCoveragePercent: &coverage,
		MaxConcurrentOffline: 2,
	}
	value, err := want.Value()
	require.NoError(t, err)
	encoded, ok := value.(string)
	require.True(t, ok, "database value must be JSON text")
	for _, input := range []any{value, []byte(encoded)} {
		var got RolloutBehaviorSnapshot
		require.NoError(t, got.Scan(input))
		require.Equal(t, want, got)
		require.Nil(t, got.MaxEfficiencyIncreasePercent)
		require.NotNil(t, got.MaxTempIncreaseC, "an explicit zero limit must survive")
	}
	var defaults RolloutBehaviorSnapshot
	require.NoError(t, defaults.Scan(`{"method":"all_at_once","order_by":"least_efficient_first"}`))
	require.Equal(t, "all_at_once", defaults.Method)
	require.Zero(t, defaults.BatchSize)
}

func TestRolloutBehaviorSnapshotRejectsMalformedData(t *testing.T) {
	for name, input := range map[string]any{
		"sql null":           nil,
		"json null":          `null`,
		"array":              `[]`,
		"missing identity":   `{}`,
		"unknown method":     `{"method":"canary","order_by":"random"}`,
		"unknown order":      `{"method":"batched","order_by":"alphabetical"}`,
		"unknown field":      `{"method":"batched","order_by":"random","batch_szie":5}`,
		"case variant":       `{"method":"batched","order_by":"random","BATCH_SIZE":5}`,
		"conflicting alias":  `{"method":"batched","METHOD":"all_at_once","order_by":"random"}`,
		"null setting":       `{"method":"batched","order_by":"random","batch_size":null}`,
		"wrong setting type": `{"method":"batched","order_by":"random","batch_size":"5"}`,
		"negative setting":   `{"method":"batched","order_by":"random","batch_size":-1}`,
		"overflow":           `{"method":"batched","order_by":"random","batch_size":2147483648}`,
		"invalid coverage":   `{"method":"batched","order_by":"random","min_sample_coverage_percent":0}`,
		"trailing object":    `{"method":"batched","order_by":"random"}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := RolloutBehaviorSnapshot{Method: "all_at_once", OrderBy: "random"}
			before := got
			require.Error(t, got.Scan(input))
			require.Equal(t, before, got, "a failed scan must not leave a partial snapshot")
		})
	}
}

func TestRolloutBehaviorSnapshotRejectsInvalidWrites(t *testing.T) {
	for _, limit := range []float64{math.NaN(), math.Inf(1), -1, 101} {
		snapshot := RolloutBehaviorSnapshot{
			Method: "batched", OrderBy: "random", MaxHashrateDropPercent: &limit,
		}
		_, err := snapshot.Value()
		require.Error(t, err)
	}
	_, err := (RolloutBehaviorSnapshot{}).Value()
	require.Error(t, err)
}
