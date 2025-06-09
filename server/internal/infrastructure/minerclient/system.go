package minerclient

import (
	"connectrpc.com/connect"
	"context"
	minerPbCommon "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api"
	minerPb "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
)

// GetPairingInfo executes the GetPairingInfo RPC on a miner
func (s *Service) GetPairingInfo(ctx context.Context, url string) (*connect.Response[miner_system_api.GetPairingInfoResponse], error) {
	request := Request[minerPbCommon.EmptyRequest, miner_system_api.GetPairingInfoResponse, minerPb.MinerSystemApiClient]{
		ClientFactory: minerPb.NewMinerSystemApiClient,
		RPCCall:       minerPb.MinerSystemApiClient.GetPairingInfo,
		RequestDTO:    &minerPbCommon.EmptyRequest{},
	}

	return ExecuteWithoutAuth(ctx, s, url, request)
}
