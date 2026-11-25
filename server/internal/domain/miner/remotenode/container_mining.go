package remotenode

import (
	"context"
	"encoding/json"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *Miner) ContainerMining(ctx context.Context, r sdk.ContainerMiningRequest) (sdk.ContainerMiningResult, error) {
	var result sdk.ContainerMiningResult
	if err := r.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	ack, err := m.send(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_ContainerMining{ContainerMining: &gatewaypb.ContainerMiningAction{Request: data}}})
	if err != nil {
		return result, err
	}
	if err := ackToError(ack); err != nil {
		return result, err
	}
	return sdk.DecodeContainerMiningResult(ack.GetPayload(), r)
}
