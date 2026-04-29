package plugins

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// mockSDKDevice is a mock implementation of sdk.Device for testing
type mockSDKDevice struct {
	id                      string
	statusFunc              func(ctx context.Context) (sdk.DeviceMetrics, error)
	describeDeviceFunc      func(ctx context.Context) (sdk.DeviceInfo, sdk.Capabilities, error)
	closeFunc               func(ctx context.Context) error
	startMiningFunc         func(ctx context.Context) error
	stopMiningFunc          func(ctx context.Context) error
	blinkLEDFunc            func(ctx context.Context) error
	rebootFunc              func(ctx context.Context) error
	setCoolingModeFunc      func(ctx context.Context, mode sdk.CoolingMode) error
	getCoolingModeFunc      func(ctx context.Context) (sdk.CoolingMode, error)
	setPowerTargetFunc      func(ctx context.Context, performanceMode sdk.PerformanceMode) error
	updatePoolsFunc         func(ctx context.Context, pools []sdk.MiningPoolConfig) error
	downloadLogsFunc        func(ctx context.Context, since *time.Time, uuid string) (string, bool, error)
	firmwareUpdateFunc      func(ctx context.Context, firmware sdk.FirmwareFile) error
	getErrorsFunc           func(ctx context.Context) (sdk.DeviceErrors, error)
	tryGetWebViewFunc       func(ctx context.Context) (string, bool, error)
	updateMinerPasswordFunc func(ctx context.Context, currentPassword string, newPassword string) error
	curtailFunc             func(ctx context.Context, level sdk.CurtailLevel) error
	uncurtailFunc           func(ctx context.Context) error
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

func (m *mockSDKDevice) Curtail(ctx context.Context, level sdk.CurtailLevel) error {
	if m.curtailFunc != nil {
		return m.curtailFunc(ctx, level)
	}
	return nil
}

func (m *mockSDKDevice) Uncurtail(ctx context.Context) error {
	if m.uncurtailFunc != nil {
		return m.uncurtailFunc(ctx)
	}
	return nil
}

func (m *mockSDKDevice) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	if m.setCoolingModeFunc != nil {
		return m.setCoolingModeFunc(ctx, mode)
	}
	return nil
}

func (m *mockSDKDevice) GetCoolingMode(ctx context.Context) (sdk.CoolingMode, error) {
	if m.getCoolingModeFunc != nil {
		return m.getCoolingModeFunc(ctx)
	}
	return sdk.CoolingModeUnspecified, nil
}

func (m *mockSDKDevice) SetPowerTarget(ctx context.Context, performanceMode sdk.PerformanceMode) error {
	if m.setPowerTargetFunc != nil {
		return m.setPowerTargetFunc(ctx, performanceMode)
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

func (m *mockSDKDevice) FirmwareUpdate(ctx context.Context, firmware sdk.FirmwareFile) error {
	if m.firmwareUpdateFunc != nil {
		return m.firmwareUpdateFunc(ctx, firmware)
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

func (m *mockSDKDevice) TryBatchStatus(ctx context.Context, _ []string) (map[string]sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

func (m *mockSDKDevice) UpdateMinerPassword(ctx context.Context, currentPassword string, newPassword string) error {
	if m.updateMinerPasswordFunc != nil {
		return m.updateMinerPasswordFunc(ctx, currentPassword, newPassword)
	}
	return nil
}

func (m *mockSDKDevice) TrySubscribe(ctx context.Context, _ []string) (<-chan sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}

func (m *mockSDKDevice) TryGetTimeSeriesData(ctx context.Context, _ []string, _, _ time.Time, _ *time.Duration, _ int32, _ string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}

func (m *mockSDKDevice) GetMiningPools(ctx context.Context) ([]sdk.ConfiguredPool, error) {
	return nil, nil
}

type mockSDKDeviceWithoutCurtailment struct {
	id string
}

func (m *mockSDKDeviceWithoutCurtailment) ID() string { return m.id }
func (m *mockSDKDeviceWithoutCurtailment) Status(context.Context) (sdk.DeviceMetrics, error) {
	return sdk.DeviceMetrics{}, nil
}
func (m *mockSDKDeviceWithoutCurtailment) DescribeDevice(context.Context) (sdk.DeviceInfo, sdk.Capabilities, error) {
	return sdk.DeviceInfo{}, sdk.Capabilities{}, nil
}
func (m *mockSDKDeviceWithoutCurtailment) Close(context.Context) error { return nil }
func (m *mockSDKDeviceWithoutCurtailment) StartMining(context.Context) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) StopMining(context.Context) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) BlinkLED(context.Context) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) Reboot(context.Context) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) SetCoolingMode(context.Context, sdk.CoolingMode) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) GetCoolingMode(context.Context) (sdk.CoolingMode, error) {
	return sdk.CoolingModeUnspecified, nil
}
func (m *mockSDKDeviceWithoutCurtailment) SetPowerTarget(context.Context, sdk.PerformanceMode) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) UpdateMiningPools(context.Context, []sdk.MiningPoolConfig) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) UpdateMinerPassword(context.Context, string, string) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) GetMiningPools(context.Context) ([]sdk.ConfiguredPool, error) {
	return nil, nil
}
func (m *mockSDKDeviceWithoutCurtailment) DownloadLogs(context.Context, *time.Time, string) (string, bool, error) {
	return "", false, nil
}
func (m *mockSDKDeviceWithoutCurtailment) FirmwareUpdate(context.Context, sdk.FirmwareFile) error {
	return nil
}
func (m *mockSDKDeviceWithoutCurtailment) Unpair(context.Context) error { return nil }
func (m *mockSDKDeviceWithoutCurtailment) GetErrors(context.Context) (sdk.DeviceErrors, error) {
	return sdk.DeviceErrors{}, nil
}
func (m *mockSDKDeviceWithoutCurtailment) TryGetWebViewURL(context.Context) (string, bool, error) {
	return "", false, nil
}
func (m *mockSDKDeviceWithoutCurtailment) TryBatchStatus(context.Context, []string) (map[string]sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}
func (m *mockSDKDeviceWithoutCurtailment) TrySubscribe(context.Context, []string) (<-chan sdk.DeviceMetrics, bool, error) {
	return nil, false, nil
}
func (m *mockSDKDeviceWithoutCurtailment) TryGetTimeSeriesData(context.Context, []string, time.Time, time.Time, *time.Duration, int32, string) ([]sdk.DeviceMetrics, string, bool, error) {
	return nil, "", false, nil
}

const testOrgID = int64(1)

func createTestPluginMinerWithDevice(device sdk.Device) *PluginMiner {
	connInfo, _ := networking.NewConnectionInfo("192.168.1.100", "4028", networking.ProtocolHTTP)
	return NewPluginMiner(
		testOrgID,
		models.DeviceIdentifier("test-device-123"),
		"antminer",
		nil,
		"SN123456",
		*connInfo,
		device,
		sdk.DeviceInfo{
			Host: "192.168.1.100",
			Port: 4028,
		},
		nil,
	)
}

func createTestPluginMiner() (*PluginMiner, *mockSDKDevice) {
	mockDevice := &mockSDKDevice{id: "test-device"}
	pm := createTestPluginMinerWithDevice(mockDevice)

	return pm, mockDevice
}

func TestPluginMiner_CurtailReturnsUnimplementedWhenDeviceLacksCurtailment(t *testing.T) {
	pm := createTestPluginMinerWithDevice(&mockSDKDeviceWithoutCurtailment{id: "test-device"})

	err := pm.Curtail(t.Context(), sdk.CurtailLevelFull)

	require.Error(t, err)
	assert.True(t, fleeterror.IsUnimplementedError(err))
	assert.Contains(t, err.Error(), "device does not support curtailment")
}

func TestPluginMiner_CurtailUnavailablePreservesTransientTaxonomy(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()
	mockDevice.curtailFunc = func(context.Context, sdk.CurtailLevel) error {
		return grpcstatus.Error(codes.Unavailable, "temporary transport outage")
	}

	err := pm.Curtail(t.Context(), sdk.CurtailLevelFull)

	require.Error(t, err)
	assert.True(t, fleeterror.IsUnavailableError(err))
	assert.False(t, fleeterror.IsUnimplementedError(err))
	assert.Contains(t, err.Error(), "failed to curtail device")
	assert.Contains(t, err.Error(), "temporary transport outage")
}

func TestPluginMiner_UncurtailReturnsUnimplementedWhenDeviceLacksCurtailment(t *testing.T) {
	pm := createTestPluginMinerWithDevice(&mockSDKDeviceWithoutCurtailment{id: "test-device"})

	err := pm.Uncurtail(t.Context())

	require.Error(t, err)
	assert.True(t, fleeterror.IsUnimplementedError(err))
	assert.Contains(t, err.Error(), "device does not support curtailment")
}

func TestPluginMiner_UncurtailUnavailablePreservesTransientTaxonomy(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()
	mockDevice.uncurtailFunc = func(context.Context) error {
		return grpcstatus.Error(codes.Unavailable, "temporary transport outage")
	}

	err := pm.Uncurtail(t.Context())

	require.Error(t, err)
	assert.True(t, fleeterror.IsUnavailableError(err))
	assert.False(t, fleeterror.IsUnimplementedError(err))
	assert.Contains(t, err.Error(), "failed to uncurtail device")
	assert.Contains(t, err.Error(), "temporary transport outage")
}

// mockLogSaver captures the rows passed to SaveLogs for assertion in tests.
type mockLogSaver struct {
	savedLines []string
	err        error
}

func (m *mockLogSaver) SaveLogs(_ string, _ string, logLines []string) (string, error) {
	m.savedLines = logLines
	return "", m.err
}

func TestPluginMiner_DownloadLogs_Success(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()
	pm.driverName = "proto"
	pm.caps = sdk.Capabilities{sdk.CapabilityAsymmetricAuth: true}
	saver := &mockLogSaver{}
	pm.filesService = saver

	mockDevice.downloadLogsFunc = func(_ context.Context, _ *time.Time, _ string) (string, bool, error) {
		return "Jun 1 00:00:01 miner mcdd[1]: 2024-06-01 00:00:01.000000 | INFO  | module:1 | started", false, nil
	}

	err := pm.DownloadLogs(context.Background(), "batch-uuid")

	require.NoError(t, err)
	require.Len(t, saver.savedLines, 2) // header + 1 data row
	assert.Equal(t, "Time,Type,Message", saver.savedLines[0])
	assert.Equal(t, `"2024-06-01 00:00:01","INFO","module:1 | started"`, saver.savedLines[1])
}

func TestPluginMiner_DownloadLogs_TrailingNewlineTrimmed(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()
	saver := &mockLogSaver{}
	pm.filesService = saver

	mockDevice.downloadLogsFunc = func(_ context.Context, _ *time.Time, _ string) (string, bool, error) {
		// Log data with a trailing newline — should not produce a spurious empty row.
		return "[2026-01-01T00:00:00Z] line one\n[2026-01-01T00:00:01Z] line two\n", false, nil
	}

	err := pm.DownloadLogs(context.Background(), "batch-uuid")

	require.NoError(t, err)
	require.Len(t, saver.savedLines, 3) // header + 2 data rows, no empty row
	assert.Equal(t, "Time,Message", saver.savedLines[0])
	assert.Equal(t, `"2026-01-01T00:00:00Z","line one"`, saver.savedLines[1])
	assert.Equal(t, `"2026-01-01T00:00:01Z","line two"`, saver.savedLines[2])
}

func TestPluginMiner_DownloadLogs_SDKError(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()
	pm.filesService = &mockLogSaver{}

	mockDevice.downloadLogsFunc = func(_ context.Context, _ *time.Time, _ string) (string, bool, error) {
		return "", false, errors.New("connection refused")
	}

	err := pm.DownloadLogs(context.Background(), "batch-uuid")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download logs")
}

func TestPluginMiner_DownloadLogs_SaveError(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()
	pm.filesService = &mockLogSaver{err: errors.New("disk full")}

	mockDevice.downloadLogsFunc = func(_ context.Context, _ *time.Time, _ string) (string, bool, error) {
		return "[2026-01-01T00:00:00Z] hello\n", false, nil
	}

	err := pm.DownloadLogs(context.Background(), "batch-uuid")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save logs")
}

func TestFormatLogLineToCSVRow(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		includeType bool
		expected    string
	}{
		{
			name:        "Proto miner INFO log",
			includeType: true,
			line:        "Jun 14 16:01:58 proto-miner-001D mcdd[716]: 2024-06-14 16:01:58.470952 | INFO  | mcdd::temp::temp_control:322 | [TempCtrl] Control temps",
			expected:    `"2024-06-14 16:01:58","INFO","mcdd::temp::temp_control:322 | [TempCtrl] Control temps"`,
		},
		{
			name:        "Proto miner WARN log",
			includeType: true,
			line:        "Jun 14 16:02:04 proto-miner-001D mcdd[716]: 2024-06-14 16:02:04.512536 | WARN  | mcdd::pool_interface:379 | Share rejected",
			expected:    `"2024-06-14 16:02:04","WARN","mcdd::pool_interface:379 | Share rejected"`,
		},
		{
			name:        "Proto miner ERROR log",
			includeType: true,
			line:        "Jun 14 16:02:06 proto-miner-001D mcdd[716]: 2024-06-14 16:02:06.575555 | ERROR | mcdd::hashboard:649 | Error during SetWork",
			expected:    `"2024-06-14 16:02:06","ERROR","mcdd::hashboard:649 | Error during SetWork"`,
		},
		{
			name:        "Proto miner DEBUG log",
			includeType: true,
			line:        "Jun 14 16:00:01 proto-miner-001D mcdd[716]: 2024-06-14 16:00:01.123456 | DEBUG | mcdd::debug:10 | debug info",
			expected:    `"2024-06-14 16:00:01","DEBUG","mcdd::debug:10 | debug info"`,
		},
		{
			name:        "Proto miner BX firmware log (syslog prefix, no mcdd timestamp)",
			includeType: true,
			line:        "Feb 23 12:33:24 proto-miner-D202 mcdd[664]: | INFO  | mcdd::hashboard::bx::hashboard:1213 | [b3a 3] Board energy - voltage: 18.78V",
			expected:    `"Feb 23 12:33:24","INFO","mcdd::hashboard::bx::hashboard:1213 | [b3a 3] Board energy - voltage: 18.78V"`,
		},
		{
			name:        "Proto miner log without syslog prefix",
			includeType: true,
			line:        "2024-06-14 16:01:58.470952 | INFO  | mcdd::hashboard::bx::hashboard:1227 | [b3a 9] ASIC frequencies - AVG: 360.00MHz",
			expected:    `"2024-06-14 16:01:58","INFO","mcdd::hashboard::bx::hashboard:1227 | [b3a 9] ASIC frequencies - AVG: 360.00MHz"`,
		},
		{
			name:        "Antminer ISO timestamp log",
			includeType: false,
			line:        "[2026-02-20T17:35:18Z] Mining operation normal - hashrate: 140.0 TH/s",
			expected:    `"2026-02-20T17:35:18Z","Mining operation normal - hashrate: 140.0 TH/s"`,
		},
		{
			name:        "Antminer kernel seconds-since-boot falls through to raw message",
			includeType: false,
			line:        "[    0.000000] Booting Linux on physical CPU 0x0",
			expected:    `"","[    0.000000] Booting Linux on physical CPU 0x0"`,
		},
		{
			name:        "Antminer kernel log with CPU core indicator falls through to raw message",
			includeType: false,
			line:        "[  258.894452@1] NET: Registered protocol family 10",
			expected:    `"","[  258.894452@1] NET: Registered protocol family 10"`,
		},
		{
			name:        "Antminer application log YYYY-MM-DD HH:MM:SS",
			includeType: false,
			line:        "2026-02-24 07:52:12 30m avg rate is 84933.16 in 30 mins",
			expected:    `"2026-02-24 07:52:12","30m avg rate is 84933.16 in 30 mins"`,
		},
		{
			name:        "Bracketed keyword without digits falls through to raw message",
			includeType: false,
			line:        "[INFO] Proto Miner Simulator started",
			expected:    `"","[INFO] Proto Miner Simulator started"`,
		},
		{
			name:        "Message with double quotes is escaped",
			includeType: false,
			line:        "[2026-01-01T00:00:00Z] error: \"disk full\"",
			expected:    `"2026-01-01T00:00:00Z","error: ""disk full"""`,
		},
		{
			name:        "Unrecognised format falls through to raw message",
			includeType: false,
			line:        "some unstructured log line",
			expected:    `"","some unstructured log line"`,
		},
		{
			name:        "Empty line",
			includeType: false,
			line:        "",
			expected:    `"",""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLogLineToCSVRow(tt.line, tt.includeType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPluginMiner_GetOrgID(t *testing.T) {
	pm, _ := createTestPluginMiner()
	assert.Equal(t, testOrgID, pm.GetOrgID())
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

func TestPluginMiner_GetDeviceMetrics_DefaultPasswordActive_ReturnsForbidden(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	mockDevice.statusFunc = func(ctx context.Context) (sdk.DeviceMetrics, error) {
		return sdk.DeviceMetrics{}, grpcstatus.Error(codes.PermissionDenied, "default password must be changed")
	}

	_, err := pm.GetDeviceMetrics(t.Context())

	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err), "expected forbidden error, got: %v", err)
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

	require.NotNil(t, url)
	assert.Equal(t, "http://192.168.1.100:4028", url.String())
}

func TestPluginMiner_MinerInfo(t *testing.T) {
	pm, _ := createTestPluginMiner()

	assert.Equal(t, models.DeviceIdentifier("test-device-123"), pm.GetID())
	assert.Equal(t, "antminer", pm.GetDriverName())
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
		mode        commonpb.CoolingMode
		expectedSDK sdk.CoolingMode
	}{
		{"air cooled", commonpb.CoolingMode_COOLING_MODE_AIR_COOLED, sdk.CoolingModeAirCooled},
		{"immersion", commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED, sdk.CoolingModeImmersionCooled},
		{"unspecified", commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, sdk.CoolingModeUnspecified},
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

func TestPluginMiner_GetCoolingMode(t *testing.T) {
	tests := []struct {
		name       string
		sdkMode    sdk.CoolingMode
		sdkErr     error
		expectedPB commonpb.CoolingMode
		wantErr    bool
	}{
		{"air cooled", sdk.CoolingModeAirCooled, nil, commonpb.CoolingMode_COOLING_MODE_AIR_COOLED, false},
		{"immersion", sdk.CoolingModeImmersionCooled, nil, commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED, false},
		{"unspecified", sdk.CoolingModeUnspecified, nil, commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, false},
		{"manual", sdk.CoolingModeManual, nil, commonpb.CoolingMode_COOLING_MODE_MANUAL, false},
		{"sdk error", sdk.CoolingModeUnspecified, errors.New("device error"), commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, mockDevice := createTestPluginMiner()

			mockDevice.getCoolingModeFunc = func(ctx context.Context) (sdk.CoolingMode, error) {
				return tt.sdkMode, tt.sdkErr
			}

			mode, err := pm.GetCoolingMode(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to get cooling mode")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.expectedPB, mode)
		})
	}
}

func TestPluginMiner_SetPowerTarget(t *testing.T) {
	tests := []struct {
		name        string
		mode        pb.PerformanceMode
		expectedSDK sdk.PerformanceMode
	}{
		{"maximum hashrate", pb.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE, sdk.PerformanceModeMaximumHashrate},
		{"efficiency", pb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY, sdk.PerformanceModeEfficiency},
		{"unspecified", pb.PerformanceMode_PERFORMANCE_MODE_UNSPECIFIED, sdk.PerformanceModeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, mockDevice := createTestPluginMiner()

			var receivedMode sdk.PerformanceMode
			mockDevice.setPowerTargetFunc = func(ctx context.Context, mode sdk.PerformanceMode) error {
				receivedMode = mode
				return nil
			}

			err := pm.SetPowerTarget(t.Context(), dto.PowerTargetPayload{
				PerformanceMode: tt.mode,
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

func TestPluginMiner_StartMining_DefaultPasswordActiveUnknown_ReturnsForbidden(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	mockDevice.startMiningFunc = func(ctx context.Context) error {
		return grpcstatus.Error(codes.Unknown, "failed to start mining: forbidden: default password must be changed")
	}

	err := pm.StartMining(t.Context())

	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err), "expected forbidden error, got: %v", err)
	assert.Contains(t, err.Error(), "failed to start mining")
}

func TestPluginMiner_GetWebViewURL_InvalidURL(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	// Return an invalid URL from SDK
	mockDevice.tryGetWebViewFunc = func(ctx context.Context) (string, bool, error) {
		return "://invalid-url", true, nil
	}

	url := pm.GetWebViewURL()

	assert.Nil(t, url)
}

func TestPluginMiner_GetErrors_Success(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	now := time.Now()
	componentID := "0"
	mockDevice.getErrorsFunc = func(ctx context.Context) (sdk.DeviceErrors, error) {
		return sdk.DeviceErrors{
			DeviceID: "test-device",
			Errors: []sdk.DeviceError{
				{
					MinerError:   1003, // PSU_FAULT_GENERIC
					Severity:     1,    // Critical
					Summary:      "PSU fault detected",
					FirstSeenAt:  now,
					LastSeenAt:   now,
					ComponentID:  &componentID,
					DeviceID:     "test-device",
					CauseSummary: "Power supply unit failure",
				},
			},
		}, nil
	}

	deviceErrors, err := pm.GetErrors(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "test-device", deviceErrors.DeviceID)
	require.Len(t, deviceErrors.Errors, 1)
	assert.Equal(t, "PSU fault detected", deviceErrors.Errors[0].Summary)
}

func TestPluginMiner_GetErrors_SDKError(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	expectedErr := errors.New("device communication error")
	mockDevice.getErrorsFunc = func(ctx context.Context) (sdk.DeviceErrors, error) {
		return sdk.DeviceErrors{}, expectedErr
	}

	_, err := pm.GetErrors(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get device errors")
}

func TestPluginMiner_GetErrors_EmptyErrors(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	mockDevice.getErrorsFunc = func(ctx context.Context) (sdk.DeviceErrors, error) {
		return sdk.DeviceErrors{
			DeviceID: "test-device",
			Errors:   []sdk.DeviceError{},
		}, nil
	}

	deviceErrors, err := pm.GetErrors(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "test-device", deviceErrors.DeviceID)
	assert.Empty(t, deviceErrors.Errors)
}

func TestPluginMiner_GetErrors_MapsAllFields(t *testing.T) {
	pm, mockDevice := createTestPluginMiner()

	now := time.Now()
	closedAt := now.Add(time.Hour)
	componentID := "2"
	mockDevice.getErrorsFunc = func(ctx context.Context) (sdk.DeviceErrors, error) {
		return sdk.DeviceErrors{
			DeviceID: "device-123",
			Errors: []sdk.DeviceError{
				{
					MinerError:        2000, // FAN_FAILED
					CauseSummary:      "Fan stopped spinning",
					RecommendedAction: "Replace the fan",
					Severity:          2, // Major
					FirstSeenAt:       now,
					LastSeenAt:        now.Add(time.Minute),
					ClosedAt:          &closedAt,
					VendorAttributes: map[string]string{
						"vendor_code": "FAN_001",
						"firmware":    "v1.2.3",
					},
					DeviceID:    "device-123",
					ComponentID: &componentID,
					Impact:      "Reduced cooling capacity",
					Summary:     "Fan stall detected on fan 2",
				},
			},
		}, nil
	}

	deviceErrors, err := pm.GetErrors(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "device-123", deviceErrors.DeviceID)
	require.Len(t, deviceErrors.Errors, 1)

	errMsg := deviceErrors.Errors[0]
	assert.NotZero(t, errMsg.MinerError, "MinerError should be mapped")
	assert.Equal(t, "Fan stopped spinning", errMsg.CauseSummary)
	assert.Equal(t, "Replace the fan", errMsg.RecommendedAction)
	assert.NotZero(t, errMsg.Severity, "Severity should be mapped")
	assert.Equal(t, now, errMsg.FirstSeenAt)
	assert.Equal(t, now.Add(time.Minute), errMsg.LastSeenAt)
	assert.NotNil(t, errMsg.ClosedAt)
	assert.Equal(t, closedAt, *errMsg.ClosedAt)
	assert.Equal(t, "device-123", errMsg.DeviceID)
	require.NotNil(t, errMsg.ComponentID)
	assert.Equal(t, "2", *errMsg.ComponentID)
	assert.Equal(t, "Reduced cooling capacity", errMsg.Impact)
	assert.Equal(t, "Fan stall detected on fan 2", errMsg.Summary)
	assert.Equal(t, "FAN_001", errMsg.VendorCode)
	assert.Equal(t, "v1.2.3", errMsg.Firmware)
}

func TestPluginMiner_GetDeviceStatus_NetworkError_ReturnsConnectionError(t *testing.T) {
	// Create a mock SDK device that returns a network error
	netErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: errors.New("connection refused"),
	}

	mockDevice := &mockSDKDevice{
		id: "test-device",
		statusFunc: func(ctx context.Context) (sdk.DeviceMetrics, error) {
			return sdk.DeviceMetrics{}, netErr
		},
	}

	deviceID := models.DeviceIdentifier("device-123")
	connInfo, _ := networking.NewConnectionInfo("192.168.1.100", "4028", networking.ProtocolHTTP)

	pluginMiner := NewPluginMiner(
		testOrgID,
		deviceID,
		"proto",
		nil,
		"serial-123",
		*connInfo,
		mockDevice,
		sdk.DeviceInfo{
			Host: "192.168.1.100",
			Port: 4028,
		},
		nil,
	)

	status, err := pluginMiner.GetDeviceStatus(context.Background())

	// Should return offline status
	assert.Equal(t, models.MinerStatusOffline, status)

	// Error should be wrapped as ConnectionError
	require.Error(t, err)
	assert.True(t, fleeterror.IsConnectionError(err), "Error should be a ConnectionError")

	// Should be able to extract the ConnectionError
	var connErr fleeterror.ConnectionError
	require.True(t, errors.As(err, &connErr))
	assert.Equal(t, string(deviceID), connErr.DeviceIdentifier)
}

func TestPluginMiner_GetDeviceStatus_NonNetworkError_ReturnsInternalError(t *testing.T) {
	// Create a mock SDK device that returns a non-network error
	genericErr := errors.New("some internal SDK error")

	mockDevice := &mockSDKDevice{
		id: "test-device",
		statusFunc: func(ctx context.Context) (sdk.DeviceMetrics, error) {
			return sdk.DeviceMetrics{}, genericErr
		},
	}

	deviceID := models.DeviceIdentifier("device-456")
	connInfo, _ := networking.NewConnectionInfo("192.168.1.100", "4028", networking.ProtocolHTTP)

	pluginMiner := NewPluginMiner(
		testOrgID,
		deviceID,
		"proto",
		nil,
		"serial-456",
		*connInfo,
		mockDevice,
		sdk.DeviceInfo{
			Host: "192.168.1.100",
			Port: 4028,
		},
		nil,
	)

	status, err := pluginMiner.GetDeviceStatus(context.Background())

	// Should return offline status
	assert.Equal(t, models.MinerStatusOffline, status)

	// Error should NOT be a ConnectionError (should be InternalError)
	require.Error(t, err)
	assert.False(t, fleeterror.IsConnectionError(err), "Error should NOT be a ConnectionError")
}

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name: "net.OpError",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("connection refused"),
			},
			expected: true,
		},
		{
			name: "url.Error wrapping net.OpError",
			err: &url.Error{
				Op:  "Get",
				URL: "http://device.local",
				Err: &net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: errors.New("connection refused"),
				},
			},
			expected: true,
		},
		{
			name: "url.Error wrapping generic error",
			err: &url.Error{
				Op:  "Get",
				URL: "http://device.local",
				Err: errors.New("some other error"),
			},
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("generic error"),
			expected: false,
		},
		{
			name:     "gRPC error with i/o timeout",
			err:      errors.New("rpc error: code = Unknown desc = failed to connect: dial tcp 172.16.2.23:4028: i/o timeout"),
			expected: true,
		},
		{
			name:     "gRPC error with connection refused",
			err:      errors.New("rpc error: code = Unknown desc = failed to connect: connection refused"),
			expected: true,
		},
		{
			name:     "error with dial tcp indicator",
			err:      errors.New("failed to connect: dial tcp 192.168.1.100:4028: connection refused"),
			expected: true,
		},
		{
			name:     "error with connection reset",
			err:      errors.New("read failed: connection reset by peer"),
			expected: true,
		},
		{
			name:     "error with no route to host",
			err:      errors.New("dial tcp: no route to host"),
			expected: true,
		},
		{
			name:     "syscall.ECONNREFUSED",
			err:      fmt.Errorf("connection failed: %w", syscall.ECONNREFUSED),
			expected: true,
		},
		{
			name:     "syscall.ETIMEDOUT",
			err:      fmt.Errorf("dial failed: %w", syscall.ETIMEDOUT),
			expected: true,
		},
		{
			name:     "syscall.ENETUNREACH",
			err:      fmt.Errorf("network error: %w", syscall.ENETUNREACH),
			expected: true,
		},
		{
			name:     "syscall.EHOSTDOWN",
			err:      fmt.Errorf("host error: %w", syscall.EHOSTDOWN),
			expected: true,
		},
		{
			name:     "syscall.EPIPE",
			err:      fmt.Errorf("write failed: %w", syscall.EPIPE),
			expected: true,
		},
		{
			name:     "syscall.ENOTCONN",
			err:      fmt.Errorf("socket error: %w", syscall.ENOTCONN),
			expected: true,
		},
		{
			name:     "syscall.ESHUTDOWN",
			err:      fmt.Errorf("send failed: %w", syscall.ESHUTDOWN),
			expected: true,
		},
		{
			name:     "gRPC error with context deadline exceeded",
			err:      errors.New("rpc error: code = DeadlineExceeded desc = context deadline exceeded"),
			expected: true,
		},
		{
			name:     "wrapped context deadline exceeded",
			err:      fmt.Errorf("failed to create device: %w", context.DeadlineExceeded),
			expected: true,
		},
		{
			name: "net.DNSError",
			err: &net.DNSError{
				Err:        "no such host",
				Name:       "device.local",
				IsTimeout:  false,
				IsNotFound: true,
			},
			expected: true,
		},
		{
			name: "os.SyscallError wrapping ECONNREFUSED",
			err: &os.SyscallError{
				Syscall: "connect",
				Err:     syscall.ECONNREFUSED,
			},
			expected: true,
		},
		{
			name:     "gRPC error with broken pipe",
			err:      errors.New("rpc error: code = Unavailable desc = write tcp: broken pipe"),
			expected: true,
		},
		{
			name:     "generic authentication error",
			err:      errors.New("authentication failed: invalid credentials"),
			expected: false,
		},
		{
			name:     "gRPC NotFound status (DEVICE_NOT_FOUND from plugin)",
			err:      grpcstatus.Error(codes.NotFound, "DEVICE_NOT_FOUND: Device not found: 172.16.2.103"),
			expected: true,
		},
		{
			name:     "gRPC Unavailable status (device unreachable from plugin)",
			err:      grpcstatus.Error(codes.Unavailable, "DEVICE_UNAVAILABLE: device not responding"),
			expected: true,
		},
		{
			name:     "gRPC DeadlineExceeded status",
			err:      grpcstatus.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			expected: true,
		},
		{
			name:     "gRPC Unauthenticated status is not a network error",
			err:      grpcstatus.Error(codes.Unauthenticated, "authentication failed"),
			expected: false,
		},
		{
			name:     "gRPC Internal status is not a network error",
			err:      grpcstatus.Error(codes.Internal, "internal error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNetworkError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWrapPluginError(t *testing.T) {
	tests := []struct {
		name                  string
		err                   error
		format                string
		args                  []any
		expectNil             bool
		expectUnimplemented   bool
		expectUnauthenticated bool
		expectForbidden       bool
		expectContains        string
	}{
		{
			name:      "nil error returns nil",
			err:       nil,
			format:    "reboot failed",
			expectNil: true,
		},
		{
			name:                "gRPC Unimplemented maps to fleeterror Unimplemented",
			err:                 grpcstatus.Error(codes.Unimplemented, "capability not supported"),
			format:              "reboot failed for device %s",
			args:                []any{"device-123"},
			expectUnimplemented: true,
			expectContains:      "reboot failed for device device-123",
		},
		{
			name:                  "gRPC Unauthenticated maps to fleeterror Unauthenticated",
			err:                   grpcstatus.Error(codes.Unauthenticated, "token expired"),
			format:                "reboot failed for device %s",
			args:                  []any{"device-456"},
			expectUnauthenticated: true,
			expectContains:        "reboot failed for device device-456",
		},
		{
			name:            "gRPC PermissionDenied maps to fleeterror Forbidden",
			err:             grpcstatus.Error(codes.PermissionDenied, "default password must be changed"),
			format:          "reboot failed for device %s",
			args:            []any{"device-789"},
			expectForbidden: true,
			expectContains:  "reboot failed for device device-789",
		},
		{
			name:            "gRPC Unknown with default-password marker maps to fleeterror Forbidden",
			err:             grpcstatus.Error(codes.Unknown, "failed to start mining: forbidden: default password must be changed"),
			format:          "start mining failed for device %s",
			args:            []any{"device-999"},
			expectForbidden: true,
			expectContains:  "start mining failed for device device-999",
		},
		{
			name:                "gRPC Internal maps to fleeterror Internal (not Unimplemented)",
			err:                 grpcstatus.Error(codes.Internal, "something broke"),
			format:              "reboot failed",
			expectUnimplemented: false,
			expectContains:      "reboot failed",
		},
		{
			name:                "generic error maps to fleeterror Internal",
			err:                 errors.New("connection refused"),
			format:              "set power target failed",
			expectUnimplemented: false,
			expectContains:      "set power target failed",
		},
		{
			name:                "gRPC Unavailable maps to fleeterror Internal (not Unimplemented)",
			err:                 grpcstatus.Error(codes.Unavailable, "service unavailable"),
			format:              "stop mining failed",
			expectUnimplemented: false,
			expectContains:      "stop mining failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			result := wrapPluginError(tt.err, tt.format, tt.args...)

			// Assert
			if tt.expectNil {
				assert.Nil(t, result)
				return
			}
			require.NotNil(t, result)
			assert.Equal(t, tt.expectUnimplemented, fleeterror.IsUnimplementedError(result))
			assert.Equal(t, tt.expectUnauthenticated, fleeterror.IsAuthenticationError(result))
			assert.Equal(t, tt.expectForbidden, fleeterror.IsForbiddenError(result))
			assert.Contains(t, result.Error(), tt.expectContains)
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "gRPC Unauthenticated status (real plugin transport path)",
			err:      grpcstatus.Error(codes.Unauthenticated, "token expired"),
			expected: true,
		},
		{
			name:     "sdk.SDKError with AUTHENTICATION_FAILED (in-process path)",
			err:      sdk.SDKError{Code: sdk.ErrCodeAuthenticationFailed, Message: "bad credentials"},
			expected: true,
		},
		{
			name:     "gRPC Internal is not an auth error",
			err:      grpcstatus.Error(codes.Internal, "internal error"),
			expected: false,
		},
		{
			name:     "generic error is not an auth error",
			err:      errors.New("authentication failed: invalid credentials"),
			expected: false,
		},
		{
			name:     "gRPC Unavailable is not an auth error",
			err:      grpcstatus.Error(codes.Unavailable, "service unavailable"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			result := isAuthError(tt.err)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDefaultPasswordActiveError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "permission denied with default-password marker",
			err:      grpcstatus.Error(codes.PermissionDenied, "default password must be changed"),
			expected: true,
		},
		{
			name:     "permission denied without default-password marker",
			err:      grpcstatus.Error(codes.PermissionDenied, "access denied"),
			expected: false,
		},
		{
			name:     "plain error with default-password marker",
			err:      errors.New("forbidden: default password must be changed"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isDefaultPasswordActiveError(tt.err))
		})
	}
}

func TestPluginMiner_GetDeviceStatus_AuthError_ReturnsUnauthenticated(t *testing.T) {
	connInfo, _ := networking.NewConnectionInfo("192.168.1.100", "4028", networking.ProtocolHTTP)

	tests := []struct {
		name   string
		sdkErr error
	}{
		{
			name:   "gRPC Unauthenticated (real plugin transport path)",
			sdkErr: grpcstatus.Error(codes.Unauthenticated, "token expired"),
		},
		{
			name:   "sdk.SDKError AUTHENTICATION_FAILED (in-process path)",
			sdkErr: sdk.SDKError{Code: sdk.ErrCodeAuthenticationFailed, Message: "bad credentials"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			deviceID := models.DeviceIdentifier("device-auth-err")
			mockDevice := &mockSDKDevice{
				id: "device-auth-err",
				statusFunc: func(ctx context.Context) (sdk.DeviceMetrics, error) {
					return sdk.DeviceMetrics{}, tt.sdkErr
				},
			}
			pluginMiner := NewPluginMiner(
				testOrgID, deviceID, "proto", nil, "serial-auth",
				*connInfo, mockDevice,
				sdk.DeviceInfo{Host: "192.168.1.100", Port: 4028},
				nil,
			)

			// Act
			status, err := pluginMiner.GetDeviceStatus(context.Background())

			// Assert
			assert.Equal(t, models.MinerStatusUnknown, status)
			require.Error(t, err)
			assert.True(t, fleeterror.IsAuthenticationError(err), "expected UnauthenticatedError, got: %v", err)
			assert.False(t, fleeterror.IsConnectionError(err), "auth error must not be misclassified as connection error")
		})
	}
}

func TestPluginMiner_GetDeviceStatus_DefaultPasswordActive_ReturnsForbidden(t *testing.T) {
	connInfo, _ := networking.NewConnectionInfo("192.168.1.100", "4028", networking.ProtocolHTTP)

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "permission denied status",
			err:  grpcstatus.Error(codes.PermissionDenied, "default password must be changed"),
		},
		{
			name: "wrapped unknown status with default-password marker",
			err:  grpcstatus.Error(codes.Unknown, "failed to get miner status: forbidden: default password must be changed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pluginMiner := NewPluginMiner(
				testOrgID,
				models.DeviceIdentifier("device-default-password"),
				"proto",
				nil,
				"serial-default-password",
				*connInfo,
				&mockSDKDevice{
					id: "device-default-password",
					statusFunc: func(ctx context.Context) (sdk.DeviceMetrics, error) {
						return sdk.DeviceMetrics{}, tt.err
					},
				},
				sdk.DeviceInfo{Host: "192.168.1.100", Port: 4028},
				nil,
			)

			status, err := pluginMiner.GetDeviceStatus(context.Background())

			assert.Equal(t, models.MinerStatusUnknown, status)
			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err), "expected forbidden error, got: %v", err)
		})
	}
}
