package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCurtailmentOperationalMetricsPersistValuesAndLabels(t *testing.T) {
	store := NewInMemoryStore()
	provider := SetupWithStore(t.Context(), "test", Config{Enabled: true}, store)
	provider.ObserveTickDuration(125 * time.Millisecond)
	provider.IncTickFailure()
	provider.IncConfirmationPassFailure()
	provider.IncCandidateExcluded("stale_telemetry", 5000)
	provider.IncCandidateExcluded("stale_telemetry", 0)
	provider.IncCandidateExcluded("stale_telemetry", -1)
	provider.IncMaintenanceOverride()
	provider.IncEventStateRaceLoss()
	provider.IncTargetWriteFailure()
	provider.IncAuditWriteFailure("curtailment_facility_fan_command_failed")
	provider.IncAllPairedPendingStall()
	provider.EmitCurtailmentPendingAges(2*time.Minute, 30*time.Second)
	provider.EmitCurtailmentPendingAges(0, 0)
	require.NoError(t, provider.Shutdown(t.Context()))
	rows := store.Snapshot()
	require.Len(t, rows, 13)
	byName := map[string][]Sample{}
	for _, row := range rows {
		byName[row.Metric] = append(byName[row.Metric], row)
		require.Empty(t, row.Labels.OrganizationID)
	}
	require.Equal(t, 0.125, byName[MetricCurtailmentTickDurationSeconds][0].Value)
	require.Equal(t, "stale_telemetry", byName[MetricCurtailmentCandidateExcludedTotal][0].Labels.Kind)
	require.Equal(t, 5000.0, byName[MetricCurtailmentCandidateExcludedTotal][0].Value)
	require.Equal(t, "curtailment_facility_fan_command_failed", byName[MetricCurtailmentAuditWriteFailureTotal][0].Labels.Kind)
	require.Equal(t, 120.0, byName[MetricCurtailmentOldestPendingEventAgeSeconds][0].Value)
	require.Zero(t, byName[MetricCurtailmentOldestPendingEventAgeSeconds][1].Value)
	require.Equal(t, 30.0, byName[MetricCurtailmentOldestPendingMQTTEdgeAgeSeconds][0].Value)
	require.Zero(t, byName[MetricCurtailmentOldestPendingMQTTEdgeAgeSeconds][1].Value)
}
