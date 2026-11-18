package remotenode

import (
	"context"
	"errors"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// BoardASICStreamUpgradeReason is shown when the container's Fleet Node is too
// old to relay live ASIC readings.
const BoardASICStreamUpgradeReason = "Live ASIC readings need a newer Fleet Node. Update the Fleet Node that reaches this container, then reopen the board."

// CommandStreamSender relays one streaming command's frames and returns the
// node's terminal ack. *control.Registry satisfies it.
type CommandStreamSender interface {
	StreamCommand(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, stream control.CommandStream) (*gatewaypb.ControlAck, error)
}

var _ sdk.DeviceBoardASICStreamer = (*Miner)(nil)

// StreamBoardASICReadings relays the node's stream for one confirmed board. It
// returns the same kinds of outcome as a directly reached plugin: nil when the
// controller ended the stream, a gRPC status for a controller or node outcome,
// and send's error as is. It takes no per-node command slot: the registry caps
// concurrent streams per node instead, so a viewer cannot hold up commands.
func (m *Miner) StreamBoardASICReadings(ctx context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	if err := board.Validate(); err != nil {
		return fleeterror.NewInvalidArgumentError(err.Error())
	}
	streamer, ok := m.sender.(CommandStreamSender)
	if !ok {
		return streamStatusf(codes.Unimplemented, "fleet node sender cannot relay board ASIC streams")
	}
	cmd, err := m.controlCommandForMiner(id.GenerateID(), &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_StreamBoardAsicReadings{
		StreamBoardAsicReadings: &gatewaypb.StreamBoardASICReadingsAction{
			Tank: board.Tank, Module: board.Module, Slot: board.Slot, ModuleReportedId: board.ModuleReportedID, MacAddress: board.MACAddress,
		},
	}})
	if err != nil {
		return err
	}
	var sendErr error
	ack, err := streamer.StreamCommand(ctx, m.fleetNodeID, cmd, control.CommandStream{
		Kind:     "board_asic_stream",
		Coalesce: keepBoardASICLayout,
		OnFrame: func(payload []byte) error {
			var frame gatewaypb.BoardASICStreamFrame
			if proto.Unmarshal(payload, &frame) != nil || protovalidate.Validate(&frame) != nil {
				return errInvalidBoardASICFrame
			}
			sendErr = send(control.BoardASICFrameFromProto(&frame))
			return sendErr
		},
	})
	switch {
	case sendErr != nil:
		return sendErr
	case err == nil:
		return boardASICStreamAckError(ack)
	case ctx.Err() != nil:
		return streamStatusf(status.FromContextError(ctx.Err()).Code(), "%s", ctx.Err())
	case errors.Is(err, control.ErrCommandProtocolUpgradeRequired):
		return fleeterror.NewFailedPreconditionError(BoardASICStreamUpgradeReason)
	case errors.Is(err, control.ErrCommandStreamCapacity):
		return streamStatusf(codes.ResourceExhausted, "%s", err.Error())
	default:
		// No active stream, or a frame the server refused: the viewer retries.
		return streamStatusf(codes.Unavailable, "%s", err.Error())
	}
}

var errInvalidBoardASICFrame = errors.New("fleet node sent an invalid ASIC reading frame")

// keepBoardASICLayout carries a skipped frame's layout onto the frame that
// replaces it, because the node sends the layout only on the first frame and
// when it changes. A frame it cannot decode is left for OnFrame to refuse.
func keepBoardASICLayout(skipped, next []byte) []byte {
	var from, to gatewaypb.BoardASICStreamFrame
	if proto.Unmarshal(skipped, &from) != nil || from.GetLayout() == nil {
		return next
	}
	if proto.Unmarshal(next, &to) != nil || to.GetLayout() != nil {
		return next
	}
	to.Layout = from.GetLayout()
	merged, err := proto.Marshal(&to)
	if err != nil {
		return next
	}
	return merged
}

// boardASICStreamAckError maps the node's terminal ack to the outcome a direct
// plugin stream would return. Node text is kept for the server log only.
func boardASICStreamAckError(ack *gatewaypb.ControlAck) error {
	var result gatewaypb.StreamBoardASICReadingsResult
	if len(ack.GetPayload()) > 0 && proto.Unmarshal(ack.GetPayload(), &result) == nil {
		//nolint:exhaustive // Every other end is reported as unavailable below.
		switch result.GetEnd() {
		case gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CONTROLLER_ENDED:
			return nil
		case gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_BOARD_CHANGED:
			return streamStatusf(codes.FailedPrecondition, "board no longer at its confirmed slot")
		case gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNSUPPORTED:
			return streamStatusf(codes.Unimplemented, "board ASIC readings unsupported")
		case gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_AT_CAPACITY:
			return streamStatusf(codes.ResourceExhausted, "fleet node board ASIC stream capacity reached")
		}
	}
	reason := clampAckReason(ack.GetErrorMessage())
	code := ack.GetCode()
	if code == gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED || code == gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE {
		return streamStatusf(codes.Unimplemented, "fleet node cannot stream board ASIC readings: %s", reason)
	}
	return streamStatusf(codes.Unavailable, "fleet node board ASIC stream ended (%s): %s", code, reason)
}

// streamStatusf returns a gRPC status, as a directly reached plugin does, so
// the command service maps it to a fixed customer message. A fleeterror would
// reach the customer as is.
func streamStatusf(code codes.Code, format string, args ...any) error {
	return status.Errorf(code, format, args...) //nolint:wrapcheck // stands in for the plugin's gRPC status
}
