// Package minerchannel wires the miner channel RPC surface.
package minerchannel

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/minerchannel/v1"
	"github.com/block/proto-fleet/server/generated/grpc/minerchannel/v1/minerchannelv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	minerChannel "github.com/block/proto-fleet/server/internal/domain/minerchannel"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the MinerChannelService Connect-RPC surface.
type Handler struct {
	service *minerChannel.Service
}

var _ minerchannelv1connect.MinerChannelServiceHandler = &Handler{}

func NewHandler(service *minerChannel.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateMinerChannel(ctx context.Context, req *connect.Request[pb.CreateMinerChannelRequest]) (*connect.Response[pb.CreateMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	params, err := toCreateMinerChannelParams(req.Msg, info)
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.CreateMinerChannel(ctx, params)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) GetMinerChannel(ctx context.Context, req *connect.Request[pb.GetMinerChannelRequest]) (*connect.Response[pb.GetMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.GetMinerChannel(ctx, info.OrganizationID, req.Msg.GetMinerChannelId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) ListMinerChannels(ctx context.Context, req *connect.Request[pb.ListMinerChannelsRequest]) (*connect.Response[pb.ListMinerChannelsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.ListMinerChannels(ctx, toListMinerChannelsParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListMinerChannelsResponse{
		MinerChannels: toProtoMinerChannelSummaries(result.MinerChannels),
		NextPageToken: result.NextPageToken,
		TotalCount:    result.TotalCount,
	}), nil
}

func (h *Handler) DeleteMinerChannel(ctx context.Context, req *connect.Request[pb.DeleteMinerChannelRequest]) (*connect.Response[pb.DeleteMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.DeleteMinerChannel(ctx, info.OrganizationID, req.Msg.GetMinerChannelId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) UpdateMinerChannel(ctx context.Context, req *connect.Request[pb.UpdateMinerChannelRequest]) (*connect.Response[pb.UpdateMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	params, err := toUpdateMinerChannelParams(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.UpdateMinerChannel(ctx, params)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) SetMinerChannelFirmwareTarget(ctx context.Context, req *connect.Request[pb.SetMinerChannelFirmwareTargetRequest]) (*connect.Response[pb.SetMinerChannelFirmwareTargetResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.SetMinerChannelFirmwareTarget(ctx, toSetMinerChannelFirmwareTargetParams(req.Msg, info))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetMinerChannelFirmwareTargetResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) AddDevicesToMinerChannel(ctx context.Context, req *connect.Request[pb.AddDevicesToMinerChannelRequest]) (*connect.Response[pb.AddDevicesToMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.AddDevicesToMinerChannel(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetMinerChannelId(), req.Msg.GetDeviceIdentifiers()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AddDevicesToMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) RemoveDevicesFromMinerChannel(ctx context.Context, req *connect.Request[pb.RemoveDevicesFromMinerChannelRequest]) (*connect.Response[pb.RemoveDevicesFromMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.RemoveDevicesFromMinerChannel(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetMinerChannelId(), req.Msg.GetDeviceIdentifiers()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RemoveDevicesFromMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) ReleaseMinerChannel(ctx context.Context, req *connect.Request[pb.ReleaseMinerChannelRequest]) (*connect.Response[pb.ReleaseMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	minerChannel, err := h.service.ReleaseMinerChannel(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetMinerChannelId(), nil))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ReleaseMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) GetMyMinerChannels(ctx context.Context, req *connect.Request[pb.GetMyMinerChannelsRequest]) (*connect.Response[pb.GetMyMinerChannelsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.ListMinerChannelsByOwner(ctx, toListMinerChannelsByOwnerParams(req.Msg, info))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetMyMinerChannelsResponse{
		MinerChannels: toProtoMinerChannelSummaries(result.MinerChannels),
		NextPageToken: result.NextPageToken,
		TotalCount:    result.TotalCount,
	}), nil
}

func (h *Handler) ListDevices(ctx context.Context, req *connect.Request[pb.ListDevicesRequest]) (*connect.Response[pb.ListDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.ListDevices(ctx, toListDevicesParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListDevicesResponse{
		Devices:        toProtoMinerChannelDevices(result.Devices),
		NextPageToken:  result.NextPageToken,
		TotalCount:     result.TotalCount,
		AvailableCount: result.AvailableCount,
		ReservedCount:  result.ReservedCount,
	}), nil
}

func (h *Handler) AdminReassign(ctx context.Context, req *connect.Request[pb.AdminReassignRequest]) (*connect.Response[pb.AdminReassignResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequireSuperAdmin(ctx, "reassign miner channel"); err != nil {
		return nil, err
	}
	minerChannel, err := h.service.AddDevicesToMinerChannel(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetTargetMinerChannelId(), req.Msg.GetDeviceIdentifiers()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdminReassignResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}

func (h *Handler) AdminReleaseMinerChannel(ctx context.Context, req *connect.Request[pb.AdminReleaseMinerChannelRequest]) (*connect.Response[pb.AdminReleaseMinerChannelResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermMinerChannelManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequireSuperAdmin(ctx, "release any miner channel"); err != nil {
		return nil, err
	}
	minerChannel, err := h.service.ReleaseMinerChannel(ctx, toMembershipMutationParams(info.OrganizationID, info.UserID, info.Role, req.Msg.GetMinerChannelId(), nil))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AdminReleaseMinerChannelResponse{MinerChannel: toProtoMinerChannel(minerChannel)}), nil
}
