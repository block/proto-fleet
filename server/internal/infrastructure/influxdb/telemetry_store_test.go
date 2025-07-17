package influxdb

import (
	"context"
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

	t.Parallel()

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

func TestNewTelemetryStore(t *testing.T) {
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
	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	singlePoint := createTestTelemetry("test-device", "temperature", 22.1)

	err := store.Store(ctx, singlePoint)
	require.NoError(t, err, "Should successfully store single telemetry point")

	t.Log("Successfully stored single telemetry point to InfluxDB")
}

func TestInfluxTelemetryStore_GetLatestTelemetry(t *testing.T) {
	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	baseTime := time.Now().Add(-1 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryWithTimestamp("device1", "temperature", 25.5, baseTime.Add(-30*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "temperature", 26.0, baseTime.Add(-20*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "hashrate", 100.0, baseTime.Add(-15*time.Minute)),
		createTestTelemetryWithTimestamp("device2", "temperature", 30.2, baseTime.Add(-10*time.Minute)),
		createTestTelemetryWithTimestamp("device2", "hashrate", 150.0, baseTime.Add(-5*time.Minute)),
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
	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	baseTime := time.Now().Add(-2 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryWithTimestamp("device1", "temperature", 25.0, baseTime),
		createTestTelemetryWithTimestamp("device1", "temperature", 25.5, baseTime.Add(10*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "temperature", 26.0, baseTime.Add(20*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "hashrate", 100.0, baseTime.Add(5*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "hashrate", 105.0, baseTime.Add(15*time.Minute)),

		createTestTelemetryWithTimestamp("device2", "temperature", 26.0, baseTime.Add(20*time.Minute)),
		createTestTelemetryWithTimestamp("device2", "hashrate", 100.0, baseTime.Add(5*time.Minute)),
		createTestTelemetryWithTimestamp("device2", "hashrate", 105.0, baseTime.Add(15*time.Minute)),

		// out of range data
		createTestTelemetryWithTimestamp("device1", "hashrate", 100.0, baseTime.Add(-6*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "hashrate", 105.0, baseTime.Add(31*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "temperature", 25.5, baseTime.Add(-7*time.Minute)),
		createTestTelemetryWithTimestamp("device1", "temperature", 26.0, baseTime.Add(30*time.Minute)),
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
	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	// Create test data with metadata tags
	baseTime := time.Now().Add(-30 * time.Minute)
	testData := []models.Telemetry{
		createTestTelemetryWithMetadata("device1", "temperature", 25.5, "miner", "datacenter1", baseTime.Add(-20*time.Minute)),
		createTestTelemetryWithMetadata("device1", "hashrate", 100.0, "miner", "datacenter1", baseTime.Add(-10*time.Minute)),
		createTestTelemetryWithMetadata("device2", "temperature", 30.2, "controller", "datacenter2", baseTime),
		createTestTelemetryWithMetadata("device3", "temperature", 18.2, "controller", "datacenter2", baseTime),
		createTestTelemetryWithMetadata("device3", "power", 18.2, "controller", "datacenter2", baseTime),
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
	store, container, ctx := setupIntegrationTest(t)
	defer cleanupIntegrationTest(t, store, container)

	testCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Create initial telemetry data to ensure the table is created.
	data := []models.Telemetry{
		createTestTelemetry("not-the-device-you-are-looking-for", "temperature", 1000.5),
		createTestTelemetry("not-the-device-you-are-looking-for", "power", 20.0),
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
		testData := createTestTelemetry("stream-device1", "temperature", 25.5)
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
					if update.Data.Measurement == "temperature" {
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
	store, container, ctx := setupIntegrationTest(t)
	t.Cleanup(func() {
		cleanupIntegrationTest(t, store, container)
	})

	// Create test data for aggregation
	baseTime := time.Now().Add(-1 * time.Hour)
	testData := []models.Telemetry{
		createTestTelemetryWithTimestamp("agg-device1", "temperature", 25.0, baseTime.Add(-30*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device1", "temperature", 26.0, baseTime.Add(-20*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device1", "temperature", 27.0, baseTime.Add(-10*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device2", "temperature", 30.0, baseTime.Add(-25*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device2", "temperature", 32.0, baseTime.Add(-15*time.Minute)),

		createTestTelemetryWithTimestamp("agg-device1", "hashrate", 1000.0, baseTime.Add(-10*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device1", "temperature", 99.0, baseTime.Add(-41*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device2", "temperature", 30.0, baseTime.Add(-25*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device2", "temperature", 32.0, baseTime.Add(-15*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device3", "temperature", 33.0, baseTime.Add(-25*time.Minute)),
		createTestTelemetryWithTimestamp("agg-device3", "temperature", 99.0, baseTime.Add(-15*time.Minute)),
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
