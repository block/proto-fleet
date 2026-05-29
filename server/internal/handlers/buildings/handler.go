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

func (h *Handler) ListBuildingRacks(ctx context.Context, req *connect.Request[pb.ListBuildingRacksRequest]) (*connect.Response[pb.ListBuildingRacksResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	racks, nextPageToken, err := h.service.ListBuildingRacks(
		ctx,
		info.OrganizationID,
		req.Msg.GetBuildingId(),
		req.Msg.GetPageSize(),
		req.Msg.GetPageToken(),
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListBuildingRacksResponse(racks, nextPageToken)), nil
}

func (h *Handler) AssignRackToBuilding(ctx context.Context, req *connect.Request[pb.AssignRackToBuildingRequest]) (*connect.Response[pb.AssignRackToBuildingResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.AssignRackToBuilding(ctx, toAssignRackToBuildingParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AssignRackToBuildingResponse{
		SiteReassignedDeviceCount: out.SiteReassignedDeviceCount,
	}), nil
}

func (h *Handler) GetBuildingStats(ctx context.Context, req *connect.Request[pb.GetBuildingStatsRequest]) (*connect.Response[pb.GetBuildingStatsResponse], error) {
	// GetBuildingStats returns telemetry rollups + per-rack health +
	// device_identifiers, so it layers three permissions: site:read for
	// the building-existence surface, fleet:read for the aggregate
	// telemetry, and miner:read because device_identifiers is a miner-
	// inventory surface (the FE uses it to scope downstream telemetry +
	// component-error fetches). Future migration to site-scoped
	// narrowing on PermSiteRead requires resolving building→site before
	// the authz check.
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	out, err := h.service.GetBuildingStats(ctx, info.OrganizationID, req.Msg.GetBuildingId())
	if err != nil {
		return nil, err
	}
	rackHealth := make([]*pb.BuildingRackHealth, 0, len(out.RackHealth))
	for _, r := range out.RackHealth {
		rackHealth = append(rackHealth, &pb.BuildingRackHealth{
			RackId:          r.RackID,
			RackLabel:       r.RackLabel,
			AisleIndex:      r.AisleIndex,
			PositionInAisle: r.PositionInAisle,
			HashingCount:    r.HashingCount,
			BrokenCount:     r.BrokenCount,
			OfflineCount:    r.OfflineCount,
			SleepingCount:   r.SleepingCount,
		})
	}
	return connect.NewResponse(&pb.GetBuildingStatsResponse{
		BuildingId:               out.BuildingID,
		RackCount:                out.RackCount,
		DeviceCount:              out.DeviceCount,
		ReportingCount:           out.ReportingCount,
		HashrateReportingCount:   out.HashrateReportingCount,
		EfficiencyReportingCount: out.EfficiencyReportingCount,
		PowerReportingCount:      out.PowerReportingCount,
		TotalHashrateThs:         out.TotalHashrateThs,
		AvgEfficiencyJth:         out.AvgEfficiencyJth,
		TotalPowerKw:             out.TotalPowerKw,
		HashingCount:             out.HashingCount,
		BrokenCount:              out.BrokenCount,
		OfflineCount:             out.OfflineCount,
		SleepingCount:            out.SleepingCount,
		RackHealth:               rackHealth,
		DeviceIdentifiers:        out.DeviceIdentifiers,
	}), nil
}
