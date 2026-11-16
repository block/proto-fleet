package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ContainerPowerFeed reads a module's power feed. Inspection never changes
// controller state, so it takes no physical-write admission.
func (p *PluginMiner) ContainerPowerFeed(ctx context.Context, r sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	var result sdk.ContainerPowerFeedResult
	if err := r.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	c, ok := p.sdkDevice.(sdk.DeviceContainerPowerFeed)
	if !ok {
		return result, devicewrite.NotDispatched(sdk.NewErrUnsupportedCapability(sdk.CapabilityContainerPowerFeed))
	}
	err := devicewrite.Observe(ctx, func(ctx context.Context) error {
		var err error
		result, err = c.ContainerPowerFeed(ctx, r)
		if err == nil {
			err = result.Validate(r)
		}
		return wrapPluginError(err, "container power feed")
	})
	return result, err
}
