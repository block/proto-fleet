package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// SetBoardIdentify is indicator control like BlinkLED: it bypasses the
// physical-write gate, but never claims success the controller did not confirm.
func (p *PluginMiner) SetBoardIdentify(ctx context.Context, req sdk.BoardIdentifyRequest) error {
	if err := req.Validate(); err != nil {
		return devicewrite.NotDispatched(fleeterror.NewInvalidArgumentError(err.Error()))
	}
	controller, ok := p.sdkDevice.(sdk.DeviceBoardIdentifier)
	if !ok || !p.caps[sdk.CapabilityBoardIdentify] {
		return devicewrite.NotDispatched(fleeterror.NewUnimplementedError("board identify unsupported"))
	}
	return wrapPluginError(controller.SetBoardIdentify(ctx, req), "board identify")
}
