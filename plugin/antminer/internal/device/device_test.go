package device

import (
	"math"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/antminer/internal/types"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/mocks"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants to reduce duplication
const (
	testDeviceID     = "test-device-001"
	testHost         = "192.168.1.100"
	testUsername     = "admin"
	testPassword     = "password"
	testFirmware     = "test-firmware"
	testModel        = "Antminer S19"
	testManufacturer = "Bitmain"
)

// testDeviceInfo returns a standard DeviceInfo for testing
func testDeviceInfo() sdk.DeviceInfo {
	return sdk.DeviceInfo{
		Host:         testHost,
		Port:         80,
		URLScheme:    "http",
		Model:        testModel,
		Manufacturer: testManufacturer,
		Type:         sdk.DeviceTypeASIC,
	}
}

// testCredentials returns standard credentials for testing
func testCredentials() sdk.UsernamePassword {
	return sdk.UsernamePassword{
		Username: testUsername,
		Password: testPassword,
	}
}

// mockClientFactory creates a client factory that returns the given mock client
func mockClientFactory(mockClient antminer.AntminerClient) types.ClientFactory {
	return func(_ string, _, _ int32, _ string) (antminer.AntminerClient, error) {
		return mockClient, nil
	}
}

// mockClientFactoryWithAssertions creates a client factory with parameter assertions
func mockClientFactoryWithAssertions(t *testing.T, mockClient antminer.AntminerClient) types.ClientFactory {
	return func(host string, rpcPort, webPort int32, urlScheme string) (antminer.AntminerClient, error) {
		assert.Equal(t, testHost, host)
		assert.Equal(t, int32(4028), rpcPort)
		assert.Equal(t, int32(80), webPort)
		assert.Equal(t, "http", urlScheme)
		return mockClient, nil
	}
}

// setupMockForDeviceCreation sets up standard mock expectations for device creation (New only)
func setupMockForDeviceCreation(mockClient *mocks.MockAntminerClient) {
	mockClient.EXPECT().SetCredentials(sdk.UsernamePassword{Username: testUsername, Password: testPassword}).Return(nil)
}

// setupMockForDeviceConnection sets up standard mock expectations for device connection (Connect)
func setupMockForDeviceConnection(mockClient *mocks.MockAntminerClient, status *antminer.Status, telemetry *antminer.Telemetry) {
	mockClient.EXPECT().GetStatus(gomock.Any()).Return(status, nil)

	if telemetry != nil {
		mockClient.EXPECT().GetTelemetry(gomock.Any()).Return(telemetry, nil)
	} else {
		mockClient.EXPECT().GetTelemetry(gomock.Any()).Return(nil, assert.AnError)
	}
}

// createTestDevice creates a device with standard test setup
func createTestDevice(t *testing.T, mockClient *mocks.MockAntminerClient, status *antminer.Status, telemetry *antminer.Telemetry) *Device {
	setupMockForDeviceCreation(mockClient)
	setupMockForDeviceConnection(mockClient, status, telemetry)

	device, err := New(
		testDeviceID,
		testDeviceInfo(),
		testCredentials(),
		mockClientFactory(mockClient),
	)
	require.NoError(t, err)
	require.NotNil(t, device)

	err = device.Connect(t.Context())
	require.NoError(t, err)
	return device
}

// cleanupDevice closes the device and expects the Close call on the mock
func cleanupDevice(t *testing.T, device *Device, mockClient *mocks.MockAntminerClient) {
	mockClient.EXPECT().Close()
	err := device.Close(t.Context())
	require.NoError(t, err)
}

// setupTestWithDevice creates a test device with standard setup and returns cleanup function
func setupTestWithDevice(t *testing.T) (*Device, *mocks.MockAntminerClient, func()) {
	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())

	cleanup := func() {
		cleanupDevice(t, device, mockClient)
		ctrl.Finish()
	}

	return device, mockClient, cleanup
}

// assertMetricValue validates that a telemetry value matches a metric value
func assertMetricValue(t *testing.T, expected *float64, actual *sdk.MetricValue, msgAndArgs ...interface{}) {
	if expected != nil && *expected > 0 {
		require.NotNil(t, actual, msgAndArgs...)
		assert.InEpsilon(t, *expected, actual.Value, 0.01, msgAndArgs...)
	}
}

// defaultStatus returns a standard healthy status for testing
func defaultStatus() *antminer.Status {
	return &antminer.Status{
		State:           sdk.HealthHealthyActive,
		FirmwareVersion: testFirmware,
		ErrorMessage:    "",
	}
}

// defaultTelemetry returns standard telemetry data for testing
func defaultTelemetry() *antminer.Telemetry {
	return &antminer.Telemetry{
		HashrateHS:    ptrFloat64(100e12), // 100 TH/s
		UptimeSeconds: ptrInt64(86400),    // 1 day uptime
	}
}

func TestDevice_New(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)

	// For this test, we only want to verify device creation, not connection
	setupMockForDeviceCreation(mockClient)

	device, err := New(
		testDeviceID,
		testDeviceInfo(),
		testCredentials(),
		mockClientFactoryWithAssertions(t, mockClient), // Use the version that validates parameters
	)
	require.NoError(t, err)
	require.NotNil(t, device)

	// Verify device properties
	assert.Equal(t, testDeviceID, device.ID())
	assert.Equal(t, testDeviceInfo(), device.deviceInfo)
	assert.Equal(t, testCredentials(), device.credentials)
	assert.Equal(t, mockClient, device.client)

	// Clean up - just expect Close call
	mockClient.EXPECT().Close()
	err = device.Close(t.Context())
	require.NoError(t, err)
}

func TestDevice_Connect(t *testing.T) {
	ctx := t.Context()

	t.Run("successful_connection", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)

		// Set up expectations for device creation
		setupMockForDeviceCreation(mockClient)

		device, err := New(
			testDeviceID,
			testDeviceInfo(),
			testCredentials(),
			mockClientFactory(mockClient),
		)
		require.NoError(t, err)
		require.NotNil(t, device)

		// Set up expectations for connection
		setupMockForDeviceConnection(mockClient, defaultStatus(), defaultTelemetry())

		// Test Connect
		err = device.Connect(ctx)
		require.NoError(t, err)

		// Clean up
		mockClient.EXPECT().Close()
		err = device.Close(ctx)
		require.NoError(t, err)
	})

	t.Run("connection_failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)

		// Set up expectations for device creation
		setupMockForDeviceCreation(mockClient)

		device, err := New(
			testDeviceID,
			testDeviceInfo(),
			testCredentials(),
			mockClientFactory(mockClient),
		)
		require.NoError(t, err)
		require.NotNil(t, device)

		// Set up expectations for failed connection
		mockClient.EXPECT().GetStatus(gomock.Any()).Return(nil, assert.AnError)
		mockClient.EXPECT().Close() // Should be called when connection fails

		// Test Connect failure
		err = device.Connect(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to verify device communication")

		// Device should already be closed due to connection failure
	})
}

func TestDevice_Status(t *testing.T) {
	ctx := t.Context()

	testCases := []struct {
		name           string
		minerStatus    *antminer.Status
		telemetry      *antminer.Telemetry
		expectedHealth sdk.HealthStatus
	}{
		{
			name: "mining_with_hashrate",
			minerStatus: &antminer.Status{
				State:           sdk.HealthHealthyActive,
				FirmwareVersion: testFirmware,
				ErrorMessage:    "",
			},
			telemetry: &antminer.Telemetry{
				HashrateHS:         ptrFloat64(100e12), // 100 TH/s
				PowerWatts:         ptrFloat64(3000),
				TemperatureCelsius: ptrFloat64(70),
				EfficiencyJPerHash: ptrFloat64(30),
				FanRPM:             ptrFloat64(4000),
				UptimeSeconds:      ptrInt64(86400),
			},
			expectedHealth: sdk.HealthHealthyActive,
		},
		{
			name: "mining_no_hashrate",
			minerStatus: &antminer.Status{
				State:           sdk.HealthHealthyActive,
				FirmwareVersion: testFirmware,
				ErrorMessage:    "",
			},
			telemetry: &antminer.Telemetry{
				HashrateHS: ptrFloat64(0), // No hashrate
			},
			expectedHealth: sdk.HealthWarning,
		},
		{
			name: "idle_state",
			minerStatus: &antminer.Status{
				State:           sdk.HealthHealthyInactive,
				FirmwareVersion: testFirmware,
				ErrorMessage:    "",
			},
			telemetry:      nil,
			expectedHealth: sdk.HealthHealthyInactive,
		},
		{
			name: "warning_state",
			minerStatus: &antminer.Status{
				State:           sdk.HealthWarning,
				FirmwareVersion: testFirmware,
				ErrorMessage:    "High temperature",
			},
			telemetry:      nil,
			expectedHealth: sdk.HealthWarning,
		},
		{
			name: "error_state",
			minerStatus: &antminer.Status{
				State:           sdk.HealthCritical,
				FirmwareVersion: testFirmware,
				ErrorMessage:    "Hardware failure",
			},
			telemetry:      nil,
			expectedHealth: sdk.HealthCritical,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockClient := mocks.NewMockAntminerClient(ctrl)
			device := createTestDevice(t, mockClient, tc.minerStatus, tc.telemetry)
			defer cleanupDevice(t, device, mockClient)

			// Get the status (should use cached result from creation)
			status, err := device.Status(ctx)
			require.NoError(t, err)

			// Verify results
			assert.Equal(t, testDeviceID, status.DeviceID)
			assert.Equal(t, tc.expectedHealth, status.Health)

			// Verify health reason for error cases
			if tc.minerStatus.ErrorMessage != "" {
				require.NotNil(t, status.HealthReason)
				assert.Equal(t, tc.minerStatus.ErrorMessage, *status.HealthReason)
			}

			// Verify telemetry data if provided - now wrapped in MetricValue
			if tc.telemetry != nil {
				assertMetricValue(t, tc.telemetry.HashrateHS, status.HashrateHS)
				assertMetricValue(t, tc.telemetry.PowerWatts, status.PowerW)
				assertMetricValue(t, tc.telemetry.TemperatureCelsius, status.TempC)
			}

			// Verify SensorMetrics for uptime if provided
			if tc.telemetry != nil && tc.telemetry.UptimeSeconds != nil {
				require.NotNil(t, status.SensorMetrics)
				require.Len(t, status.SensorMetrics, 1)
				uptimeSensor := status.SensorMetrics[0]
				assert.Equal(t, "uptime", uptimeSensor.Type)
				assert.Equal(t, "seconds", uptimeSensor.Unit)
				assert.Equal(t, "uptime", uptimeSensor.Name)
				assert.Equal(t, sdk.ComponentStatusHealthy, uptimeSensor.Status)
				require.NotNil(t, uptimeSensor.Value)
				assert.InEpsilon(t, float64(*tc.telemetry.UptimeSeconds), uptimeSensor.Value.Value, 0.01)
				assert.Equal(t, sdk.MetricKindCounter, uptimeSensor.Value.Kind)
			}
		})
	}
}

func TestDevice_StatusCaching(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)

	// Set up expectations for device creation
	setupMockForDeviceCreation(mockClient)

	// Create device (no status calls yet)
	device, err := New(
		testDeviceID,
		testDeviceInfo(),
		testCredentials(),
		mockClientFactory(mockClient),
	)
	require.NoError(t, err)
	require.NotNil(t, device)
	defer cleanupDevice(t, device, mockClient)

	// Set up expectations for connection (this will populate the cache)
	setupMockForDeviceConnection(mockClient, defaultStatus(), defaultTelemetry())

	// Connect the device (this will cache the first status)
	err = device.Connect(ctx)
	require.NoError(t, err)

	// First call should use cached result from Connect (no additional RPC calls)
	status1, err := device.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthHealthyActive, status1.Health)

	// Second call should also use cached result (no additional RPC calls)
	status2, err := device.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, status1, status2)

	// Verify that both calls returned the same cached data
	assert.Equal(t, status1.Timestamp, status2.Timestamp, "Cached status should have same timestamp")
	assert.Equal(t, status1.Health, status2.Health, "Cached status should have same health")
}

func TestDevice_StatusNoCache(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)

	// Set up expectations for device creation
	setupMockForDeviceCreation(mockClient)

	// Create device (no status calls yet)
	device, err := New(
		testDeviceID,
		testDeviceInfo(),
		testCredentials(),
		mockClientFactory(mockClient),
		WithStatusTTL(0), // Disable caching for this test
	)
	require.NoError(t, err)
	require.NotNil(t, device)
	defer cleanupDevice(t, device, mockClient)

	// Set up expectations for first status call (Connect calls Status internally)
	mockClient.EXPECT().GetStatus(gomock.Any()).Return(defaultStatus(), nil)
	mockClient.EXPECT().GetTelemetry(gomock.Any()).Return(defaultTelemetry(), nil)

	// Connect the device (this will call Status once)
	err = device.Connect(ctx)
	require.NoError(t, err)

	// Set up expectations for second status call (should invoke RPC again due to no caching)
	mockClient.EXPECT().GetStatus(gomock.Any()).Return(defaultStatus(), nil)
	mockClient.EXPECT().GetTelemetry(gomock.Any()).Return(defaultTelemetry(), nil)

	// First explicit call should fetch fresh data (no cache due to TTL=0)
	status1, err := device.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthHealthyActive, status1.Health)

	// Set up expectations for third status call (should invoke RPC again)
	updatedStatus := &antminer.Status{
		State:           sdk.HealthHealthyInactive,
		FirmwareVersion: testFirmware,
		ErrorMessage:    "",
	}
	mockClient.EXPECT().GetStatus(gomock.Any()).Return(updatedStatus, nil)
	mockClient.EXPECT().GetTelemetry(gomock.Any()).Return(nil, assert.AnError) // No telemetry

	// Second explicit call should fetch fresh data again
	status2, err := device.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthHealthyInactive, status2.Health)

	// Verify that the two statuses are different
	assert.NotEqual(t, status1.Timestamp, status2.Timestamp, "Statuses should have different timestamps")
	assert.NotEqual(t, status1.Health, status2.Health, "Statuses should have different health statuses")
}

func TestDevice_DescribeDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Test DescribeDevice
	info, capabilities, err := device.DescribeDevice(t.Context())
	require.NoError(t, err)

	// Verify device info
	assert.Equal(t, testDeviceInfo(), info)

	// Verify capabilities
	assert.True(t, capabilities[sdk.CapabilityPollingHost])
	assert.False(t, capabilities[sdk.CapabilityReboot])
	assert.False(t, capabilities[sdk.CapabilityFirmware])
	assert.False(t, capabilities[sdk.CapabilityPoolConfig])
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrInt64(v int64) *int64 {
	return &v
}

func TestDevice_StopMining(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Set up expectation for StopMining
	mockClient.EXPECT().StopMining(gomock.Any()).Return(nil)

	// Test StopMining
	err := device.StopMining(t.Context())
	require.NoError(t, err)
}

func TestDevice_StartMining(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Set up expectation for StartMining
	mockClient.EXPECT().StartMining(gomock.Any()).Return(nil)

	// Test StartMining
	err := device.StartMining(t.Context())
	require.NoError(t, err)
}

func TestDevice_Reboot(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Set up expectation for Reboot
	mockClient.EXPECT().Reboot(gomock.Any()).Return(nil)

	// Test Reboot
	err := device.Reboot(t.Context())
	require.NoError(t, err)
}

func TestDevice_UpdateMiningPools(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Define new pools to update
	expectedPools := []antminer.Pool{
		{
			Priority:   1,
			URL:        "stratum+tcp://pool1.example.com:3333",
			WorkerName: "worker1",
		},
		{
			Priority:   2,
			URL:        "stratum+tcp://pool2.example.com:4444",
			WorkerName: "worker2",
		},
	}

	newPools := []sdk.MiningPoolConfig{}
	for _, p := range expectedPools {
		if p.Priority < 1 || p.Priority > math.MaxInt32 {
			t.Fatalf("invalid pool priority: %d", p.Priority)
		}
		priority := int32(p.Priority)

		newPools = append(newPools, sdk.MiningPoolConfig{
			Priority:   priority,
			URL:        p.URL,
			WorkerName: p.WorkerName,
		})
	}

	// Set up expectation for UpdatePools
	mockClient.EXPECT().UpdatePools(t.Context(), expectedPools).Return(nil)

	// Test UpdateMiningPools
	err := device.UpdateMiningPools(t.Context(), newPools)
	require.NoError(t, err)
}

func TestDevice_GetWebViewURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Test GetWebViewURL
	url, ok, err := device.TryGetWebViewURL(t.Context())
	require.True(t, ok)
	require.NoError(t, err)
	assert.Equal(t, "http://192.168.1.100", url)
}

func TestDevice_ID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Test ID getter
	id := device.ID()
	assert.Equal(t, testDeviceID, id)
}

func TestDevice_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())

	// Test Close - should call client.Close() and clear cached data
	mockClient.EXPECT().Close()
	err := device.Close(t.Context())
	require.NoError(t, err)

	// Verify cached data is cleared
	assert.Nil(t, device.lastStatus)
}

func TestDevice_SetCoolingMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Test SetCoolingMode
	testMode := sdk.CoolingModeManual
	mockClient.EXPECT().SetCoolingMode(gomock.Any(), web.CoolingMode(testMode)).Return(nil)

	err := device.SetCoolingMode(t.Context(), testMode)
	require.NoError(t, err)
}

func TestDevice_BlinkLED(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	// Test BlinkLED
	mockClient.EXPECT().BlinkLED(gomock.Any(), blinkLEDDuration).Return(nil)

	err := device.BlinkLED(t.Context())
	require.NoError(t, err)
}

func TestDevice_WithStatusTTL(t *testing.T) {
	t.Run("valid_ttl", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		setupMockForDeviceCreation(mockClient)

		customTTL := 10 * time.Second
		device, err := New(
			testDeviceID,
			testDeviceInfo(),
			testCredentials(),
			mockClientFactory(mockClient),
			WithStatusTTL(customTTL),
		)
		require.NoError(t, err)
		require.NotNil(t, device)
		assert.Equal(t, customTTL, device.statusTTL)

		// Clean up
		mockClient.EXPECT().Close()
		err = device.Close(t.Context())
		require.NoError(t, err)
	})

	t.Run("negative_ttl_error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		// Don't set up mock expectations since device creation should fail early

		// Test negative TTL should return error
		device, err := New(
			testDeviceID,
			testDeviceInfo(),
			testCredentials(),
			mockClientFactory(mockClient),
			WithStatusTTL(-1*time.Second),
		)
		require.Error(t, err)
		require.Nil(t, device)
		assert.Contains(t, err.Error(), "status TTL must be positive")
	})

	t.Run("zero_ttl_valid", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		setupMockForDeviceCreation(mockClient)

		// Test zero TTL (disables caching) should be valid
		device, err := New(
			testDeviceID,
			testDeviceInfo(),
			testCredentials(),
			mockClientFactory(mockClient),
			WithStatusTTL(0),
		)
		require.NoError(t, err)
		require.NotNil(t, device)
		assert.Equal(t, time.Duration(0), device.statusTTL)

		// Clean up
		mockClient.EXPECT().Close()
		err = device.Close(t.Context())
		require.NoError(t, err)
	})
}
