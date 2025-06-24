package command

import (
	"context"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"log/slog"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/command"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	commandSvc *command.Service
}

var _ minercommandv1connect.MinerCommandServiceHandler = &Handler{}

func NewHandler(commandSvc *command.Service) *Handler {
	return &Handler{
		commandSvc: commandSvc,
	}
}

func (h *Handler) StopMining(
	ctx context.Context,
	req *connect.Request[pb.StopMiningRequest],
) (*connect.Response[pb.StopMiningResponse], error) {
	resp, err := h.commandSvc.StopMining(ctx, req.Msg.DeviceIdentifiers)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) StartMining(
	ctx context.Context,
	req *connect.Request[pb.StartMiningRequest],
) (*connect.Response[pb.StartMiningResponse], error) {
	resp, err := h.commandSvc.StartMining(ctx, req.Msg.DeviceIdentifiers)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) StreamCommandBatchUpdates(ctx context.Context, r *connect.Request[pb.StreamCommandBatchUpdatesRequest], stream *connect.ServerStream[pb.StreamCommandBatchUpdatesResponse]) error {
	slog.Debug("handling request to stream command batch updates", "request", r)
	responseChan, err := h.commandSvc.StreamCommandBatchUpdates(ctx, r.Msg)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			slog.Debug("context closed")
			return fleeterror.NewInternalErrorf("context done with error: %v", ctx.Err())
		case resp, ok := <-responseChan:
			if !ok {
				slog.Warn("channel closed")
				return nil
			}
			slog.Debug("sending update", "payload", resp)
			if err := stream.Send(resp); err != nil {
				return fleeterror.NewInternalErrorf("error sending response to stream: %v", err)
			}
		}
	}
}
