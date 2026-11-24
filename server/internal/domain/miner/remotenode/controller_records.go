package remotenode

import (
	"context"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ControllerRecords runs the bounded read on the assigned Fleet Node, next to
// the controller; only the bounded result crosses the gateway.
func (m *Miner) ControllerRecords(ctx context.Context, req sdk.ControllerRecordsRequest) (sdk.ControllerRecordsResult, error) {
	var result sdk.ControllerRecordsResult
	if err := req.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	data, err := sdk.EncodeControllerRecords(req)
	if err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	ack, err := m.send(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_ControllerRecords{ControllerRecords: &gatewaypb.ControllerRecordsAction{Request: data}}})
	if err != nil {
		return result, err
	}
	if err := ackToError(ack); err != nil {
		return result, err
	}
	return sdk.DecodeControllerRecordsResult(ack.GetPayload(), req)
}
