package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"

	"connectrpc.com/connect"
	pingv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/ping/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/ping/v1/pingv1connect"
)

// PingServer stub implementation intended for testing
type PingServer struct {
}

var _ pingv1connect.PingServiceHandler = PingServer{}

func (PingServer) Ping(_ context.Context, req *connect.Request[pingv1.PingRequest]) (*connect.Response[pingv1.PingResponse], error) {
	return connect.NewResponse(&pingv1.PingResponse{Text: req.Msg.Text}), nil
}

func (PingServer) Echo(_ context.Context, req *connect.Request[pingv1.EchoRequest]) (*connect.Response[pingv1.EchoResponse], error) {
	return connect.NewResponse(&pingv1.EchoResponse{Text: req.Msg.Text}), nil
}

func (PingServer) PingStream(_ context.Context, stream *connect.BidiStream[pingv1.PingStreamRequest, pingv1.PingStreamResponse]) error {
	for {
		req, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("failed to process request: %w", err)
		}
		if err := stream.Send(&pingv1.PingStreamResponse{Text: req.Text}); err != nil {
			return fmt.Errorf("failed to process request: %w", err)
		}
	}
}
