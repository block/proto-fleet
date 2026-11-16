package main

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// asicStreamInterval matches Frigate's telemetry stream cadence.
var asicStreamInterval = time.Second

// asicHealthFrame reads one board through the shared request lifecycle, so
// stream frames and unary reads agree within a sample interval.
func (s *ContainerState) asicHealthFrame(ctx context.Context, identifier string) (*mdk.StreamASICHealthResponse, error) {
	var (
		frame *mdk.StreamASICHealthResponse
		err   error
	)
	if serveErr := s.serve(ctx, false, func() {
		s.mu.RLock()
		defer s.mu.RUnlock()
		board := s.findBoard(identifier)
		if board == nil {
			err = notFoundf("unknown hashboard %q", identifier)
			return
		}
		var asics *mdk.GetHashboardASICsResponse
		asics, err = project(s.asicsResponse(board), nil, new(mdk.GetHashboardASICsResponse))
		if err != nil {
			return
		}
		frame = &mdk.StreamASICHealthResponse{
			MacAddress: board.MAC,
			Asics:      asics.GetAsics(),
			Layout:     asics.GetLayout(),
			Timestamp:  timestamppb.New(s.observedAt),
		}
	}); serveErr != nil {
		return nil, status.FromContextError(serveErr).Err()
	}
	return frame, err
}

// streamASICHealth mirrors Frigate: one frame per interval until the client
// disconnects, with the layout only on the first frame and when it changes.
func (s *ContainerState) streamASICHealth(ctx context.Context, identifier string, send func(*mdk.StreamASICHealthResponse) error) error {
	if identifier == "" {
		return status.Error(codes.InvalidArgument, "identifier is required")
	}
	var lastLayout *mdk.HashboardASICLayout
	ticker := time.NewTicker(asicStreamInterval)
	defer ticker.Stop()
	for {
		frame, err := s.asicHealthFrame(ctx, identifier)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if proto.Equal(lastLayout, frame.GetLayout()) {
			frame.Layout = nil
		} else {
			lastLayout = frame.GetLayout()
		}
		if err := send(frame); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (a *grpcAPI) StreamASICHealth(req *mdk.StreamASICHealthRequest, stream mdk.TelemetryService_StreamASICHealthServer) error {
	return a.state.streamASICHealth(stream.Context(), req.GetIdentifier(), stream.Send)
}

func (a *connectAPI) StreamASICHealth(ctx context.Context, req *connect.Request[mdk.StreamASICHealthRequest], stream *connect.ServerStream[mdk.StreamASICHealthResponse]) error {
	err := a.native.state.streamASICHealth(ctx, req.Msg.GetIdentifier(), stream.Send)
	if st, ok := status.FromError(err); ok && err != nil {
		return connect.NewError(connect.Code(st.Code()), fmt.Errorf("%s", st.Message()))
	}
	return err
}
