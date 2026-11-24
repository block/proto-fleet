package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (p *PluginMiner) SetContainerControl(ctx context.Context, req sdk.ContainerControlRequest) error {
	controller, ok := p.sdkDevice.(sdk.DeviceContainerController)
	if !ok {
		return fleeterror.NewUnimplementedError("plugin does not support equipment commands")
	}
	return p.dispatchWrite(ctx, commandtype.SetContainerControl, func(ctx context.Context) error {
		return wrapPluginError(controller.SetContainerControl(ctx, req), "equipment write")
	})
}
