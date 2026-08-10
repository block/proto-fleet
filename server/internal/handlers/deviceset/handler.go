package deviceset

import (
	"context"

	"connectrpc.com/connect"
	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/generated/grpc/device_set/v1/device_setv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the DeviceSetService gRPC handler.
// It adapts between the new DeviceSet proto types and the existing collection.Service
// which still uses the old Collection proto types internally.
type Handler struct {
	svc *collection.Service
}

var _ device_setv1connect.DeviceSetServiceHandler = &Handler{}

// NewHandler creates a new device set handler.
func NewHandler(svc *collection.Service) *Handler {
	return &Handler{svc: svc}
}

// authorizeRackPlacement gates a rack placement on site:manage and returns a
// context carrying the authorized placement for the service to bind the write
// to (see authz.AuthorizedPlacement).
//
// Placing a rack is a site-management action, so a rack:manage-only caller may
// edit rack contents but not move the rack. The check is scoped to the site
// the rack actually lands in: a building_id resolves to its parent site (it
// dictates the site; a disagreeing site_id is rejected later under lock) so a
// caller narrowed out of that site can't bypass the check by naming a building
// instead. Crucially, a MOVE is authorized against BOTH the rack's current
// site (currentSiteID) and the destination — otherwise a caller who manages
// only the destination could pull a rack out of a site they cannot manage.
// currentSiteID is nil for a new rack (create paths). A site the placement
// does not touch (nil source or nil destination) is not checked.
func (h *Handler) authorizeRackPlacement(ctx context.Context, orgID int64, currentSiteID, siteID, buildingID *int64) (context.Context, error) {
	targetSiteID, err := h.resolvePlacementTargetSite(ctx, orgID, siteID, buildingID)
	if err != nil {
		return ctx, err
	}
	for _, site := range distinctSites(currentSiteID, targetSiteID) {
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
		return h.svc.ResolveBuildingSite(ctx, orgID, *buildingID)
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

func (h *Handler) CreateDeviceSet(ctx context.Context, r *connect.Request[dspb.CreateDeviceSetRequest]) (*connect.Response[dspb.CreateDeviceSetResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// Creating a rack under a site/building persists that placement (and can
	// cascade added devices to it), so mirror the UpdateDeviceSet/SaveRack gate:
	// require site:manage when rack_info carries explicit placement. The rack is
	// new, so there is no source site — authorize the destination only.
	if ri, ok := r.Msg.TypeDetails.(*dspb.CreateDeviceSetRequest_RackInfo); ok && ri.RackInfo != nil && (ri.RackInfo.SiteId != nil || ri.RackInfo.BuildingId != nil) {
		ctx, err = h.authorizeRackPlacement(ctx, info.OrganizationID, nil /* currentSiteID */, ri.RackInfo.SiteId, ri.RackInfo.BuildingId)
		if err != nil {
			return nil, err
		}
	}
	req := toCollectionCreateReq(r.Msg)
	result, err := h.svc.CreateCollection(ctx, req)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.CreateDeviceSetResponse{
		DeviceSet:  toDeviceSet(result.Collection),
		AddedCount: result.AddedCount,
	}), nil
}

func (h *Handler) GetDeviceSet(ctx context.Context, r *connect.Request[dspb.GetDeviceSetRequest]) (*connect.Response[dspb.GetDeviceSetResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.GetCollection(ctx, &collectionpb.GetCollectionRequest{
		CollectionId: r.Msg.DeviceSetId,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.GetDeviceSetResponse{
		DeviceSet: toDeviceSet(result.Collection),
	}), nil
}

func (h *Handler) UpdateDeviceSet(ctx context.Context, r *connect.Request[dspb.UpdateDeviceSetRequest]) (*connect.Response[dspb.UpdateDeviceSetResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// A rack's placement is now persisted here too (zone/dims + site/building
	// in one settings save). Placing a rack under a site/building is a
	// site-management action, so — mirroring SaveRack — require site:manage
	// when the request carries explicit placement intent (site_id/building_id,
	// including 0 to unassign). This is a MOVE of an existing rack, so authorize
	// both its current site and the destination. Metadata-only edits
	// (label/zone/dims, or a membership change) stay rack:manage.
	if ri, ok := r.Msg.TypeDetails.(*dspb.UpdateDeviceSetRequest_RackInfo); ok && ri.RackInfo != nil && (ri.RackInfo.SiteId != nil || ri.RackInfo.BuildingId != nil) {
		currentSiteID, err := h.svc.ResolveRackSite(ctx, info.OrganizationID, r.Msg.DeviceSetId)
		if err != nil {
			return nil, err
		}
		ctx, err = h.authorizeRackPlacement(ctx, info.OrganizationID, currentSiteID, ri.RackInfo.SiteId, ri.RackInfo.BuildingId)
		if err != nil {
			return nil, err
		}
	}
	req := toCollectionUpdateReq(r.Msg)
	result, err := h.svc.UpdateCollection(ctx, req)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.UpdateDeviceSetResponse{
		DeviceSet: toDeviceSet(result.Collection),
	}), nil
}

func (h *Handler) DeleteDeviceSet(ctx context.Context, r *connect.Request[dspb.DeleteDeviceSetRequest]) (*connect.Response[dspb.DeleteDeviceSetResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	_, err := h.svc.DeleteCollection(ctx, &collectionpb.DeleteCollectionRequest{
		CollectionId: r.Msg.DeviceSetId,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.DeleteDeviceSetResponse{}), nil
}

func (h *Handler) ListDeviceSets(ctx context.Context, r *connect.Request[dspb.ListDeviceSetsRequest]) (*connect.Response[dspb.ListDeviceSetsResponse], error) {
	if _, err := requireDeviceSetReadPermission(ctx, r.Msg.SiteIds); err != nil {
		return nil, err
	}
	params, err := toListCollectionsParams(r.Msg)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.ListCollectionsDomain(ctx, params)
	if err != nil {
		return nil, err
	}
	deviceSets := make([]*dspb.DeviceSet, len(result.Collections))
	for i, c := range result.Collections {
		deviceSets[i] = toDeviceSet(c)
	}
	return connect.NewResponse(&dspb.ListDeviceSetsResponse{
		DeviceSets:    deviceSets,
		NextPageToken: result.NextPageToken,
		TotalCount:    result.TotalCount,
	}), nil
}

func (h *Handler) AddDevicesToGroup(ctx context.Context, r *connect.Request[dspb.AddDevicesToGroupRequest]) (*connect.Response[dspb.AddDevicesToGroupResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.AddDevicesToGroup(ctx, collection.AddDevicesToGroupParams{
		TargetGroupID:  r.Msg.GetTargetGroupId(),
		DeviceSelector: r.Msg.GetDeviceSelector(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.AddDevicesToGroupResponse{
		AddedCount: result.AddedCount,
	}), nil
}

func (h *Handler) RemoveDevicesFromGroup(ctx context.Context, r *connect.Request[dspb.RemoveDevicesFromGroupRequest]) (*connect.Response[dspb.RemoveDevicesFromGroupResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.RemoveDevicesFromGroup(ctx, collection.RemoveDevicesFromGroupParams{
		TargetGroupID:  r.Msg.GetTargetGroupId(),
		DeviceSelector: r.Msg.GetDeviceSelector(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.RemoveDevicesFromGroupResponse{
		RemovedCount: result.RemovedCount,
	}), nil
}

func (h *Handler) ListDeviceSetMembers(ctx context.Context, r *connect.Request[dspb.ListDeviceSetMembersRequest]) (*connect.Response[dspb.ListDeviceSetMembersResponse], error) {
	if _, err := requireDeviceSetReadPermission(ctx, r.Msg.SiteIds); err != nil {
		return nil, err
	}
	result, err := h.svc.ListCollectionMembersDomain(ctx, collection.ListCollectionMembersParams{
		CollectionID: r.Msg.DeviceSetId,
		PageSize:     r.Msg.PageSize,
		PageToken:    r.Msg.PageToken,
		Filter: &interfaces.DeviceSetFilter{
			SiteIDs:           r.Msg.SiteIds,
			IncludeUnassigned: r.Msg.IncludeUnassigned,
		},
	})
	if err != nil {
		return nil, err
	}
	members := make([]*dspb.DeviceSetMember, len(result.Members))
	for i, m := range result.Members {
		members[i] = toDeviceSetMember(m)
	}
	return connect.NewResponse(&dspb.ListDeviceSetMembersResponse{
		Members:       members,
		NextPageToken: result.NextPageToken,
	}), nil
}

func requireDeviceSetReadPermission(ctx context.Context, siteIDs []int64) (*session.Info, error) {
	if err := validateDeviceSetSiteIDs(siteIDs); err != nil {
		return nil, err
	}

	info, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}

	for i := range siteIDs {
		if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{SiteID: &siteIDs[i]}); err != nil {
			return nil, err
		}
	}

	return info, nil
}

func validateDeviceSetSiteIDs(siteIDs []int64) error {
	if len(siteIDs) > maxDeviceSetFilterValues {
		return fleeterror.NewInvalidArgumentErrorf("site_ids exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	for i, id := range siteIDs {
		if id <= 0 {
			return fleeterror.NewInvalidArgumentErrorf("site_ids[%d] must be positive", i)
		}
	}
	return nil
}

func (h *Handler) GetDeviceDeviceSets(ctx context.Context, r *connect.Request[dspb.GetDeviceDeviceSetsRequest]) (*connect.Response[dspb.GetDeviceDeviceSetsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.GetDeviceCollections(ctx, &collectionpb.GetDeviceCollectionsRequest{
		DeviceIdentifier: r.Msg.DeviceIdentifier,
		Type:             toCollectionType(r.Msg.Type),
	})
	if err != nil {
		return nil, err
	}
	deviceSets := make([]*dspb.DeviceSet, len(result.Collections))
	for i, c := range result.Collections {
		deviceSets[i] = toDeviceSet(c)
	}
	return connect.NewResponse(&dspb.GetDeviceDeviceSetsResponse{
		DeviceSets: deviceSets,
	}), nil
}

func (h *Handler) SetRackSlotPosition(ctx context.Context, r *connect.Request[dspb.SetRackSlotPositionRequest]) (*connect.Response[dspb.SetRackSlotPositionResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.SetRackSlotPosition(ctx, &collectionpb.SetRackSlotPositionRequest{
		CollectionId:     r.Msg.DeviceSetId,
		DeviceIdentifier: r.Msg.DeviceIdentifier,
		Position:         toCollectionRackSlotPosition(r.Msg.Position),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.SetRackSlotPositionResponse{
		DeviceSetId: result.CollectionId,
		Slot:        toDeviceSetRackSlot(result.Slot),
	}), nil
}

func (h *Handler) ClearRackSlotPosition(ctx context.Context, r *connect.Request[dspb.ClearRackSlotPositionRequest]) (*connect.Response[dspb.ClearRackSlotPositionResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	_, err := h.svc.ClearRackSlotPosition(ctx, &collectionpb.ClearRackSlotPositionRequest{
		CollectionId:     r.Msg.DeviceSetId,
		DeviceIdentifier: r.Msg.DeviceIdentifier,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.ClearRackSlotPositionResponse{}), nil
}

func (h *Handler) GetRackSlots(ctx context.Context, r *connect.Request[dspb.GetRackSlotsRequest]) (*connect.Response[dspb.GetRackSlotsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.GetRackSlots(ctx, &collectionpb.GetRackSlotsRequest{
		CollectionId: r.Msg.DeviceSetId,
	})
	if err != nil {
		return nil, err
	}
	slots := make([]*dspb.RackSlot, len(result.Slots))
	for i, s := range result.Slots {
		slots[i] = toDeviceSetRackSlot(s)
	}
	return connect.NewResponse(&dspb.GetRackSlotsResponse{
		Slots: slots,
	}), nil
}

func (h *Handler) GetDeviceSetStats(ctx context.Context, r *connect.Request[dspb.GetDeviceSetStatsRequest]) (*connect.Response[dspb.GetDeviceSetStatsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.GetCollectionStats(ctx, &collectionpb.GetCollectionStatsRequest{
		CollectionIds: r.Msg.DeviceSetIds,
	})
	if err != nil {
		return nil, err
	}
	stats := make([]*dspb.DeviceSetStats, len(result.Stats))
	for i, s := range result.Stats {
		stats[i] = toDeviceSetStats(s)
	}
	return connect.NewResponse(&dspb.GetDeviceSetStatsResponse{
		Stats: stats,
	}), nil
}

func (h *Handler) ListRackZones(ctx context.Context, r *connect.Request[dspb.ListRackZonesRequest]) (*connect.Response[dspb.ListRackZonesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.ListRackZones(ctx, &collectionpb.ListRackZonesRequest{})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.ListRackZonesResponse{
		Zones: result.Zones,
	}), nil
}

func (h *Handler) ListRackZoneRefs(ctx context.Context, r *connect.Request[dspb.ListRackZoneRefsRequest]) (*connect.Response[dspb.ListRackZoneRefsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	refs, err := h.svc.ListRackZoneRefs(ctx)
	if err != nil {
		return nil, err
	}
	zones := make([]*commonpb.ZoneRef, len(refs))
	for i, ref := range refs {
		zones[i] = &commonpb.ZoneRef{
			BuildingId:    ref.BuildingID,
			BuildingLabel: ref.BuildingLabel,
			SiteId:        ref.SiteID,
			SiteLabel:     ref.SiteLabel,
			Zone:          ref.Zone,
		}
	}
	return connect.NewResponse(&dspb.ListRackZoneRefsResponse{
		Zones: zones,
	}), nil
}

func (h *Handler) ListRackTypes(ctx context.Context, r *connect.Request[dspb.ListRackTypesRequest]) (*connect.Response[dspb.ListRackTypesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.svc.ListRackTypes(ctx, &collectionpb.ListRackTypesRequest{})
	if err != nil {
		return nil, err
	}
	types := make([]*dspb.RackType, len(result.RackTypes))
	for i, rt := range result.RackTypes {
		types[i] = &dspb.RackType{
			Rows:      rt.Rows,
			Columns:   rt.Columns,
			RackCount: rt.RackCount,
		}
	}
	return connect.NewResponse(&dspb.ListRackTypesResponse{
		RackTypes: types,
	}), nil
}

func (h *Handler) SaveRack(ctx context.Context, r *connect.Request[dspb.SaveRackRequest]) (*connect.Response[dspb.SaveRackResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// Placing a rack under a site/building is a site-management action —
	// matching the dedicated AssignRacksToSite/Building RPCs (site:manage).
	// A rack:manage-only caller may edit rack contents but not place the rack,
	// so require site:manage when the request carries placement intent (an
	// explicit site_id/building_id, including 0 to unassign). SaveRack can move
	// an existing rack, so for an update authorize its current site as well as
	// the destination; a create has no source. Omitted placement preserves the
	// rack's current site/building and stays rack:manage.
	if ri := r.Msg.RackInfo; ri != nil && (ri.SiteId != nil || ri.BuildingId != nil) {
		var currentSiteID *int64
		if r.Msg.DeviceSetId != nil {
			currentSiteID, err = h.svc.ResolveRackSite(ctx, info.OrganizationID, *r.Msg.DeviceSetId)
			if err != nil {
				return nil, err
			}
		}
		ctx, err = h.authorizeRackPlacement(ctx, info.OrganizationID, currentSiteID, ri.SiteId, ri.BuildingId)
		if err != nil {
			return nil, err
		}
	}
	req := toCollectionSaveRackReq(r.Msg)
	result, err := h.svc.SaveRack(ctx, req, r.Msg.GetForceClearConflictingSite())
	if err != nil {
		return nil, err
	}
	// Site-strip conflicts: the save wrote nothing; return the per-device list
	// so the client can confirm and retry with force_clear_conflicting_site.
	if len(result.Conflicts) > 0 {
		return connect.NewResponse(&dspb.SaveRackResponse{
			Conflicts: toProtoRackConflicts(result.Conflicts),
		}), nil
	}
	return connect.NewResponse(&dspb.SaveRackResponse{
		DeviceSet:           toDeviceSet(result.Collection),
		AssignedCount:       result.AssignedCount,
		SiteReassignedCount: result.SiteReassignedCount,
	}), nil
}

func (h *Handler) CreateRacks(ctx context.Context, r *connect.Request[dspb.CreateRacksRequest]) (*connect.Response[dspb.CreateRacksResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	// Placing racks into a site/building is a site-management action, so a
	// rack:manage-only caller can create unplaced racks but not place them.
	// Every rack here is new (no source site), so authorize the destination:
	// an explicit site_id directly, or a building_id via its parent site.
	if r.Msg.SiteId != nil || r.Msg.BuildingId != nil {
		ctx, err = h.authorizeRackPlacement(ctx, info.OrganizationID, nil /* currentSiteID */, r.Msg.SiteId, r.Msg.BuildingId)
		if err != nil {
			return nil, err
		}
	}
	created, rejected, err := h.svc.CreateRacks(ctx, toCreateRacksParams(r.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	// Collisions: nothing was written. Return the per-row list so the form
	// can mark the offending lines.
	if len(rejected) > 0 {
		return connect.NewResponse(&dspb.CreateRacksResponse{
			Errors: toDeviceSetRackCreateErrors(rejected),
		}), nil
	}
	racks := make([]*dspb.DeviceSet, 0, len(created))
	for _, rack := range created {
		racks = append(racks, toDeviceSet(rack))
	}
	return connect.NewResponse(&dspb.CreateRacksResponse{Racks: racks}), nil
}

func (h *Handler) AssignDevicesToRack(ctx context.Context, r *connect.Request[dspb.AssignDevicesToRackRequest]) (*connect.Response[dspb.AssignDevicesToRackResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	params, err := toAssignDevicesToRackParams(r.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.AssignDevicesToRack(ctx, params)
	if err != nil {
		return nil, err
	}
	// Site-strip conflicts: the batch wrote nothing; return the per-device
	// list so the client can confirm and retry with force.
	if len(result.Conflicts) > 0 {
		return connect.NewResponse(&dspb.AssignDevicesToRackResponse{
			Conflicts: toProtoRackConflicts(result.Conflicts),
		}), nil
	}
	return connect.NewResponse(&dspb.AssignDevicesToRackResponse{
		AssignedCount:       result.AssignedCount,
		RemovedCount:        result.RemovedCount,
		SiteReassignedCount: result.SiteReassignedCount,
	}), nil
}
