package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// StreamBoardASICReadings is a read: it bypasses the physical-write gate and
// runs until ctx ends or the plugin ends the stream.
func (p *PluginMiner) StreamBoardASICReadings(ctx context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	if err := board.Validate(); err != nil {
		return fleeterror.NewInvalidArgumentError(err.Error())
	}
	streamer, ok := p.sdkDevice.(sdk.DeviceBoardASICStreamer)
	if !ok || !p.caps[sdk.CapabilityBoardASICStream] {
		return fleeterror.NewUnimplementedError("board ASIC readings unsupported")
	}
	return streamer.StreamBoardASICReadings(ctx, board, send) //nolint:wrapcheck // the plugin's gRPC status is mapped by the caller
}
