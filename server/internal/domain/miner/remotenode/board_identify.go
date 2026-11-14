package remotenode

import (
	"context"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *Miner) SetBoardIdentify(ctx context.Context, req sdk.BoardIdentifyRequest) error {
	if err := req.Validate(); err != nil {
		return devicewrite.NotDispatched(err)
	}
	b := req.Board
	action := &gatewaypb.SetBoardIdentifyAction{Tank: b.Tank, Module: b.Module, Slot: b.Slot, ModuleReportedId: b.ModuleReportedID, MacAddress: b.MACAddress, Mode: string(req.Mode)}
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_SetBoardIdentify{SetBoardIdentify: action}})
}
