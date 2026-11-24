package remotenode

import (
	"context"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ContainerAlarmCommandProtocolVersion is the oldest Fleet Node command
// protocol that carries plant alarm actions. Admission, dispatch and the
// upgrade message all read it.
const ContainerAlarmCommandProtocolVersion = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V14

// RunContainerAlarmAction needs ContainerAlarmCommandProtocolVersion; sendInSlot
// attaches that version so an older node is refused before dispatch.
func (m *Miner) RunContainerAlarmAction(ctx context.Context, req sdk.ContainerAlarmRequest) error {
	if err := req.Validate(); err != nil {
		return devicewrite.NotDispatched(err)
	}
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_RunContainerAlarmAction{RunContainerAlarmAction: &gatewaypb.RunContainerAlarmAction{Action: req.Action}}})
}
