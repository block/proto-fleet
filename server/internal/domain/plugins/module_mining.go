package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (p *PluginMiner) SetModuleMining(ctx context.Context, req sdk.ModuleMiningRequest) error {
	if err := req.Validate(); err != nil {
		return devicewrite.NotDispatched(fleeterror.NewInvalidArgumentError(err.Error()))
	}
	controller, ok := p.sdkDevice.(sdk.DeviceModuleMiner)
	if !ok || !p.caps[sdk.CapabilityModuleMining] {
		return devicewrite.NotDispatched(fleeterror.NewUnimplementedError("module mining unsupported"))
	}
	return p.dispatchWrite(ctx, commandtype.SetModuleMining, func(ctx context.Context) error {
		return wrapPluginError(controller.SetModuleMining(ctx, req), "module mining")
	})
}
