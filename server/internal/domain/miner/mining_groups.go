package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if c, ok := m.delegate.(sdk.DeviceMiningGroups); ok {
		return c.MiningGroups(ctx, r)
	}
	return sdk.MiningGroupResult{}, m.unsupported("mining groups")
}
