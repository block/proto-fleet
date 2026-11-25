package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) RunContainerAlarmAction(ctx context.Context, req sdk.ContainerAlarmRequest) error {
	if controller, ok := m.delegate.(sdk.DeviceContainerAlarmController); ok {
		return controller.RunContainerAlarmAction(ctx, req)
	}
	return m.unsupported("container alarm actions")
}
