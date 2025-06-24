package antminer_test

import (
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/web/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestAntminer_StartMining(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWebClient := mocks.NewMockWebAPIClient(ctrl)
	deviceID := int64(123)
	ipAddress := "192.168.1.100"
	port := uint16(80)
	rpcPort := "4028"
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

	miner := antminer.NewAntminer(
		deviceID,
		ipAddress,
		port,
		rpcPort,
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
	deviceID := int64(123)
	ipAddress := "192.168.1.100"
	port := uint16(80)
	rpcPort := "4028"
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

	miner := antminer.NewAntminer(
		deviceID,
		ipAddress,
		port,
		rpcPort,
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
