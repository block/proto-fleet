package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (p *PluginMiner) SetTankMining(ctx context.Context, req sdk.TankMiningRequest) error {
	controller, ok := p.sdkDevice.(sdk.DeviceTankMiner)
	if !ok || !p.caps[sdk.CapabilityTankMining] {
		return fleeterror.NewUnimplementedError("tank mining unsupported")
	}
	return wrapPluginError(controller.SetTankMining(ctx, req), "tank mining")
}
