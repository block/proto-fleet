package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) SetBoardIdentify(ctx context.Context, req sdk.BoardIdentifyRequest) error {
	if controller, ok := m.delegate.(sdk.DeviceBoardIdentifier); ok {
		return controller.SetBoardIdentify(ctx, req)
	}
	return m.unsupported("board identify")
}
