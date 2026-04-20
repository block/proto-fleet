package ping

import (
	"context"
	"errors"
	"io"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"

	"connectrpc.com/connect"
	pingv1 "github.com/block/proto-fleet/server/generated/grpc/ping/v1"
	"github.com/block/proto-fleet/server/generated/grpc/ping/v1/pingv1connect"
)

// Handler stub implementation intended for testing
type Handler struct {
}

var _ pingv1connect.PingServiceHandler = Handler{}

func (Handler) Ping(_ context.Context, req *connect.Request[pingv1.PingRequest]) (*connect.Response[pingv1.PingResponse], error) {
	return connect.NewResponse(&pingv1.PingResponse{Text: req.Msg.Text}), nil
}

func (Handler) Echo(_ context.Context, req *connect.Request[pingv1.EchoRequest]) (*connect.Response[pingv1.EchoResponse], error) {
	return connect.NewResponse(&pingv1.EchoResponse{Text: req.Msg.Text}), nil
}

func (Handler) PingStream(_ context.Context, stream *connect.BidiStream[pingv1.PingStreamRequest, pingv1.PingStreamResponse]) error {
	for {
		req, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fleeterror.NewInternalErrorf("failed to process request: %v", err)
		}
		if err := stream.Send(&pingv1.PingStreamResponse{Text: req.Text}); err != nil {
			return fleeterror.NewInternalErrorf("failed to process request: %v", err)
		}
	}
}
