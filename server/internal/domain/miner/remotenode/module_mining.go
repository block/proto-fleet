package remotenode

import (
	"context"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *Miner) SetModuleMining(ctx context.Context, req sdk.ModuleMiningRequest) error {
	if err := req.Validate(); err != nil {
		return devicewrite.NotDispatched(err)
	}
	action := &gatewaypb.SetModuleMiningAction{Mining: req.Mining}
	for _, t := range req.Modules {
		action.Modules = append(action.Modules, &gatewaypb.ModuleMiningTarget{Tank: t.Tank, Module: t.Module, ReportedId: t.ReportedID})
	}
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_SetModuleMining{SetModuleMining: action}})
}
