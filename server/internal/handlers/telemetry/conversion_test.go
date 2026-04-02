package telemetry

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// TestConvertMetricsToProto_NaNFromDatabase verifies that NaN values originating from
// poisoned CAGG buckets (e.g., AVG() over a set containing NaN) are sanitized to zero
// before reaching the protobuf response. This is the primary defense-in-depth scenario:
// a single NaN stored in device_metrics poisons the continuous aggregate via AVG().
func TestConvertMetricsToProto_NaNFromDatabase(t *testing.T) {
	// Arrange
	nanMetrics := []models.Metric{
		{
			MeasurementType: models.MeasurementTypeHashrate,
			OpenTime:        time.Now(),
			AggregatedValues: []models.AggregatedValue{
				{Type: models.AggregationTypeAverage, Value: math.NaN()},
				{Type: models.AggregationTypeSum, Value: math.Inf(1)},
			},
			DeviceCount: 5,
		},
		{
			MeasurementType: models.MeasurementTypeTemperature,
			OpenTime:        time.Now(),
			AggregatedValues: []models.AggregatedValue{
				{Type: models.AggregationTypeAverage, Value: 65.0},
			},
			DeviceCount: 5,
		},
	}

	// Act
	protoMetrics, err := convertMetricsToProto(nanMetrics)

	// Assert
	require.NoError(t, err)
	require.Len(t, protoMetrics, 2)

	for _, aggVal := range protoMetrics[0].AggregatedValues {
		assert.False(t, math.IsNaN(aggVal.Value), "NaN must not appear in protobuf response")
		assert.False(t, math.IsInf(aggVal.Value, 0), "Inf must not appear in protobuf response")
		assert.Equal(t, float64(0), aggVal.Value)
	}

	assert.InDelta(t, 65.0, protoMetrics[1].AggregatedValues[0].Value, 1e-9)
}

// TestConvertMetricsToProto_MixedNaNAndValidValues verifies that NaN in one metric
// doesn't corrupt adjacent valid metrics in the same response.
func TestConvertMetricsToProto_MixedNaNAndValidValues(t *testing.T) {
	// Arrange
	metrics := []models.Metric{
		{
			MeasurementType: models.MeasurementTypePower,
			OpenTime:        time.Now(),
			AggregatedValues: []models.AggregatedValue{
				{Type: models.AggregationTypeAverage, Value: math.NaN()},
			},
			DeviceCount: 3,
		},
		{
			MeasurementType: models.MeasurementTypePower,
			OpenTime:        time.Now().Add(time.Hour),
			AggregatedValues: []models.AggregatedValue{
				{Type: models.AggregationTypeAverage, Value: 3000.0},
			},
			DeviceCount: 3,
		},
	}

	// Act
	protoMetrics, err := convertMetricsToProto(metrics)

	// Assert
	require.NoError(t, err)
	require.Len(t, protoMetrics, 2)

	assert.Equal(t, float64(0), protoMetrics[0].AggregatedValues[0].Value)
	// 3000 W → 3 kW after unit conversion
	assert.InDelta(t, 3.0, protoMetrics[1].AggregatedValues[0].Value, 1e-9)
}
