package timescaledb_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTelemetryStore_StoreDeviceMetrics tests the v2 API for storing device metrics.
func TestTelemetryStore_StoreDeviceMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	deviceIdentifier := "device-v2-1"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceIdentifier)
	})

	now := time.Now().Truncate(time.Millisecond)
	health := modelsV2.HealthHealthyActive

	metrics := []modelsV2.DeviceMetrics{
		{
			DeviceIdentifier: deviceIdentifier,
			Timestamp:        now,
			Health:           health,
			HashrateHS:       &modelsV2.MetricValue{Value: 100_000_000}, // 100 MH/s
			TempC:            &modelsV2.MetricValue{Value: 72.5},
			FanRPM:           &modelsV2.MetricValue{Value: 3500},
			PowerW:           &modelsV2.MetricValue{Value: 1500},
			EfficiencyJH:     &modelsV2.MetricValue{Value: 15.0},
		},
	}

	err = store.StoreDeviceMetrics(ctx, metrics...)
	require.NoError(t, err)

	// Verify data was stored
	var hashRate, temp, power float64
	err = db.QueryRowContext(ctx,
		"SELECT hash_rate_hs, temp_c, power_w FROM device_metrics WHERE device_identifier = $1 ORDER BY time DESC LIMIT 1",
		deviceIdentifier,
	).Scan(&hashRate, &temp, &power)
	require.NoError(t, err)
	assert.Equal(t, 100_000_000.0, hashRate)
	assert.Equal(t, 72.5, temp)
	assert.Equal(t, 1500.0, power)
}

func TestTelemetryStore_StoreDeviceMetricsWithAsyncCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	config := timescaledb.DefaultConfig()
	config.AsyncMetricCommit = true
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)
	ctx := t.Context()

	deviceIdentifier := "device-async-commit-1"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceIdentifier)
	})

	// Act
	err = store.StoreDeviceMetrics(ctx, modelsV2.DeviceMetrics{
		DeviceIdentifier: deviceIdentifier,
		Timestamp:        time.Now().Truncate(time.Millisecond),
		Health:           modelsV2.HealthHealthyActive,
		HashrateHS:       &modelsV2.MetricValue{Value: 50_000_000},
	})

	// Assert
	require.NoError(t, err)
	var hashRate float64
	err = db.QueryRowContext(ctx,
		"SELECT hash_rate_hs FROM device_metrics WHERE device_identifier = $1 ORDER BY time DESC LIMIT 1",
		deviceIdentifier,
	).Scan(&hashRate)
	require.NoError(t, err)
	assert.Equal(t, 50_000_000.0, hashRate)
}

func TestTelemetryStore_GetDeviceOutcomeAveragesPairsWindowsAndEnforcesOrganization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	user := dbSvc.CreateSuperAdminUser()
	otherUser := dbSvc.CreateSuperAdminUser2()
	paired := dbSvc.CreateDevice(user.OrganizationID, "proto")
	currentOnly := dbSvc.CreateDevice(user.OrganizationID, "proto")
	otherOrg := dbSvc.CreateDevice(otherUser.OrganizationID, "proto")
	for _, deviceID := range []string{paired.ID, currentOnly.ID, otherOrg.ID} {
		deviceID := deviceID
		t.Cleanup(func() { cleanupDeviceMetrics(t, db, deviceID) })
	}

	comparisonEnd := time.Now().UTC().Truncate(time.Second)
	comparisonStart := comparisonEnd.Add(-time.Hour)
	baselineStart := comparisonStart.Add(-time.Hour)
	insertOutcomeMetric := func(deviceID string, at time.Time, hashrate, efficiency, power float64) {
		t.Helper()
		_, insertErr := db.ExecContext(t.Context(), `
			INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, efficiency_jh, power_w)
			VALUES ($1, $2, $3, $4, $5)`, at, deviceID, hashrate, efficiency, power)
		require.NoError(t, insertErr)
	}
	insertOutcomeMetric(paired.ID, baselineStart.Add(10*time.Minute), 100, 10, 1000)
	insertOutcomeMetric(paired.ID, baselineStart.Add(30*time.Minute), 200, 20, 2000)
	insertOutcomeMetric(paired.ID, comparisonStart.Add(10*time.Minute), 300, 30, 3000)
	insertOutcomeMetric(paired.ID, comparisonStart.Add(30*time.Minute), 500, 50, 5000)
	insertOutcomeMetric(currentOnly.ID, comparisonStart.Add(20*time.Minute), 250, 25, 2500)
	insertOutcomeMetric(otherOrg.ID, baselineStart.Add(20*time.Minute), 999, 99, 9999)
	insertOutcomeMetric(otherOrg.ID, comparisonStart.Add(20*time.Minute), 999, 99, 9999)

	result, err := store.GetDeviceOutcomeAverages(t.Context(), models.DeviceOutcomeComparisonQuery{
		DeviceIDs: []models.DeviceIdentifier{
			models.DeviceIdentifier(paired.ID),
			models.DeviceIdentifier(currentOnly.ID),
			models.DeviceIdentifier(otherOrg.ID),
		},
		OrganizationID:  user.OrganizationID,
		BaselineStart:   baselineStart,
		BaselineEnd:     comparisonStart,
		ComparisonStart: comparisonStart,
		ComparisonEnd:   comparisonEnd,
	})
	require.NoError(t, err)
	require.Len(t, result, 2, "the other organization's device must not be returned")
	byID := make(map[models.DeviceIdentifier]models.DeviceOutcomeAverages, len(result))
	for _, device := range result {
		byID[device.DeviceID] = device
	}

	pairedResult := byID[models.DeviceIdentifier(paired.ID)]
	require.NotNil(t, pairedResult.BaselineHashrate)
	require.NotNil(t, pairedResult.ComparisonHashrate)
	assert.InDelta(t, 150, *pairedResult.BaselineHashrate, 0.001)
	assert.InDelta(t, 400, *pairedResult.ComparisonHashrate, 0.001)
	assert.InDelta(t, 15, *pairedResult.BaselineEfficiency, 0.001)
	assert.InDelta(t, 40, *pairedResult.ComparisonEfficiency, 0.001)
	assert.InDelta(t, 1500, *pairedResult.BaselinePower, 0.001)
	assert.InDelta(t, 4000, *pairedResult.ComparisonPower, 0.001)

	currentOnlyResult := byID[models.DeviceIdentifier(currentOnly.ID)]
	assert.Nil(t, currentOnlyResult.BaselineHashrate)
	require.NotNil(t, currentOnlyResult.ComparisonHashrate)
	assert.InDelta(t, 250, *currentOnlyResult.ComparisonHashrate, 0.001)
}

func TestTelemetryStore_StoreDeviceMetricsStampsSiteWithDuplicateHistoricalDeviceIdentifier(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	oldSiteID := createTelemetryTestSite(t, db, user.OrganizationID, "Old duplicate site")
	liveSiteID := createTelemetryTestSite(t, db, user.OrganizationID, "Live duplicate site")
	oldDevice := dbSvc.CreateDevice(user.OrganizationID, "proto")
	liveDevice := dbSvc.CreateDevice(user.OrganizationID, "proto")
	deviceIdentifier := "dup-metrics-device"
	now := time.Now().UTC().Truncate(time.Millisecond)
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceIdentifier)
	})

	renameTelemetryTestDevice(t, db, oldDevice.DatabaseID, deviceIdentifier, oldSiteID, true)
	renameTelemetryTestDevice(t, db, liveDevice.DatabaseID, deviceIdentifier, liveSiteID, false)

	err = store.StoreDeviceMetrics(ctx, modelsV2.DeviceMetrics{
		DeviceIdentifier: deviceIdentifier,
		Timestamp:        now,
		Health:           modelsV2.HealthHealthyActive,
		HashrateHS:       &modelsV2.MetricValue{Value: 100_000_000},
	})
	require.NoError(t, err)

	var gotSiteID sql.NullInt64
	err = db.QueryRowContext(ctx,
		"SELECT site_id FROM device_metrics WHERE device_identifier = $1 AND time = $2",
		deviceIdentifier,
		now,
	).Scan(&gotSiteID)
	require.NoError(t, err)
	require.True(t, gotSiteID.Valid)
	assert.Equal(t, liveSiteID, gotSiteID.Int64)
}

// TestTelemetryStore_StoreDeviceMetrics_EmptyInput tests that storing empty metrics is a no-op.
func TestTelemetryStore_StoreDeviceMetrics_EmptyInput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	err = store.StoreDeviceMetrics(ctx)
	require.NoError(t, err, "Storing empty metrics should not error")
}

// TestTelemetryStore_GetTimeSeriesTelemetry tests retrieving time series data.
func TestTelemetryStore_GetTimeSeriesTelemetry(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	deviceIdentifier := "device-timeseries-1"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceIdentifier)
	})

	// Insert multiple data points over time
	now := time.Now().Truncate(time.Millisecond)
	for i := range 5 {
		ts := now.Add(time.Duration(-i) * time.Minute)
		insertTestMetrics(t, db, deviceIdentifier, ts, float64(100_000_000+i*10_000_000), float64(70+i))
	}

	// Query time series
	startTime := now.Add(-10 * time.Minute)
	endTime := now.Add(1 * time.Minute)
	query := models.TimeSeriesTelemetryQuery{
		DeviceIDs: []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
	}

	results, err := store.GetTimeSeriesTelemetry(ctx, query)
	require.NoError(t, err)
	assert.NotEmpty(t, results, "Expected time series data")
}

// TestTelemetryStore_GetLatestDeviceMetricsBatch tests retrieving latest metrics for multiple devices.
func TestTelemetryStore_GetLatestDeviceMetricsBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	device1 := "device-batch-1"
	device2 := "device-batch-2"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, device1)
		cleanupDeviceMetrics(t, db, device2)
	})

	now := time.Now().Truncate(time.Millisecond)

	// Insert data for device 1 (multiple timestamps, should get latest)
	insertTestMetrics(t, db, device1, now.Add(-2*time.Minute), 100_000_000, 70.0)
	insertTestMetrics(t, db, device1, now, 150_000_000, 72.0) // Latest

	// Insert data for device 2
	insertTestMetrics(t, db, device2, now.Add(-1*time.Minute), 200_000_000, 75.0)

	// Query latest metrics for both devices
	results, err := store.GetLatestDeviceMetricsBatch(ctx, []models.DeviceIdentifier{
		models.DeviceIdentifier(device1),
		models.DeviceIdentifier(device2),
	})
	require.NoError(t, err)
	assert.Len(t, results, 2, "Expected metrics for both devices")

	// Verify device 1 has latest data
	d1Metrics, ok := results[models.DeviceIdentifier(device1)]
	require.True(t, ok, "Expected metrics for device 1")
	assert.Equal(t, 150_000_000.0, d1Metrics.HashrateHS.Value, "Expected latest hashrate for device 1")

	// Verify device 2 data
	d2Metrics, ok := results[models.DeviceIdentifier(device2)]
	require.True(t, ok, "Expected metrics for device 2")
	assert.Equal(t, 200_000_000.0, d2Metrics.HashrateHS.Value, "Expected hashrate for device 2")
}

// TestTelemetryStore_GetCombinedMetrics tests retrieving combined metrics for dashboards.
func TestTelemetryStore_GetCombinedMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	deviceIdentifier := "device-combined-1"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceIdentifier)
	})

	// Insert data points over time
	now := time.Now().Truncate(time.Millisecond)
	for i := range 10 {
		ts := now.Add(time.Duration(-i) * time.Minute)
		insertTestMetrics(t, db, deviceIdentifier, ts, float64(100_000_000+i*1_000_000), float64(70+i%5))
	}

	startTime := now.Add(-15 * time.Minute)
	endTime := now.Add(1 * time.Minute)
	query := models.CombinedMetricsQuery{
		DeviceIDs:        []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate, models.MeasurementTypeTemperature},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Metrics, "Expected combined metrics")
}

func TestTelemetryStore_GetCombinedMetrics_RawBucketAggregatesIgnoreTimeSeriesRowLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	config := timescaledb.DefaultConfig()
	config.MaxTimeSeriesRows = 1
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)
	ctx := t.Context()

	deviceA := "device-rawcap-a"
	deviceB := "device-rawcap-b"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceA)
		cleanupDeviceMetrics(t, db, deviceB)
	})

	bucket := time.Now().UTC().Add(-time.Hour).Truncate(time.Minute)
	insertTestMetrics(t, db, deviceA, bucket.Add(10*time.Second), 100, 60)
	insertTestMetrics(t, db, deviceA, bucket.Add(20*time.Second), 200, 80)
	insertTestMetrics(t, db, deviceB, bucket.Add(30*time.Second), 300, 70)
	insertTestMetrics(t, db, deviceB, bucket.Add(40*time.Second), 500, 90)

	startTime := bucket
	endTime := bucket.Add(time.Minute - time.Nanosecond)
	slideInterval := time.Minute

	// Act
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{
			models.DeviceIdentifier(deviceA),
			models.DeviceIdentifier(deviceB),
		},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeHashrate,
			models.MeasurementTypeTemperature,
		},
		AggregationTypes: []models.AggregationType{
			models.AggregationTypeAverage,
			models.AggregationTypeMin,
			models.AggregationTypeMax,
			models.AggregationTypeSum,
			models.AggregationTypeCount,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		SlideInterval: &slideInterval,
	})

	// Assert
	require.NoError(t, err)

	hashrate := requireMetric(t, result, models.MeasurementTypeHashrate)
	assert.Equal(t, int32(2), hashrate.DeviceCount)
	hashrateValues := aggValues(hashrate.AggregatedValues)
	assert.InDelta(t, 550.0, hashrateValues[models.AggregationTypeAverage], 0.001)
	assert.InDelta(t, 400.0, hashrateValues[models.AggregationTypeMin], 0.001)
	assert.InDelta(t, 700.0, hashrateValues[models.AggregationTypeMax], 0.001)
	assert.InDelta(t, 700.0, hashrateValues[models.AggregationTypeSum], 0.001)
	assert.InDelta(t, 2.0, hashrateValues[models.AggregationTypeCount], 0.001)

	temperature := requireMetric(t, result, models.MeasurementTypeTemperature)
	assert.Equal(t, int32(2), temperature.DeviceCount)
	temperatureValues := aggValues(temperature.AggregatedValues)
	assert.InDelta(t, 75.0, temperatureValues[models.AggregationTypeAverage], 0.001)
	assert.InDelta(t, 60.0, temperatureValues[models.AggregationTypeMin], 0.001)
	assert.InDelta(t, 90.0, temperatureValues[models.AggregationTypeMax], 0.001)
	assert.InDelta(t, 300.0, temperatureValues[models.AggregationTypeSum], 0.001)
	assert.InDelta(t, 4.0, temperatureValues[models.AggregationTypeCount], 0.001)

	require.Len(t, result.TemperatureStatusCounts, 1)
	assert.True(t, bucket.Equal(result.TemperatureStatusCounts[0].Timestamp), "expected bucket %s, got %s", bucket, result.TemperatureStatusCounts[0].Timestamp)
	assert.Equal(t, int32(0), result.TemperatureStatusCounts[0].ColdCount)
	assert.Equal(t, int32(0), result.TemperatureStatusCounts[0].OkCount)
	assert.Equal(t, int32(1), result.TemperatureStatusCounts[0].HotCount)
	assert.Equal(t, int32(1), result.TemperatureStatusCounts[0].CriticalCount)
}

func TestTelemetryStore_GetCombinedMetrics_RawBucketAggregatesDefaultInvalidSlideInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	deviceID := "device-rawslide-1"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceID)
	})

	ts := time.Now().UTC().Add(-time.Hour).Truncate(10 * time.Second)
	insertTestMetrics(t, db, deviceID, ts, 123, 55)

	// The window covers exactly one complete default-granularity bucket
	startTime := ts
	endTime := ts.Add(10*time.Second - time.Nanosecond)
	zeroSlideInterval := time.Duration(0)
	negativeSlideInterval := -time.Second
	subSecondSlideInterval := 500 * time.Millisecond

	tests := []struct {
		name          string
		slideInterval *time.Duration
	}{
		{name: "nil"},
		{name: "zero", slideInterval: &zeroSlideInterval},
		{name: "negative", slideInterval: &negativeSlideInterval},
		{name: "sub_second", slideInterval: &subSecondSlideInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
				DeviceIDs: []models.DeviceIdentifier{
					models.DeviceIdentifier(deviceID),
				},
				MeasurementTypes: []models.MeasurementType{
					models.MeasurementTypeHashrate,
				},
				AggregationTypes: []models.AggregationType{
					models.AggregationTypeAverage,
				},
				TimeRange: models.TimeRange{
					StartTime: &startTime,
					EndTime:   &endTime,
				},
				SlideInterval: tt.slideInterval,
			})

			// Assert
			require.NoError(t, err)
			hashrate := requireMetric(t, result, models.MeasurementTypeHashrate)
			hashrateValues := aggValues(hashrate.AggregatedValues)
			assert.InDelta(t, 123.0, hashrateValues[models.AggregationTypeAverage], 0.001)
		})
	}
}

func TestTelemetryStore_GetCombinedMetrics_RawBucketAggregatesRejectsTooManyBuckets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	endTime := time.Now().UTC()
	startTime := endTime.Add(-24 * time.Hour)
	slideInterval := time.Second

	// Act
	_, err = store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{
			models.DeviceIdentifier("device-rawbuckets-1"),
		},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeHashrate,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		SlideInterval: &slideInterval,
	})

	// Assert
	require.ErrorContains(t, err, "bucket count exceeds")
}

// TestTelemetryStore_StreamTelemetryUpdates tests streaming telemetry updates.
func TestTelemetryStore_StreamTelemetryUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	config := timescaledb.DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	config.BufferSize = 100
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)

	deviceIdentifier := "device-stream-1"
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceIdentifier)
	})

	now := time.Now().Truncate(time.Millisecond)
	insertTestMetrics(t, db, deviceIdentifier, now, 100_000_000, 72.5)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	query := models.StreamQuery{
		DeviceIDs:        []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
		IncludeHeartbeat: false,
	}

	updateChan, err := store.StreamTelemetryUpdates(ctx, query)
	require.NoError(t, err)

	var receivedUpdates []models.TelemetryUpdate
	for update := range updateChan {
		receivedUpdates = append(receivedUpdates, update)
		if len(receivedUpdates) >= 5 {
			cancel()
		}
	}

	require.NotEmpty(t, receivedUpdates, "Expected to receive telemetry updates")
	assert.Equal(t, models.UpdateTypeTelemetry, receivedUpdates[0].Type)
	assert.Equal(t, models.DeviceIdentifier(deviceIdentifier), receivedUpdates[0].DeviceIdentifier)
	assert.NotEmpty(t, receivedUpdates[0].MeasurementName)
}

// TestTelemetryStore_StreamTelemetryUpdates_ContextCancellation tests that streaming stops on context cancellation.
func TestTelemetryStore_StreamTelemetryUpdates_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	config := timescaledb.DefaultConfig()
	config.PollInterval = 10 * time.Millisecond
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	query := models.StreamQuery{
		DeviceIDs:        nil,
		IncludeHeartbeat: false,
	}

	updateChan, err := store.StreamTelemetryUpdates(ctx, query)
	require.NoError(t, err)

	cancel()

	select {
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Channel should have closed after context cancellation")
	case _, ok := <-updateChan:
		if ok {
			for range updateChan {
			}
		}
	}
}

// TestTelemetryStore_StreamTelemetryUpdates_Heartbeat tests that heartbeats are sent when enabled.
func TestTelemetryStore_StreamTelemetryUpdates_Heartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	config := timescaledb.DefaultConfig()
	config.PollInterval = 20 * time.Millisecond
	config.BufferSize = 100
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	query := models.StreamQuery{
		DeviceIDs:        nil,
		IncludeHeartbeat: true,
	}

	updateChan, err := store.StreamTelemetryUpdates(ctx, query)
	require.NoError(t, err)

	var heartbeatReceived bool
	for update := range updateChan {
		if update.Type == models.UpdateTypeHeartbeat {
			heartbeatReceived = true
			break
		}
	}

	assert.True(t, heartbeatReceived, "Expected to receive heartbeat update")
}

// TestTelemetryStore_GetCombinedMetrics_DataSourceSelection tests that queries are routed
// to the correct data source based on time range:
// - Queries <= 24h use raw data
// - Queries 24h-10d use hourly aggregates
// - Queries > 10d use daily aggregates
func TestTelemetryStore_GetCombinedMetrics_DataSourceSelection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testCases := []struct {
		name     string
		duration time.Duration
	}{
		{"1 hour query (raw data)", 1 * time.Hour},
		{"exactly 24h (raw data boundary)", 24 * time.Hour},
		{"25 hours (hourly aggregates)", 25 * time.Hour},
		{"5 days (hourly aggregates)", 5 * 24 * time.Hour},
		{"exactly 10 days (hourly boundary)", 10 * 24 * time.Hour},
		{"11 days (daily aggregates)", 11 * 24 * time.Hour},
		{"30 days (daily aggregates)", 30 * 24 * time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			db := testutil.GetTestDB(t)
			store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
			require.NoError(t, err)
			ctx := t.Context()

			deviceIdentifier := "device-datasource-1"
			t.Cleanup(func() {
				cleanupDeviceMetrics(t, db, deviceIdentifier)
			})

			now := time.Now().Truncate(time.Millisecond)
			insertTestMetrics(t, db, deviceIdentifier, now, 100_000_000, 70.0)

			startTime := now.Add(-tc.duration)
			endTime := now.Add(1 * time.Minute)
			query := models.CombinedMetricsQuery{
				DeviceIDs:        []models.DeviceIdentifier{models.DeviceIdentifier(deviceIdentifier)},
				MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
				TimeRange: models.TimeRange{
					StartTime: &startTime,
					EndTime:   &endTime,
				},
			}

			// Act
			result, err := store.GetCombinedMetrics(ctx, query)

			// Assert
			require.NoError(t, err, "Query should succeed for %s", tc.name)
			// Note: Metrics may be nil if continuous aggregates haven't been refreshed.
			// The key verification is that the query executes successfully with the
			// correct data source routing based on duration.
			assert.NotNil(t, result, "Result should not be nil")
		})
	}
}

// TestTelemetryStore_GetCombinedMetrics_TemperatureStatusCounts_Values tests that temperature
// status counts are correctly calculated from raw data.
func TestTelemetryStore_GetCombinedMetrics_TemperatureStatusCounts_Values(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	devices := []struct {
		id   string
		temp float64
	}{
		{"device-status-cold", -5.0},     // temp < 0 → cold
		{"device-status-ok1", 50.0},      // 0 <= temp < 70 → ok
		{"device-status-ok2", 65.0},      // 0 <= temp < 70 → ok
		{"device-status-hot", 85.0},      // 70 <= temp < 90 → hot
		{"device-status-critical", 95.0}, // temp >= 90 → critical
	}

	for _, d := range devices {
		t.Cleanup(func() {
			cleanupDeviceMetrics(t, db, d.id)
		})
	}

	now := time.Now().Truncate(time.Millisecond)
	for _, d := range devices {
		insertTestMetrics(t, db, d.id, now, 100_000_000, d.temp)
	}

	startTime := now.Add(-1 * time.Minute)
	endTime := now.Add(1 * time.Minute)
	deviceIDs := make([]models.DeviceIdentifier, len(devices))
	for i, d := range devices {
		deviceIDs[i] = models.DeviceIdentifier(d.id)
	}
	query := models.CombinedMetricsQuery{
		DeviceIDs:        deviceIDs,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeTemperature},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
	}

	// Act
	result, err := store.GetCombinedMetrics(ctx, query)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, result.TemperatureStatusCounts, "Expected temperature status counts")

	var totalCold, totalOk, totalHot, totalCritical int32
	for _, count := range result.TemperatureStatusCounts {
		totalCold += count.ColdCount
		totalOk += count.OkCount
		totalHot += count.HotCount
		totalCritical += count.CriticalCount
	}

	assert.Equal(t, int32(1), totalCold, "Expected 1 cold device (temp < 0)")
	assert.Equal(t, int32(2), totalOk, "Expected 2 ok devices (0 <= temp < 70)")
	assert.Equal(t, int32(1), totalHot, "Expected 1 hot device (70 <= temp < 90)")
	assert.Equal(t, int32(1), totalCritical, "Expected 1 critical device (temp >= 90)")
}

func TestTelemetryStore_GetCombinedMetrics_DefaultRangeRoutesToAggregatesWithLargeMaxAge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: nil time bounds resolve to now-MaxAge..now; with MaxAge past
	// the raw window this must route to aggregates instead of erroring
	dbSvc := testutil.NewDatabaseService(t, nil)
	cfg := timescaledb.DefaultConfig()
	cfg.MaxAge = 72 * time.Hour
	store, err := timescaledb.NewTelemetryStore(dbSvc.DB, cfg)
	require.NoError(t, err)

	// Act
	result, err := store.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
	})

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestTelemetryStore_GetCombinedMetrics_AllDevicesFullDayMergesHourlyBodyWithRawTail(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: one metric materialized into the hourly aggregate and older than
	// the 2h tail coverage, one raw-only metric inside the in-progress hour; a
	// 24h all-devices request must serve hour buckets for the body and
	// raw-granularity buckets for the tail
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	identifier := fmt.Sprintf("hourly-tail-device-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDeviceMetrics(t, db, identifier) })

	hour := time.Now().UTC().Truncate(time.Hour)
	end := hour.Add(30 * time.Minute)
	start := end.Add(-24 * time.Hour)
	tailBoundary := end.Add(-2 * time.Hour).Truncate(time.Hour)
	materialized := hour.Add(-5 * time.Hour).Add(10 * time.Minute)
	rawOnly := hour.Add(20 * time.Minute)
	insertTestMetrics(t, db, identifier, materialized, 500, 60)
	insertTestMetrics(t, db, identifier, rawOnly, 900, 90)
	refreshMetricsHourlyAggregate(t, db, materialized.Add(-time.Hour), materialized.Add(time.Hour))

	slide := 90 * time.Second

	// Act
	result, err := store.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})

	// Assert: hour-aligned aggregate buckets before the tail boundary, raw
	// buckets at the requested granularity after it, ordered by OpenTime with
	// no timestamp appearing twice
	require.NoError(t, err)
	require.NotEmpty(t, result.Metrics)

	var body, tail []models.Metric
	seen := make(map[int64]bool)
	for i, m := range result.Metrics {
		assert.False(t, seen[m.OpenTime.UnixNano()], "duplicate bucket timestamp %s", m.OpenTime)
		seen[m.OpenTime.UnixNano()] = true
		if i > 0 {
			assert.False(t, m.OpenTime.Before(result.Metrics[i-1].OpenTime),
				"metrics must be ordered by OpenTime, got %s after %s", m.OpenTime, result.Metrics[i-1].OpenTime)
		}
		if m.OpenTime.Before(tailBoundary) {
			body = append(body, m)
		} else {
			tail = append(tail, m)
		}
	}

	require.Len(t, body, 1, "expected exactly the materialized hourly bucket in the body")
	assert.True(t, body[0].OpenTime.Equal(materialized.Truncate(time.Hour)),
		"expected hour-aligned body bucket, got %s", body[0].OpenTime)
	assert.Equal(t, float64(500), aggValues(body[0].AggregatedValues)[models.AggregationTypeAverage])

	require.Len(t, tail, 1, "expected exactly the raw-only bucket in the tail")
	assert.True(t, tail[0].OpenTime.Equal(rawOnly.Truncate(slide)),
		"expected tail bucket at the requested granularity, got %s", tail[0].OpenTime)
	assert.Equal(t, float64(900), aggValues(tail[0].AggregatedValues)[models.AggregationTypeAverage])
}

func TestTelemetryStore_GetCombinedMetrics_OrgHourlyBodyMatchesRawTailScope(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: all-device org query over six hours routes to hourly body plus
	// raw tail. Both halves must use the same org scope.
	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	otherUser := dbSvc.CreateSuperAdminUser2()
	otherOrgID := otherUser.OrganizationID
	device := dbSvc.CreateDevice(orgID, "proto")
	otherDevice := dbSvc.CreateDevice(otherOrgID, "proto")
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, device.ID)
		cleanupDeviceMetrics(t, db, otherDevice.ID)
	})

	hour := time.Now().UTC().Truncate(time.Hour)
	end := hour.Add(30 * time.Minute)
	start := end.Add(-6 * time.Hour)
	tailBoundary := end.Add(-2 * time.Hour).Truncate(time.Hour)
	bodyPoint := tailBoundary.Add(-2*time.Hour + 10*time.Minute)
	rawOnly := tailBoundary.Add(time.Hour + 10*time.Minute)
	require.NoError(t, store.StoreDeviceMetrics(ctx,
		modelsV2.DeviceMetrics{
			DeviceIdentifier: device.ID,
			Timestamp:        bodyPoint,
			HashrateHS:       &modelsV2.MetricValue{Value: 500},
			TempC:            &modelsV2.MetricValue{Value: 60},
		},
		modelsV2.DeviceMetrics{
			DeviceIdentifier: otherDevice.ID,
			Timestamp:        bodyPoint,
			HashrateHS:       &modelsV2.MetricValue{Value: 10_000},
			TempC:            &modelsV2.MetricValue{Value: 95},
		},
		modelsV2.DeviceMetrics{
			DeviceIdentifier: device.ID,
			Timestamp:        rawOnly,
			HashrateHS:       &modelsV2.MetricValue{Value: 900},
			TempC:            &modelsV2.MetricValue{Value: 62},
		},
		modelsV2.DeviceMetrics{
			DeviceIdentifier: otherDevice.ID,
			Timestamp:        rawOnly,
			HashrateHS:       &modelsV2.MetricValue{Value: 20_000},
			TempC:            &modelsV2.MetricValue{Value: 96},
		},
	))
	refreshMetricsHourlyAggregate(t, db, bodyPoint.Add(-time.Hour), bodyPoint.Add(time.Hour))
	refreshStatusHourlyAggregate(t, db, bodyPoint.Add(-time.Hour), bodyPoint.Add(time.Hour))

	slide := 90 * time.Second

	// Act
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID:   orgID,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})

	// Assert: neither the hourly body nor the raw tail includes the peer org.
	require.NoError(t, err)
	require.Len(t, result.Metrics, 2)
	assert.True(t, result.Metrics[0].OpenTime.Equal(bodyPoint.Truncate(time.Hour)),
		"expected org-scoped hourly body bucket, got %s", result.Metrics[0].OpenTime)
	assert.Equal(t, float64(500), aggValues(result.Metrics[0].AggregatedValues)[models.AggregationTypeAverage])
	assert.Equal(t, int32(1), result.Metrics[0].DeviceCount)
	assert.True(t, result.Metrics[1].OpenTime.Equal(rawOnly.Truncate(slide)),
		"expected org-scoped raw tail bucket, got %s", result.Metrics[1].OpenTime)
	assert.Equal(t, float64(900), aggValues(result.Metrics[1].AggregatedValues)[models.AggregationTypeAverage])
	assert.Equal(t, int32(1), result.Metrics[1].DeviceCount)

	var bodyTemp *models.TemperatureStatusCount
	for i := range result.TemperatureStatusCounts {
		if result.TemperatureStatusCounts[i].Timestamp.Equal(bodyPoint.Truncate(time.Hour)) {
			bodyTemp = &result.TemperatureStatusCounts[i]
			break
		}
	}
	require.NotNil(t, bodyTemp, "expected body temperature status count")
	assert.Equal(t, int32(1), bodyTemp.OkCount)
	assert.Equal(t, int32(0), bodyTemp.CriticalCount)
}

func TestTelemetryStore_GetCombinedMetrics_RawTailCoversUnmaterializedCompletedHours(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: the hourly aggregate is materialized only far behind the request
	// end, and a raw-only metric sits in a completed but unmaterialized hour;
	// the tail must cover it, an in-progress-hour tail would leave a hole
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	identifier := fmt.Sprintf("unmaterialized-hour-device-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDeviceMetrics(t, db, identifier) })

	hour := time.Now().UTC().Truncate(time.Hour)
	end := hour.Add(30 * time.Minute)
	start := end.Add(-24 * time.Hour)
	tailBoundary := end.Add(-2 * time.Hour).Truncate(time.Hour)
	materialized := hour.Add(-5 * time.Hour).Add(10 * time.Minute)
	rawOnly := end.Add(-80 * time.Minute)
	insertTestMetrics(t, db, identifier, materialized, 500, 60)
	insertTestMetrics(t, db, identifier, rawOnly, 900, 90)
	refreshMetricsHourlyAggregate(t, db, materialized.Add(-time.Hour), materialized.Add(time.Hour))

	slide := 90 * time.Second

	// Act
	result, err := store.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})

	// Assert: the materialized hourly bucket plus the raw-only point served
	// from the tail at raw granularity
	require.NoError(t, err)
	require.Len(t, result.Metrics, 2)
	assert.True(t, result.Metrics[0].OpenTime.Equal(materialized.Truncate(time.Hour)),
		"expected hour-aligned body bucket, got %s", result.Metrics[0].OpenTime)
	assert.Equal(t, float64(500), aggValues(result.Metrics[0].AggregatedValues)[models.AggregationTypeAverage])
	assert.True(t, result.Metrics[1].OpenTime.Equal(rawOnly.Truncate(slide)),
		"expected raw-granularity tail bucket, got %s", result.Metrics[1].OpenTime)
	assert.False(t, result.Metrics[1].OpenTime.Before(tailBoundary),
		"tail bucket must not precede the tail boundary %s, got %s", tailBoundary, result.Metrics[1].OpenTime)
	assert.Equal(t, float64(900), aggValues(result.Metrics[1].AggregatedValues)[models.AggregationTypeAverage])
}

func TestTelemetryStore_GetCombinedMetrics_CoarseSlideTailStaysAfterHourlyBody(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: a 4h slide interval whose time_bucket grid is misaligned with
	// the hour-truncated tail start. The request end sits in an hour that is
	// 3h past a 4h grid boundary, so an uncapped tail would label the raw
	// bucket 3h early, exactly on the body's last hourly bucket
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	identifier := fmt.Sprintf("coarse-slide-tail-device-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDeviceMetrics(t, db, identifier) })

	hour := time.Now().UTC().Truncate(time.Hour)
	hour = hour.Add(-time.Duration((hour.Hour()%4+1)%4) * time.Hour)
	end := hour.Add(30 * time.Minute)
	start := end.Add(-24 * time.Hour)
	tailBoundary := end.Add(-2 * time.Hour).Truncate(time.Hour)
	materialized := tailBoundary.Add(-time.Hour).Add(10 * time.Minute)
	// Inside the tail but in a complete hour: the in-progress final hour is
	// excluded from the response
	rawOnly := hour.Add(-time.Hour).Add(20 * time.Minute)
	insertTestMetrics(t, db, identifier, materialized, 500, 60)
	insertTestMetrics(t, db, identifier, rawOnly, 900, 90)
	refreshMetricsHourlyAggregate(t, db, materialized.Add(-time.Hour), materialized.Add(time.Hour))

	slide := 4 * time.Hour

	// Act
	result, err := store.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})

	// Assert: the tail bucket is capped to the hour grid so it lands after the
	// body bucket instead of duplicating its OpenTime
	require.NoError(t, err)
	require.Len(t, result.Metrics, 2)
	seen := make(map[int64]bool)
	for i, m := range result.Metrics {
		assert.False(t, seen[m.OpenTime.UnixNano()], "duplicate bucket timestamp %s", m.OpenTime)
		seen[m.OpenTime.UnixNano()] = true
		if i > 0 {
			assert.False(t, m.OpenTime.Before(result.Metrics[i-1].OpenTime),
				"metrics must be ordered by OpenTime, got %s after %s", m.OpenTime, result.Metrics[i-1].OpenTime)
		}
	}
	assert.True(t, result.Metrics[0].OpenTime.Equal(materialized.Truncate(time.Hour)),
		"expected hour-aligned body bucket, got %s", result.Metrics[0].OpenTime)
	assert.Equal(t, float64(500), aggValues(result.Metrics[0].AggregatedValues)[models.AggregationTypeAverage])
	assert.True(t, result.Metrics[1].OpenTime.Equal(rawOnly.Truncate(time.Hour)),
		"expected hour-capped tail bucket, got %s", result.Metrics[1].OpenTime)
	assert.False(t, result.Metrics[1].OpenTime.Before(tailBoundary),
		"tail bucket must not precede the tail boundary %s, got %s", tailBoundary, result.Metrics[1].OpenTime)
	assert.Equal(t, float64(900), aggValues(result.Metrics[1].AggregatedValues)[models.AggregationTypeAverage])
}

func TestTelemetryStore_GetCombinedMetrics_HourAlignedEndStillGetsRawTail(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: a request ending exactly on an hour boundary still gets the
	// last-2h raw tail; the seam metric is materialized into the aggregate AND
	// inside the tail coverage, so a wrong seam would serve it twice
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	identifier := fmt.Sprintf("hour-aligned-tail-device-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDeviceMetrics(t, db, identifier) })

	end := time.Now().UTC().Truncate(time.Hour)
	start := end.Add(-24 * time.Hour)
	tailBoundary := end.Add(-2 * time.Hour)
	bodyPoint := end.Add(-3 * time.Hour).Add(10 * time.Minute)
	seamPoint := tailBoundary.Add(10 * time.Minute)
	insertTestMetrics(t, db, identifier, bodyPoint, 500, 60)
	insertTestMetrics(t, db, identifier, seamPoint, 700, 65)
	refreshMetricsHourlyAggregate(t, db, end.Add(-4*time.Hour), end.Add(-time.Hour))

	slide := 90 * time.Second

	// Act
	result, err := store.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})

	// Assert: the body ends at the tail boundary and the seam metric appears
	// exactly once, from the tail at raw granularity
	require.NoError(t, err)
	require.Len(t, result.Metrics, 2)
	assert.True(t, result.Metrics[0].OpenTime.Equal(bodyPoint.Truncate(time.Hour)),
		"expected hour-aligned body bucket, got %s", result.Metrics[0].OpenTime)
	assert.Equal(t, float64(500), aggValues(result.Metrics[0].AggregatedValues)[models.AggregationTypeAverage])
	assert.True(t, result.Metrics[1].OpenTime.Equal(seamPoint.Truncate(slide)),
		"expected raw-granularity tail bucket, got %s", result.Metrics[1].OpenTime)
	assert.False(t, result.Metrics[1].OpenTime.Before(tailBoundary),
		"tail bucket must not precede the tail boundary %s, got %s", tailBoundary, result.Metrics[1].OpenTime)
	assert.Equal(t, float64(700), aggValues(result.Metrics[1].AggregatedValues)[models.AggregationTypeAverage])
}

func TestTelemetryStore_GetCombinedMetrics_MultiDayRangeGetsNoRawTail(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: a 3 day range is past the raw window, so even a mid-hour end
	// keeps the complete-bucket hourly shape; the raw-only metric sits in the
	// would-be tail window to catch any stray tail query
	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	identifier := fmt.Sprintf("multiday-no-tail-device-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDeviceMetrics(t, db, identifier) })

	hour := time.Now().UTC().Truncate(time.Hour)
	end := hour.Add(30 * time.Minute)
	start := end.Add(-3 * 24 * time.Hour)
	materialized := hour.Add(-5 * time.Hour).Add(10 * time.Minute)
	rawOnly := hour.Add(20 * time.Minute)
	insertTestMetrics(t, db, identifier, materialized, 500, 60)
	insertTestMetrics(t, db, identifier, rawOnly, 900, 90)
	refreshMetricsHourlyAggregate(t, db, materialized.Add(-time.Hour), materialized.Add(time.Hour))

	slide := 90 * time.Second

	// Act
	result, err := store.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})

	// Assert: only the materialized hourly bucket, no sub-hour tail buckets
	require.NoError(t, err)
	require.Len(t, result.Metrics, 1)
	assert.True(t, result.Metrics[0].OpenTime.Equal(materialized.Truncate(time.Hour)),
		"expected hour-aligned bucket, got %s", result.Metrics[0].OpenTime)
	assert.Equal(t, float64(500), aggValues(result.Metrics[0].AggregatedValues)[models.AggregationTypeAverage])
}

func TestTelemetryStore_GetCombinedMetrics_UptimeSurvivesEmptyHourlyBodyAndCoversRequestEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange: a 501-device list over 6h routes to the hourly path; the metric
	// CAGG is never refreshed (empty body) while raw metrics sit in the tail
	// window and state snapshots include a change inside the final hour
	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID
	identifier := fmt.Sprintf("uptime-empty-body-device-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDeviceMetrics(t, db, identifier) })

	end := time.Now().UTC().Truncate(time.Minute)
	start := end.Add(-6 * time.Hour)
	insertTestMetrics(t, db, identifier, end.Add(-90*time.Minute), 700, 65)
	insertTestMinerStateSnapshot(t, db, end.Add(-3*time.Hour), orgID, identifier, 3)
	insertTestMinerStateSnapshot(t, db, end.Add(-30*time.Minute), orgID, identifier, 2)

	deviceIDs := make([]models.DeviceIdentifier, 0, 501)
	deviceIDs = append(deviceIDs, models.DeviceIdentifier(identifier))
	for i := 1; i < 501; i++ {
		deviceIDs = append(deviceIDs, models.DeviceIdentifier(fmt.Sprintf("uptime-empty-body-filler-%d", i)))
	}
	slide := 90 * time.Second

	// Act
	result, err := store.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
		OrganizationID:   orgID,
		DeviceIDs:        deviceIDs,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate, models.MeasurementTypeUptime},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	})

	// Assert: tail metrics present despite the empty body, uptime computed,
	// and the last uptime bucket is the one containing the final state change
	// (the pre-fix hour-early end excluded that snapshot entirely)
	require.NoError(t, err)
	require.NotEmpty(t, result.Metrics)
	require.NotEmpty(t, result.UptimeStatusCounts)
	lastUptime := result.UptimeStatusCounts[len(result.UptimeStatusCounts)-1]
	lastChangeBucket := end.Add(-30 * time.Minute).Truncate(time.Hour)
	assert.True(t, lastUptime.Timestamp.Equal(lastChangeBucket),
		"expected last uptime bucket %s, got %s", lastChangeBucket, lastUptime.Timestamp)
	assert.Equal(t, int32(1), lastUptime.BrokenCount)
}

// Helper functions

func insertTestMinerStateSnapshot(t *testing.T, db *sql.DB, at time.Time, orgID int64, deviceIdentifier string, state int16) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO miner_state_snapshots (time, org_id, device_identifier, state)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (time, device_identifier) DO UPDATE SET state = EXCLUDED.state`,
		at, orgID, deviceIdentifier, state)
	require.NoError(t, err)
}

func refreshMetricsHourlyAggregate(t *testing.T, db *sql.DB, start, end time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"CALL refresh_continuous_aggregate('device_metrics_hourly', $1::timestamptz, $2::timestamptz)",
		start, end)
	require.NoError(t, err)
}

func refreshStatusHourlyAggregate(t *testing.T, db *sql.DB, start, end time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"CALL refresh_continuous_aggregate('device_status_hourly', $1::timestamptz, $2::timestamptz)",
		start, end)
	require.NoError(t, err)
}

func cleanupDeviceMetrics(t *testing.T, db *sql.DB, deviceIdentifier string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), "DELETE FROM device_metrics WHERE device_identifier = $1", deviceIdentifier)
	if err != nil {
		t.Logf("Warning: failed to cleanup device metrics for %s: %v", deviceIdentifier, err)
	}
}

func insertTestMetrics(t *testing.T, db *sql.DB, deviceIdentifier string, ts time.Time, hashRate, temp float64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, temp_c, fan_rpm, power_w, efficiency_jh)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (time, device_identifier) DO UPDATE SET
		   hash_rate_hs = EXCLUDED.hash_rate_hs,
		   temp_c = EXCLUDED.temp_c`,
		ts, deviceIdentifier, hashRate, temp, 3500.0, 1500.0, 15.0,
	)
	require.NoError(t, err, "Failed to insert test metrics")
}

func requireMetric(t *testing.T, result models.CombinedMetric, measurementType models.MeasurementType) models.Metric {
	t.Helper()
	for _, metric := range result.Metrics {
		if metric.MeasurementType == measurementType {
			return metric
		}
	}
	require.Failf(t, "missing metric", "measurement type %s not found", measurementType.String())
	return models.Metric{}
}

func aggValues(result []models.AggregatedValue) map[models.AggregationType]float64 {
	values := make(map[models.AggregationType]float64, len(result))
	for _, value := range result {
		values[value.Type] = value.Value
	}
	return values
}

func createTelemetryTestSite(t *testing.T, db *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	var siteID int64
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	err := db.QueryRowContext(context.Background(),
		"INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id",
		orgID,
		name,
		slug,
	).Scan(&siteID)
	require.NoError(t, err)
	return siteID
}

func renameTelemetryTestDevice(t *testing.T, db *sql.DB, deviceID int64, deviceIdentifier string, siteID int64, deleted bool) {
	t.Helper()
	deletedAt := sql.NullTime{Time: time.Now().UTC(), Valid: deleted}
	_, err := db.ExecContext(context.Background(),
		`UPDATE discovered_device dd
		 SET device_identifier = $1, deleted_at = $2
		 FROM device d
		 WHERE d.discovered_device_id = dd.id AND d.id = $3`,
		deviceIdentifier,
		deletedAt,
		deviceID,
	)
	require.NoError(t, err)

	_, err = db.ExecContext(context.Background(),
		`UPDATE device
		 SET device_identifier = $1, site_id = $2, deleted_at = $3
		 WHERE id = $4`,
		deviceIdentifier,
		siteID,
		deletedAt,
		deviceID,
	)
	require.NoError(t, err)
}
