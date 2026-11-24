package containers

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func authorizeContainerMining(ctx context.Context, site *int64, req sdk.ContainerMiningRequest) error {
	permissions := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead}
	switch req.Action {
	case "set_mining":
		if req.Mining == nil {
			return fleeterror.NewInvalidArgumentError("mining state required")
		}
		if *req.Mining {
			permissions = append(permissions, authz.PermMinerStartMining)
		} else {
			permissions = append(permissions, authz.PermMinerStopMining)
		}
	case "set_power_target":
		permissions = append(permissions, authz.PermMinerSetPowerTarget)
	}
	for _, permission := range permissions {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: site}); err != nil {
			return err
		}
	}
	return nil
}
func (h *Handler) GetMiningStatus(ctx context.Context, req *connect.Request[pb.GetMiningStatusRequest]) (*connect.Response[pb.GetMiningStatusResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("container commands unavailable")
	}
	result, err := h.commands.ReadContainerMining(ctx, req.Msg.DeviceIdentifier, sdk.ContainerMiningRequest{Action: "status"}, authorizeContainerMining)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetMiningStatusResponse{Mining: *result.Status.Mining, OperatingState: result.Status.OperatingState}), nil
}
func (h *Handler) GetPowerTarget(ctx context.Context, req *connect.Request[pb.GetPowerTargetRequest]) (*connect.Response[pb.GetPowerTargetResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("container commands unavailable")
	}
	result, err := h.commands.ReadContainerMining(ctx, req.Msg.DeviceIdentifier, sdk.ContainerMiningRequest{Action: "power_target"}, authorizeContainerMining)
	if err != nil {
		return nil, err
	}
	p := result.Power
	return connect.NewResponse(&pb.GetPowerTargetResponse{Watts: *p.Watts, MinimumWatts: p.MinimumWatts, MaximumWatts: p.MaximumWatts, PerformanceMode: p.PerformanceMode, BalanceBays: p.BalanceBays, HashOnDisconnect: p.HashOnDisconnect}), nil
}
func (h *Handler) SetMining(ctx context.Context, req *connect.Request[pb.SetMiningRequest]) (*connect.Response[pb.SetMiningResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("container commands unavailable")
	}
	result, err := h.commands.SetContainerMining(ctx, req.Msg.DeviceIdentifier, sdk.ContainerMiningRequest{Action: "set_mining", Mining: req.Msg.Mining}, authorizeContainerMining)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetMiningResponse{BatchIdentifier: result.BatchIdentifier}), nil
}
func (h *Handler) SetPowerTarget(ctx context.Context, req *connect.Request[pb.SetPowerTargetRequest]) (*connect.Response[pb.SetPowerTargetResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("container commands unavailable")
	}
	result, err := h.commands.SetContainerMining(ctx, req.Msg.DeviceIdentifier, sdk.ContainerMiningRequest{Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: req.Msg.Watts, PerformanceMode: req.Msg.PerformanceMode, BalanceBays: req.Msg.BalanceBays, HashOnDisconnect: req.Msg.HashOnDisconnect}}, authorizeContainerMining)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetPowerTargetResponse{BatchIdentifier: result.BatchIdentifier}), nil
}
