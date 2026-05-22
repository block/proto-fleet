// Package buildings is the Connect-RPC surface for BuildingService.
package buildings

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/buildings/v1"
	"github.com/block/proto-fleet/server/generated/grpc/buildings/v1/buildingsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/buildings"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the BuildingService Connect-RPC surface.
type Handler struct {
	service *buildings.Service
}

var _ buildingsv1connect.BuildingServiceHandler = &Handler{}

// NewHandler returns a BuildingService handler bound to the supplied
// domain service.
func NewHandler(service *buildings.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListBuildings(ctx context.Context, req *connect.Request[pb.ListBuildingsRequest]) (*connect.Response[pb.ListBuildingsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	rows, err := h.service.ListBuildings(ctx, toListFilter(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListBuildingsResponse(rows)), nil
}

func (h *Handler) GetBuilding(ctx context.Context, req *connect.Request[pb.GetBuildingRequest]) (*connect.Response[pb.GetBuildingResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	building, err := h.service.GetBuilding(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetBuildingResponse{
		Building: toProtoBuilding(building),
	}), nil
}

func (h *Handler) CreateBuilding(ctx context.Context, req *connect.Request[pb.CreateBuildingRequest]) (*connect.Response[pb.CreateBuildingResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	building, err := h.service.CreateBuilding(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateBuildingResponse{
		Building: toProtoBuilding(building),
	}), nil
}

func (h *Handler) UpdateBuilding(ctx context.Context, req *connect.Request[pb.UpdateBuildingRequest]) (*connect.Response[pb.UpdateBuildingResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	building, err := h.service.UpdateBuilding(ctx, toUpdateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateBuildingResponse{
		Building: toProtoBuilding(building),
	}), nil
}

func (h *Handler) DeleteBuilding(ctx context.Context, req *connect.Request[pb.DeleteBuildingRequest]) (*connect.Response[pb.DeleteBuildingResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.DeleteBuilding(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteBuildingResponse{
		UnassignedRackCount: out.UnassignedRackCount,
	}), nil
}
