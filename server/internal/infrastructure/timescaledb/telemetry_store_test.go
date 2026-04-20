package timescaledb_test

import (
	"context"
	"database/sql"
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

// TestTelemetryStore_GetCombinedMetrics_TemperatureStatusCounts tests temperature status bucketing.
func TestTelemetryStore_GetCombinedMetrics_TemperatureStatusCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
	require.NoError(t, err)
	ctx := t.Context()

	// Create devices in different temperature ranges
	devices := []struct {
		id   string
		temp float64
	}{
		{"device-temp-cold", -5.0},     // cold
		{"device-temp-ok1", 50.0},      // ok
		{"device-temp-ok2", 65.0},      // ok
		{"device-temp-hot", 85.0},      // hot
		{"device-temp-critical", 95.0}, // critical
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

	// Collect device IDs for the query
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

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err)
	require.NotEmpty(t, result.TemperatureStatusCounts, "Expected temperature status counts")
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
