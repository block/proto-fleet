package collection

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/generated/grpc/collection/v1/collectionv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the DeviceCollectionService gRPC handler.
type Handler struct {
	collectionSvc *collection.Service
}

var _ collectionv1connect.DeviceCollectionServiceHandler = &Handler{}

// NewHandler creates a new collection handler.
func NewHandler(svc *collection.Service) *Handler {
	return &Handler{
		collectionSvc: svc,
	}
}

// authorizeRackPlacement gates a rack placement on site:manage and returns a
// context carrying the authorized placement for the service to bind the write
// to (see authz.AuthorizedPlacement). It mirrors the device_set.v1 handler:
// the check is scoped to the site the rack lands in (a building_id resolves to
// its parent site), and a MOVE is authorized against BOTH the rack's current
// site and the destination so a caller who manages only the destination can't
// pull a rack out of a site they cannot manage. currentSiteID is nil for a new
// rack (the source side is skipped when nil). Callers invoke this only when the
// request carries placement intent, so the destination is always authorized: at
// its resolved site, or — when the placement resolves site-less and no source
// site is checked either — at org scope, so rack:manage alone can never place a
// rack.
func (h *Handler) authorizeRackPlacement(ctx context.Context, orgID int64, currentSiteID, siteID, buildingID *int64) (context.Context, error) {
	targetSiteID, err := h.resolvePlacementTargetSite(ctx, orgID, siteID, buildingID)
	if err != nil {
		return ctx, err
	}
	sites := distinctSites(currentSiteID, targetSiteID)
	if len(sites) == 0 {
		// Placement intent present but nothing to narrow on (site-less building
		// or explicit unassign): require site:manage at org scope.
		sites = []*int64{nil}
	}
	for _, site := range sites {
		if _, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: site}); err != nil {
			return ctx, err
		}
	}
	return authz.WithAuthorizedPlacement(ctx, authz.AuthorizedPlacement{
		CurrentSiteID: currentSiteID,
		TargetSiteID:  targetSiteID,
	}), nil
}

// resolvePlacementTargetSite maps a placement request to the destination
// site_id: a building_id resolves to its parent site, an explicit site_id is
// used directly, and an unassign (id == 0) or a site-less building resolves to
// no site (nil).
func (h *Handler) resolvePlacementTargetSite(ctx context.Context, orgID int64, siteID, buildingID *int64) (*int64, error) {
	if buildingID != nil && *buildingID > 0 {
		return h.collectionSvc.ResolveBuildingSite(ctx, orgID, *buildingID)
	}
	if siteID != nil && *siteID > 0 {
		return siteID, nil
	}
	return nil, nil
}

// distinctSites returns the distinct non-nil site ids among the given sites,
// so an in-place re-assert (source == destination) is checked once and a nil
// (untouched) side is skipped.
func distinctSites(sites ...*int64) []*int64 {
	var out []*int64
	seen := make(map[int64]struct{}, len(sites))
	for _, s := range sites {
		if s == nil {
			continue
		}
		if _, dup := seen[*s]; dup {
			continue
		}
		seen[*s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// CreateCollection creates a new collection.
func (h *Handler) CreateCollection(ctx context.Context, r *connect.Request[pb.CreateCollectionRequest]) (*connect.Response[pb.CreateCollectionResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// Creating a rack under a site/building persists that placement, so mirror
	// the Update/SaveRack gate: require site:manage when rack_info carries
	// explicit placement (site_id/building_id). The rack is new (no source
	// site), so authorize the destination only. Otherwise a rack:manage-only
	// caller could place a rack via the create path, bypassing the boundary.
	if ri := r.Msg.GetRackInfo(); ri != nil && (ri.SiteId != nil || ri.BuildingId != nil) {
		ctx, err = h.authorizeRackPlacement(ctx, info.OrganizationID, nil /* currentSiteID */, ri.SiteId, ri.BuildingId)
		if err != nil {
			return nil, err
		}
	}
	result, err := h.collectionSvc.CreateCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetCollection retrieves a collection by ID.
func (h *Handler) GetCollection(ctx context.Context, r *connect.Request[pb.GetCollectionRequest]) (*connect.Response[pb.GetCollectionResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.GetCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// UpdateCollection updates a collection's label and/or description.
func (h *Handler) UpdateCollection(ctx context.Context, r *connect.Request[pb.UpdateCollectionRequest]) (*connect.Response[pb.UpdateCollectionResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// UpdateCollection now persists a rack's placement (site/building) when
	// rack_info carries it — the same reparent + cascade the DeviceSet handler
	// exposes — so mirror its gate: require site:manage when placement intent is
	// present (explicit site_id/building_id, incl. 0 to unassign). This moves an
	// existing rack, so authorize both its current site and the destination.
	// Metadata-only edits (label/zone/dims, membership) stay rack:manage.
	if ri := r.Msg.GetRackInfo(); ri != nil && (ri.SiteId != nil || ri.BuildingId != nil) {
		currentSiteID, err := h.collectionSvc.ResolveRackSite(ctx, info.OrganizationID, r.Msg.CollectionId)
		if err != nil {
			return nil, err
		}
		ctx, err = h.authorizeRackPlacement(ctx, info.OrganizationID, currentSiteID, ri.SiteId, ri.BuildingId)
		if err != nil {
			return nil, err
		}
	}
	result, err := h.collectionSvc.UpdateCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// DeleteCollection soft-deletes a collection.
func (h *Handler) DeleteCollection(ctx context.Context, r *connect.Request[pb.DeleteCollectionRequest]) (*connect.Response[pb.DeleteCollectionResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.DeleteCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListCollections returns all collections for the organization.
func (h *Handler) ListCollections(ctx context.Context, r *connect.Request[pb.ListCollectionsRequest]) (*connect.Response[pb.ListCollectionsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.ListCollections(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListCollectionMembers returns all members of a collection.
func (h *Handler) ListCollectionMembers(ctx context.Context, r *connect.Request[pb.ListCollectionMembersRequest]) (*connect.Response[pb.ListCollectionMembersResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.ListCollectionMembers(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetDeviceCollections returns all collections a device belongs to.
func (h *Handler) GetDeviceCollections(ctx context.Context, r *connect.Request[pb.GetDeviceCollectionsRequest]) (*connect.Response[pb.GetDeviceCollectionsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.GetDeviceCollections(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// SetRackSlotPosition sets a device's slot position within a rack.
func (h *Handler) SetRackSlotPosition(ctx context.Context, r *connect.Request[pb.SetRackSlotPositionRequest]) (*connect.Response[pb.SetRackSlotPositionResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.SetRackSlotPosition(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ClearRackSlotPosition clears a device's slot position within a rack.
func (h *Handler) ClearRackSlotPosition(ctx context.Context, r *connect.Request[pb.ClearRackSlotPositionRequest]) (*connect.Response[pb.ClearRackSlotPositionResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.ClearRackSlotPosition(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetRackSlots lists all occupied slot positions in a rack.
func (h *Handler) GetRackSlots(ctx context.Context, r *connect.Request[pb.GetRackSlotsRequest]) (*connect.Response[pb.GetRackSlotsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.GetRackSlots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetCollectionStats returns aggregated telemetry stats for collections.
func (h *Handler) GetCollectionStats(ctx context.Context, r *connect.Request[pb.GetCollectionStatsRequest]) (*connect.Response[pb.GetCollectionStatsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.GetCollectionStats(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListRackTypes returns all distinct rack types for the organization.
func (h *Handler) ListRackTypes(ctx context.Context, r *connect.Request[pb.ListRackTypesRequest]) (*connect.Response[pb.ListRackTypesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.ListRackTypes(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListRackZones returns all distinct rack zones for the organization.
func (h *Handler) ListRackZones(ctx context.Context, r *connect.Request[pb.ListRackZonesRequest]) (*connect.Response[pb.ListRackZonesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.collectionSvc.ListRackZones(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// SaveRack atomically creates or updates a rack with membership and slot assignments.
func (h *Handler) SaveRack(ctx context.Context, r *connect.Request[pb.SaveRackRequest]) (*connect.Response[pb.SaveRackResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// Placement (site/building) is a site-management action, matching the
	// dedicated AssignRacksToSite/Building RPCs. Require site:manage when the
	// request carries placement intent; omitted placement preserves the
	// current site/building and stays rack:manage. SaveRack can move an existing
	// rack, so for an update authorize its current site as well as the
	// destination; a create has no source. Mirrors the device_set.v1 handler.
	if ri := r.Msg.RackInfo; ri != nil && (ri.SiteId != nil || ri.BuildingId != nil) {
		var currentSiteID *int64
		if r.Msg.CollectionId != nil {
			currentSiteID, err = h.collectionSvc.ResolveRackSite(ctx, info.OrganizationID, *r.Msg.CollectionId)
			if err != nil {
				return nil, err
			}
		}
		ctx, err = h.authorizeRackPlacement(ctx, info.OrganizationID, currentSiteID, ri.SiteId, ri.BuildingId)
		if err != nil {
			return nil, err
		}
	}
	// collection.v1 is deprecated and its SaveRackResponse has no conflict
	// field, so it can't carry the site-strip confirmation contract that
	// device_set.v1 uses. Pass force=false and, if the save would strip a
	// member's site/building, fail loudly instead of silently clearing it;
	// callers needing the confirm-and-force flow must use device_set.v1.
	result, err := h.collectionSvc.SaveRack(ctx, r.Msg, false)
	if err != nil {
		return nil, err
	}
	if len(result.Conflicts) > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf(
			"%d device(s) would lose their site/building placement by joining this rack; use device_set.v1.SaveRack with force_clear_conflicting_site to confirm",
			len(result.Conflicts)))
	}
	return connect.NewResponse(&pb.SaveRackResponse{
		Collection:          result.Collection,
		AssignedCount:       result.AssignedCount,
		SiteReassignedCount: result.SiteReassignedCount,
	}), nil
}
