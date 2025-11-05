package client

import (
	"context"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

// GetPairingInfo retrieves pairing information from a Proto miner at the specified address
func GetPairingInfo(ctx context.Context, ipAddress string, port string, protocol networking.Protocol) (*connect.Response[miner_system_api.GetPairingInfoResponse], error) {
	connectionInfo, err := networking.NewConnectionInfo(ipAddress, port, protocol)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create connection info: %v", err)
	}

	minerClient, err := CreateClient(
		miner_system_apiconnect.NewMinerPairingApiClient,
		*connectionInfo,
	)
	if err != nil {
		return nil, err
	}

	pairingInfo, err := minerClient.GetPairingInfo(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, err
	}
	return pairingInfo, nil
}
