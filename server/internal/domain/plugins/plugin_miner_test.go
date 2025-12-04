package plugins

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSDKDevice is a mock implementation of sdk.Device for testing
type mockSDKDevice struct {
	id                 string
	statusFunc         func(ctx context.Context) (sdk.DeviceMetrics, error)
	describeDeviceFunc func(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error)
	closeFunc          func(ctx context.Context) error
	startMiningFunc    func(ctx context.Context) error
	stopMiningFunc     func(ctx context.Context) error
	blinkLEDFunc       func(ctx context.Context) error
	rebootFunc         func(ctx context.Context) error
	setCoolingModeFunc func(ctx context.Context, mode sdk.CoolingMode) error
	updatePoolsFunc    func(ctx context.Context, pools []sdk.MiningPoolConfig) error
	downloadLogsFunc   func(ctx context.Context, since *time.Time, uuid string) (string, bool, error)
	firmwareUpdateFunc func(ctx context.Context) error
	getErrorsFunc      func(ctx context.Context) (sdk.DeviceErrors, error)
	tryGetWebViewFunc  func(ctx context.Context) (string, bool, error)
}

func (m *mockSDKDevice) ID() string {
	return m.id
}

func (m *mockSDKDevice) Status(ctx context.Context) (sdk.DeviceMetrics, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}
	return sdk.DeviceMetrics{}, nil
}

func (m *mockSDKDevice) DescribeDevice(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	if m.describeDeviceFunc != nil {
		return m.describeDeviceFunc(ctx)
	}
	return sdk.DeviceInfo{}, sdk.Capabilities{}, nil
}

func (m *mockSDKDevice) Close(ctx context.Context) error {
	if m.closeFunc != nil {
		return m.closeFunc(ctx)
	}
	return nil
}

func (m *mockSDKDevice) StartMining(ctx context.Context) error {
	if m.startMiningFunc != nil {
		return m.startMiningFunc(ctx)
	}
	return nil
}

func (m *mockSDKDevice) StopMining(ctx context.Context) error {
	if m.stopMiningFunc != nil {
		return m.stopMiningFunc(ctx)
	}
	return nil
}

func (m *mockSDKDevice) BlinkLED(ctx context.Context) error {
	if m.blinkLEDFunc != nil {
		return m.blinkLEDFunc(ctx)
	}
	return nil
}

func (m *mockSDKDevice) Reboot(ctx context.Context) error {
	if m.rebootFunc != nil {
		return m.rebootFunc(ctx)
	}
	return nil
}

func (m *mockSDKDevice) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	if m.setCoolingModeFunc != nil {
		return m.setCoolingModeFunc(ctx, mode)
	}
	return nil
}

func (m *mockSDKDevice) UpdateMiningPools(ctx context.Context, pools []sdk.MiningPoolConfig) error {
	if m.updatePoolsFunc != nil {
		return m.updatePoolsFunc(ctx, pools)
	}
	return nil
}

func (m *mockSDKDevice) DownloadLogs(ctx context.Context, since *time.Time, uuid string) (string, bool, error) {
	if m.downloadLogsFunc != nil {
		return m.downloadLogsFunc(ctx, since, uuid)
	}
	return "", false, nil
}

func (m *mockSDKDevice) FirmwareUpdate(ctx context.Context) error {
	if m.firmwareUpdateFunc != nil {
		return m.firmwareUpdateFunc(ctx)
	}
	return nil
}

func (m *mockSDKDevice) Unpair(ctx context.Context) error {
	return nil
}

func (m *mockSDKDevice) GetErrors(ctx context.Context) (sdk.DeviceErrors, error) {
	if m.getErrorsFunc != nil {
		return m.getErrorsFunc(ctx)
	}
	return sdk.DeviceErrors{}, nil
}

func (m *mockSDKDevice) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
	if m.tryGetWebViewFunc != nil {
		return m.tryGetWebViewFunc(ctx)
	}
	return "", false, nil
}

// Implement remaining optional methods
func (m *mockSDKDevice) TryBatchStatus(ctx context.Context, _ []string) (map[string]sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

func (m *mockSDKDevice) TrySubscribe(ctx context.Context, _ []string) (<-chan sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

func (m *mockSDKDevice) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}

func createTestPluginMiner() (*PluginMiner, *mockSDKDevice) {
	connInfo, _ := networking.NewConnectionInfo("192.168.1.100", "4028", networking.ProtocolHTTP)
	mockDevice := &mockSDKDevice{id: "test-device"}

	pm := NewPluginMiner(
		models.DeviceIdentifier("test-device-123"),
		models.TypeAntminer,
		"SN123456",
		*connInfo,
		mockDevice,
		sdk.DeviceInfo{
			Host: "192.168.1.100",
			Port: 4028,
		},
	)

	return pm, mockDevice
}

func TestPluginMiner_GetDeviceMetrics_Success(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	hashrate := 100.0
	mockDevice.statusFunc = func(ctx context.Context) (sdk.DeviceMetrics, error) {
		return sdk.DeviceMetrics{
			DeviceID:  "test-device",
			Timestamp: time.Now(),
			Health:    sdk.HealthHealthyActive,
			HashrateHS: &sdk.MetricValue{
				Value: hashrate,
				Kind:  sdk.MetricKindGauge,
			},
		}, nil
	}

	metrics, err := pm.GetDeviceMetrics(t.Context())

	require.NoError(t, err)
	assert.NotNil(t, metrics.HashrateHS)
	assert.InDelta(t, hashrate, metrics.HashrateHS.Value, 0.0001)
}

func TestPluginMiner_GetDeviceMetrics_Error(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	expectedErr := errors.New("device communication error")
	mockDevice.statusFunc = func(ctx context.Context) (sdk.DeviceMetrics, error) {
		return sdk.DeviceMetrics{}, expectedErr
	}

	_, err := pm.GetDeviceMetrics(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get SDK device metrics")
}

func TestPluginMiner_GetDeviceStatus_HealthMapping(t *testing.T) {
	tests := []struct {
		name           string
		sdkHealth      sdk.HealthStatus
		expectedStatus models.MinerStatus
	}{
		{
			name:           "healthy active",
			sdkHealth:      sdk.HealthHealthyActive,
			expectedStatus: models.MinerStatusActive,
		},
		{
			name:           "healthy inactive",
			sdkHealth:      sdk.HealthHealthyInactive,
			expectedStatus: models.MinerStatusInactive,
		},
		{
			name:           "warning still operational",
			sdkHealth:      sdk.HealthWarning,
			expectedStatus: models.MinerStatusActive,
		},
		{
			name:           "critical error",
			sdkHealth:      sdk.HealthCritical,
			expectedStatus: models.MinerStatusError,
		},
		{
			name:           "unknown offline",
			sdkHealth:      sdk.HealthUnknown,
			expectedStatus: models.MinerStatusOffline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, mockDevice := createTestPluginMiner()

			mockDevice.statusFunc = func(ctx context.Context) (sdk.DeviceMetrics, error) {
				return sdk.DeviceMetrics{
					Health: tt.sdkHealth,
				}, nil
			}

			status, err := pm.GetDeviceStatus(t.Context())

			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, status)
		})
	}
}

func TestPluginMiner_GetWebViewURL_FromSDK(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	expectedURL := "http://192.168.1.100:8080/dashboard"
	mockDevice.tryGetWebViewFunc = func(ctx context.Context) (string, bool, error) {
		return expectedURL, true, nil
	}

	url := pm.GetWebViewURL()

	require.NotNil(t, url)
	assert.Equal(t, expectedURL, url.String())
}

func TestPluginMiner_GetWebViewURL_FallbackToConnectionInfo(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	// SDK doesn't provide web view URL
	mockDevice.tryGetWebViewFunc = func(ctx context.Context) (string, bool, error) {
		return "", false, nil
	}

	url := pm.GetWebViewURL()

	require.NotNil(t, url)
	assert.Equal(t, "http://192.168.1.100:4028", url.String())
}

func TestPluginMiner_GetWebViewURL_SDKError(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	mockDevice.tryGetWebViewFunc = func(ctx context.Context) (string, bool, error) {
		return "", false, errors.New("network error")
	}

	url := pm.GetWebViewURL()

	// Should fall back to connection info on error
	require.NotNil(t, url)
	assert.Equal(t, "http://192.168.1.100:4028", url.String())
}

func TestPluginMiner_MinerInfo(t *testing.T) {
	pm, _ := createTestPluginMiner()

	assert.Equal(t, models.DeviceIdentifier("test-device-123"), pm.GetID())
	assert.Equal(t, models.TypeAntminer, pm.GetType())
	assert.Equal(t, "SN123456", pm.GetSerialNumber())
	assert.NotNil(t, pm.GetConnectionInfo())
}

func TestPluginMiner_ControlOperations(t *testing.T) {
	tests := []struct {
		name   string
		action func(pm *PluginMiner) error
		setup  func(mock *mockSDKDevice)
	}{
		{
			name: "start mining",
			action: func(pm *PluginMiner) error {
				return pm.StartMining(t.Context())
			},
			setup: func(mock *mockSDKDevice) {
				mock.startMiningFunc = func(ctx context.Context) error {
					return nil
				}
			},
		},
		{
			name: "stop mining",
			action: func(pm *PluginMiner) error {
				return pm.StopMining(t.Context())
			},
			setup: func(mock *mockSDKDevice) {
				mock.stopMiningFunc = func(ctx context.Context) error {
					return nil
				}
			},
		},
		{
			name: "reboot",
			action: func(pm *PluginMiner) error {
				return pm.Reboot(t.Context())
			},
			setup: func(mock *mockSDKDevice) {
				mock.rebootFunc = func(ctx context.Context) error {
					return nil
				}
			},
		},
		{
			name: "blink LED",
			action: func(pm *PluginMiner) error {
				return pm.BlinkLED(t.Context())
			},
			setup: func(mock *mockSDKDevice) {
				mock.blinkLEDFunc = func(ctx context.Context) error {
					return nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, mockDevice := createTestPluginMiner()
			tt.setup(mockDevice)

			err := tt.action(pm)

			require.NoError(t, err)
		})
	}
}

func TestPluginMiner_SetCoolingMode(t *testing.T) {
	tests := []struct {
		name        string
		mode        pb.CoolingMode
		expectedSDK sdk.CoolingMode
	}{
		{"air cooled", pb.CoolingMode_COOLING_MODE_AIR_COOLED, sdk.CoolingModeAirCooled},
		{"immersion", pb.CoolingMode_COOLING_MODE_IMMERSION_COOLED, sdk.CoolingModeImmersionCooled},
		{"unspecified", pb.CoolingMode_COOLING_MODE_UNSPECIFIED, sdk.CoolingModeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, mockDevice := createTestPluginMiner()

			var receivedMode sdk.CoolingMode
			mockDevice.setCoolingModeFunc = func(ctx context.Context, mode sdk.CoolingMode) error {
				receivedMode = mode
				return nil
			}

			err := pm.SetCoolingMode(t.Context(), dto.CoolingModePayload{
				Mode: tt.mode,
			})

			require.NoError(t, err)
			assert.Equal(t, tt.expectedSDK, receivedMode)
		})
	}
}

func TestPluginMiner_UpdateMiningPools(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	var receivedPools []sdk.MiningPoolConfig
	mockDevice.updatePoolsFunc = func(ctx context.Context, pools []sdk.MiningPoolConfig) error {
		receivedPools = pools
		return nil
	}

	payload := dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority: 1,
			URL:      "stratum+tcp://pool1.example.com:3333",
			Username: "worker1",
		},
		Backup1Pool: &dto.MiningPool{
			Priority: 2,
			URL:      "stratum+tcp://pool2.example.com:3333",
			Username: "worker2",
		},
	}

	err := pm.UpdateMiningPools(t.Context(), payload)

	require.NoError(t, err)
	assert.Len(t, receivedPools, 2)
	assert.Equal(t, int32(1), receivedPools[0].Priority)
	assert.Equal(t, "stratum+tcp://pool1.example.com:3333", receivedPools[0].URL)
	assert.Equal(t, "worker1", receivedPools[0].WorkerName)
}

func TestPluginMiner_GetTelemetry_ReturnsEmpty(t *testing.T) {
	pm, _ := createTestPluginMiner()

	telemetry, err := pm.GetTelemetry(t.Context(), time.Now())

	require.NoError(t, err)
	assert.Empty(t, telemetry, "SDK devices don't support legacy telemetry format")
}

func TestPluginMiner_ErrorPropagation(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	expectedErr := errors.New("device error")
	mockDevice.rebootFunc = func(ctx context.Context) error {
		return expectedErr
	}

	err := pm.Reboot(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reboot device")
}

func TestPluginMiner_GetWebViewURL_InvalidURL(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	// Return an invalid URL from SDK
	mockDevice.tryGetWebViewFunc = func(ctx context.Context) (string, bool, error) {
		return "://invalid-url", true, nil
	}

	url := pm.GetWebViewURL()

	// Should return nil for invalid URLs
	assert.Nil(t, url)
}
