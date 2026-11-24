package containers

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func (h *Handler) GetModuleMiningOptions(ctx context.Context, req *connect.Request[pb.GetModuleMiningOptionsRequest]) (*connect.Response[pb.GetModuleMiningOptionsResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("module commands unavailable")
	}
	result, err := h.commands.GetModuleMiningOptions(ctx, req.Msg.ModuleIds, authorizeScopedMining)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
func (h *Handler) SetModuleMining(ctx context.Context, req *connect.Request[pb.SetModuleMiningRequest]) (*connect.Response[pb.SetModuleMiningResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("module commands unavailable")
	}
	result, err := h.commands.SetModuleMining(ctx, req.Msg.ModuleIds, req.Msg.Mining, authorizeScopedMining)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
