package timescaledb

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
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
	result, devCount := calculateCumulativeAggregations(data, models.MeasurementTypeHashrate, aggTypes)

	assert.Len(t, result, 2)
	assert.Equal(t, 3, devCount, "Should count 3 unique devices")

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
	result, devCount := calculateCumulativeAggregations(data, models.MeasurementTypeHashrate, aggTypes)

	assert.Len(t, result, 1)
	assert.Equal(t, 2, devCount, "Should count 2 unique devices")
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

func TestAggregateHourlyBucket_WeightedAverage(t *testing.T) {
	// Device A: 360 data points, avg temp 70°C (full hour of reporting)
	// Device B: 10 data points, avg temp 90°C (sparse reporting)
	// Unweighted: (70 + 90) / 2 = 80
	// Weighted: (70*360 + 90*10) / (360+10) = 26100/370 ≈ 70.54
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgTemp:          70.0,
			MaxTemp:          sql.NullFloat64{Float64: 75.0, Valid: true},
			MinTemp:          sql.NullFloat64{Float64: 65.0, Valid: true},
			DataPoints:       360,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgTemp:          90.0,
			MaxTemp:          sql.NullFloat64{Float64: 95.0, Valid: true},
			MinTemp:          sql.NullFloat64{Float64: 85.0, Valid: true},
			DataPoints:       10,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeAverage}
	result, devCount := store.aggregateHourlyBucket(rows, models.MeasurementTypeTemperature, aggTypes)

	assert.Len(t, result, 1)
	assert.Equal(t, 2, devCount, "Should count 2 devices with temperature data")
	expected := (70.0*360 + 90.0*10) / (360 + 10) // ≈ 70.54
	assert.InDelta(t, expected, result[0].Value, 0.01,
		"Non-cumulative average should be weighted by data points")
}

func TestAggregateHourlyBucket_CumulativeUnweighted(t *testing.T) {
	// Cumulative metrics (power) should sum per-device averages for fleet total,
	// regardless of data point counts.
	// Device A: 360 points, avg power 1500W
	// Device B: 10 points, avg power 500W
	// Fleet total: 1500 + 500 = 2000W (not weighted)
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgPower:         1500.0,
			DataPoints:       360,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgPower:         500.0,
			DataPoints:       10,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeAverage}
	result, devCount := store.aggregateHourlyBucket(rows, models.MeasurementTypePower, aggTypes)

	assert.Len(t, result, 1)
	assert.Equal(t, 2, devCount, "Should count 2 devices with power data")
	assert.Equal(t, 2000.0, result[0].Value,
		"Cumulative average should be fleet total (sum of per-device averages)")
}

func TestAggregateDailyBucket_WeightedAverage(t *testing.T) {
	// Same weighting logic applies to daily buckets.
	// Device A: 8640 points (full day), avg efficiency 30 J/TH
	// Device B: 4320 points (half day), avg efficiency 40 J/TH
	// Weighted: (30*8640 + 40*4320) / (8640+4320) = 432000/12960 ≈ 33.33
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsDaily{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgEfficiency:    30.0,
			DataPoints:       8640,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgEfficiency:    40.0,
			DataPoints:       4320,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeAverage}
	result, devCount := store.aggregateDailyBucket(rows, models.MeasurementTypeEfficiency, aggTypes)

	assert.Len(t, result, 1)
	assert.Equal(t, 2, devCount, "Should count 2 devices with efficiency data")
	expected := (30.0*8640 + 40.0*4320) / (8640 + 4320) // ≈ 33.33
	assert.InDelta(t, expected, result[0].Value, 0.01,
		"Non-cumulative daily average should be weighted by data points")
}

func TestAggregateHourlyBucket_SingleDevice(t *testing.T) {
	// With a single device, weighted and unweighted produce the same result.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgTemp:          72.5,
			MaxTemp:          sql.NullFloat64{Float64: 75.0, Valid: true},
			MinTemp:          sql.NullFloat64{Float64: 70.0, Valid: true},
			DataPoints:       360,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeAverage}
	result, devCount := store.aggregateHourlyBucket(rows, models.MeasurementTypeTemperature, aggTypes)

	assert.Len(t, result, 1)
	assert.Equal(t, 1, devCount, "Should count 1 device with temperature data")
	assert.Equal(t, 72.5, result[0].Value,
		"Single device average should equal device average regardless of weighting")
}

func TestAggregateHourlyBucket_TemperatureMinMax_GlobalExtrema(t *testing.T) {
	// Non-cumulative: fleet MIN is the coldest reading any device produced,
	// MAX is the hottest — global extrema across devices, not a sum.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgTemp:          70.0,
			MinTemp:          sql.NullFloat64{Float64: 65.0, Valid: true},
			MaxTemp:          sql.NullFloat64{Float64: 75.0, Valid: true},
			DataPoints:       360,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgTemp:          80.0,
			MinTemp:          sql.NullFloat64{Float64: 72.0, Valid: true},
			MaxTemp:          sql.NullFloat64{Float64: 88.0, Valid: true},
			DataPoints:       360,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeMin, models.AggregationTypeMax}
	result, devCount := store.aggregateHourlyBucket(rows, models.MeasurementTypeTemperature, aggTypes)

	assert.Equal(t, 2, devCount)
	values := aggValues(result)
	assert.Equal(t, 65.0, values[models.AggregationTypeMin], "MIN should be coldest single reading")
	assert.Equal(t, 88.0, values[models.AggregationTypeMax], "MAX should be hottest single reading")
}

func TestAggregateHourlyBucket_HashrateMinMax_FleetTotals(t *testing.T) {
	// Cumulative: fleet MIN/MAX are sums of per-device mins/maxes, matching
	// the raw-data path in calculateCumulativeAggregations. A per-device
	// extremum approach would under-report the fleet trough/spike.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgHashRate:      110.0,
			MinHashRate:      sql.NullFloat64{Float64: 100.0, Valid: true},
			MaxHashRate:      sql.NullFloat64{Float64: 120.0, Valid: true},
			DataPoints:       360,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgHashRate:      210.0,
			MinHashRate:      sql.NullFloat64{Float64: 200.0, Valid: true},
			MaxHashRate:      sql.NullFloat64{Float64: 220.0, Valid: true},
			DataPoints:       360,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeMin, models.AggregationTypeMax}
	result, devCount := store.aggregateHourlyBucket(rows, models.MeasurementTypeHashrate, aggTypes)

	assert.Equal(t, 2, devCount)
	values := aggValues(result)
	assert.Equal(t, 300.0, values[models.AggregationTypeMin],
		"Cumulative MIN should sum per-device mins (100 + 200)")
	assert.Equal(t, 340.0, values[models.AggregationTypeMax],
		"Cumulative MAX should sum per-device maxes (120 + 220)")
}

func TestAggregateHourlyBucket_PowerMinMax_Omitted(t *testing.T) {
	// The power continuous aggregate view does not materialize min/max columns.
	// Emitting MIN/MAX would mean fabricating them from avg, which this function
	// must refuse to do.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{Bucket: now, DeviceIdentifier: "device-a", AvgPower: 1500.0, DataPoints: 360},
		{Bucket: now, DeviceIdentifier: "device-b", AvgPower: 1800.0, DataPoints: 360},
	}

	aggTypes := []models.AggregationType{
		models.AggregationTypeAverage,
		models.AggregationTypeMin,
		models.AggregationTypeMax,
	}
	result, devCount := store.aggregateHourlyBucket(rows, models.MeasurementTypePower, aggTypes)

	assert.Equal(t, 2, devCount)
	values := aggValues(result)
	assert.Contains(t, values, models.AggregationTypeAverage, "AVG must still be emitted")
	assert.NotContains(t, values, models.AggregationTypeMin,
		"MIN must not be emitted — backing view lacks min column")
	assert.NotContains(t, values, models.AggregationTypeMax,
		"MAX must not be emitted — backing view lacks max column")
}

func TestAggregateHourlyBucket_EfficiencyMinMax_Omitted(t *testing.T) {
	// Same guarantee as power — efficiency view has no min/max columns either.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{Bucket: now, DeviceIdentifier: "device-a", AvgEfficiency: 30.0, DataPoints: 360},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeMin, models.AggregationTypeMax}
	result, _ := store.aggregateHourlyBucket(rows, models.MeasurementTypeEfficiency, aggTypes)
	assert.Empty(t, result, "Efficiency MIN/MAX must not be emitted at rollup level")
}

func TestAggregateHourlyBucket_TemperatureMinMax_PartialRealMinMax_Omitted(t *testing.T) {
	// If some device rows have NULL min/max columns, emitting an aggregate MIN/MAX
	// would bias the result. Skip emission rather than report a partial answer.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsHourly{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgTemp:          70.0,
			MinTemp:          sql.NullFloat64{Float64: 65.0, Valid: true},
			MaxTemp:          sql.NullFloat64{Float64: 75.0, Valid: true},
			DataPoints:       360,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgTemp:          80.0, // real min/max missing for this row
			DataPoints:       360,
		},
	}

	aggTypes := []models.AggregationType{
		models.AggregationTypeAverage,
		models.AggregationTypeMin,
		models.AggregationTypeMax,
	}
	result, devCount := store.aggregateHourlyBucket(rows, models.MeasurementTypeTemperature, aggTypes)

	assert.Equal(t, 2, devCount, "Both devices still contribute to AVG")
	values := aggValues(result)
	assert.Contains(t, values, models.AggregationTypeAverage)
	assert.NotContains(t, values, models.AggregationTypeMin,
		"MIN must not be emitted when any contributing row lacks real min")
	assert.NotContains(t, values, models.AggregationTypeMax,
		"MAX must not be emitted when any contributing row lacks real max")
}

func TestAggregateDailyBucket_TemperatureMinMax_GlobalExtrema(t *testing.T) {
	// Daily rollup mirrors the hourly non-cumulative semantics.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsDaily{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgTemp:          70.0,
			MinTemp:          sql.NullFloat64{Float64: 60.0, Valid: true},
			MaxTemp:          sql.NullFloat64{Float64: 80.0, Valid: true},
			DataPoints:       8640,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgTemp:          75.0,
			MinTemp:          sql.NullFloat64{Float64: 68.0, Valid: true},
			MaxTemp:          sql.NullFloat64{Float64: 90.0, Valid: true},
			DataPoints:       8640,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeMin, models.AggregationTypeMax}
	result, devCount := store.aggregateDailyBucket(rows, models.MeasurementTypeTemperature, aggTypes)

	assert.Equal(t, 2, devCount)
	values := aggValues(result)
	assert.Equal(t, 60.0, values[models.AggregationTypeMin])
	assert.Equal(t, 90.0, values[models.AggregationTypeMax])
}

func TestAggregateDailyBucket_HashrateMinMax_FleetTotals(t *testing.T) {
	// Daily rollup mirrors the hourly cumulative semantics.
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsDaily{
		{
			Bucket:           now,
			DeviceIdentifier: "device-a",
			AvgHashRate:      110.0,
			MinHashRate:      sql.NullFloat64{Float64: 90.0, Valid: true},
			MaxHashRate:      sql.NullFloat64{Float64: 130.0, Valid: true},
			DataPoints:       8640,
		},
		{
			Bucket:           now,
			DeviceIdentifier: "device-b",
			AvgHashRate:      210.0,
			MinHashRate:      sql.NullFloat64{Float64: 190.0, Valid: true},
			MaxHashRate:      sql.NullFloat64{Float64: 230.0, Valid: true},
			DataPoints:       8640,
		},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeMin, models.AggregationTypeMax}
	result, _ := store.aggregateDailyBucket(rows, models.MeasurementTypeHashrate, aggTypes)

	values := aggValues(result)
	assert.Equal(t, 280.0, values[models.AggregationTypeMin],
		"Daily cumulative MIN should sum per-device mins (90 + 190)")
	assert.Equal(t, 360.0, values[models.AggregationTypeMax],
		"Daily cumulative MAX should sum per-device maxes (130 + 230)")
}

func TestAggregateDailyBucket_PowerMinMax_Omitted(t *testing.T) {
	now := time.Now()
	store := &TimescaleTelemetryStore{}

	rows := []sqlc.DeviceMetricsDaily{
		{Bucket: now, DeviceIdentifier: "device-a", AvgPower: 1500.0, DataPoints: 8640},
	}

	aggTypes := []models.AggregationType{models.AggregationTypeMin, models.AggregationTypeMax}
	result, _ := store.aggregateDailyBucket(rows, models.MeasurementTypePower, aggTypes)
	assert.Empty(t, result, "Daily power MIN/MAX must not be emitted")
}

// aggValues builds a lookup from an AggregatedValue slice keyed by aggregation type.
// Used in MIN/MAX tests so assertions are independent of result ordering.
func aggValues(result []models.AggregatedValue) map[models.AggregationType]float64 {
	out := make(map[models.AggregationType]float64, len(result))
	for _, v := range result {
		out[v.Type] = v.Value
	}
	return out
}

func TestEstimateEnergyKWh(t *testing.T) {
	tests := []struct {
		name       string
		avgPowerW  float64
		dataPoints int64
		expected   float64
	}{
		{
			name:       "full day at 1500W",
			avgPowerW:  1500.0,
			dataPoints: 8640, // 24h * 360 points/hour
			expected:   36.0, // 1500W * 24h / 1000
		},
		{
			name:       "half day at 1500W",
			avgPowerW:  1500.0,
			dataPoints: 4320, // 12h * 360 points/hour
			expected:   18.0, // 1500W * 12h / 1000
		},
		{
			name:       "one hour at 3000W",
			avgPowerW:  3000.0,
			dataPoints: 360, // 1h * 360 points/hour
			expected:   3.0, // 3000W * 1h / 1000
		},
		{
			name:       "zero data points",
			avgPowerW:  1500.0,
			dataPoints: 0,
			expected:   0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimateEnergyKWh(tt.avgPowerW, tt.dataPoints)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

// TestCalculateTemperatureStatusCount_DedupesPerDevice verifies that buckets
// containing many samples per device (raw ~10s polling) count each device
// once, not once per sample. Regression for issue #87.
func TestCalculateTemperatureStatusCount_DedupesPerDevice(t *testing.T) {
	now := time.Now()
	bucket := now.Truncate(time.Minute)

	// Two miners, each reporting many samples in the same bucket.
	// Miner A: stable at 50°C (Ok). Miner B: stable at 85°C (Hot).
	// Without dedup the per-sample counter returns 6 Ok + 6 Hot = 12 entries.
	data := []modelsV2.DeviceMetrics{
		{DeviceIdentifier: "miner-a", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 50}},
		{DeviceIdentifier: "miner-a", Timestamp: now.Add(10 * time.Second), TempC: &modelsV2.MetricValue{Value: 50}},
		{DeviceIdentifier: "miner-a", Timestamp: now.Add(20 * time.Second), TempC: &modelsV2.MetricValue{Value: 50}},
		{DeviceIdentifier: "miner-b", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 85}},
		{DeviceIdentifier: "miner-b", Timestamp: now.Add(10 * time.Second), TempC: &modelsV2.MetricValue{Value: 85}},
		{DeviceIdentifier: "miner-b", Timestamp: now.Add(20 * time.Second), TempC: &modelsV2.MetricValue{Value: 85}},
	}

	result := calculateTemperatureStatusCount(data, bucket)

	assert.Equal(t, int32(0), result.ColdCount)
	assert.Equal(t, int32(1), result.OkCount, "miner-a should count once")
	assert.Equal(t, int32(1), result.HotCount, "miner-b should count once")
	assert.Equal(t, int32(0), result.CriticalCount)
	assert.Equal(t, bucket, result.Timestamp)
}

// TestCalculateTemperatureStatusCount_UsesLatestSamplePerDevice verifies that
// when a device crosses a threshold within a bucket, the latest sample wins
// (represents the device's state at end of bucket).
func TestCalculateTemperatureStatusCount_UsesLatestSamplePerDevice(t *testing.T) {
	now := time.Now()
	bucket := now.Truncate(time.Minute)

	// miner-a starts Ok (50°C), warms into Critical (95°C) by end.
	data := []modelsV2.DeviceMetrics{
		{DeviceIdentifier: "miner-a", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 50}},
		{DeviceIdentifier: "miner-a", Timestamp: now.Add(30 * time.Second), TempC: &modelsV2.MetricValue{Value: 75}},
		{DeviceIdentifier: "miner-a", Timestamp: now.Add(50 * time.Second), TempC: &modelsV2.MetricValue{Value: 95}},
	}

	result := calculateTemperatureStatusCount(data, bucket)

	assert.Equal(t, int32(0), result.OkCount)
	assert.Equal(t, int32(0), result.HotCount)
	assert.Equal(t, int32(1), result.CriticalCount, "latest sample (95°C) wins")
}

// TestCalculateTemperatureStatusCount_AllThresholds covers the 4 buckets.
func TestCalculateTemperatureStatusCount_AllThresholds(t *testing.T) {
	now := time.Now()
	data := []modelsV2.DeviceMetrics{
		{DeviceIdentifier: "cold-1", Timestamp: now, TempC: &modelsV2.MetricValue{Value: -5}},
		{DeviceIdentifier: "ok-1", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 25}},
		{DeviceIdentifier: "ok-2", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 69.9}},
		{DeviceIdentifier: "hot-1", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 70}},
		{DeviceIdentifier: "hot-2", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 89.9}},
		{DeviceIdentifier: "crit-1", Timestamp: now, TempC: &modelsV2.MetricValue{Value: 90}},
		{DeviceIdentifier: "no-temp", Timestamp: now}, // missing TempC: ignored
	}

	result := calculateTemperatureStatusCount(data, now)

	assert.Equal(t, int32(1), result.ColdCount)
	assert.Equal(t, int32(2), result.OkCount)
	assert.Equal(t, int32(2), result.HotCount)
	assert.Equal(t, int32(1), result.CriticalCount)
}

// TestCalculateUptimeStatusCount_DedupesPerDevice mirrors the temperature
// regression — uptime status was overcounted the same way. Regression for
// issue #87.
func TestCalculateUptimeStatusCount_DedupesPerDevice(t *testing.T) {
	now := time.Now()
	bucket := now.Truncate(time.Minute)

	data := []modelsV2.DeviceMetrics{
		{DeviceIdentifier: "hashing-1", Timestamp: now, Health: modelsV2.HealthHealthyActive},
		{DeviceIdentifier: "hashing-1", Timestamp: now.Add(10 * time.Second), Health: modelsV2.HealthHealthyActive},
		{DeviceIdentifier: "hashing-1", Timestamp: now.Add(20 * time.Second), Health: modelsV2.HealthHealthyActive},
		{DeviceIdentifier: "down-1", Timestamp: now, Health: modelsV2.HealthWarning},
		{DeviceIdentifier: "down-1", Timestamp: now.Add(10 * time.Second), Health: modelsV2.HealthWarning},
	}

	result := calculateUptimeStatusCount(data, bucket)

	assert.Equal(t, int32(1), result.HashingCount)
	assert.Equal(t, int32(1), result.NotHashingCount)
}

// TestCalculateUptimeStatusCount_LatestHealthWins confirms a device that
// recovers within the bucket is counted as hashing (latest sample wins).
func TestCalculateUptimeStatusCount_LatestHealthWins(t *testing.T) {
	now := time.Now()
	data := []modelsV2.DeviceMetrics{
		{DeviceIdentifier: "miner-a", Timestamp: now, Health: modelsV2.HealthWarning},
		{DeviceIdentifier: "miner-a", Timestamp: now.Add(30 * time.Second), Health: modelsV2.HealthHealthyActive},
	}

	result := calculateUptimeStatusCount(data, now)

	assert.Equal(t, int32(1), result.HashingCount)
	assert.Equal(t, int32(0), result.NotHashingCount)
}

func TestLatestSamplePerDevice_Empty(t *testing.T) {
	assert.Nil(t, latestSamplePerDevice(nil))
	assert.Nil(t, latestSamplePerDevice([]modelsV2.DeviceMetrics{}))
}
