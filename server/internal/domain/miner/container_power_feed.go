package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) ContainerPowerFeed(ctx context.Context, req sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	if c, ok := m.delegate.(sdk.DeviceContainerPowerFeed); ok {
		return c.ContainerPowerFeed(ctx, req)
	}
	return sdk.ContainerPowerFeedResult{}, m.unsupported("container power feed")
}
