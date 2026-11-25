package main

import (
	"testing"
	"time"

	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	models "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/stretchr/testify/require"
)

func TestTelemetryFreshnessAgePreservesCachedAndUnknownObservations(t *testing.T) {
	for _, offset := range []time.Duration{-10 * time.Minute, 0, time.Hour} {
		observed := time.Now().Add(offset)
		metrics := models.DeviceMetrics{Timestamp: observed, HashrateHS: &models.MetricValue{Value: 0}}
		result, err := telemetryResultFromV2("miner", metrics, telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE)
		require.NoError(t, err)
		if offset > 0 {
			require.Nil(t, result.TelemetryObservationAge)
		} else {
			require.NotNil(t, result.TelemetryObservationAge)
			require.GreaterOrEqual(t, result.TelemetryObservationAge.AsDuration(), -offset)
		}
	}
	result, err := telemetryResultFromV2("miner", models.DeviceMetrics{HashrateHS: &models.MetricValue{Value: 1}}, telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE)
	require.NoError(t, err)
	require.NotNil(t, result.Timestamp, "legacy wire timestamp remains valid")
	require.Nil(t, result.TelemetryObservationAge, "fallback must not create freshness evidence")
}
