package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (p *PluginMiner) ContainerMining(ctx context.Context, r sdk.ContainerMiningRequest) (sdk.ContainerMiningResult, error) {
	var result sdk.ContainerMiningResult
	if err := r.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	c, ok := p.sdkDevice.(sdk.DeviceContainerMining)
	if !ok {
		return result, devicewrite.NotDispatched(sdk.NewErrUnsupportedCapability(sdk.CapabilityContainerMining))
	}
	call := func(ctx context.Context) error {
		var err error
		result, err = c.ContainerMining(ctx, r)
		if err == nil {
			err = result.Validate(r)
		}
		return wrapPluginError(err, "container mining")
	}
	var err error
	if r.ReadOnly() {
		err = devicewrite.Observe(ctx, call)
	} else {
		err = p.dispatchWrite(ctx, commandtype.ContainerMining, call)
	}
	return result, err
}
