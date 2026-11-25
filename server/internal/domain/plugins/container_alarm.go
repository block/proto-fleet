package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// RunContainerAlarmAction is a physical plant write: it goes through the write
// gate like SetContainerControl and is never replayed by this layer.
func (p *PluginMiner) RunContainerAlarmAction(ctx context.Context, req sdk.ContainerAlarmRequest) error {
	if err := req.Validate(); err != nil {
		return devicewrite.NotDispatched(fleeterror.NewInvalidArgumentError(err.Error()))
	}
	// A gRPC plugin always satisfies the interface, so the advertised
	// capability is what proves an older plugin is not sent the write.
	controller, ok := p.sdkDevice.(sdk.DeviceContainerAlarmController)
	if !ok || !p.caps[sdk.CapabilityContainerAlarm] {
		return devicewrite.NotDispatched(fleeterror.NewUnimplementedError("plugin does not support container alarm actions"))
	}
	return p.dispatchWrite(ctx, commandtype.ContainerAlarmAction, func(ctx context.Context) error {
		return wrapPluginError(controller.RunContainerAlarmAction(ctx, req), "container alarm action")
	})
}
