package influxdb

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/influxdb/testutils"
)

// Test helper functions

func setupIntegrationTest(t *testing.T) (*InfluxTelemetryStore, testcontainers.Container, context.Context) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container, testConfig := testutils.SetupInfluxDBContainer(t)

	config := Config{
		URL:           testConfig.URL,
		Organization:  testConfig.Organization,
		Bucket:        testConfig.Bucket,
		Token:         testConfig.Token,
		WriteTimeout:  testConfig.WriteTimeout,
		QueryTimeout:  testConfig.QueryTimeout,
		RetryAttempts: 3,
		RetryDelay:    50 * time.Millisecond,
	}

	store, err := NewTelemetryStore(config)
	require.NoError(t, err)

	ctx := t.Context()
	err = store.Ping(ctx)
	require.NoError(t, err, "Should be able to ping InfluxDB")

	return store, container, ctx
}

func cleanupIntegrationTest(t *testing.T, store *InfluxTelemetryStore, container testcontainers.Container) {
	if store != nil {
		store.Close()
	}
	if container != nil {
		if err := container.Terminate(t.Context()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}
}

func storeTestDataWithErrorHandling(ctx context.Context, t *testing.T, store *InfluxTelemetryStore, testData []models.Telemetry, operation string) {
	err := store.Store(ctx, testData...)
	require.NoError(t, err, "Should successfully store %s", operation)
	time.Sleep(100 * time.Millisecond) // Give InfluxDB time to process writes
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}

func createTestTelemetry(deviceID, measurement string, value float64) models.Telemetry {
	return models.Telemetry{
		Measurement: measurement,
		Fields: map[string]any{
			"value": value,
		},
		Tags: map[string]string{
			"device_id": deviceID,
			"test":      "true",
		},
		Timestamp: time.Now(),
	}
}

// createTestTelemetryByType creates test telemetry using the correct InfluxDB measurement name
func createTestTelemetryByType(deviceID string, measurementType models.MeasurementType, value float64) models.Telemetry {
	return models.Telemetry{
		Measurement: measurementType.InfluxMeasurementName(),
		Fields: map[string]any{
			"value": value,
		},
		Tags: map[string]string{
			"device_id": deviceID,
			"test":      "true",
		},
		Timestamp: time.Now(),
	}
}

func createTestTelemetryWithTimestamp(deviceID, measurement string, value float64, timestamp time.Time) models.Telemetry {
	return models.Telemetry{
		Measurement: measurement,
		Fields: map[string]any{
			"value": value,
		},
		Tags: map[string]string{
			"device_id": deviceID,
			"test":      "integration",
		},
		Timestamp: timestamp,
	}
}

// createTestTelemetryByTypeWithTimestamp creates test telemetry using the correct InfluxDB measurement name with timestamp
func createTestTelemetryByTypeWithTimestamp(deviceID string, measurementType models.MeasurementType, value float64, timestamp time.Time) models.Telemetry {
	return models.Telemetry{
		Measurement: measurementType.InfluxMeasurementName(),
		Fields: map[string]any{
			"value": value,
		},
		Tags: map[string]string{
			"device_id": deviceID,
			"test":      "integration",
		},
		Timestamp: timestamp,
	}
}

func createTestTelemetryWithMetadata(deviceID, measurement string, value float64, deviceType, location string, timestamp time.Time) models.Telemetry {
	return models.Telemetry{
		Measurement: measurement,
		Fields: map[string]any{
			"value": value,
		},
		Tags: map[string]string{
			"device_id":   deviceID,
			"device_type": deviceType,
			"location":    location,
			"test":        "metadata",
		},
		Timestamp: timestamp,
	}
}

// createTestTelemetryByTypeWithMetadata creates test telemetry using the correct InfluxDB measurement name with metadata
func createTestTelemetryByTypeWithMetadata(deviceID string, measurementType models.MeasurementType, value float64, deviceType, location string, timestamp time.Time) models.Telemetry {
	return models.Telemetry{
		Measurement: measurementType.InfluxMeasurementName(),
		Fields: map[string]any{
			"value": value,
		},
		Tags: map[string]string{
			"device_id":   deviceID,
			"device_type": deviceType,
			"location":    location,
			"test":        "metadata",
		},
		Timestamp: timestamp,
	}
}

func TestNewTelemetryStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: Config{
				URL:          "http://localhost:8181",
				Organization: "testorg",
				Bucket:       "testbucket",
				Token:        "testtoken",
				WriteTimeout: 30 * time.Second,
				QueryTimeout: 60 * time.Second,
			},
			expectError: false,
		},
		{
			name: "missing URL",
			config: Config{
				Organization: "testorg",
				Bucket:       "testbucket",
				Token:        "testtoken",
			},
			expectError: true,
			errorMsg:    "URL is required",
		},
		{
			name: "missing organization",
			config: Config{
				URL:    "http://localhost:8181",
				Bucket: "testbucket",
				Token:  "testtoken",
			},
			expectError: true,
			errorMsg:    "organization is required",
		},
		{
			name: "missing bucket",
			config: Config{
				URL:          "http://localhost:8181",
				Organization: "testorg",
				Token:        "testtoken",
			},
			expectError: true,
			errorMsg:    "bucket is required",
		},
		{
			name: "missing token",
			config: Config{
				URL:          "http://localhost:8181",
				Organization: "testorg",
				Bucket:       "testbucket",
			},
			expectError: true,
			errorMsg:    "token is required",
		},
		{
			name: "invalid URL",
			config: Config{
				URL:          "://invalid-url",
				Organization: "testorg",
				Bucket:       "testbucket",
				Token:        "testtoken",
			},
			expectError: true,
			errorMsg:    "invalid URL format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, err := NewTelemetryStore(tt.config)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, store)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, store)
				assert.NotNil(t, store.client)
				assert.NotNil(t, store.logger)

				if store != nil {
					_ = store.Close()
				}
			}
		})
	}
}

func TestInfluxTelemetryStore_Store_EmptyData(t *testing.T) {
	t.Parallel()

	config := Config{
		URL:          "http://localhost:8181",
		Organization: "testorg",
		Bucket:       "testbucket",
		Token:        "testtoken",
		WriteTimeout: 30 * time.Second,
		QueryTimeout: 60 * time.Second,
	}

	store, err := NewTelemetryStore(config)
	require.NoError(t, err)
	defer store.Close()

	err = store.Store(t.Context())
	require.NoError(t, err)
}

func TestInfluxTelemetryStore_Store_Data(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	testData := []models.Telemetry{
		createTestTelemetry("device1", "temperature", 25.5),
		createTestTelemetry("device1", "hashrate", 100.0),
		createTestTelemetry("device2", "temperature", 30.2),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "telemetry data")
	t.Logf("Successfully stored %d telemetry points to InfluxDB", len(testData))
}

func TestInfluxTelemetryStore_Store_SinglePoint(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	singlePoint := createTestTelemetry("test-device", "temperature", 22.1)

	err := store.Store(ctx, singlePoint)
	require.NoError(t, err, "Should successfully store single telemetry point")

	t.Log("Successfully stored single telemetry point to InfluxDB")
}

func TestInfluxTelemetryStore_GetLatestTelemetry(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	baseTime := time.Now().Add(-1 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeTemperature, 25.5, baseTime.Add(-30*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(-20*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeHashrate, 100.0, baseTime.Add(-15*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device2", models.MeasurementTypeTemperature, 30.2, baseTime.Add(-10*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device2", models.MeasurementTypeHashrate, 150.0, baseTime.Add(-5*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "telemetry data")

	query := models.LatestTelemetryQuery{
		DeviceIDs: []models.DeviceIdentifier{"device1", "device2"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
			models.MeasurementTypeHashrate,
		},
		MaxAge: durationPtr(2 * time.Hour),
	}

	results, err := store.GetLatestTelemetry(ctx, query)
	require.NoError(t, err, "GetLatestTelemetry should succeed - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d latest telemetry points", len(results))

	deviceIDs := make(map[string]bool)
	for _, result := range results {
		if deviceID, exists := result.Tags["device_id"]; exists {
			deviceIDs[deviceID] = true
		}
	}

	assert.NotEmpty(t, deviceIDs, "Should have telemetry from test devices")
	assert.Len(t, deviceIDs, 2, "Should have telemetry from exactly 2 devices")
	t.Logf("Found data from devices: %v", deviceIDs)
}

func TestInfluxTelemetryStore_GetTimeSeriesTelemetry(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	baseTime := time.Now().Add(-2 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeTemperature, 25.0, baseTime),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeTemperature, 25.5, baseTime.Add(10*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(20*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeHashrate, 100.0, baseTime.Add(5*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeHashrate, 105.0, baseTime.Add(15*time.Minute)),

		createTestTelemetryByTypeWithTimestamp("device2", models.MeasurementTypeTemperature, 26.0, baseTime.Add(20*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device2", models.MeasurementTypeHashrate, 100.0, baseTime.Add(5*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device2", models.MeasurementTypeHashrate, 105.0, baseTime.Add(15*time.Minute)),

		// out of range data
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeHashrate, 100.0, baseTime.Add(-6*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeHashrate, 105.0, baseTime.Add(31*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeTemperature, 25.5, baseTime.Add(-7*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(30*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "time series data")

	startTime := baseTime.Add(-5 * time.Minute)
	endTime := baseTime.Add(30 * time.Minute)
	limit := 100

	query := models.TimeSeriesTelemetryQuery{
		DeviceIDs: []models.DeviceIdentifier{"device1"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
			models.MeasurementTypeHashrate,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Limit: &limit,
	}

	results, err := store.GetTimeSeriesTelemetry(ctx, query)
	require.NoError(t, err, "GetTimeSeriesTelemetry should succeed - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d time series telemetry points", len(results))

	require.Len(t, results, 6, "Should retrieve exactly 6 telemetry points")
	for i := 1; i < len(results); i++ {
		assert.True(t, results[i].Timestamp.After(results[i-1].Timestamp) || results[i].Timestamp.Equal(results[i-1].Timestamp),
			"Results should be ordered by time ASC")
	}
}

func TestInfluxTelemetryStore_GetTelemetryMetadata(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data with metadata tags
	baseTime := time.Now().Add(-30 * time.Minute)
	testData := []models.Telemetry{
		createTestTelemetryByTypeWithMetadata("device1", models.MeasurementTypeTemperature, 25.5, "miner", "datacenter1", baseTime.Add(-20*time.Minute)),
		createTestTelemetryByTypeWithMetadata("device1", models.MeasurementTypeHashrate, 100.0, "miner", "datacenter1", baseTime.Add(-10*time.Minute)),
		createTestTelemetryByTypeWithMetadata("device2", models.MeasurementTypeTemperature, 30.2, "controller", "datacenter2", baseTime),
		createTestTelemetryByTypeWithMetadata("device3", models.MeasurementTypeTemperature, 18.2, "controller", "datacenter2", baseTime),
		createTestTelemetryByTypeWithMetadata("device3", models.MeasurementTypePower, 18.2, "controller", "datacenter2", baseTime),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "telemetry with metadata")

	// Test GetTelemetryMetadata
	query := models.MetadataQuery{
		DeviceIDs: []models.DeviceIdentifier{"device1", "device2"},
	}

	results, err := store.GetTelemetryMetadata(ctx, query)
	require.NoError(t, err, "GetTelemetryMetadata should succeed - if this fails, there's a bug in the implementation")

	// We should get results
	t.Logf("Retrieved metadata for %d devices", len(results))

	// Verify we have metadata for our test devices
	deviceIDs := make(map[string]bool)
	for _, result := range results {
		deviceIDs[string(result.DeviceID)] = true
		assert.NotEmpty(t, result.DeviceID, "DeviceID should not be empty")
		assert.False(t, result.LastSeen.IsZero(), "LastSeen should not be zero")
	}

	// We should have metadata for at least one of our test devices
	assert.NotEmpty(t, deviceIDs, "Should have metadata for test devices")
	assert.NotContains(t, deviceIDs, "device3", "Should not have metadata for device3 since it wasn't queried")
}

func TestInfluxTelemetryStore_StreamTelemetryUpdates(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	testCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Create initial telemetry data to ensure the table is created.
	data := []models.Telemetry{
		createTestTelemetryByType("not-the-device-you-are-looking-for", models.MeasurementTypeTemperature, 1000.5),
		createTestTelemetryByType("not-the-device-you-are-looking-for", models.MeasurementTypePower, 20.0),
	}
	err := store.Store(testCtx, data...)
	require.NoError(t, err, "Should successfully store initial telemetry data")
	time.Sleep(100 * time.Millisecond) // Give InfluxDB time to create the table

	query := models.StreamQuery{
		DeviceIDs: []models.DeviceIdentifier{"stream-device1"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
		},
		IncludeHeartbeat:  true,
		HeartbeatInterval: durationPtr(100 * time.Millisecond),
	}

	updateChan, err := store.StreamTelemetryUpdates(testCtx, query)
	require.NoError(t, err, "Should successfully start streaming")
	require.NotNil(t, updateChan, "Should return a valid update channel")

	// Store data in a goroutine after a short delay to simulate real-world scenario
	go func() {
		time.Sleep(300 * time.Millisecond) // Give stream time to start polling
		testData := createTestTelemetryByType("stream-device1", models.MeasurementTypeTemperature, 25.5)
		err := store.Store(ctx, testData)
		if err != nil {
			t.Logf("Failed to store test data: %v", err)
		} else {
			t.Log("Stored test data for streaming")
		}
	}()

	var updates []models.TelemetryUpdate
	var heartbeatCount, telemetryCount, errorCount int
	foundExpectedData := false

	// Collect updates for up to 1 seconds
	timeout := time.After(1 * time.Second)

	collectingUpdates := true
	for collectingUpdates {
		select {
		case update, ok := <-updateChan:
			if !ok {
				t.Log("Channel closed")
				collectingUpdates = false
				continue
			}

			updates = append(updates, update)

			//nolint:exhaustive // This is limited to just this test case
			switch update.Type {
			case models.UpdateTypeHeartbeat:
				heartbeatCount++
				t.Log("Received heartbeat")
			case models.UpdateTypeTelemetry:
				telemetryCount++
				t.Logf("Received telemetry from device %s", update.DeviceID)

				// Check if this is our expected data
				if update.DeviceID == "stream-device1" && update.Data != nil {
					if update.Data.Measurement == models.MeasurementTypeTemperature.InfluxMeasurementName() {
						if valueField, exists := update.Data.Fields["value"]; exists {
							if value, ok := valueField.(float64); ok && value == 25.5 {
								foundExpectedData = true
								t.Log("✓ Found our stored data in the stream!")
								collectingUpdates = false
							}
						}
					}
				}
			case models.UpdateTypeError:
				errorCount++
				if update.Error != nil {
					t.Logf("Received error: %s", *update.Error)
				}
			}

		case <-timeout:
			t.Log("Test timeout reached")
			collectingUpdates = false
		case <-testCtx.Done():
			t.Log("Context cancelled")
			collectingUpdates = false
		}
	}

	t.Logf("Received %d updates: %d heartbeat, %d telemetry, %d error",
		len(updates), heartbeatCount, telemetryCount, errorCount)

	assert.NotEmpty(t, updates, "Should receive some updates")
	assert.Positive(t, heartbeatCount, "Should receive at least one heartbeat")

	assert.True(t, foundExpectedData, "Should find our expected telemetry data in the stream")
}

func TestInfluxTelemetryStore_GetAggregatedTelemetry(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	t.Cleanup(func() {
		cleanupIntegrationTest(t, store, container)
	})

	// Create test data for aggregation
	baseTime := time.Now().Add(-1 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryByTypeWithTimestamp("agg-device1", models.MeasurementTypeTemperature, 25.0, baseTime.Add(-30*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(-20*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device1", models.MeasurementTypeTemperature, 27.0, baseTime.Add(-10*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device2", models.MeasurementTypeTemperature, 30.0, baseTime.Add(-25*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device2", models.MeasurementTypeTemperature, 32.0, baseTime.Add(-15*time.Minute)),

		createTestTelemetryByTypeWithTimestamp("agg-device1", models.MeasurementTypeHashrate, 1000.0, baseTime.Add(-10*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device1", models.MeasurementTypeTemperature, 99.0, baseTime.Add(-41*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device2", models.MeasurementTypeTemperature, 30.0, baseTime.Add(-25*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device2", models.MeasurementTypeTemperature, 32.0, baseTime.Add(-15*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device3", models.MeasurementTypeTemperature, 33.0, baseTime.Add(-25*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("agg-device3", models.MeasurementTypeTemperature, 99.0, baseTime.Add(-15*time.Minute)),
	}

	// Store the test data
	err := store.Store(ctx, testData...)
	require.NoError(t, err, "Should successfully store aggregation test data")

	time.Sleep(100 * time.Millisecond) // Give InfluxDB time to process writes

	// Test GetAggregatedTelemetry with different aggregation types
	testCases := []struct {
		name    string
		aggType models.AggregationType
		d1Value float64
		d2Value float64
	}{
		{"Average", models.AggregationTypeAverage, 26, 31},
		{"Min", models.AggregationTypeMin, 25.0, 30.0},
		{"Max", models.AggregationTypeMax, 27, 32.0},
		{"Sum", models.AggregationTypeSum, 78, 62},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			startTime := baseTime.Add(-40 * time.Minute)
			endTime := baseTime

			query := models.AggregationQuery{
				DeviceIDs: []models.DeviceIdentifier{"agg-device1", "agg-device2"},
				MeasurementTypes: []models.MeasurementType{
					models.MeasurementTypeTemperature,
				},
				TimeRange: models.TimeRange{
					StartTime: &startTime,
					EndTime:   &endTime,
				},
				AggregationType: tc.aggType,
			}

			results, err := store.GetAggregatedTelemetry(ctx, query)
			require.NoError(t, err, "GetAggregatedTelemetry should succeed for %s - if this fails, there's a bug in the implementation", tc.name)

			t.Logf("Retrieved %d aggregated telemetry points for %s", len(results), tc.name)

			for _, result := range results {
				assert.NotEmpty(t, result.DeviceID, "DeviceID should not be empty")
				assert.NotEmpty(t, result.MeasurementType, "MeasurementType should not be empty")
				assert.GreaterOrEqual(t, result.DataPoints, 0, "DataPoints should be >= 0")
				if result.DeviceID == "agg-device1" {
					assert.Equal(t, 3, result.DataPoints, "Device1 should have 3 data points for %s aggregation", tc.name)
					assert.InDelta(t, tc.d1Value, result.Value, 0.001, "Device1 value should match expected for %s aggregation", tc.name)
				}
				if result.DeviceID == "agg-device2" {
					assert.Equal(t, 2, result.DataPoints, "Device2 should have 2 data points for %s aggregation", tc.name)
					assert.InDelta(t, tc.d2Value, result.Value, 0.001, "Device2 value should match expected for %s aggregation", tc.name)
				}
			}
		})
	}
}

func TestInfluxTelemetryStore_Close(t *testing.T) {
	t.Parallel()

	store, container, _ := setupIntegrationTest(t)
	// Note: Don't use cleanupIntegrationTest here since we want to test Close() explicitly
	defer func() {
		if container != nil {
			if err := container.Terminate(t.Context()); err != nil {
				t.Logf("Failed to terminate container: %v", err)
			}
		}
	}()

	// Test Close method
	err := store.Close()
	require.NoError(t, err, "Should successfully close the store")

	t.Log("Store closed successfully")
}

func TestInfluxTelemetryStore_GetCombinedMetrics_NonCumulative(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data for non-cumulative measurements (temperature, voltage)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(-2 * time.Hour)
	testData := []models.Telemetry{
		// Temperature data - non-cumulative
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeTemperature, 25.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(-80*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeTemperature, 27.0, baseTime.Add(-69*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypeTemperature, 30.0, baseTime.Add(-85*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypeTemperature, 32.0, baseTime.Add(-75*time.Minute)),

		// Voltage data - non-cumulative
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeVoltage, 12.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeVoltage, 12.5, baseTime.Add(-80*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypeVoltage, 11.8, baseTime.Add(-85*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypeVoltage, 12.2, baseTime.Add(-75*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics non-cumulative data")

	startTime := baseTime.Add(-100 * time.Minute)
	endTime := baseTime.Add(-60 * time.Minute)

	query := models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{"combined-device1", "combined-device2"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
			models.MeasurementTypeVoltage,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Granularity: 10 * time.Minute,
		PageSize:    50,
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed for non-cumulative measurements - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d combined metrics", len(result.Metrics))

	// Verify we have metrics
	require.NotEmpty(t, result.Metrics, "Should have combined metrics")

	assert.True(t, slices.IsSortedFunc(result.Metrics, func(a, b models.Metric) int {
		return a.OpenTime.Compare(b.OpenTime)
	}), "Metrics should be sorted by OpenTime")

	slices.SortFunc(result.Metrics, func(a, b models.Metric) int {
		if a.MeasurementType != b.MeasurementType {
			return int(a.MeasurementType) - int(b.MeasurementType)
		}
		return a.OpenTime.Compare(b.OpenTime)
	})

	expected := []struct {
		MeasurementType models.MeasurementType
		Sum             float64
		Avg             float64
		Min             float64
		Max             float64
		OpenTime        time.Time
	}{
		{models.MeasurementTypeTemperature, 55, 27.5, 25, 30, baseTime.Add(-90 * time.Minute)},
		{models.MeasurementTypeTemperature, 58, 29, 26, 32, baseTime.Add(-80 * time.Minute)},
		{models.MeasurementTypeTemperature, 27, 27, 27, 27, baseTime.Add(-70 * time.Minute)},
		{models.MeasurementTypeVoltage, 23.8, 11.9, 11.8, 12, baseTime.Add(-90 * time.Minute)},
		{models.MeasurementTypeVoltage, 24.7, 12.35, 12.2, 12.5, baseTime.Add(-80 * time.Minute)},
	}

	assert.Len(t, result.Metrics, len(expected), "Should have correct number of metrics")

	for i, metric := range result.Metrics {
		assert.Equal(t, expected[i].MeasurementType, metric.MeasurementType, "Metric should have correct measurement type")
		assert.WithinDuration(t, expected[i].OpenTime, metric.OpenTime, 1*time.Second, "Metric should have correct open time")
		assert.NotEmpty(t, metric.AggregatedValues, "Metric should have aggregated values")
		for _, aggValue := range metric.AggregatedValues {
			//nolint:exhaustive // This is limited to just this test case
			switch aggValue.Type {
			case models.AggregationTypeSum:
				assert.InDelta(t, expected[i].Sum, aggValue.Value, 0.1, "Sum should match expected value")
			case models.AggregationTypeAverage:
				assert.InDelta(t, expected[i].Avg, aggValue.Value, 0.1, "Average should match expected value")
			case models.AggregationTypeMin:
				assert.InDelta(t, expected[i].Min, aggValue.Value, 0.1, "Min should match expected value")
			case models.AggregationTypeMax:
				assert.InDelta(t, expected[i].Max, aggValue.Value, 0.1, "Max should match expected value")
			default:
				t.Errorf("Unexpected aggregation type: %s", aggValue.Type)
			}
		}
		assert.False(t, metric.OpenTime.IsZero(), "OpenTime should not be zero")
	}
}

func TestInfluxTelemetryStore_GetCombinedMetrics_Cumulative(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data for cumulative measurements (power, hashrate)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(-2 * time.Hour)
	testData := []models.Telemetry{
		// Power data - cumulative
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypePower, 100.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypePower, 105.0, baseTime.Add(-80*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypePower, 120.0, baseTime.Add(-72*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypePower, 110.0, baseTime.Add(-69*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypePower, 200.0, baseTime.Add(-85*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypePower, 210.0, baseTime.Add(-75*time.Minute)),

		// Hashrate data - cumulative
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeHashrate, 1000.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeHashrate, 1050.0, baseTime.Add(-80*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device1", models.MeasurementTypeHashrate, 1005.0, baseTime.Add(-78*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypeHashrate, 2000.0, baseTime.Add(-85*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("combined-device2", models.MeasurementTypeHashrate, 2100.0, baseTime.Add(-75*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics cumulative data")

	startTime := baseTime.Add(-100 * time.Minute)
	endTime := baseTime.Add(-60 * time.Minute)

	query := models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{"combined-device1", "combined-device2"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypePower,
			models.MeasurementTypeHashrate,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Granularity: 10 * time.Minute,
		PageSize:    50,
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed for cumulative measurements - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d combined metrics", len(result.Metrics))

	// Verify we have metrics
	require.NotEmpty(t, result.Metrics, "Should have combined metrics")

	assert.True(t, slices.IsSortedFunc(result.Metrics, func(a, b models.Metric) int {
		return a.OpenTime.Compare(b.OpenTime)
	}), "Metrics should be sorted by OpenTime")

	slices.SortFunc(result.Metrics, func(a, b models.Metric) int {
		if a.MeasurementType != b.MeasurementType {
			return int(a.MeasurementType) - int(b.MeasurementType)
		}
		return a.OpenTime.Compare(b.OpenTime)
	})

	// Expected values for power metrics (cumulative template uses different field names)
	expected := []struct {
		MeasurementType models.MeasurementType
		bucketTime      time.Time
		total           float64 // sum of last values per device
		minTotal        float64 // sum of min values per device
		maxTotal        float64 // sum of max values per device
		meanChange      float64 // average of (max - min) per device
	}{
		{models.MeasurementTypeHashrate, baseTime.Add(-90 * time.Minute), 3000.0, 3000.0, 3000.0, 0.0},  // device1: 1000, device2: 2000 (no change in bucket)
		{models.MeasurementTypeHashrate, baseTime.Add(-80 * time.Minute), 3105.0, 3105.0, 3150.0, 22.5}, // device1: 1050 (change: 50), device2:
		{models.MeasurementTypePower, baseTime.Add(-90 * time.Minute), 300.0, 300.0, 300.0, 0.0},        // device1: 100, device2: 200 (no change in bucket)
		{models.MeasurementTypePower, baseTime.Add(-80 * time.Minute), 330.0, 315.0, 330.0, 7.5},        // device1: 105 (change: 5), device2: 210 (change: 10), mean: 7.5
		{models.MeasurementTypePower, baseTime.Add(-70 * time.Minute), 110.0, 110.0, 110.0, 0.0},        // device1: 110 only
	}

	require.Len(t, result.Metrics, len(expected), "Should have correct number of metrics")
	for i, metric := range result.Metrics {
		assert.Equal(t, expected[i].MeasurementType, metric.MeasurementType, "Metric should have correct measurement type")
		assert.WithinDuration(t, expected[i].bucketTime, metric.OpenTime, 1*time.Second, "Metric should have correct open time")
		assert.NotEmpty(t, metric.AggregatedValues, "Metric should have aggregated values")

		for _, aggValue := range metric.AggregatedValues {
			//nolint:exhaustive // This is limited to just this test case
			switch aggValue.Type {
			case models.AggregationTypeSum:
				assert.InDelta(t, expected[i].total, aggValue.Value, 0.1, "Total should match expected value for %s", aggValue.Type)
			case models.AggregationTypeMin:
				assert.InDelta(t, expected[i].minTotal, aggValue.Value, 0.1, "Min total should match expected value for %s", aggValue.Type)
			case models.AggregationTypeMax:
				assert.InDelta(t, expected[i].maxTotal, aggValue.Value, 0.1, "Max total should match expected value for %s", aggValue.Type)
			case models.AggregationTypeMeanChange:
				assert.InDelta(t, expected[i].meanChange, aggValue.Value, 0.1, "Mean change should match expected value for %s", aggValue.Type)
			default:
				t.Errorf("Unexpected aggregation type: %s", aggValue.Type)
			}
		}
	}
}

func TestInfluxTelemetryStore_GetCombinedMetrics_MixedMeasurements(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data for mixed cumulative and non-cumulative measurements
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(-2 * time.Hour)
	testData := []models.Telemetry{
		// Temperature (non-cumulative) - bucket at -90 and -75
		createTestTelemetryByTypeWithTimestamp("mixed-device1", models.MeasurementTypeTemperature, 25.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(-79*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device2", models.MeasurementTypeTemperature, 30.0, baseTime.Add(-85*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device2", models.MeasurementTypeTemperature, 33.0, baseTime.Add(-70*time.Minute)),

		// Power (cumulative) - bucket at -90 and -75
		createTestTelemetryByTypeWithTimestamp("mixed-device1", models.MeasurementTypePower, 100.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device1", models.MeasurementTypePower, 105.0, baseTime.Add(-79*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device2", models.MeasurementTypePower, 200.0, baseTime.Add(-85*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device3", models.MeasurementTypePower, 1000.0, baseTime.Add(-70*time.Minute)),

		// Voltage (non-cumulative) - bucket at -90
		createTestTelemetryByTypeWithTimestamp("mixed-device1", models.MeasurementTypeVoltage, 12.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device2", models.MeasurementTypeVoltage, 11.8, baseTime.Add(-85*time.Minute)),

		// Hashrate (cumulative) - bucket at -90
		createTestTelemetryByTypeWithTimestamp("mixed-device1", models.MeasurementTypeHashrate, 1000.0, baseTime.Add(-89*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("mixed-device2", models.MeasurementTypeHashrate, 2000.0, baseTime.Add(-85*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics mixed data")

	startTime := baseTime.Add(-100 * time.Minute)
	endTime := baseTime.Add(-60 * time.Minute)

	query := models.CombinedMetricsQuery{
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature, // non-cumulative
			models.MeasurementTypePower,       // cumulative
			models.MeasurementTypeVoltage,     // non-cumulative
			models.MeasurementTypeHashrate,    // cumulative
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Granularity: 15 * time.Minute,
		PageSize:    100,
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed for mixed measurements - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d combined metrics", len(result.Metrics))

	// Verify we have metrics
	require.NotEmpty(t, result.Metrics, "Should have combined metrics")

	assert.True(t, slices.IsSortedFunc(result.Metrics, func(a, b models.Metric) int {
		return a.OpenTime.Compare(b.OpenTime)
	}), "Metrics should be sorted by OpenTime")

	slices.SortFunc(result.Metrics, func(a, b models.Metric) int {
		if a.MeasurementType != b.MeasurementType {
			return int(a.MeasurementType) - int(b.MeasurementType)
		}
		return a.OpenTime.Compare(b.OpenTime)
	})

	type cumulativeMetric struct {
		measurementType models.MeasurementType
		bucketTime      time.Time
		total           float64 // sum of last values per device
		minTotal        float64 // sum of min values per device
		maxTotal        float64 // sum of max values per device
		meanChange      float64 // mean change per device
	}

	type nonCumulativeMetric struct {
		measurementType models.MeasurementType
		bucketTime      time.Time
		sum             float64 // sum of values per device
		avg             float64 // average of values per device
		min             float64 // min value per device
		max             float64 // max value per device
	}

	expectedMetrics := []struct {
		cumulative    *cumulativeMetric
		nonCumulative *nonCumulativeMetric
	}{
		{
			nonCumulative: &nonCumulativeMetric{
				measurementType: models.MeasurementTypeTemperature,
				bucketTime:      baseTime.Add(-90 * time.Minute),
				sum:             56.0,
				avg:             28.0,
				min:             26.0, // this is based on latest value for non-cumulative
				max:             30.0,
			},
		},
		{
			nonCumulative: &nonCumulativeMetric{
				measurementType: models.MeasurementTypeTemperature,
				bucketTime:      baseTime.Add(-75 * time.Minute),
				sum:             33.0, // device2: 33 only
				avg:             33.0, // (33) / 1
				min:             33.0, // min of device2: 33
				max:             33.0, // max of device2: 33
			},
		},
		{
			cumulative: &cumulativeMetric{
				measurementType: models.MeasurementTypeHashrate,
				bucketTime:      baseTime.Add(-90 * time.Minute),
				total:           3000.0, // device1: 1000 + device2: 2000
				minTotal:        3000.0, // device1: 1000 + device2: 2000
				maxTotal:        3000.0, // device1: 1000 + device2: 2000
				meanChange:      0.0,    // no change in bucket
			},
		},
		{
			cumulative: &cumulativeMetric{
				measurementType: models.MeasurementTypePower,
				bucketTime:      baseTime.Add(-90 * time.Minute),
				total:           305.0, // device1: 100 + device2: 200 + device3: 0
				minTotal:        300.0, //	 device1: 100 + device2: 200 + device3: 0
				maxTotal:        305.0, // device1: 100 + device2: 200 + device3: 0
				meanChange:      2.5,   // no change in bucket
			},
		},
		{
			cumulative: &cumulativeMetric{
				measurementType: models.MeasurementTypePower,
				bucketTime:      baseTime.Add(-75 * time.Minute),
				total:           1000.0, // device1: 105 only
				minTotal:        1000.0, // device1: 105 only
				maxTotal:        1000.0, // device1: 105 only
				meanChange:      0.0,    // no change in bucket
			},
		},
		{
			nonCumulative: &nonCumulativeMetric{
				measurementType: models.MeasurementTypeVoltage,
				bucketTime:      baseTime.Add(-90 * time.Minute),
				sum:             23.8, // device1: 12.0 + device2: 11.8
				avg:             11.9, // (12.0 + 11.8) / 2
				min:             11.8, // min of device1: 12.0, device2: 11.8
				max:             12.0, // max of device1: 12.0, device2: 11.8
			},
		},
	}

	require.Len(t, result.Metrics, len(expectedMetrics), "Should have correct number of metrics")

	for i, expected := range expectedMetrics {
		if expected.cumulative != nil {
			assert.Equal(t, expected.cumulative.measurementType, result.Metrics[i].MeasurementType, "Metric should have correct measurement type")
			assert.WithinDuration(t, expected.cumulative.bucketTime, result.Metrics[i].OpenTime, 1*time.Second, "Metric should have correct bucket time")
			for _, aggValue := range result.Metrics[i].AggregatedValues {
				//nolint:exhaustive // This is limited to just this test case
				switch aggValue.Type {
				case models.AggregationTypeSum:
					assert.InDelta(t, expected.cumulative.total, aggValue.Value, 0.1, "Total should match expected value for %s", aggValue.Type)
				case models.AggregationTypeMin:
					assert.InDelta(t, expected.cumulative.minTotal, aggValue.Value, 0.1, "Min total should match expected value for %s", aggValue.Type)
				case models.AggregationTypeMax:
					assert.InDelta(t, expected.cumulative.maxTotal, aggValue.Value, 0.1, "Max total should match expected value for %s", aggValue.Type)
				case models.AggregationTypeMeanChange:
					assert.InDelta(t, expected.cumulative.meanChange, aggValue.Value, 0.1, "Mean change should match expected value for %s", aggValue.Type)
				default:
					t.Errorf("Unexpected aggregation type: %s", aggValue.Type)
				}
			}
		}
		if expected.nonCumulative != nil {
			assert.Equal(t, expected.nonCumulative.measurementType, result.Metrics[i].MeasurementType, "Metric should have correct measurement type")
			assert.WithinDuration(t, expected.nonCumulative.bucketTime, result.Metrics[i].OpenTime, 1*time.Second, "Metric should have correct bucket time")
			for _, aggValue := range result.Metrics[i].AggregatedValues {
				//nolint:exhaustive // This is limited to just this test case
				switch aggValue.Type {
				case models.AggregationTypeSum:
					assert.InDelta(t, expected.nonCumulative.sum, aggValue.Value, 0.1, "Sum should match expected value for %s", aggValue.Type)
				case models.AggregationTypeAverage:
					assert.InDelta(t, expected.nonCumulative.avg, aggValue.Value, 0.1, "Average should match expected value for %s", aggValue.Type)
				case models.AggregationTypeMin:
					assert.InDelta(t, expected.nonCumulative.min, aggValue.Value, 0.1, "Min should match expected value for %s", aggValue.Type)
				case models.AggregationTypeMax:
					assert.InDelta(t, expected.nonCumulative.max, aggValue.Value, 0.1, "Max should match expected value for %s", aggValue.Type)
				default:
					t.Errorf("Unexpected aggregation type: %s", aggValue.Type)
				}
			}
		}
	}
}

func TestInfluxTelemetryStore_GetCombinedMetrics_WithAggregationFilter(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(-1 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryByTypeWithTimestamp("filter-device1", models.MeasurementTypeTemperature, 25.0, baseTime.Add(-44*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("filter-device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(-34*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("filter-device2", models.MeasurementTypeTemperature, 30.0, baseTime.Add(-39*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics filter data")

	startTime := baseTime.Add(-50 * time.Minute)
	endTime := baseTime.Add(-30 * time.Minute)

	query := models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{"filter-device1", "filter-device2"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
		},
		AggregationTypes: []models.AggregationType{
			models.AggregationTypeAverage,
			models.AggregationTypeMax,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Granularity: 10 * time.Minute,
		PageSize:    50,
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed with aggregation filter - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d combined metrics with aggregation filter", len(result.Metrics))

	// Verify we have metrics
	require.NotEmpty(t, result.Metrics, "Should have combined metrics")

	assert.True(t, slices.IsSortedFunc(result.Metrics, func(a, b models.Metric) int {
		return a.OpenTime.Compare(b.OpenTime)
	}), "Metrics should be sorted by OpenTime")

	expected := []struct {
		MeasurementType models.MeasurementType
		Avg             float64
		Max             float64
		OpenTime        time.Time
	}{
		{models.MeasurementTypeTemperature, 25.0, 25.0, baseTime.Add(-50 * time.Minute)},
		{models.MeasurementTypeTemperature, 28, 30.0, baseTime.Add(-40 * time.Minute)},
	}

	assert.Len(t, result.Metrics, len(expected), "Should have correct number of metrics")

	for i, metric := range result.Metrics {
		assert.Equal(t, expected[i].MeasurementType, metric.MeasurementType, "Metric should have correct measurement type")
		assert.WithinDuration(t, expected[i].OpenTime, metric.OpenTime, 1*time.Second, "Metric should have correct open time")
		assert.NotEmpty(t, metric.AggregatedValues, "Metric should have aggregated values")

		// Verify only requested aggregation types are present
		aggregationTypes := make(map[models.AggregationType]bool)
		for _, aggValue := range metric.AggregatedValues {
			aggregationTypes[aggValue.Type] = true
			//nolint:exhaustive // This is limited to just this test case
			switch aggValue.Type {
			case models.AggregationTypeAverage:
				assert.InDelta(t, expected[i].Avg, aggValue.Value, 0.1, "Average should match expected value")
			case models.AggregationTypeMax:
				assert.InDelta(t, expected[i].Max, aggValue.Value, 0.1, "Max should match expected value")
			default:
				t.Errorf("Unexpected aggregation type: %s", aggValue.Type)
			}
		}

		// Should only have the requested aggregation types
		assert.Contains(t, aggregationTypes, models.AggregationTypeAverage, "Should have average aggregation")
		assert.Contains(t, aggregationTypes, models.AggregationTypeMax, "Should have max aggregation")
		assert.NotContains(t, aggregationTypes, models.AggregationTypeSum, "Should not have sum aggregation")
		assert.NotContains(t, aggregationTypes, models.AggregationTypeMin, "Should not have min aggregation")
	}
}

func TestInfluxTelemetryStore_GetCombinedMetrics_WithPagination(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data with many time buckets to test pagination
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(-3 * time.Hour)
	var testData []models.Telemetry

	// Create data points every 5 minutes for 2 hours (24 buckets)
	for i := range 24 {
		timestamp := baseTime.Add(time.Duration(i*5) * time.Minute)
		testData = append(testData,
			createTestTelemetryByTypeWithTimestamp("page-device1", models.MeasurementTypeTemperature, 25.0+float64(i), timestamp),
		)
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics pagination data")

	startTime := baseTime.Add(-10 * time.Minute)
	endTime := baseTime.Add(130 * time.Minute)

	// Test first page
	query := models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{"page-device1"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Granularity: 5 * time.Minute,
		PageSize:    10, // Small page size to test pagination
	}

	firstPage, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed for first page - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d metrics on first page", len(firstPage.Metrics))

	// Verify we have metrics
	require.NotEmpty(t, firstPage.Metrics, "Should have combined metrics on first page")

	// Verify sorting
	assert.True(t, slices.IsSortedFunc(firstPage.Metrics, func(a, b models.Metric) int {
		return a.OpenTime.Compare(b.OpenTime)
	}), "First page metrics should be sorted by OpenTime")

	// Should have page size metrics or less
	assert.LessOrEqual(t, len(firstPage.Metrics), 10, "First page should have at most 10 metrics")
	assert.NotEmpty(t, firstPage.NextPageToken, "Should have next page token")

	// Test second page
	query.PaginationToken = firstPage.NextPageToken
	secondPage, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed for second page - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d metrics on second page", len(secondPage.Metrics))

	// Verify we have metrics
	require.NotEmpty(t, secondPage.Metrics, "Should have combined metrics on second page")

	// Verify sorting
	assert.True(t, slices.IsSortedFunc(secondPage.Metrics, func(a, b models.Metric) int {
		return a.OpenTime.Compare(b.OpenTime)
	}), "Second page metrics should be sorted by OpenTime")

	// Verify pagination ordering - second page should have later timestamps
	if len(firstPage.Metrics) > 0 && len(secondPage.Metrics) > 0 {
		lastFirstPageTime := firstPage.Metrics[len(firstPage.Metrics)-1].OpenTime
		firstSecondPageTime := secondPage.Metrics[0].OpenTime
		assert.True(t, firstSecondPageTime.After(lastFirstPageTime) || firstSecondPageTime.Equal(lastFirstPageTime),
			"Second page should have later or equal timestamps")
	}

	// Verify all metrics have proper structure
	for _, metric := range append(firstPage.Metrics, secondPage.Metrics...) {
		assert.Equal(t, models.MeasurementTypeTemperature, metric.MeasurementType, "All metrics should be temperature")
		assert.NotEmpty(t, metric.AggregatedValues, "All metrics should have aggregated values")
		assert.False(t, metric.OpenTime.IsZero(), "OpenTime should not be zero")
	}
}

func TestInfluxTelemetryStore_GetCombinedMetrics_NoDeviceIDs(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data
	baseTime := time.Now().Add(-1 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryByTypeWithTimestamp("org-device1", models.MeasurementTypeTemperature, 25.0, baseTime.Add(-45*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("org-device2", models.MeasurementTypeTemperature, 30.0, baseTime.Add(-40*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics org data")

	startTime := baseTime.Add(-50 * time.Minute)
	endTime := baseTime.Add(-30 * time.Minute)

	// Query without device IDs (should use organization)
	query := models.CombinedMetricsQuery{
		// No DeviceIDs - should use organization
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Granularity: 10 * time.Minute,
		PageSize:    50,
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed without device IDs - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d combined metrics without device IDs", len(result.Metrics))

	// Should still get metrics (organization-wide query)
	// Note: This test may have varying results depending on what other data exists
	// The key is that it should not fail
	assert.NotNil(t, result.Metrics, "Should have metrics array (may be empty)")
}

func TestInfluxTelemetryStore_GetCombinedMetrics_DefaultValues(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data
	baseTime := time.Now().Add(-1 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryByTypeWithTimestamp("default-device1", models.MeasurementTypeTemperature, 25.0, baseTime.Add(-45*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("default-device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(-35*time.Minute)),
		createTestTelemetryByTypeWithTimestamp("default-device1", models.MeasurementTypeHashrate, 100.0, baseTime.Add(-35*time.Minute)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics default values data")

	startTime := baseTime.Add(-50 * time.Minute)

	// Query with minimal parameters (test defaults)
	query := models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{"default-device1"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &baseTime,
		},
		// No Granularity - should default to 1 minute
		// No PageSize - should default to 100
		// No AggregationTypes - should return all
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed with default values - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d combined metrics with default values", len(result.Metrics))

	// Should handle defaults gracefully
	assert.NotNil(t, result.Metrics, "Should have metrics array")
	assert.Equal(t, "", result.NextPageToken, "Should have empty next page token for small result set")
}

func TestInfluxTelemetryStore_GetCombinedMetrics_EmptyResult(t *testing.T) {
	t.Parallel()

	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Query for data that doesn't exist
	baseTime := time.Now().Add(-1 * time.Hour)
	startTime := baseTime.Add(-50 * time.Minute)
	endTime := baseTime.Add(-40 * time.Minute)

	testData := []models.Telemetry{
		createTestTelemetryByTypeWithTimestamp("default-device1", models.MeasurementTypeTemperature, 25.0, baseTime.Add(-2*time.Hour)),
		createTestTelemetryByTypeWithTimestamp("default-device1", models.MeasurementTypeTemperature, 26.0, baseTime.Add(-35*time.Hour)),
		createTestTelemetryByTypeWithTimestamp("default-device1", models.MeasurementTypeHashrate, 100.0, baseTime.Add(-35*time.Hour)),
	}

	storeTestDataWithErrorHandling(ctx, t, store, testData, "combined metrics default values data")

	query := models.CombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{"nonexistent-device"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
		},
		TimeRange: models.TimeRange{
			StartTime: &startTime,
			EndTime:   &endTime,
		},
		Granularity: 5 * time.Minute,
		PageSize:    50,
	}

	result, err := store.GetCombinedMetrics(ctx, query)
	require.NoError(t, err, "GetCombinedMetrics should succeed even with empty result - if this fails, there's a bug in the implementation")

	t.Logf("Retrieved %d combined metrics for empty result", len(result.Metrics))

	// Should return empty result without error
	assert.Empty(t, result.Metrics, "Should have empty metrics for nonexistent data")
	assert.Equal(t, "", result.NextPageToken, "Should have empty next page token for empty result")
}
