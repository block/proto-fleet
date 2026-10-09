package containers

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func authorizeScopedMining(ctx context.Context, site *int64, mining bool) error {
	action := authz.PermMinerStopMining
	if mining {
		action = authz.PermMinerStartMining
	}
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, action} {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: site}); err != nil {
			return err
		}
	}
	return nil
}
func (h *Handler) GetTankMiningOptions(ctx context.Context, req *connect.Request[pb.GetTankMiningOptionsRequest]) (*connect.Response[pb.GetTankMiningOptionsResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("tank commands unavailable")
	}
	result, err := h.commands.GetTankMiningOptions(ctx, req.Msg.TankIds, authorizeScopedMining)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
func (h *Handler) SetTankMining(ctx context.Context, req *connect.Request[pb.SetTankMiningRequest]) (*connect.Response[pb.SetTankMiningResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("tank commands unavailable")
	}
	result, err := h.commands.SetTankMining(ctx, req.Msg.TankIds, req.Msg.Mining, authorizeScopedMining)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
