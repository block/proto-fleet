// Package sites is the Connect-RPC surface for SiteService.
// Translation between proto and domain types lives in translate.go;
// this file is the wiring + auth gate.
package sites

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	"github.com/block/proto-fleet/server/generated/grpc/sites/v1/sitesv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the SiteService Connect-RPC surface.
type Handler struct {
	service *sites.Service
}

var _ sitesv1connect.SiteServiceHandler = &Handler{}

// NewHandler returns a SiteService handler bound to the supplied
// domain service.
func NewHandler(service *sites.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListSites(ctx context.Context, _ *connect.Request[pb.ListSitesRequest]) (*connect.Response[pb.ListSitesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.ListSites(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListSitesResponse(out)), nil
}

func (h *Handler) CreateSite(ctx context.Context, req *connect.Request[pb.CreateSiteRequest]) (*connect.Response[pb.CreateSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.CreateSite(ctx, toCreateSiteParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateSiteResponse{
		Site:                  toProtoSite(result.Site),
		NetworkConfigWarnings: result.NetworkConfigWarnings,
	}), nil
}

func (h *Handler) UpdateSite(ctx context.Context, req *connect.Request[pb.UpdateSiteRequest]) (*connect.Response[pb.UpdateSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	result, err := h.service.UpdateSite(ctx, toUpdateSiteParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateSiteResponse{
		Site:                  toProtoSite(result.Site),
		NetworkConfigWarnings: result.NetworkConfigWarnings,
	}), nil
}

func (h *Handler) DeleteSite(ctx context.Context, req *connect.Request[pb.DeleteSiteRequest]) (*connect.Response[pb.DeleteSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.DeleteSite(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteSiteResponse{
		UnassignedDeviceCount: out.UnassignedDeviceCount,
		DeletedBuildingCount:  out.DeletedBuildingCount,
		UnassignedRackCount:   out.UnassignedRackCount,
	}), nil
}

func (h *Handler) ReassignDevicesToSite(ctx context.Context, req *connect.Request[pb.ReassignDevicesToSiteRequest]) (*connect.Response[pb.ReassignDevicesToSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	count, conflicts, err := h.service.ReassignDevicesToSite(ctx, toReassignParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ReassignDevicesToSiteResponse{
		ReassignedCount: count,
		Conflicts:       toProtoConflicts(conflicts),
	}), nil
}

func (h *Handler) AssignBuildingToSite(ctx context.Context, req *connect.Request[pb.AssignBuildingToSiteRequest]) (*connect.Response[pb.AssignBuildingToSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.AssignBuildingToSite(ctx, toAssignBuildingParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AssignBuildingToSiteResponse{
		ReassignedRackCount:   out.ReassignedRackCount,
		ReassignedDeviceCount: out.ReassignedDeviceCount,
	}), nil
}
