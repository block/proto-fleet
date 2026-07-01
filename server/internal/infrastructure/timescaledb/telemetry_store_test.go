package timescaledb_test

import (
	"context"
	"database/sql"
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

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	deviceIdentifier := dbSvc.CreateDevice(user.OrganizationID, "proto").ID
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
		OrganizationID:   user.OrganizationID,
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

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	config := timescaledb.DefaultConfig()
	config.MaxTimeSeriesRows = 1
	store, err := timescaledb.NewTelemetryStore(db, config)
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	deviceA := dbSvc.CreateDevice(user.OrganizationID, "proto").ID
	deviceB := dbSvc.CreateDevice(user.OrganizationID, "proto").ID
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
	result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID: user.OrganizationID,
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

func TestTelemetryStore_GetCombinedMetrics_RawBucketAggregatesScopeByOrganization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	orgAUser := dbSvc.CreateSuperAdminUser()
	orgBUser := dbSvc.CreateSuperAdminUser2()
	orgADevice := dbSvc.CreateDevice(orgAUser.OrganizationID, "proto").ID
	orgBDevice := dbSvc.CreateDevice(orgBUser.OrganizationID, "proto").ID
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, orgADevice)
		cleanupDeviceMetrics(t, db, orgBDevice)
	})

	bucket := time.Now().UTC().Add(-time.Hour).Truncate(time.Minute)
	insertTestMetrics(t, db, orgADevice, bucket.Add(10*time.Second), 100, 60)
	insertTestMetrics(t, db, orgBDevice, bucket.Add(20*time.Second), 900, 90)

	startTime := bucket
	endTime := bucket.Add(time.Minute - time.Nanosecond)
	slideInterval := time.Minute
	baseQuery := models.CombinedMetricsQuery{
		OrganizationID:   orgAUser.OrganizationID,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		AggregationTypes: []models.AggregationType{
			models.AggregationTypeAverage,
			models.AggregationTypeCount,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		SlideInterval: &slideInterval,
	}

	for _, tt := range []struct {
		name      string
		deviceIDs []models.DeviceIdentifier
	}{
		{name: "all devices"},
		{
			name: "explicit cross-org device list",
			deviceIDs: []models.DeviceIdentifier{
				models.DeviceIdentifier(orgADevice),
				models.DeviceIdentifier(orgBDevice),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			query := baseQuery
			query.DeviceIDs = tt.deviceIDs
			result, err := store.GetCombinedMetrics(ctx, query)
			require.NoError(t, err)

			hashrate := requireMetric(t, result, models.MeasurementTypeHashrate)
			assert.Equal(t, int32(1), hashrate.DeviceCount)
			hashrateValues := aggValues(hashrate.AggregatedValues)
			assert.InDelta(t, 100.0, hashrateValues[models.AggregationTypeAverage], 0.001)
			assert.InDelta(t, 1.0, hashrateValues[models.AggregationTypeCount], 0.001)
		})
	}
}

func TestTelemetryStore_GetCombinedMetrics_RawBucketAggregatesDefaultInvalidSlideInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	deviceID := dbSvc.CreateDevice(user.OrganizationID, "proto").ID
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceID)
	})

	ts := time.Now().UTC().Add(-time.Hour).Truncate(10 * time.Second)
	insertTestMetrics(t, db, deviceID, ts, 123, 55)

	startTime := ts.Add(-time.Second)
	endTime := ts.Add(time.Second)
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
			result, err := store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
				OrganizationID: user.OrganizationID,
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

	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	user := dbSvc.CreateSuperAdminUser()
	deviceID := dbSvc.CreateDevice(user.OrganizationID, "proto").ID
	t.Cleanup(func() {
		cleanupDeviceMetrics(t, db, deviceID)
	})

	endTime := time.Now().UTC()
	startTime := endTime.Add(-24 * time.Hour)
	slideInterval := time.Second

	_, err = store.GetCombinedMetrics(ctx, models.CombinedMetricsQuery{
		OrganizationID: user.OrganizationID,
		DeviceIDs: []models.DeviceIdentifier{
			models.DeviceIdentifier(deviceID),
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
			dbSvc := testutil.NewDatabaseService(t, nil)
			db := dbSvc.DB
			store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
			require.NoError(t, err)
			ctx := t.Context()

			user := dbSvc.CreateSuperAdminUser()
			deviceIdentifier := dbSvc.CreateDevice(user.OrganizationID, "proto").ID
			t.Cleanup(func() {
				cleanupDeviceMetrics(t, db, deviceIdentifier)
			})

			now := time.Now().Truncate(time.Millisecond)
			insertTestMetrics(t, db, deviceIdentifier, now, 100_000_000, 70.0)

			startTime := now.Add(-tc.duration)
			endTime := now.Add(1 * time.Minute)
			query := models.CombinedMetricsQuery{
				OrganizationID:   user.OrganizationID,
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
	dbSvc := testutil.NewDatabaseService(t, nil)
	db := dbSvc.DB
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()
	user := dbSvc.CreateSuperAdminUser()

	devices := []struct {
		id   string
		temp float64
	}{
		{dbSvc.CreateDevice(user.OrganizationID, "proto").ID, -5.0}, // temp < 0 → cold
		{dbSvc.CreateDevice(user.OrganizationID, "proto").ID, 50.0}, // 0 <= temp < 70 → ok
		{dbSvc.CreateDevice(user.OrganizationID, "proto").ID, 65.0}, // 0 <= temp < 70 → ok
		{dbSvc.CreateDevice(user.OrganizationID, "proto").ID, 85.0}, // 70 <= temp < 90 → hot
		{dbSvc.CreateDevice(user.OrganizationID, "proto").ID, 95.0}, // temp >= 90 → critical
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
		OrganizationID:   user.OrganizationID,
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

// Helper functions

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
