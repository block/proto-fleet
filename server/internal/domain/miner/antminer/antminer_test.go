package antminer_test

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/dto"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	rpcMocks "github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc/mocks"
)

func TestAntminer_StartMining(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWebClient := mocks.NewMockWebAPIClient(ctrl)
	deviceID := models.DeviceIdentifier("123")
	ipAddress := "192.168.1.100"
	port := uint16(80)
	username := "admin"
	password := *secrets.NewText("password")

	// Expectations
	expectedConnInfo := &web.AntminerConnectionInfo{
		ConnectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(ipAddress),
			Port:      networking.Port(port),
		},
		Username: username,
		Password: password,
	}

	expectedConfig := &web.MinerConfig{
		BitmainWorkMode: web.BitmainWorkModeStart,
	}

	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Eq(expectedConnInfo), gomock.Eq(expectedConfig)).
		Return(nil)

	minerInfo := antminer.NewAntminerInfo(deviceID, ipAddress, port)
	miner := antminer.NewAntminer(
		minerInfo,
		username,
		password,
		mockWebClient,
		nil,
	)

	// Act
	err := miner.StartMining(t.Context())

	// Assert
	assert.NoError(t, err)
}

func TestAntminer_StopMining(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWebClient := mocks.NewMockWebAPIClient(ctrl)
	deviceID := models.DeviceIdentifier("123")
	ipAddress := "192.168.1.100"
	port := uint16(80)
	username := "admin"
	password := *secrets.NewText("password")

	// Expectations
	expectedConnInfo := &web.AntminerConnectionInfo{
		ConnectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(ipAddress),
			Port:      networking.Port(port),
		},
		Username: username,
		Password: password,
	}

	expectedConfig := &web.MinerConfig{
		BitmainWorkMode: web.BitmainWorkModeSleep,
	}

	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Eq(expectedConnInfo), gomock.Eq(expectedConfig)).
		Return(nil)

	minerInfo := antminer.NewAntminerInfo(deviceID, ipAddress, port)
	miner := antminer.NewAntminer(
		minerInfo,
		username,
		password,
		mockWebClient,
		nil,
	)

	// Act
	err := miner.StopMining(t.Context())

	// Assert
	assert.NoError(t, err)
}

func TestAntminer_UpdateMiningPools(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWebClient := mocks.NewMockWebAPIClient(ctrl)
	deviceID := models.DeviceIdentifier("123")
	ipAddress := "192.168.1.100"
	port := uint16(80)
	username := "admin"
	password := *secrets.NewText("password")

	// Expectations
	expectedConnInfo := &web.AntminerConnectionInfo{
		ConnectionInfo: networking.ConnectionInfo{
			IPAddress: networking.IPAddress(ipAddress),
			Port:      networking.Port(port),
		},
		Username: username,
		Password: password,
	}

	expectedConfig := &web.MinerConfig{
		Pools: []web.Pool{
			{
				URL:      "https://default.pool.example.com",
				Username: "defaultuser",
				Password: "defaultpass",
			},
			{
				URL:      "https://backup1.pool.example.com",
				Username: "backup1user",
				Password: "backup1pass",
			},
			{
				URL:      "https://backup2.pool.example.com",
				Username: "backup2user",
				Password: "backup2pass",
			},
		},
	}

	mockWebClient.EXPECT().
		SetMinerConfig(gomock.Any(), gomock.Eq(expectedConnInfo), gomock.Eq(expectedConfig)).
		Return(nil)

	minerInfo := antminer.NewAntminerInfo(deviceID, ipAddress, port)
	miner := antminer.NewAntminer(
		minerInfo,
		username,
		password,
		mockWebClient,
		nil,
	)

	// Act
	err := miner.UpdateMiningPools(t.Context(), dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			URL:      "https://default.pool.example.com",
			Username: "defaultuser",
			Password: "defaultpass",
		},
		Backup1Pool: &dto.MiningPool{
			URL:      "https://backup1.pool.example.com",
			Username: "backup1user",
			Password: "backup1pass",
		},
		Backup2Pool: &dto.MiningPool{
			URL:      "https://backup2.pool.example.com",
			Username: "backup2user",
			Password: "backup2pass",
		},
	})

	// Assert
	assert.NoError(t, err)
}

func TestAntminer_GetTelemetry(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWebClient := mocks.NewMockWebAPIClient(ctrl)
	mockRPCClient := rpcMocks.NewMockRPCClient(ctrl)
	deviceID := models.DeviceIdentifier("123")
	ipAddress := "192.168.1.100"
	port := uint16(80)
	username := "admin"
	password := *secrets.NewText("password")

	// Create response mocks
	summaryResponse := &rpc.SummaryResponse{
		Summary: []rpc.SummaryInfo{
			{
				Elapsed:        3600,
				GHS5s:          14.5,
				GHSAv:          15.0,
				GHS30m:         14.8,
				HardwareErrors: 5,
				BestShare:      12345678,
			},
		},
	}

	devsResponse := &rpc.DevsResponse{
		Devs: []rpc.DevInfo{
			{
				ASC:            0,
				Name:           "ASC0",
				ID:             0,
				Temperature:    80.5,
				MHS5s:          5000,
				MHSAv:          5100,
				HardwareErrors: 2,
			},
			{
				ASC:            1,
				Name:           "ASC1",
				ID:             1,
				Temperature:    82.0,
				MHS5s:          4900,
				MHSAv:          4950,
				HardwareErrors: 3,
			},
		},
	}

	// Set expectations
	expectedConnInfo := &networking.ConnectionInfo{
		IPAddress: networking.IPAddress(ipAddress),
		Port:      networking.Port(port),
		Protocol:  networking.ProtocolTCP,
	}

	mockRPCClient.EXPECT().
		GetSummary(gomock.Any(), gomock.Eq(expectedConnInfo)).
		Return(summaryResponse, nil)

	mockRPCClient.EXPECT().
		GetDevs(gomock.Any(), gomock.Eq(expectedConnInfo)).
		Return(devsResponse, nil)

	minerInfo := antminer.NewAntminerInfo(deviceID, ipAddress, port)
	miner := antminer.NewAntminer(
		minerInfo,
		username,
		password,
		mockWebClient,
		mockRPCClient,
	)

	// Act
	timestamp := time.Now().Add(-1 * time.Hour)
	actualTelemetry, err := miner.GetTelemetry(t.Context(), timestamp)

	// Assert
	require.NoError(t, err)
	assert.NotEmpty(t, actualTelemetry)
	assert.Len(t, actualTelemetry, 6, "Should return 6 telemetry metrics (2 miner-level + 4 ASIC-level)")

	// Create expected telemetry objects - using the first element's timestamp to match actual
	actualTimestamp := actualTelemetry[0].Timestamp
	componentID := "0"

	expectedTelemetry := []telemetryModels.Telemetry{
		// Miner-level hashrate
		{
			Measurement: "hashrate_mhs",
			Fields: map[string]any{
				"value": 15.0 * 1000, // GHSAv * 1000 (converted to MHS)
			},
			Tags: map[string]string{
				"device_id":      deviceID.String(),
				"component_type": "miner",
				"component_id":   componentID,
				"hashrate_type":  "HASHRATE_TYPE_AVERAGE",
			},
			Timestamp: actualTimestamp,
		},
		{
			Measurement: "temperature_c",
			Fields: map[string]any{
				"value": 82.0, // Max temperature from ASICs
			},
			Tags: map[string]string{
				"device_id":      deviceID.String(),
				"component_type": "miner",
				"component_id":   componentID,
			},
			Timestamp: actualTimestamp,
		},
		{
			Measurement: "temperature_c",
			Fields: map[string]any{
				"value": 80.5,
			},
			Tags: map[string]string{
				"device_id":      deviceID.String(),
				"component_type": "asic",
				"component_id":   "0",
			},
			Timestamp: actualTimestamp,
		},
		{
			Measurement: "hashrate_mhs",
			Fields: map[string]any{
				"value": 5100.0,
			},
			Tags: map[string]string{
				"device_id":      deviceID.String(),
				"component_type": "asic",
				"component_id":   "0",
				"hashrate_type":  "HASHRATE_TYPE_AVERAGE",
			},
			Timestamp: actualTimestamp,
		},
		{
			Measurement: "temperature_c",
			Fields: map[string]any{
				"value": 82.0,
			},
			Tags: map[string]string{
				"device_id":      deviceID.String(),
				"component_type": "asic",
				"component_id":   "1",
			},
			Timestamp: actualTimestamp,
		},
		{
			Measurement: "hashrate_mhs",
			Fields: map[string]any{
				"value": 4950.0,
			},
			Tags: map[string]string{
				"device_id":      deviceID.String(),
				"component_type": "asic",
				"component_id":   "1",
				"hashrate_type":  "HASHRATE_TYPE_AVERAGE",
			},
			Timestamp: actualTimestamp,
		},
	}

	assert.Equal(t, len(expectedTelemetry), len(actualTelemetry), "Should have same number of telemetry points")

	// Using a custom comparison since the component_id for miner-level metrics isn't predictable
	for i, expected := range expectedTelemetry {
		if i < len(actualTelemetry) {
			actual := actualTelemetry[i]

			assert.Equal(t, expected.Measurement, actual.Measurement, "Measurement should match for item %d", i)
			assert.Equal(t, expected.Fields["value"], actual.Fields["value"], "Value should match for item %d", i)
			assert.Equal(t, expected.Tags, actual.Tags, "Tags should match for item %d", i)
		}
	}
}
