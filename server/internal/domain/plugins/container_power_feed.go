package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ContainerPowerFeed reads or writes a module's power feed. Inspection never
// changes controller state, so only writes take physical-write admission.
func (p *PluginMiner) ContainerPowerFeed(ctx context.Context, r sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	var result sdk.ContainerPowerFeedResult
	if err := r.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	c, ok := p.sdkDevice.(sdk.DeviceContainerPowerFeed)
	if !ok {
		return result, devicewrite.NotDispatched(sdk.NewErrUnsupportedCapability(sdk.CapabilityContainerPowerFeed))
	}
	call := func(ctx context.Context) error {
		var err error
		result, err = c.ContainerPowerFeed(ctx, r)
		if err == nil {
			err = result.Validate(r)
		}
		return wrapPluginError(err, "container power feed")
	}
	if r.ReadOnly() {
		return result, devicewrite.Observe(ctx, call)
	}
	if !p.caps[sdk.CapabilityContainerPowerFeedControl] {
		return result, devicewrite.NotDispatched(sdk.NewErrUnsupportedCapability(sdk.CapabilityContainerPowerFeedControl))
	}
	err := p.dispatchWrite(ctx, commandtype.ControlPowerFeed, call)
	return result, err
}
