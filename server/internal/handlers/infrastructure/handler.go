// Package infrastructure is the Connect-RPC surface for
// InfrastructureService.
package infrastructure

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1"
	"github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1/infrastructurev1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the InfrastructureService Connect-RPC surface.
type Handler struct {
	service *infrastructure.Service
}

var _ infrastructurev1connect.InfrastructureServiceHandler = &Handler{}

// NewHandler returns an InfrastructureService handler bound to the
// supplied domain service.
func NewHandler(service *infrastructure.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListInfrastructureDevices(ctx context.Context, req *connect.Request[pb.ListInfrastructureDevicesRequest]) (*connect.Response[pb.ListInfrastructureDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	devices, err := h.service.List(ctx, toListFilter(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListResponse(devices)), nil
}

func (h *Handler) GetInfrastructureDevice(ctx context.Context, req *connect.Request[pb.GetInfrastructureDeviceRequest]) (*connect.Response[pb.GetInfrastructureDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	device, err := h.service.Get(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetInfrastructureDeviceResponse{
		Device: toProtoDevice(device),
	}), nil
}

func (h *Handler) CreateInfrastructureDevice(ctx context.Context, req *connect.Request[pb.CreateInfrastructureDeviceRequest]) (*connect.Response[pb.CreateInfrastructureDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	device, err := h.service.Create(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateInfrastructureDeviceResponse{
		Device: toProtoDevice(device),
	}), nil
}

func (h *Handler) UpdateInfrastructureDevice(ctx context.Context, req *connect.Request[pb.UpdateInfrastructureDeviceRequest]) (*connect.Response[pb.UpdateInfrastructureDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	device, err := h.service.Update(ctx, toUpdateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateInfrastructureDeviceResponse{
		Device: toProtoDevice(device),
	}), nil
}

func (h *Handler) DeleteInfrastructureDevice(ctx context.Context, req *connect.Request[pb.DeleteInfrastructureDeviceRequest]) (*connect.Response[pb.DeleteInfrastructureDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.service.Delete(ctx, info.OrganizationID, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteInfrastructureDeviceResponse{}), nil
}
