package miner

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

var _ sdk.DeviceBoardASICStreamer = (*RemoteFleetNodeMiner)(nil)

// StreamBoardASICReadings relays the stream through the command miner that
// reaches the Fleet Node. Without one it returns a gRPC status, as a plugin
// would, so the command service shows its fixed unsupported message.
func (m *RemoteFleetNodeMiner) StreamBoardASICReadings(ctx context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	if streamer, ok := m.delegate.(sdk.DeviceBoardASICStreamer); ok {
		return streamer.StreamBoardASICReadings(ctx, board, send)
	}
	return status.Error(codes.Unimplemented, "fleet node remote board ASIC stream is not implemented") //nolint:wrapcheck // stands in for the plugin's gRPC status
}
