package antminer

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc/mocks"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	webmocks "github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web/mocks"
	"github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewClient(t *testing.T) {
	client, err := NewClient("192.168.1.100", 4028, 80, "http")
	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Equal(t, "192.168.1.100", client.host)
	assert.Equal(t, int32(4028), client.rpcPort)
	assert.Equal(t, int32(80), client.webPort)
	assert.Equal(t, "http", client.urlScheme)
	assert.NotNil(t, client.httpClient)
	assert.NotNil(t, client.webClient)
}

func TestClient_SetCredentials(t *testing.T) {
	client, err := NewClient("192.168.1.100", 4028, 80, "http")
	require.NoError(t, err)

	err = client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	require.NotNil(t, client.credentials)
	assert.Equal(t, "admin", client.credentials.Username)
	assert.Equal(t, "password", client.credentials.Password)
}

// Helper functions for test setup
func createTestClient(t *testing.T) *Client {
	client, err := NewClient("192.168.1.100", 4028, 80, "http")
	require.NoError(t, err)
	return client
}

func createTestClientWithMocks(t *testing.T, webClient web.WebAPIClient, rpcClient rpc.RPCClient) *Client {
	client, err := NewClient("192.168.1.100", 4028, 80, "http")
	require.NoError(t, err)

	// Inject mock clients directly
	if webClient != nil {
		client.webClient = webClient
	}
	if rpcClient != nil {
		client.rpcClient = rpcClient
	}

	return client
}

func setupMockWebClient(t *testing.T) (*webmocks.MockWebAPIClient, *gomock.Controller) {
	ctrl := gomock.NewController(t)
	mockWebClient := webmocks.NewMockWebAPIClient(ctrl)
	return mockWebClient, ctrl
}

func setupMockRPCClient(t *testing.T) (*mocks.MockRPCClient, *gomock.Controller) {
	ctrl := gomock.NewController(t)
	mockRPCClient := mocks.NewMockRPCClient(ctrl)
	return mockRPCClient, ctrl
}

func TestClient_UpdatePools(t *testing.T) {
	mockWebClient, ctrl := setupMockWebClient(t)
	defer ctrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	pools := []Pool{
		{
			Priority:   1,
			URL:        "stratum+tcp://pool.example.com:4444",
			WorkerName: "worker1",
		},
	}

	// Test without credentials - should fail
	err := client.UpdatePools(t.Context(), pools)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials required")

	// Test with credentials - should succeed
	err = client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	// Mock successful config operations
	mockConfig := &web.MinerConfig{
		Pools: []web.Pool{},
	}
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(mockConfig, nil)

	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	err = client.UpdatePools(t.Context(), pools)
	require.NoError(t, err)

	// Test error case: config fetch fails
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("config fetch failed"))

	err = client.UpdatePools(t.Context(), pools)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get current config")
}

func TestClient_BlinkLED(t *testing.T) {
	mockWebClient, ctrl := setupMockWebClient(t)
	defer ctrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	// Test without credentials - should fail
	err := client.BlinkLED(t.Context(), 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials required")

	// Test with credentials - should succeed
	err = client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	// Mock successful blink operations
	mockWebClient.EXPECT().
		StartBlink(gomock.Any(), gomock.Any()).
		Return(nil)

	mockWebClient.EXPECT().
		StopBlink(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	err = client.BlinkLED(t.Context(), 100*time.Millisecond)
	require.NoError(t, err)

	// Test StartBlink API error
	mockWebClient.EXPECT().
		StartBlink(gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("blink failed"))

	err = client.BlinkLED(t.Context(), 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start LED blink")
}

func TestClient_Reboot(t *testing.T) {
	mockWebClient, ctrl := setupMockWebClient(t)
	defer ctrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	// Test without credentials
	err := client.Reboot(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials required")

	// Test with credentials and successful mock response
	err = client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	// Mock the Reboot call
	mockWebClient.EXPECT().
		Reboot(gomock.Any(), gomock.Any()).
		Return(nil)

	err = client.Reboot(t.Context())
	require.NoError(t, err)
}

func TestClient_NotImplementedMethods(t *testing.T) {
	client := createTestClient(t)

	err := client.UpdateFirmware(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestClient_GetLogs_NoCredentials(t *testing.T) {
	client := createTestClient(t)

	_, _, err := client.GetLogs(t.Context(), nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials required")
}

func TestClient_GetLogs_Success(t *testing.T) {
	mockRPCClient, rpcCtrl := setupMockRPCClient(t)
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer rpcCtrl.Finish()
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, mockRPCClient)
	err := client.SetCredentials(sdk.UsernamePassword{Username: "root", Password: "root"})
	require.NoError(t, err)

	expectedLogs := "kernel log content here\nmore log lines"
	mockWebClient.EXPECT().
		GetKernelLog(gomock.Any(), gomock.Any()).
		Return(expectedLogs, nil)

	logs, hasMore, err := client.GetLogs(t.Context(), nil, 0)
	require.NoError(t, err)
	assert.Equal(t, expectedLogs, logs)
	assert.False(t, hasMore)
}

func TestClient_GetLogs_WebAPIError(t *testing.T) {
	mockRPCClient, rpcCtrl := setupMockRPCClient(t)
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer rpcCtrl.Finish()
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, mockRPCClient)
	err := client.SetCredentials(sdk.UsernamePassword{Username: "root", Password: "root"})
	require.NoError(t, err)

	mockWebClient.EXPECT().
		GetKernelLog(gomock.Any(), gomock.Any()).
		Return("", fmt.Errorf("connection timeout"))

	_, _, err = client.GetLogs(t.Context(), nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get kernel log")
}

func TestClient_GetDeviceInfo(t *testing.T) {
	mockRPCClient, rpcCtrl := setupMockRPCClient(t)
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer rpcCtrl.Finish()
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, mockRPCClient)

	// Mock the GetVersion RPC call
	mockVersionResponse := &rpc.VersionResponse{
		Version: []rpc.VersionInfo{
			{
				Miner:   "uart_trans.1.3",
				Type:    "Antminer S19",
				BMMiner: "1.0.0",
			},
		},
	}
	mockRPCClient.EXPECT().
		GetVersion(gomock.Any(), gomock.Any()).
		Return(mockVersionResponse, nil)

	// Test without credentials (no web API call)
	deviceInfo, err := client.GetDeviceInfo(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Antminer S19", deviceInfo.Model)
	assert.Equal(t, "Bitmain", deviceInfo.Manufacturer)
	assert.Equal(t, "", deviceInfo.SerialNumber) // No credentials, so no web API call
	assert.Equal(t, "", deviceInfo.MacAddress)

	// Test with credentials (includes web API call)
	err = client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	// Mock the GetVersion RPC call again
	mockRPCClient.EXPECT().
		GetVersion(gomock.Any(), gomock.Any()).
		Return(mockVersionResponse, nil)

	// Mock the GetSystemInfo web API call
	mockSystemInfo := &web.SystemInfo{
		SerialNumber: "ABC123456",
		MacAddr:      "00:11:22:33:44:55",
	}
	mockWebClient.EXPECT().
		GetSystemInfo(gomock.Any(), gomock.Any()).
		Return(mockSystemInfo, nil)

	deviceInfo, err = client.GetDeviceInfo(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Antminer S19", deviceInfo.Model)
	assert.Equal(t, "Bitmain", deviceInfo.Manufacturer)
	assert.Equal(t, "ABC123456", deviceInfo.SerialNumber)
	assert.Equal(t, "00:11:22:33:44:55", deviceInfo.MacAddress)
}

func TestClient_GetStatus(t *testing.T) {
	mockRPCClient, rpcCtrl := setupMockRPCClient(t)
	defer rpcCtrl.Finish()

	client := createTestClientWithMocks(t, nil, mockRPCClient)

	// Mock the GetSummary RPC call
	mockSummaryResponse := &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				GHS5s:          100.5,
				HardwareErrors: 0,
			},
		},
	}
	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Any()).
		Return(mockSummaryResponse, nil)

	// Mock the GetVersion RPC call for firmware version
	mockVersionResponse := &rpc.VersionResponse{
		Version: []rpc.VersionInfo{
			{
				BMMiner: "1.0.0",
			},
		},
	}
	mockRPCClient.EXPECT().
		GetVersion(gomock.Any(), gomock.Any()).
		Return(mockVersionResponse, nil)

	status, err := client.GetStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthHealthyActive, status.State)
	assert.Equal(t, "", status.ErrorMessage)
	assert.Equal(t, "1.0.0", status.FirmwareVersion)

	// Test with zero hashrate (inactive).
	// Note: HardwareErrors is a cumulative counter and should NOT affect health status.
	// A device with accumulated errors but zero hashrate is simply inactive, not critical.
	mockSummaryInactive := &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				GHS5s:          0,
				HardwareErrors: 5,
			},
		},
	}
	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Any()).
		Return(mockSummaryInactive, nil)

	mockRPCClient.EXPECT().
		GetVersion(gomock.Any(), gomock.Any()).
		Return(mockVersionResponse, nil)

	status, err = client.GetStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthHealthyInactive, status.State)
	assert.Empty(t, status.ErrorMessage)

	// Test with hardware errors but active hashrate (device is healthy despite accumulated errors)
	mockSummaryActiveWithErrors := &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				GHS5s:          150.0,
				HardwareErrors: 100,
			},
		},
	}
	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Any()).
		Return(mockSummaryActiveWithErrors, nil)

	mockRPCClient.EXPECT().
		GetVersion(gomock.Any(), gomock.Any()).
		Return(mockVersionResponse, nil)

	status, err = client.GetStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthHealthyActive, status.State)
	assert.Empty(t, status.ErrorMessage)
}

func TestClient_GetStatus_WorkModeCheck(t *testing.T) {
	tests := []struct {
		name          string
		workMode      web.BitmainWorkMode
		legacyMode    string
		expectedState sdk.HealthStatus
	}{
		{
			name:          "zero hashrate in sleep mode is inactive",
			workMode:      web.BitmainWorkModeSleep,
			expectedState: sdk.HealthHealthyInactive,
		},
		{
			name:          "zero hashrate in normal mode is warning",
			workMode:      web.BitmainWorkModeStart,
			expectedState: sdk.HealthWarning,
		},
		{
			name:          "zero hashrate with legacy miner-mode sleep is inactive",
			legacyMode:    string(web.BitmainWorkModeSleep),
			expectedState: sdk.HealthHealthyInactive,
		},
		{
			name:          "zero hashrate with legacy miner-mode normal is warning",
			legacyMode:    string(web.BitmainWorkModeStart),
			expectedState: sdk.HealthWarning,
		},
		{
			name:          "zero hashrate in low power mode is warning",
			workMode:      web.BitmainWorkModeLowPower,
			expectedState: sdk.HealthWarning,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			mockRPCClient, rpcCtrl := setupMockRPCClient(t)
			mockWebClient, webCtrl := setupMockWebClient(t)
			defer rpcCtrl.Finish()
			defer webCtrl.Finish()

			client := createTestClientWithMocks(t, mockWebClient, mockRPCClient)
			err := client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
			require.NoError(t, err)

			mockRPCClient.EXPECT().
				GetSummary(gomock.Any(), gomock.Any()).
				Return(&rpc.SummaryResponse{
					Summary: []rpc.SummaryInfo{{GHS5s: 0}},
				}, nil)
			mockRPCClient.EXPECT().
				GetVersion(gomock.Any(), gomock.Any()).
				Return(&rpc.VersionResponse{Version: []rpc.VersionInfo{{BMMiner: "1.0.0"}}}, nil)
			mockWebClient.EXPECT().
				GetMinerConfig(gomock.Any(), gomock.Any()).
				Return(&web.MinerConfig{
					BitmainWorkMode: tc.workMode,
					MinerMode:       tc.legacyMode,
				}, nil)

			// Act
			status, err := client.GetStatus(t.Context())

			// Assert
			require.NoError(t, err)
			assert.Equal(t, tc.expectedState, status.State)
		})
	}
}

func TestClient_GetStatus_WorkModeCheckFallback(t *testing.T) {
	// When GetMinerConfig fails, GetStatus should fall back to HealthHealthyInactive
	// rather than propagating the error, since the hashrate check already determined
	// the device is not actively mining.

	// Arrange
	mockRPCClient, rpcCtrl := setupMockRPCClient(t)
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer rpcCtrl.Finish()
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, mockRPCClient)
	err := client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Any()).
		Return(&rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{{GHS5s: 0}},
		}, nil)
	mockRPCClient.EXPECT().
		GetVersion(gomock.Any(), gomock.Any()).
		Return(&rpc.VersionResponse{Version: []rpc.VersionInfo{{BMMiner: "1.0.0"}}}, nil)
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("connection refused"))

	// Act
	status, err := client.GetStatus(t.Context())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthHealthyInactive, status.State)
}

func TestClient_GetTelemetry(t *testing.T) {
	mockRPCClient, rpcCtrl := setupMockRPCClient(t)
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer rpcCtrl.Finish()
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, mockRPCClient)

	// Set credentials first (required for stats.cgi)
	err := client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	// Mock the GetSummary RPC call for hashrate
	mockSummaryResponse := &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				GHS5s:   100.5,
				Elapsed: 3600,
			},
		},
	}
	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Any()).
		Return(mockSummaryResponse, nil)

	// Mock the GetStatsInfo web API call for temperature and component metrics
	mockStatsInfo := &web.StatsInfo{
		STATUS: web.StatsStatus{
			Status:     "S",
			When:       1234567890,
			Msg:        "stats",
			APIVersion: "1.0.0",
		},
		INFO: web.StatsMinerInfo{
			MinerVersion: "uart_trans.1.3",
			CompileTime:  "Thu Jul 11 16:38:25 CST 2024",
			Type:         "Antminer S21",
		},
		STATS: []web.StatsData{
			{
				Elapsed:  3600,
				Rate5s:   100500.0, // GH/s
				ChainNum: 3,
				FanNum:   4,
				Fan:      []int{7000, 7100, 7200, 7300},
				HWPTotal: 0.0006,
				Chain: []web.ChainStats{
					{
						Index:    0,
						FreqAvg:  490,
						RateReal: 33500.0,
						ASICNum:  108,
						TempChip: []float64{59.0, 59.0, 73.0, 73.0}, // [inlet_1, inlet_2, outlet_1, outlet_2]
						HW:       0,
						SN:       "SMTTYRHBDJAAI019D",
					},
					{
						Index:    1,
						FreqAvg:  490,
						RateReal: 33500.0,
						ASICNum:  108,
						TempChip: []float64{61.0, 61.0, 75.0, 75.0},
						HW:       0,
						SN:       "SMTTYRHBDJAAI019E",
					},
					{
						Index:    2,
						FreqAvg:  490,
						RateReal: 33500.0,
						ASICNum:  108,
						TempChip: []float64{63.0, 63.0, 77.0, 77.0},
						HW:       0,
						SN:       "SMTTYRHBDJAAI019F",
					},
				},
			},
		},
	}
	mockWebClient.EXPECT().
		GetStatsInfo(gomock.Any(), gomock.Any()).
		Return(mockStatsInfo, nil)

	// Mock the RPC stats call for power data
	mockStatsResponse := &rpc.StatsResponse{
		Status: []rpc.StatusInfo{{Status: "S"}},
		Stats: []json.RawMessage{
			json.RawMessage(`{"BMMiner":"1.0.0","Type":"Antminer S21"}`),
			json.RawMessage(`{"STATS":0,"ID":"BTM_SOC0","chain_power":"3250 W"}`),
		},
	}
	mockRPCClient.EXPECT().
		GetStats(gomock.Any(), gomock.Any()).
		Return(mockStatsResponse, nil)

	telemetry, err := client.GetTelemetry(t.Context())
	require.NoError(t, err)

	// Verify device-level metrics
	expectedHashrate := 100.5 * GHSToHS
	assert.InEpsilon(t, expectedHashrate, *telemetry.HashrateHS, 0.01)
	assert.Equal(t, int64(3600), *telemetry.UptimeSeconds)

	// Verify temperature (max of all temp_chip values: 77.0°C)
	assert.InEpsilon(t, 77.0, *telemetry.TemperatureCelsius, 0.01)

	// Verify fan speed (max of all fans: 7300 RPM)
	assert.InEpsilon(t, 7300.0, *telemetry.FanRPM, 0.01)

	// Verify hardware error rate
	assert.InEpsilon(t, 0.0006, *telemetry.HardwareErrorRate, 0.0001)

	// Verify component-level metrics
	require.Len(t, telemetry.HashBoards, 3)
	require.Len(t, telemetry.Fans, 4)

	// Verify first hashboard
	assert.Equal(t, 0, telemetry.HashBoards[0].Index)
	assert.Equal(t, "SMTTYRHBDJAAI019D", telemetry.HashBoards[0].SerialNumber)
	assert.InEpsilon(t, 73.0, *telemetry.HashBoards[0].Temperature, 0.01) // max of temp_chip
	assert.InEpsilon(t, 59.0, *telemetry.HashBoards[0].InletTemp, 0.01)   // avg of first 2
	assert.InEpsilon(t, 73.0, *telemetry.HashBoards[0].OutletTemp, 0.01)  // avg of last 2
	assert.Equal(t, 108, telemetry.HashBoards[0].ChipCount)
	assert.Equal(t, 490, telemetry.HashBoards[0].ChipFrequencyMHz)

	// Verify fans
	assert.Equal(t, 0, telemetry.Fans[0].Index)
	assert.Equal(t, 7000, telemetry.Fans[0].RPM)
	assert.Equal(t, 3, telemetry.Fans[3].Index)
	assert.Equal(t, 7300, telemetry.Fans[3].RPM)

	// Verify power and efficiency
	require.NotNil(t, telemetry.PowerWatts, "PowerWatts should be set from RPC stats")
	assert.InDelta(t, 3250.0, *telemetry.PowerWatts, 0.01)

	require.NotNil(t, telemetry.EfficiencyJPerHash, "EfficiencyJPerHash should be computed")
	expectedEfficiency := 3250.0 / expectedHashrate // J/H = W / (H/s)
	assert.InEpsilon(t, expectedEfficiency, *telemetry.EfficiencyJPerHash, 0.01)
}

func TestClient_Pair(t *testing.T) {
	mockWebClient, ctrl := setupMockWebClient(t)
	defer ctrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	creds := sdk.UsernamePassword{Username: "admin", Password: "password"}

	// Mock successful system info call for pairing validation
	mockSystemInfo := &web.SystemInfo{
		SerialNumber: "ABC123456",
		MacAddr:      "00:11:22:33:44:55",
	}
	mockWebClient.EXPECT().
		GetSystemInfo(gomock.Any(), gomock.Any()).
		Return(mockSystemInfo, nil)

	err := client.Pair(t.Context(), creds)
	require.NoError(t, err)

	// Verify credentials were set
	assert.Equal(t, "admin", client.credentials.Username)
	assert.Equal(t, "password", client.credentials.Password)
}

func TestClient_StartStopMining(t *testing.T) {
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	baseConfig := &web.MinerConfig{
		Pools:           []web.Pool{{URL: "stratum+tcp://pool.example.com:3333", Username: "worker", Password: "x"}},
		BitmainWorkMode: web.BitmainWorkModeStart,
	}

	// Test StartMining - should fetch config and set work mode to "0"
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(baseConfig, nil)
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ any, config *web.MinerConfig) error {
			assert.Equal(t, web.BitmainWorkModeStart, config.BitmainWorkMode)
			assert.Equal(t, baseConfig.Pools, config.Pools) // pools preserved
			return nil
		})

	err := client.StartMining(t.Context())
	require.NoError(t, err)

	// Test StopMining - should fetch config and set work mode to "1" (sleep)
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(baseConfig, nil)
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ any, config *web.MinerConfig) error {
			assert.Equal(t, web.BitmainWorkModeSleep, config.BitmainWorkMode)
			assert.Equal(t, baseConfig.Pools, config.Pools) // pools preserved
			return nil
		})

	err = client.StopMining(t.Context())
	require.NoError(t, err)

	// Test StartMining when GetMinerConfig fails
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("config fetch failed"))

	err = client.StartMining(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get current miner config")

	// Test StartMining when SetMinerConfig fails
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(baseConfig, nil)
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("API error"))

	err = client.StartMining(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestClient_StartStopMining_LegacyMinerMode(t *testing.T) {
	// Older Antminer firmware uses "miner-mode" instead of "bitmain-work-mode".
	// setWorkMode should detect which field the device uses and update that one.
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	legacyConfig := &web.MinerConfig{
		Pools:     []web.Pool{{URL: "stratum+tcp://pool.example.com:3333", Username: "worker", Password: "x"}},
		MinerMode: string(web.BitmainWorkModeStart), // legacy device reports "miner-mode", not "bitmain-work-mode"
	}

	// Test StopMining — should update MinerMode, not BitmainWorkMode
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(legacyConfig, nil)
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ any, config *web.MinerConfig) error {
			// Act (done by the caller)
			// Assert
			assert.Equal(t, string(web.BitmainWorkModeSleep), config.MinerMode)
			assert.Equal(t, web.BitmainWorkMode(""), config.BitmainWorkMode) // not touched
			assert.Equal(t, legacyConfig.Pools, config.Pools)                // pools preserved
			return nil
		})

	err := client.StopMining(t.Context())
	require.NoError(t, err)

	// Test StartMining — should update MinerMode, not BitmainWorkMode
	mockWebClient.EXPECT().
		GetMinerConfig(gomock.Any(), gomock.Any()).
		Return(&web.MinerConfig{
			MinerMode: string(web.BitmainWorkModeSleep),
		}, nil)
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ any, config *web.MinerConfig) error {
			// Act (done by the caller)
			// Assert
			assert.Equal(t, string(web.BitmainWorkModeStart), config.MinerMode)
			assert.Equal(t, web.BitmainWorkMode(""), config.BitmainWorkMode)
			return nil
		})

	err = client.StartMining(t.Context())
	require.NoError(t, err)
}

func TestClient_ErrorCases(t *testing.T) {
	t.Run("GetDeviceInfo_NoVersionInfo", func(t *testing.T) {
		mockRPCClient, rpcCtrl := setupMockRPCClient(t)
		defer rpcCtrl.Finish()

		client := createTestClientWithMocks(t, nil, mockRPCClient)

		// Mock empty version response
		mockVersionResponse := &rpc.VersionResponse{
			Version: []rpc.VersionInfo{}, // Empty version info
		}
		mockRPCClient.EXPECT().
			GetVersion(gomock.Any(), gomock.Any()).
			Return(mockVersionResponse, nil)

		_, err := client.GetDeviceInfo(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no version information available")
	})

	t.Run("GetDeviceInfo_RPCFailure", func(t *testing.T) {
		mockRPCClient, rpcCtrl := setupMockRPCClient(t)
		defer rpcCtrl.Finish()

		client := createTestClientWithMocks(t, nil, mockRPCClient)

		// Mock RPC failure
		mockRPCClient.EXPECT().
			GetVersion(gomock.Any(), gomock.Any()).
			Return(nil, fmt.Errorf("RPC connection failed"))

		_, err := client.GetDeviceInfo(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get version info")
		assert.Contains(t, err.Error(), "RPC connection failed")
	})

	t.Run("GetStatus_NoSummaryInfo", func(t *testing.T) {
		mockRPCClient, rpcCtrl := setupMockRPCClient(t)
		defer rpcCtrl.Finish()

		client := createTestClientWithMocks(t, nil, mockRPCClient)

		// Mock empty summary response
		mockSummaryResponse := &rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{}, // Empty summary info
		}
		mockRPCClient.EXPECT().
			GetSummary(gomock.Any(), gomock.Any()).
			Return(mockSummaryResponse, nil)

		_, err := client.GetStatus(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no summary information available")
	})

	t.Run("GetTelemetry_NoSummaryInfo", func(t *testing.T) {
		mockRPCClient, rpcCtrl := setupMockRPCClient(t)
		defer rpcCtrl.Finish()

		client := createTestClientWithMocks(t, nil, mockRPCClient)

		// Mock empty summary response
		mockSummaryResponse := &rpc.SummaryResponse{
			Summary: []rpc.SummaryInfo{}, // Empty summary info
		}
		mockRPCClient.EXPECT().
			GetSummary(gomock.Any(), gomock.Any()).
			Return(mockSummaryResponse, nil)

		_, err := client.GetTelemetry(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no summary information available")
	})

	t.Run("UpdatePools_EmptyPoolList", func(t *testing.T) {
		mockWebClient, ctrl := setupMockWebClient(t)
		defer ctrl.Finish()

		client := createTestClientWithMocks(t, mockWebClient, nil)

		// Set credentials
		err := client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
		require.NoError(t, err)

		// Mock successful config operations
		mockConfig := &web.MinerConfig{
			Pools: []web.Pool{},
		}
		mockWebClient.EXPECT().
			GetMinerConfig(gomock.Any(), gomock.Any()).
			Return(mockConfig, nil)

		mockWebClient.EXPECT().
			SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		// Test with empty pool list - should succeed
		err = client.UpdatePools(t.Context(), []Pool{})
		require.NoError(t, err)
	})
}

func TestClient_BlinkLED_ConcurrentCalls(t *testing.T) {
	mockWebClient, ctrl := setupMockWebClient(t)
	defer ctrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	// Set credentials first
	err := client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	// Mock blink operations
	mockWebClient.EXPECT().
		StartBlink(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	mockWebClient.EXPECT().
		StopBlink(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	// Start first blink
	err = client.BlinkLED(t.Context(), 100*time.Millisecond)
	require.NoError(t, err)

	// Immediately try to start second blink - should fail
	err = client.BlinkLED(t.Context(), 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LED is already blinking")
}
