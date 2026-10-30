package timescaledb

import (
	"os"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestStoredContainerHistorySeparatesMetricDefinitions(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	// Put legacy values in the same chart bucket; neither coolant temperature nor
	// legacy fallback power/share-derived hashrate may affect the new series.
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,efficiency_jh,temp_c,metric_semantics) VALUES ($1,$2,900e12,90000,100e-12,20,1)`, r.base.Add(-time.Second), r.deviceID)
	require.NoError(t, err)
	start, end := r.base.Add(-time.Minute), r.base.Add(time.Minute)
	result, err := r.telemetry.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{DeviceIDs: []models.DeviceIdentifier{models.DeviceIdentifier(r.deviceID)}, TimeRange: models.TimeRange{StartTime: &start, EndTime: &end}, SlideInterval: new(time.Minute)})
	require.NoError(t, err)
	want := map[models.MeasurementType]float64{models.MeasurementTypeHashrate: 300e12, models.MeasurementTypePower: 4000, models.MeasurementTypeTemperature: 60, models.MeasurementTypeEfficiency: 4000 / 300e12}
	require.Len(t, result.Metrics, 4)
	for _, metric := range result.Metrics {
		require.Equal(t, int32(2), *metric.MetricSemantics)
		require.Len(t, metric.AggregatedValues, 1)
		require.InDelta(t, want[metric.MeasurementType], metric.AggregatedValues[0].Value, 1e-20)
	}
}

func TestStoredContainerRollupsMigrationRollback(t *testing.T) {
	r := newContainerRollupRig(t)
	testutil.RestoreLegacySummaryBootstraps(t, r.db,
		"001016_internal_ct1_metric_summaries",
		"001021_internal_hashboard_metric_summaries")
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	// Exercise the destructive rollback only in this disposable test database.
	_, err := r.db.ExecContext(t.Context(), "TRUNCATE hashboard_metrics")
	require.NoError(t, err)
	for _, migrationName := range []string{
		"001049_internal_hashboard_allocation_order.down",
		"001048_internal_automatic_hashboard_allocation.down",
		"001047_internal_hashboard_allocations.down",
		"001027_internal_container_telemetry_compression.down",
		"001022_internal_topology_compaction.down",
		"001021_internal_hashboard_metric_summaries.down",
		"001016_internal_ct1_metric_summaries.down",
		"001015_internal_ct1_poll_rollups.down",
		"001015_internal_ct1_poll_rollups.up",
		"001016_internal_ct1_metric_summaries.up",
		"001021_internal_hashboard_metric_summaries.up",
		"001022_internal_topology_compaction.up",
		"001027_internal_container_telemetry_compression.up",
		"001047_internal_hashboard_allocations.up",
		"001048_internal_automatic_hashboard_allocation.up",
		"001049_internal_hashboard_allocation_order.up",
	} {
		migration, err := os.ReadFile("../../../migrations/current/" + migrationName + ".sql")
		require.NoError(t, err)
		_, err = r.db.ExecContext(t.Context(), string(migration))
		require.NoError(t, err, migrationName)
		if migrationName == "001015_internal_ct1_poll_rollups.down" {
			_, err = r.db.ExecContext(t.Context(), "SELECT compress_chunk(chunk, if_not_compressed=>true) FROM show_chunks('device_metrics') chunk")
			require.NoError(t, err, "reapply with legacy compressed history")
		}
	}
}
