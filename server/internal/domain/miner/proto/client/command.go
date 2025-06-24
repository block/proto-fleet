package client

import (
	"context"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api"
	minerPb "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	minerPbCommon "github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
)

type MinerCommandFunc func(ctx context.Context, minerConnectionInfo *MinerConnectionInfo) (*connect.Response[miner_command_api.CommandResponse], error)

// StartMining executes the StartMining RPC on a miner
func (s *Service) StartMining(ctx context.Context, minerConnectionInfo *MinerConnectionInfo) (*connect.Response[miner_command_api.CommandResponse], error) {
	request := Request[minerPbCommon.EmptyRequest, miner_command_api.CommandResponse, minerPb.MinerCommandApiClient]{
		ClientFactory: minerPb.NewMinerCommandApiClient,
		RPCCall:       minerPb.MinerCommandApiClient.StartMining,
		RequestDTO:    &minerPbCommon.EmptyRequest{},
	}

	return ExecuteWithAuth(ctx, s, minerConnectionInfo, request)
}

// StopMining executes the StopMining RPC on a miner
func (s *Service) StopMining(ctx context.Context, minerConnectionInfo *MinerConnectionInfo) (*connect.Response[miner_command_api.CommandResponse], error) {
	request := Request[minerPbCommon.EmptyRequest, miner_command_api.CommandResponse, minerPb.MinerCommandApiClient]{
		ClientFactory: minerPb.NewMinerCommandApiClient,
		RPCCall:       minerPb.MinerCommandApiClient.StopMining,
		RequestDTO:    &minerPbCommon.EmptyRequest{},
	}

	return ExecuteWithAuth(ctx, s, minerConnectionInfo, request)
}
