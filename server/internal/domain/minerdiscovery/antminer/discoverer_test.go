package antminer_test

import (
	"errors"
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/antminer"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ipAddress = "192.168.1.100"
	port4028  = "4028"
	portWrong = "80"
)

func setup(t *testing.T) (*mocks.MockRPCClient, *antminer.Discoverer) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRPCClient := mocks.NewMockRPCClient(ctrl)
	discoverer := antminer.NewDiscoverer(mockRPCClient)
	return mockRPCClient, discoverer
}

func TestDiscoverer_Discover_Success(t *testing.T) {
	// Arrange
	mockRPCClient, discoverer := setup(t)

	model := "S19j Pro"
	manufacturer := "Bitmain"
	expectedConnInfo, err := networking.NewConnectionInfo(ipAddress, port4028, networking.ProtocolTCP)
	require.NoError(t, err)

	mockRPCClient.EXPECT().
		GetVersion(t.Context(), expectedConnInfo).
		Return(&rpc.VersionResponse{
			Status: []rpc.StatusInfo{{Status: "S", Msg: "Success"}},
			Version: []rpc.VersionInfo{{
				BMMiner: "2.0.0", API: "3.0", Miner: model, Type: "Antminer " + model,
			}},
			ID: 1,
		}, nil)

	// Act
	result, err := discoverer.Discover(t.Context(), ipAddress, port4028)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ipAddress, result.Device.IpAddress)
	assert.Equal(t, port4028, result.Device.Port)
	assert.Equal(t, model, result.Device.Model)
	assert.Equal(t, manufacturer, result.Device.Manufacturer)
	assert.Equal(t, models.TypeAntminer.String(), result.Type)
}

func TestDiscoverer_Discover_WrongPort(t *testing.T) {
	// Arrange
	_, discoverer := setup(t)

	// Act
	_, err := discoverer.Discover(t.Context(), ipAddress, portWrong)

	// Assert
	require.ErrorIs(t, err, minerdiscovery.MinerNotFoundFleetError)
}

func TestDiscoverer_Discover_NotAntminer(t *testing.T) {
	// Arrange
	mockRPCClient, discoverer := setup(t)

	expectedConnInfo, err := networking.NewConnectionInfo(ipAddress, port4028, networking.ProtocolTCP)
	require.NoError(t, err)

	mockRPCClient.EXPECT().
		GetVersion(t.Context(), expectedConnInfo).
		Return(&rpc.VersionResponse{
			Status: []rpc.StatusInfo{{Status: "S", Msg: "Success"}},
			Version: []rpc.VersionInfo{{
				Miner: "Unknown", Type: "OtherMiner",
			}},
			ID: 1,
		}, nil)

	// Act
	_, err = discoverer.Discover(t.Context(), ipAddress, port4028)

	// Assert
	require.ErrorIs(t, err, minerdiscovery.MinerNotFoundFleetError)
}

func TestDiscoverer_Discover_UnknownModel(t *testing.T) {
	// Arrange
	mockRPCClient, discoverer := setup(t)

	expectedConnInfo, err := networking.NewConnectionInfo(ipAddress, port4028, networking.ProtocolTCP)
	require.NoError(t, err)

	mockRPCClient.EXPECT().
		GetVersion(t.Context(), expectedConnInfo).
		Return(&rpc.VersionResponse{
			Status: []rpc.StatusInfo{{Status: "S", Msg: "Success"}},
			Version: []rpc.VersionInfo{{
				Miner: "", Type: "Antminer S19",
			}},
			ID: 1,
		}, nil)

	// Act
	result, err := discoverer.Discover(t.Context(), ipAddress, port4028)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Unknown Antminer", result.Device.Model)
}

func TestDiscoverer_Discover_RPCError(t *testing.T) {
	// Arrange
	mockRPCClient, discoverer := setup(t)

	expectedConnInfo, err := networking.NewConnectionInfo(ipAddress, port4028, networking.ProtocolTCP)
	require.NoError(t, err)

	mockRPCClient.EXPECT().
		GetVersion(t.Context(), expectedConnInfo).
		Return(nil, errors.New("connection refused"))

	// Act
	result, err := discoverer.Discover(t.Context(), ipAddress, port4028)

	// Assert
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to get version info")
}

func TestDiscoverer_GetMinerType(t *testing.T) {
	// Arrange
	_, discoverer := setup(t)

	// Act
	minerType := discoverer.GetMinerType()

	// Assert
	assert.Equal(t, models.TypeAntminer, minerType)
}
