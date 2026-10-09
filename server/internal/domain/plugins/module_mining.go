package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (p *PluginMiner) SetModuleMining(ctx context.Context, req sdk.ModuleMiningRequest) error {
	controller, ok := p.sdkDevice.(sdk.DeviceModuleMiner)
	if !ok || !p.caps[sdk.CapabilityModuleMining] {
		return fleeterror.NewUnimplementedError("module mining unsupported")
	}
	return wrapPluginError(controller.SetModuleMining(ctx, req), "module mining")
}
