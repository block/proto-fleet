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
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	"github.com/block/proto-fleet/server/internal/domain/session"
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

func (h *Handler) ListSites(ctx context.Context, req *connect.Request[pb.ListSitesRequest]) (*connect.Response[pb.ListSitesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	statsFilter, err := fleetlistfilter.Parse(req.Msg.GetErrorComponentTypes(), req.Msg.GetTelemetryRanges())
	if err != nil {
		return nil, err
	}
	includeStatsForSite := func(siteID int64) bool {
		_, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{SiteID: &siteID})
		return err == nil
	}
	out, err := h.service.ListSites(ctx, info.OrganizationID, statsFilter, includeStatsForSite)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListSitesResponse(out)), nil
}

func (h *Handler) ResolveSiteBySlug(ctx context.Context, req *connect.Request[pb.ResolveSiteBySlugRequest]) (*connect.Response[pb.ResolveSiteBySlugResponse], error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	site, err := h.service.GetSiteBySlug(ctx, info.OrganizationID, req.Msg.GetSlug())
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{SiteID: &site.ID}); err != nil {
		if !fleeterror.IsForbiddenError(err) {
			return nil, err
		}
		return nil, fleeterror.NewNotFoundErrorf("site %q not found", req.Msg.GetSlug())
	}
	return connect.NewResponse(&pb.ResolveSiteBySlugResponse{Site: toProtoSite(site)}), nil
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
	// The org-scoped gate above ignores narrowing, but the delete cascade
	// soft-deletes resources (infrastructure devices) whose own RPCs
	// evaluate site:manage against the concrete site. Re-check against
	// the target site so a caller narrowed away from it can't use
	// DeleteSite as a bypass. Site-scoped-only callers still fail the
	// org-scoped gate first — this check only tightens, never widens.
	siteID := req.Msg.GetId()
	if _, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: &siteID}); err != nil {
		return nil, err
	}
	out, err := h.service.DeleteSite(ctx, info.OrganizationID, siteID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteSiteResponse{
		UnassignedDeviceCount:            out.UnassignedDeviceCount,
		DeletedBuildingCount:             out.DeletedBuildingCount,
		UnassignedRackCount:              out.UnassignedRackCount,
		DeletedInfrastructureDeviceCount: out.DeletedInfrastructureDeviceCount,
	}), nil
}

func (h *Handler) AssignDevicesToSite(ctx context.Context, req *connect.Request[pb.AssignDevicesToSiteRequest]) (*connect.Response[pb.AssignDevicesToSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// force_clear_conflicting_rack_membership deletes device_set_membership
	// rows as a side effect — the same write sibling rack RPCs gate on
	// rack:manage. Require both keys when the caller opts into the
	// cascade so site-only operators can't bypass rack auth via this flag.
	if req.Msg.GetForceClearConflictingRackMembership() {
		if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
			return nil, err
		}
	}
	count, conflicts, err := h.service.AssignDevicesToSite(ctx, toAssignDevicesParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AssignDevicesToSiteResponse{
		ReassignedCount: count,
		Conflicts:       toProtoConflicts(conflicts),
	}), nil
}

func (h *Handler) AssignBuildingsToSite(ctx context.Context, req *connect.Request[pb.AssignBuildingsToSiteRequest]) (*connect.Response[pb.AssignBuildingsToSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.AssignBuildingsToSite(ctx, toAssignBuildingsParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AssignBuildingsToSiteResponse{
		ReassignedRackCount:   out.ReassignedRackCount,
		ReassignedDeviceCount: out.ReassignedDeviceCount,
	}), nil
}

func (h *Handler) AssignRacksToSite(ctx context.Context, req *connect.Request[pb.AssignRacksToSiteRequest]) (*connect.Response[pb.AssignRacksToSiteResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	out, err := h.service.AssignRacksToSite(ctx, toAssignRacksToSiteParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AssignRacksToSiteResponse{
		ReassignedDeviceCount: out.ReassignedDeviceCount,
		ClearedBuildingCount:  out.ClearedBuildingCount,
	}), nil
}

func (h *Handler) GetInfrastructureControlSubnets(ctx context.Context, req *connect.Request[pb.GetInfrastructureControlSubnetsRequest]) (*connect.Response[pb.GetInfrastructureControlSubnetsResponse], error) {
	siteID := req.Msg.GetSiteId()
	// Commissioning controls the deployment-global Modbus write boundary,
	// so a grant narrowed to one site is insufficient even for that site.
	info, err := middleware.RequireOrgWidePermission(ctx, authz.PermSiteManage)
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequireAdmin(ctx, "view infrastructure control subnets"); err != nil {
		return nil, err
	}

	subnets, err := h.service.GetInfrastructureControlSubnets(ctx, info.OrganizationID, siteID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetInfrastructureControlSubnetsResponse{
		SiteId:                       siteID,
		InfrastructureControlSubnets: subnets,
	}), nil
}

func (h *Handler) SetInfrastructureControlSubnets(ctx context.Context, req *connect.Request[pb.SetInfrastructureControlSubnetsRequest]) (*connect.Response[pb.SetInfrastructureControlSubnetsResponse], error) {
	siteID := req.Msg.GetSiteId()
	// Keep reads and writes behind the same organization-wide topology gate.
	info, err := middleware.RequireOrgWidePermission(ctx, authz.PermSiteManage)
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequireAdmin(ctx, "commission infrastructure control subnets"); err != nil {
		return nil, err
	}

	subnets, err := h.service.SetInfrastructureControlSubnets(
		ctx,
		info.OrganizationID,
		siteID,
		req.Msg.GetInfrastructureControlSubnets(),
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetInfrastructureControlSubnetsResponse{
		SiteId:                       siteID,
		InfrastructureControlSubnets: subnets,
	}), nil
}

func (h *Handler) GetSiteStats(ctx context.Context, req *connect.Request[pb.GetSiteStatsRequest]) (*connect.Response[pb.GetSiteStatsResponse], error) {
	// GetSiteStats returns telemetry rollups + miner health buckets, so
	// site:read alone isn't enough; we also gate on fleet:read. Both checks
	// pass the request's SiteID as ResourceContext so a caller with only a
	// site-scoped role (e.g. ADMIN-scoped to this site) still satisfies
	// both gates — an org-scoped fleet:read check would reject valid
	// site-scoped operators even though the rollup is scoped to the
	// requested site.
	siteID := req.Msg.GetSiteId()
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{SiteID: &siteID})
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{SiteID: &siteID}); err != nil {
		return nil, err
	}
	out, err := h.service.GetSiteStats(ctx, info.OrganizationID, siteID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetSiteStatsResponse{
		SiteId:                    out.SiteID,
		BuildingCount:             out.BuildingCount,
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
		RackCount:                 out.RackCount,
	}), nil
}
