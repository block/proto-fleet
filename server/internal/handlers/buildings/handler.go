// Package buildings is the Connect-RPC surface for BuildingService.
package buildings

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/buildings/v1"
	"github.com/block/proto-fleet/server/generated/grpc/buildings/v1/buildingsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/buildings"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	"github.com/block/proto-fleet/server/internal/domain/session"
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
	filter := toListFilter(req.Msg, info.OrganizationID)
	filter.IncludeStats = true
	statsFilter, err := fleetlistfilter.Parse(req.Msg.GetErrorComponentTypes(), req.Msg.GetTelemetryRanges())
	if err != nil {
		return nil, err
	}
	includeStatsForSite := func(siteID *int64) bool {
		_, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{SiteID: siteID})
		return err == nil
	}
	rows, err := h.service.ListBuildings(ctx, filter, statsFilter, includeStatsForSite)
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

func (h *Handler) AssignRacksToBuilding(ctx context.Context, req *connect.Request[pb.AssignRacksToBuildingRequest]) (*connect.Response[pb.AssignRacksToBuildingResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.AssignRacksToBuilding(ctx, toAssignRacksToBuildingParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AssignRacksToBuildingResponse{
		SiteReassignedDeviceCount: out.SiteReassignedDeviceCount,
	}), nil
}

func (h *Handler) AssignDevicesToBuilding(ctx context.Context, req *connect.Request[pb.AssignDevicesToBuildingRequest]) (*connect.Response[pb.AssignDevicesToBuildingResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// force_clear_conflicting_rack_membership deletes
	// device_set_membership rows as a side effect — same auth gate
	// pattern as AssignDevicesToSite so site-only operators can't
	// bypass rack auth via this flag.
	if req.Msg.GetForceClearConflictingRackMembership() {
		if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
			return nil, err
		}
	}
	out, conflicts, err := h.service.AssignDevicesToBuilding(ctx, toAssignDevicesToBuildingParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return connect.NewResponse(&pb.AssignDevicesToBuildingResponse{
			Conflicts: toProtoBuildingConflicts(conflicts),
		}), nil
	}
	return connect.NewResponse(&pb.AssignDevicesToBuildingResponse{
		ReassignedCount:           out.ReassignedCount,
		SiteReassignedDeviceCount: out.SiteReassignedDeviceCount,
	}), nil
}

func (h *Handler) GetBuildingStats(ctx context.Context, req *connect.Request[pb.GetBuildingStatsRequest]) (*connect.Response[pb.GetBuildingStatsResponse], error) {
	// GetBuildingStats returns telemetry rollups + per-rack health +
	// device_identifiers, so it layers three permissions: site:read for
	// the building-existence surface, fleet:read for the aggregate
	// telemetry, and miner:read because device_identifiers is a miner-
	// inventory surface (the FE uses it to scope downstream telemetry +
	// component-error fetches).
	//
	// Resolve building → site_id BEFORE authz so site-scoped roles get
	// narrowed checks against the building's site. Without this, a user
	// with a site-A-narrowed grant evaluated against ResourceContext{}
	// would be treated as org-scoped and could read stats for buildings
	// in other sites. Org-scoped grants still satisfy the narrower
	// ResourceContext, so this only tightens — it never broadens.
	//
	// Pre-authz GetBuilding does leak existence to same-org callers that
	// later fail the site-scoped check (timing channel). Acceptable
	// trade-off: same pattern as miner:read → miner:site resolution.
	// Cross-org callers still see NotFound because GetBuilding is
	// org-scoped via session.OrganizationID.
	sess, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	building, err := h.service.GetBuilding(ctx, sess.OrganizationID, req.Msg.GetBuildingId())
	if err != nil {
		return nil, err
	}
	// Unassigned buildings (SiteID == nil) carry an empty ResourceContext,
	// so only org-scoped grants pass — site-scoped operators legitimately
	// can't see buildings outside their site, including the unassigned
	// pool.
	rc := authz.ResourceContext{SiteID: building.SiteID}
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, rc)
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, rc); err != nil {
		return nil, err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerRead, rc); err != nil {
		return nil, err
	}
	// Pass the building's site as we saw it at authz time. The service
	// re-reads the building and rejects with NotFound if a concurrent
	// AssignBuildingsToSite moved it — otherwise a site-scoped caller
	// could end up with telemetry for a site they're not authorized for.
	out, err := h.service.GetBuildingStats(ctx, info.OrganizationID, req.Msg.GetBuildingId(), building.SiteID)
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
		BuildingId:                out.BuildingID,
		RackCount:                 out.RackCount,
		DeviceCount:               out.DeviceCount,
		ReportingCount:            out.ReportingCount,
		HashrateReportingCount:    out.HashrateReportingCount,
		EfficiencyReportingCount:  out.EfficiencyReportingCount,
		PowerReportingCount:       out.PowerReportingCount,
		TemperatureReportingCount: out.TemperatureReportingCount,
		TotalHashrateThs:          out.TotalHashrateThs,
		AvgEfficiencyJth:          out.AvgEfficiencyJth,
		TotalPowerKw:              out.TotalPowerKw,
		MinTemperatureC:           out.MinTemperatureC,
		MaxTemperatureC:           out.MaxTemperatureC,
		HashingCount:              out.HashingCount,
		BrokenCount:               out.BrokenCount,
		OfflineCount:              out.OfflineCount,
		SleepingCount:             out.SleepingCount,
		ControlBoardIssueCount:    out.ControlBoardIssueCount,
		FanIssueCount:             out.FanIssueCount,
		HashBoardIssueCount:       out.HashBoardIssueCount,
		PsuIssueCount:             out.PsuIssueCount,
		RackHealth:                rackHealth,
		DeviceIdentifiers:         out.DeviceIdentifiers,
	}), nil
}
