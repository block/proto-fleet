package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) SetModuleMining(ctx context.Context, req sdk.ModuleMiningRequest) error {
	if controller, ok := m.delegate.(sdk.DeviceModuleMiner); ok {
		return controller.SetModuleMining(ctx, req)
	}
	return m.unsupported("module mining")
}
