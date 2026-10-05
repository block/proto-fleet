package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) SetContainerControl(ctx context.Context, req sdk.ContainerControlRequest) error {
	if controller, ok := m.delegate.(sdk.DeviceContainerController); ok {
		return controller.SetContainerControl(ctx, req)
	}
	return m.unsupported("equipment commands")
}
