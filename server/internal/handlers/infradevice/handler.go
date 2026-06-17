// Package infradevice is the Connect-RPC surface for
// InfraDeviceService.
package infradevice

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/infradevice/v1"
	"github.com/block/proto-fleet/server/generated/grpc/infradevice/v1/infradevicev1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/infradevice"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the InfraDeviceService Connect-RPC surface.
type Handler struct {
	service *infradevice.Service
}

var _ infradevicev1connect.InfraDeviceServiceHandler = &Handler{}

// NewHandler returns an InfraDeviceService handler bound to the
// supplied domain service.
func NewHandler(service *infradevice.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListInfraDevices(ctx context.Context, req *connect.Request[pb.ListInfraDevicesRequest]) (*connect.Response[pb.ListInfraDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	rows, err := h.service.ListInfraDevices(ctx, toListFilter(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListInfraDevicesResponse(rows)), nil
}

func (h *Handler) GetInfraDevice(ctx context.Context, req *connect.Request[pb.GetInfraDeviceRequest]) (*connect.Response[pb.GetInfraDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	device, err := h.service.GetInfraDevice(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetInfraDeviceResponse{
		Device: toProtoInfraDevice(device),
	}), nil
}

func (h *Handler) GetInfraDeviceStats(ctx context.Context, _ *connect.Request[pb.GetInfraDeviceStatsRequest]) (*connect.Response[pb.GetInfraDeviceStatsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	stats, err := h.service.GetStats(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetInfraDeviceStatsResponse{
		TotalCount:     stats.TotalCount,
		OnlineCount:    stats.OnlineCount,
		DegradedCount:  stats.DegradedCount,
		OfflineCount:   stats.OfflineCount,
		BuildingsCount: stats.BuildingsCount,
	}), nil
}

func (h *Handler) CreateInfraDevice(ctx context.Context, req *connect.Request[pb.CreateInfraDeviceRequest]) (*connect.Response[pb.CreateInfraDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	device, err := h.service.CreateInfraDevice(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateInfraDeviceResponse{
		Device: toProtoInfraDevice(device),
	}), nil
}

func (h *Handler) UpdateInfraDevice(ctx context.Context, req *connect.Request[pb.UpdateInfraDeviceRequest]) (*connect.Response[pb.UpdateInfraDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	device, err := h.service.UpdateInfraDevice(ctx, toUpdateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateInfraDeviceResponse{
		Device: toProtoInfraDevice(device),
	}), nil
}

func (h *Handler) DeleteInfraDevice(ctx context.Context, req *connect.Request[pb.DeleteInfraDeviceRequest]) (*connect.Response[pb.DeleteInfraDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.service.DeleteInfraDevice(ctx, info.OrganizationID, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteInfraDeviceResponse{}), nil
}

func (h *Handler) BulkUpdateControlMode(ctx context.Context, req *connect.Request[pb.BulkUpdateControlModeRequest]) (*connect.Response[pb.BulkUpdateControlModeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	count, err := h.service.BulkUpdateControlMode(
		ctx,
		info.OrganizationID,
		req.Msg.GetIds(),
		int16(req.Msg.GetControlMode()), //nolint:gosec // enum is bounded by buf.validate defined_only; int32 → int16 cast is safe.
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BulkUpdateControlModeResponse{
		UpdatedCount: int32(count), //nolint:gosec // bounded by batch size
	}), nil
}

func (h *Handler) BulkDeleteInfraDevices(ctx context.Context, req *connect.Request[pb.BulkDeleteInfraDevicesRequest]) (*connect.Response[pb.BulkDeleteInfraDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	count, err := h.service.BulkSoftDelete(ctx, info.OrganizationID, req.Msg.GetIds())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BulkDeleteInfraDevicesResponse{
		DeletedCount: int32(count), //nolint:gosec // bounded by batch size
	}), nil
}

func (h *Handler) TestConnection(ctx context.Context, req *connect.Request[pb.TestConnectionRequest]) (*connect.Response[pb.TestConnectionResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	success, err := h.service.TestConnection(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.TestConnectionResponse{
		Success: success,
	}), nil
}

func (h *Handler) ScanNetwork(ctx context.Context, req *connect.Request[pb.ScanNetworkRequest]) (*connect.Response[pb.ScanNetworkResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	discovered, err := h.service.ScanNetwork(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toScanNetworkResponse(discovered)), nil
}

func (h *Handler) PairDevices(ctx context.Context, req *connect.Request[pb.PairDevicesRequest]) (*connect.Response[pb.PairDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	devices, err := h.service.PairDevices(ctx, info.OrganizationID, toPairEntries(req.Msg))
	if err != nil {
		return nil, err
	}
	out := make([]*pb.InfraDevice, 0, len(devices))
	for _, d := range devices {
		out = append(out, toProtoInfraDevice(d))
	}
	return connect.NewResponse(&pb.PairDevicesResponse{
		Devices: out,
	}), nil
}
