package antminer

import (
	"fmt"
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/rpc/mocks"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/web"
	webmocks "github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/web/mocks"
	"github.com/btc-mining/proto-fleet/server/sdk/v1"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	_, _, err := client.GetLogs(t.Context(), nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")

	err = client.UpdateFirmware(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
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

	// Test with hardware errors
	mockSummaryWithErrors := &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				GHS5s:          0,
				HardwareErrors: 5,
			},
		},
	}
	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Any()).
		Return(mockSummaryWithErrors, nil)

	mockRPCClient.EXPECT().
		GetVersion(gomock.Any(), gomock.Any()).
		Return(mockVersionResponse, nil)

	status, err = client.GetStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, sdk.HealthCritical, status.State)
	assert.Contains(t, status.ErrorMessage, "Hardware errors: 5")
}

func TestClient_GetTelemetry(t *testing.T) {
	mockRPCClient, rpcCtrl := setupMockRPCClient(t)
	mockWebClient, webCtrl := setupMockWebClient(t)
	defer rpcCtrl.Finish()
	defer webCtrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, mockRPCClient)

	// Mock the GetSummary RPC call
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

	// Mock the GetDevs RPC call for temperature
	mockDevsResponse := &rpc.DevsResponse{
		Devs: []rpc.DevInfo{
			{
				Temperature: 65.5,
			},
			{
				Temperature: 67.2,
			},
		},
	}
	mockRPCClient.EXPECT().
		GetDevs(gomock.Any(), gomock.Any()).
		Return(mockDevsResponse, nil)

	// Test without credentials (no web API call)
	telemetry, err := client.GetTelemetry(t.Context())
	require.NoError(t, err)
	expectedHashrate := 100.5 * GHSToHS
	assert.InEpsilon(t, expectedHashrate, *telemetry.HashrateHS, 0.01)
	assert.Equal(t, int64(3600), *telemetry.UptimeSeconds)
	assert.InEpsilon(t, 66.35, *telemetry.TemperatureCelsius, 0.01) // Average of 65.5 and 67.2

	// Test with credentials (includes web API call)
	err = client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"})
	require.NoError(t, err)

	// Mock the calls again for the second test
	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Any()).
		Return(mockSummaryResponse, nil)

	mockRPCClient.EXPECT().
		GetDevs(gomock.Any(), gomock.Any()).
		Return(mockDevsResponse, nil)

	// Mock the GetMinerSummary web API call
	mockWebSummary := &web.MinerSummary{
		Summary: []struct {
			Elapsed   int     `json:"elapsed"`
			Rate5s    float64 `json:"rate_5s"`
			Rate30m   float64 `json:"rate_30m"`
			RateAvg   float64 `json:"rate_avg"`
			RateIdeal float64 `json:"rate_ideal"`
			RateUnit  string  `json:"rate_unit"`
			HwAll     int     `json:"hw_all"`
			BestShare int64   `json:"bestshare"`
			Status    []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
				Code   int    `json:"code"`
				Msg    string `json:"msg"`
			} `json:"status"`
		}{
			{
				Rate5s: 110.0, // TH/s from web API
			},
		},
	}
	mockWebClient.EXPECT().
		GetMinerSummary(gomock.Any(), gomock.Any()).
		Return(mockWebSummary, nil)

	telemetry, err = client.GetTelemetry(t.Context())
	require.NoError(t, err)
	// Should use web API data: 110 TH/s * 1000 = 110000 GH/s * 1e9 = 1.1e14 H/s
	expectedHashrateFromWeb := 110000.0 * GHSToHS
	assert.InEpsilon(t, expectedHashrateFromWeb, *telemetry.HashrateHS, 0.01)
	// Temperature should still be calculated from RPC data
	assert.InEpsilon(t, 66.35, *telemetry.TemperatureCelsius, 0.01) // Average of 65.5 and 67.2
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
	mockWebClient, ctrl := setupMockWebClient(t)
	defer ctrl.Finish()

	client := createTestClientWithMocks(t, mockWebClient, nil)

	// Test StartMining - should succeed
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	err := client.StartMining(t.Context())
	require.NoError(t, err)

	// Test StopMining - should succeed
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	err = client.StopMining(t.Context())
	require.NoError(t, err)

	// Test StartMining with API error
	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("API error"))

	err = client.StartMining(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
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
