package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	fleetnodecontrol "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// boardASICStreamNodeLifetime ends a stream the server never cancelled, for
// example after a lost ControlCancel. It is longer than the server's own
// stream lifetime, so the server ends streams first. var so tests shrink it.
var boardASICStreamNodeLifetime = 6 * time.Minute

// boardASICStreamLimit bounds how many board ASIC streams the node relays at
// once.
type boardASICStreamLimit struct {
	mu      sync.Mutex
	running int
}

// start counts one stream. ok is false when the node already relays its
// maximum. end must be called when the stream finishes.
func (s *boardASICStreamLimit) start() (end func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running >= fleetnodecontrol.MaxConcurrentCommandStreamsPerNode {
		return nil, false
	}
	s.running++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.running--
	}, true
}

// boardASICStreamAtCapacity is the ack payload of a stream the node refused
// because it already relays its maximum.
func boardASICStreamAtCapacity() []byte {
	// Marshaling this fixed message cannot fail.
	payload, _ := proto.Marshal(&pb.StreamBoardASICReadingsResult{End: pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_AT_CAPACITY})
	return payload
}

// runBoardASICStream relays one board's stream as command frames bound to
// commandID and returns the terminal ack. The ack payload always names how the stream
// ended; plugin and controller error text stays in the node log.
func runBoardASICStream(ctx context.Context, stream acker, commandID string, dev sdk.Device, action *pb.StreamBoardASICReadingsAction, logger *slog.Logger) (pb.AckCode, string, []byte) {
	board := sdk.BoardIdentifyTarget{Tank: action.GetTank(), Module: action.GetModule(), Slot: action.GetSlot(), ModuleReportedID: action.GetModuleReportedId(), MACAddress: action.GetMacAddress()}
	var end pb.BoardASICStreamEnd
	var err error
	streamer, ok := dev.(sdk.DeviceBoardASICStreamer)
	switch {
	case board.Validate() != nil:
		return pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid board", nil
	case !ok:
		end = pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNSUPPORTED
	default:
		var sendErr error
		err = streamer.StreamBoardASICReadings(ctx, board, func(frame sdk.BoardASICFrame) error {
			payload, marshalErr := proto.Marshal(fleetnodecontrol.BoardASICFrameToProto(frame))
			if marshalErr != nil {
				return grpcstatus.Error(codes.Internal, "encode board ASIC frame") //nolint:wrapcheck // ends the stream as unavailable
			}
			sendErr = stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_CommandFrame{CommandFrame: &pb.CommandFrame{CommandId: commandID, Payload: payload}}})
			return sendErr
		})
		end = boardASICStreamEnd(ctx, err, sendErr)
	}
	code, message := pb.AckCode_ACK_CODE_OK, ""
	//nolint:exhaustive // The stream ran to a normal end in every other case.
	switch end {
	case pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNSUPPORTED:
		code, message = pb.AckCode_ACK_CODE_UNIMPLEMENTED, "board ASIC readings unsupported"
	case pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_BOARD_CHANGED:
		code, message = pb.AckCode_ACK_CODE_INTERNAL, "board no longer at its confirmed slot"
	case pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNAVAILABLE:
		code, message = pb.AckCode_ACK_CODE_INTERNAL, fmt.Sprintf("board ASIC stream failed: %s", grpcstatus.Code(err))
		logger.Warn("board ASIC stream failed", "command_id", commandID, "error", err)
	}
	payload, marshalErr := proto.Marshal(&pb.StreamBoardASICReadingsResult{End: end})
	if marshalErr != nil {
		return pb.AckCode_ACK_CODE_INTERNAL, "encode board ASIC stream result", nil
	}
	return code, message, payload
}

func boardASICStreamEnd(ctx context.Context, err, sendErr error) pb.BoardASICStreamEnd {
	switch {
	case ctx.Err() != nil:
		// The server cancelled the command, the node lifetime passed, or the
		// control session ended, which also fails the frame send.
		return pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CANCELED
	case sendErr != nil:
		return pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNAVAILABLE
	case err == nil:
		return pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CONTROLLER_ENDED
	}
	//nolint:exhaustive // Every other plugin outcome is unavailable.
	switch grpcstatus.Code(err) {
	case codes.FailedPrecondition:
		return pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_BOARD_CHANGED
	case codes.Unimplemented:
		return pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNSUPPORTED
	default:
		return pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNAVAILABLE
	}
}
