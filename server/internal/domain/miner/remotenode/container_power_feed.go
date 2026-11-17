package remotenode

import (
	"context"
	"encoding/json"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Power feed inspection and writes arrived in different command protocol
// versions. Admission, dispatch and the upgrade messages all read these.
const (
	PowerFeedInspectCommandProtocolVersion = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V13
	PowerFeedWriteCommandProtocolVersion   = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V16
)

func (m *Miner) ContainerPowerFeed(ctx context.Context, r sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	var result sdk.ContainerPowerFeedResult
	if err := r.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	ack, err := m.send(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_ContainerPowerFeed{ContainerPowerFeed: &gatewaypb.ContainerPowerFeedAction{Request: data}}})
	if err != nil {
		return result, err
	}
	if err := ackToError(ack); err != nil {
		return result, err
	}
	return sdk.DecodeContainerPowerFeedResult(ack.GetPayload(), r)
}
