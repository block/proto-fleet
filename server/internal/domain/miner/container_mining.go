package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) ContainerMining(ctx context.Context, req sdk.ContainerMiningRequest) (sdk.ContainerMiningResult, error) {
	if c, ok := m.delegate.(sdk.DeviceContainerMining); ok {
		return c.ContainerMining(ctx, req)
	}
	return sdk.ContainerMiningResult{}, m.unsupported("container mining")
}
