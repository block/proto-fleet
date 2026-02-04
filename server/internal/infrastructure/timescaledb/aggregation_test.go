package timescaledb

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/stretchr/testify/assert"
)

func TestIsCumulativeMetric(t *testing.T) {
	tests := []struct {
		name            string
		measurementType models.MeasurementType
		expected        bool
	}{
		{"hashrate is cumulative", models.MeasurementTypeHashrate, true},
		{"power is cumulative", models.MeasurementTypePower, true},
		{"current is cumulative", models.MeasurementTypeCurrent, true},
		{"temperature is NOT cumulative", models.MeasurementTypeTemperature, false},
		{"efficiency is NOT cumulative", models.MeasurementTypeEfficiency, false},
		{"fan speed is NOT cumulative", models.MeasurementTypeFanSpeed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCumulativeMetric(tt.measurementType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateCumulativeAggregations_FleetTotals(t *testing.T) {
	// Simulate 3 devices with hashrate data
	// Device1: 100 TH/s, Device2: 150 TH/s, Device3: 200 TH/s
	// Expected fleet total: 450 TH/s (not 150 TH/s average)
	now := time.Now()

	data := []modelsV2.DeviceMetrics{
		{
			DeviceIdentifier: "device-1",
			Timestamp:        now,
			HashrateHS:       &modelsV2.MetricValue{Value: 100_000_000_000_000}, // 100 TH/s
		},
		{
			DeviceIdentifier: "device-2",
			Timestamp:        now,
			HashrateHS:       &modelsV2.MetricValue{Value: 150_000_000_000_000}, // 150 TH/s
		},
		{
			DeviceIdentifier: "device-3",
			Timestamp:        now,
			HashrateHS:       &modelsV2.MetricValue{Value: 200_000_000_000_000}, // 200 TH/s
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeAverage, models.AggregationTypeSum}
	result := calculateCumulativeAggregations(data, models.MeasurementTypeHashrate, aggTypes)

	assert.Len(t, result, 2)

	// For cumulative metrics, "Average" should be the SUM of per-device averages (fleet total)
	var avgValue, sumValue float64
	for _, agg := range result {
		if agg.Type == models.AggregationTypeAverage {
			avgValue = agg.Value
		} else if agg.Type == models.AggregationTypeSum {
			sumValue = agg.Value
		}
	}

	expectedTotal := 450_000_000_000_000.0 // 450 TH/s = 100 + 150 + 200
	assert.Equal(t, expectedTotal, avgValue, "Average should be fleet total (sum of device values)")
	assert.Equal(t, expectedTotal, sumValue, "Sum should be fleet total")
}

func TestCalculateCumulativeAggregations_MultipleDataPointsPerDevice(t *testing.T) {
	// Simulate a device with multiple readings in the same bucket
	// Device1: [100, 200] -> avg=150
	// Device2: [300, 400] -> avg=350
	// Expected fleet average: 150 + 350 = 500 (sum of per-device averages)
	now := time.Now()

	data := []modelsV2.DeviceMetrics{
		{DeviceIdentifier: "device-1", Timestamp: now, HashrateHS: &modelsV2.MetricValue{Value: 100}},
		{DeviceIdentifier: "device-1", Timestamp: now.Add(time.Second), HashrateHS: &modelsV2.MetricValue{Value: 200}},
		{DeviceIdentifier: "device-2", Timestamp: now, HashrateHS: &modelsV2.MetricValue{Value: 300}},
		{DeviceIdentifier: "device-2", Timestamp: now.Add(time.Second), HashrateHS: &modelsV2.MetricValue{Value: 400}},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeAverage}
	result := calculateCumulativeAggregations(data, models.MeasurementTypeHashrate, aggTypes)

	assert.Len(t, result, 1)
	// Device1 avg = 150, Device2 avg = 350, Total = 500
	assert.Equal(t, 500.0, result[0].Value, "Should sum per-device averages")
}

func TestCalculateAggregation_NonCumulative(t *testing.T) {
	// For non-cumulative metrics like temperature, we want actual average
	values := []float64{70.0, 72.0, 74.0, 76.0}

	avg := calculateAggregation(values, models.AggregationTypeAverage)
	assert.Equal(t, 73.0, avg, "Temperature should be averaged normally")

	sum := calculateAggregation(values, models.AggregationTypeSum)
	assert.Equal(t, 292.0, sum, "Sum should add all values")

	minVal := calculateAggregation(values, models.AggregationTypeMin)
	assert.Equal(t, 70.0, minVal, "Min should find minimum")

	maxVal := calculateAggregation(values, models.AggregationTypeMax)
	assert.Equal(t, 76.0, maxVal, "Max should find maximum")
}

func TestAggregateMetrics_CumulativeVsNonCumulative(t *testing.T) {
	// Test that aggregateMetrics handles cumulative and non-cumulative differently
	now := time.Now()

	// 3 devices: hashrate and temperature
	data := []modelsV2.DeviceMetrics{
		{
			DeviceIdentifier: "device-1",
			Timestamp:        now,
			HashrateHS:       &modelsV2.MetricValue{Value: 100}, // cumulative
			TempC:            &modelsV2.MetricValue{Value: 70},  // non-cumulative
		},
		{
			DeviceIdentifier: "device-2",
			Timestamp:        now,
			HashrateHS:       &modelsV2.MetricValue{Value: 150},
			TempC:            &modelsV2.MetricValue{Value: 72},
		},
		{
			DeviceIdentifier: "device-3",
			Timestamp:        now,
			HashrateHS:       &modelsV2.MetricValue{Value: 200},
			TempC:            &modelsV2.MetricValue{Value: 74},
		},
	}

	store := &TimescaleTelemetryStore{}
	measurementTypes := []models.MeasurementType{
		models.MeasurementTypeHashrate,
		models.MeasurementTypeTemperature,
	}
	aggTypes := []models.AggregationType{models.AggregationTypeAverage}

	result := store.aggregateMetrics(data, measurementTypes, aggTypes, 10*time.Second)

	// Find hashrate and temperature metrics
	var hashrateAvg, tempAvg float64
	for _, m := range result.Metrics {
		if len(m.AggregatedValues) > 0 {
			if m.MeasurementType == models.MeasurementTypeHashrate {
				hashrateAvg = m.AggregatedValues[0].Value
			} else if m.MeasurementType == models.MeasurementTypeTemperature {
				tempAvg = m.AggregatedValues[0].Value
			}
		}
	}

	// Hashrate should be SUM (fleet total): 100 + 150 + 200 = 450
	assert.Equal(t, 450.0, hashrateAvg, "Hashrate average should be fleet total (sum)")

	// Temperature should be actual average: (70 + 72 + 74) / 3 = 72
	assert.Equal(t, 72.0, tempAvg, "Temperature average should be mathematical average")
}
