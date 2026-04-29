package device

import (
	"errors"
	"math"
	"testing"

	"github.com/block/proto-fleet/plugin/antminer/internal/types"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/mocks"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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
		Host:            testHost,
		Port:            80,
		URLScheme:       "http",
		Model:           testModel,
		Manufacturer:    testManufacturer,
		FirmwareVersion: testFirmware,
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
				TemperatureCelsius: ptrFloat64(70),
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
				assertMetricValue(t, tc.telemetry.TemperatureCelsius, status.TempC)
				assertMetricValue(t, tc.telemetry.FanRPM, status.FanRPM)
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
	)
	device.statusTTL = 0 // Disable caching for this test
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
	assert.True(t, capabilities[sdk.CapabilityReboot])
	assert.True(t, capabilities[sdk.CapabilityFirmware])
	assert.True(t, capabilities[sdk.CapabilityPoolConfig])
	assert.True(t, capabilities[sdk.CapabilityBasicAuth])
	assert.True(t, capabilities[sdk.CapabilityMiningStart])
	assert.True(t, capabilities[sdk.CapabilityMiningStop])
	assert.True(t, capabilities[sdk.CapabilityCurtailFull])
	assert.False(t, capabilities[sdk.CapabilityCurtailEfficiency])
	assert.False(t, capabilities[sdk.CapabilityCurtailPartial])
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

func TestDevice_CurtailFullInvalidatesStatusCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	require.NotNil(t, device.lastStatus)
	require.False(t, device.lastStatusAt.IsZero())
	mockClient.EXPECT().StopMining(gomock.Any()).Return(nil)

	err := device.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull})

	require.NoError(t, err)
	assert.Nil(t, device.lastStatus)
	assert.True(t, device.lastStatusAt.IsZero())
}

func TestDevice_CurtailFullWrapsDispatchFailureAsTransient(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	mockClient.EXPECT().StopMining(gomock.Any()).Return(assert.AnError)

	err := device.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailTransient, sdkErr.Code)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestDevice_CurtailUnsupportedLevelReturnsCapabilityNotSupported(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	err := device.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelEfficiency})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailCapabilityNotSupported, sdkErr.Code)
}

func TestDevice_UncurtailInvalidatesStatusCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	require.NotNil(t, device.lastStatus)
	require.False(t, device.lastStatusAt.IsZero())
	mockClient.EXPECT().StartMining(gomock.Any()).Return(nil)

	err := device.Uncurtail(t.Context(), sdk.UncurtailRequest{})

	require.NoError(t, err)
	assert.Nil(t, device.lastStatus)
	assert.True(t, device.lastStatusAt.IsZero())
}

func TestDevice_UncurtailWrapsDispatchFailureAsTransient(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	mockClient.EXPECT().StartMining(gomock.Any()).Return(assert.AnError)

	err := device.Uncurtail(t.Context(), sdk.UncurtailRequest{})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeCurtailTransient, sdkErr.Code)
	assert.ErrorIs(t, err, assert.AnError)
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

func TestDevice_UpdateMinerPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	currentPassword := "password" // testPassword from device initialization
	newPassword := "newpassword"

	// Expected credentials after update (username unchanged, only password updated)
	expectedCredentials := sdk.UsernamePassword{
		Username: testUsername, // Username stays the same
		Password: newPassword,  // Only password changes
	}

	// Set up expectations for ChangePassword and SetCredentials
	mockClient.EXPECT().ChangePassword(t.Context(), currentPassword, newPassword).Return(nil)
	mockClient.EXPECT().SetCredentials(expectedCredentials).Return(nil)

	// Test UpdateMinerPassword
	err := device.UpdateMinerPassword(t.Context(), currentPassword, newPassword)
	require.NoError(t, err)
}

func TestDevice_UpdateMinerPassword_ChangePasswordFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	currentPassword := "wrongpassword"
	newPassword := "newpassword"

	// Simulate password change failure (wrong current password)
	mockClient.EXPECT().ChangePassword(t.Context(), currentPassword, newPassword).Return(assert.AnError)

	// Test UpdateMinerPassword - should fail
	err := device.UpdateMinerPassword(t.Context(), currentPassword, newPassword)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update miner password")
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

func TestDevice_DownloadLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)
	device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
	defer cleanupDevice(t, device, mockClient)

	expectedLogs := "[    0.000000] Booting Linux on physical CPU 0x0\n[    1.200000] cgminer: Starting cgminer"
	mockClient.EXPECT().GetLogs(gomock.Any(), nil, 0).Return(expectedLogs, false, nil)

	logs, hasMore, err := device.DownloadLogs(t.Context(), nil, "")
	require.NoError(t, err)
	assert.Equal(t, expectedLogs, logs)
	assert.False(t, hasMore)
}

func TestDevice_GetErrors(t *testing.T) {
	ctx := t.Context()

	t.Run("healthy_device_no_errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
		defer cleanupDevice(t, device, mockClient)

		// Mock RPC calls returning healthy data
		mockClient.EXPECT().GetSummary(gomock.Any()).Return(&rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{
				{HardwareErrors: 10, DeviceHardwarePercent: 0.1, DeviceRejectedPercent: 0.5},
			},
		}, nil)
		mockClient.EXPECT().GetDevs(gomock.Any()).Return(&rpc.DevsResponse{
			Devs: []rpc.DevInfo{
				{ASC: 0, Status: "Alive", Enabled: "Y", Temperature: 70.0, MHSAv: 100000000},
			},
		}, nil)
		mockClient.EXPECT().GetPools(gomock.Any()).Return(&rpc.PoolsResponse{
			Pools: []rpc.PoolInfo{
				{Pool: 0, URL: "stratum+tcp://pool.example.com:3333", Status: "Alive"},
			},
		}, nil)
		mockClient.EXPECT().GetMinerConfig(gomock.Any()).Return(&web.MinerConfig{
			BitmainWorkMode: web.BitmainWorkModeStart,
		}, nil)
		// Stats API returns error (credentials required) - should fallback to RPC devs
		mockClient.EXPECT().GetStatsInfo(gomock.Any()).Return(nil, assert.AnError)

		errors, err := device.GetErrors(ctx)
		require.NoError(t, err)
		assert.Equal(t, testDeviceID, errors.DeviceID)
		assert.Empty(t, errors.Errors, "Expected no errors for healthy device")
	})

	t.Run("device_with_errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
		defer cleanupDevice(t, device, mockClient)

		// Mock RPC calls returning problematic data
		mockClient.EXPECT().GetSummary(gomock.Any()).Return(&rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{
				{HardwareErrors: 10, DeviceHardwarePercent: 0.1, DeviceRejectedPercent: 0.5},
			},
		}, nil)
		mockClient.EXPECT().GetDevs(gomock.Any()).Return(&rpc.DevsResponse{
			Devs: []rpc.DevInfo{
				{ASC: 0, Status: "Alive", Enabled: "Y", Temperature: 96.0, MHSAv: 100000000}, // Overheating
			},
		}, nil)
		mockClient.EXPECT().GetPools(gomock.Any()).Return(&rpc.PoolsResponse{
			Pools: []rpc.PoolInfo{
				{Pool: 0, URL: "stratum+tcp://pool.example.com:3333", Status: "Dead"}, // Pool down
			},
		}, nil)
		mockClient.EXPECT().GetMinerConfig(gomock.Any()).Return(&web.MinerConfig{
			BitmainWorkMode: web.BitmainWorkModeStart,
		}, nil)
		// Stats API returns error (credentials required) - should fallback to RPC devs
		mockClient.EXPECT().GetStatsInfo(gomock.Any()).Return(nil, assert.AnError)

		errors, err := device.GetErrors(ctx)
		require.NoError(t, err)
		assert.Equal(t, testDeviceID, errors.DeviceID)
		assert.Len(t, errors.Errors, 2, "Expected 2 errors (temperature + pool)")
	})

	t.Run("stats_api_success_no_fallback_to_devs", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
		defer cleanupDevice(t, device, mockClient)

		// Mock RPC calls for summary and pools (still needed)
		mockClient.EXPECT().GetSummary(gomock.Any()).Return(&rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{
				{HardwareErrors: 10, DeviceHardwarePercent: 0.1, DeviceRejectedPercent: 0.5},
			},
		}, nil)
		mockClient.EXPECT().GetDevs(gomock.Any()).Return(&rpc.DevsResponse{
			Devs: []rpc.DevInfo{
				// This data should NOT be used since Stats API succeeds
				{ASC: 0, Status: "Alive", Enabled: "Y", Temperature: 96.0, MHSAv: 100000000}, // Would trigger error if used
			},
		}, nil)
		mockClient.EXPECT().GetPools(gomock.Any()).Return(&rpc.PoolsResponse{
			Pools: []rpc.PoolInfo{
				{Pool: 0, URL: "stratum+tcp://pool.example.com:3333", Status: "Alive"},
			},
		}, nil)
		mockClient.EXPECT().GetMinerConfig(gomock.Any()).Return(&web.MinerConfig{
			BitmainWorkMode: web.BitmainWorkModeStart,
		}, nil)
		// Stats API succeeds with healthy chain data - should NOT fallback to RPC devs
		mockClient.EXPECT().GetStatsInfo(gomock.Any()).Return(&web.StatsInfo{
			STATS: []web.StatsData{
				{
					Chain: []web.ChainStats{
						{
							Index:     0,
							RateReal:  13500.0, // Healthy hashrate
							RateIdeal: 14000.0,
							TempChip:  []float64{70.0, 72.0, 71.0}, // Healthy temps
							HW:        50,                          // Low HW errors
							HWP:       0.05,                        // Low HW error percentage
							SN:        "test-chain-0",
						},
					},
				},
			},
		}, nil)

		errors, err := device.GetErrors(ctx)
		require.NoError(t, err)
		assert.Equal(t, testDeviceID, errors.DeviceID)
		assert.Empty(t, errors.Errors, "Expected no errors when Stats API provides healthy data")
	})

	t.Run("rpc_failures_graceful_degradation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
		defer cleanupDevice(t, device, mockClient)

		// All RPC calls fail - should still return empty errors, not fail
		mockClient.EXPECT().GetSummary(gomock.Any()).Return(nil, assert.AnError)
		mockClient.EXPECT().GetDevs(gomock.Any()).Return(nil, assert.AnError)
		mockClient.EXPECT().GetPools(gomock.Any()).Return(nil, assert.AnError)
		mockClient.EXPECT().GetStatsInfo(gomock.Any()).Return(nil, assert.AnError)
		mockClient.EXPECT().GetMinerConfig(gomock.Any()).Return(nil, assert.AnError)

		errors, err := device.GetErrors(ctx)
		require.NoError(t, err)
		assert.Equal(t, testDeviceID, errors.DeviceID)
		assert.Empty(t, errors.Errors, "Expected empty errors when RPC fails")
	})

	t.Run("sleeping_device_suppresses_not_hashing_errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
		defer cleanupDevice(t, device, mockClient)

		mockClient.EXPECT().GetSummary(gomock.Any()).Return(&rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{
				{HardwareErrors: 0, DeviceHardwarePercent: 0, DeviceRejectedPercent: 0},
			},
		}, nil)
		mockClient.EXPECT().GetDevs(gomock.Any()).Return(&rpc.DevsResponse{
			Devs: []rpc.DevInfo{
				{ASC: 0, Status: "Alive", Enabled: "Y", Temperature: 70.0, MHSAv: 0},
			},
		}, nil)
		mockClient.EXPECT().GetPools(gomock.Any()).Return(&rpc.PoolsResponse{
			Pools: []rpc.PoolInfo{
				{Pool: 0, URL: "stratum+tcp://pool.example.com:3333", Status: "Alive"},
			},
		}, nil)
		mockClient.EXPECT().GetStatsInfo(gomock.Any()).Return(&web.StatsInfo{
			STATS: []web.StatsData{
				{
					Chain: []web.ChainStats{
						{Index: 0, RateReal: 0, RateIdeal: 14000, TempChip: []float64{70, 70, 70}, SN: "chain-0"},
						{Index: 1, RateReal: 0, RateIdeal: 14000, TempChip: []float64{70, 70, 70}, SN: "chain-1"},
						{Index: 2, RateReal: 0, RateIdeal: 14000, TempChip: []float64{70, 70, 70}, SN: "chain-2"},
					},
				},
			},
		}, nil)
		mockClient.EXPECT().GetMinerConfig(gomock.Any()).Return(&web.MinerConfig{
			BitmainWorkMode: web.BitmainWorkModeSleep,
		}, nil)

		errors, err := device.GetErrors(ctx)
		require.NoError(t, err)
		assert.Equal(t, testDeviceID, errors.DeviceID)
		assert.Empty(t, errors.Errors, "Expected sleeping device to suppress not-hashing errors")
	})

	t.Run("awake_device_still_reports_not_hashing_errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockAntminerClient(ctrl)
		device := createTestDevice(t, mockClient, defaultStatus(), defaultTelemetry())
		defer cleanupDevice(t, device, mockClient)

		mockClient.EXPECT().GetSummary(gomock.Any()).Return(&rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{
				{HardwareErrors: 0, DeviceHardwarePercent: 0, DeviceRejectedPercent: 0},
			},
		}, nil)
		mockClient.EXPECT().GetDevs(gomock.Any()).Return(&rpc.DevsResponse{
			Devs: []rpc.DevInfo{
				{ASC: 0, Status: "Alive", Enabled: "Y", Temperature: 70.0, MHSAv: 0},
			},
		}, nil)
		mockClient.EXPECT().GetPools(gomock.Any()).Return(&rpc.PoolsResponse{
			Pools: []rpc.PoolInfo{
				{Pool: 0, URL: "stratum+tcp://pool.example.com:3333", Status: "Alive"},
			},
		}, nil)
		mockClient.EXPECT().GetStatsInfo(gomock.Any()).Return(&web.StatsInfo{
			STATS: []web.StatsData{
				{
					Chain: []web.ChainStats{
						{Index: 0, RateReal: 0, RateIdeal: 14000, TempChip: []float64{70, 70, 70}, SN: "chain-0"},
					},
				},
			},
		}, nil)
		mockClient.EXPECT().GetMinerConfig(gomock.Any()).Return(&web.MinerConfig{
			BitmainWorkMode: web.BitmainWorkModeStart,
		}, nil)

		errors, err := device.GetErrors(ctx)
		require.NoError(t, err)
		assert.Len(t, errors.Errors, 1, "Expected awake device to keep reporting not-hashing errors")
		assert.Equal(t, "Hashboard 0 is not producing hashrate", errors.Errors[0].Summary)
	})
}
