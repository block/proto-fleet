package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (p *PluginMiner) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	var result sdk.MiningGroupResult
	if err := r.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	c, ok := p.sdkDevice.(sdk.DeviceMiningGroups)
	if !ok {
		return result, devicewrite.NotDispatched(fleeterror.NewUnimplementedError("mining groups unsupported"))
	}
	call := func(ctx context.Context) error {
		var err error
		result, err = c.MiningGroups(ctx, r)
		return wrapPluginError(err, "mining groups")
	}
	// The domain owns one durable admission across all reconciliation steps.
	// Reads share its binding gate without retiring the parent operation.
	var err error
	if r.ReadOnly() {
		err = devicewrite.Observe(ctx, call)
	} else {
		err = p.dispatchWrite(ctx, commandtype.Type(-1), call)
	}
	return result, err
}
