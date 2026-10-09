package remotenode

import (
	"context"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *Miner) SetTankMining(ctx context.Context, req sdk.TankMiningRequest) error {
	if err := req.Validate(); err != nil {
		return devicewrite.NotDispatched(err)
	}
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_SetTankMining{SetTankMining: &gatewaypb.SetTankMiningAction{Positions: req.Positions, Mining: req.Mining}}})
}
