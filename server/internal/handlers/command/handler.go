package command

import (
	"connectrpc.com/connect"
	"context"
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
