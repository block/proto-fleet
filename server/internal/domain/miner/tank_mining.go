package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) SetTankMining(ctx context.Context, req sdk.TankMiningRequest) error {
	if controller, ok := m.delegate.(sdk.DeviceTankMiner); ok {
		return controller.SetTankMining(ctx, req)
	}
	return m.unsupported("tank mining")
}
