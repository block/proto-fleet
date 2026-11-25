package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ControllerRecords is an observation: it never takes the physical-write gate.
func (p *PluginMiner) ControllerRecords(ctx context.Context, req sdk.ControllerRecordsRequest) (sdk.ControllerRecordsResult, error) {
	var result sdk.ControllerRecordsResult
	if err := req.Validate(); err != nil {
		return result, devicewrite.NotDispatched(fleeterror.NewInvalidArgumentError(err.Error()))
	}
	reader, ok := p.sdkDevice.(sdk.DeviceControllerRecords)
	if !ok || !p.caps[sdk.CapabilityControllerRecords] {
		return result, devicewrite.NotDispatched(fleeterror.NewUnimplementedError("controller records unsupported"))
	}
	err := devicewrite.Observe(ctx, func(ctx context.Context) error {
		var err error
		result, err = reader.ControllerRecords(ctx, req)
		if err == nil {
			err = result.Validate(req)
		}
		return wrapPluginError(err, "controller records")
	})
	return result, err
}
