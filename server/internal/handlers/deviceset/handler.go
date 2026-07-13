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

func (h *Handler) CreateDeviceSet(ctx context.Context, r *connect.Request[dspb.CreateDeviceSetRequest]) (*connect.Response[dspb.CreateDeviceSetResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	// Creating a rack under a site/building persists that placement (and can
	// cascade added devices to it), so mirror the UpdateDeviceSet/SaveRack gate:
	// require site:manage when rack_info carries explicit placement. Without
	// this, a rack:manage-only caller could place a rack via the create path.
	if ri, ok := r.Msg.TypeDetails.(*dspb.CreateDeviceSetRequest_RackInfo); ok && ri.RackInfo != nil && (ri.RackInfo.SiteId != nil || ri.RackInfo.BuildingId != nil) {
		if _, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{}); err != nil {
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
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	// A rack's placement is now persisted here too (zone/dims + site/building
	// in one settings save). Placing a rack under a site/building is a
	// site-management action, so — mirroring SaveRack — require site:manage
	// when the request carries explicit placement intent (site_id/building_id,
	// including 0 to unassign). Metadata-only edits (label/zone/dims, or a
	// membership change) stay rack:manage.
	if ri, ok := r.Msg.TypeDetails.(*dspb.UpdateDeviceSetRequest_RackInfo); ok && ri.RackInfo != nil && (ri.RackInfo.SiteId != nil || ri.RackInfo.BuildingId != nil) {
		if _, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{}); err != nil {
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
	if _, err := middleware.RequirePermission(ctx, authz.PermRackManage, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	// Placing a rack under a site/building is a site-management action —
	// matching the dedicated AssignRacksToSite/Building RPCs (site:manage).
	// A rack:manage-only caller may edit rack contents but not place the rack,
	// so require site:manage when the request carries placement intent (an
	// explicit site_id/building_id, including 0 to unassign). Omitted placement
	// preserves the rack's current site/building and stays rack:manage.
	if ri := r.Msg.RackInfo; ri != nil && (ri.SiteId != nil || ri.BuildingId != nil) {
		if _, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{}); err != nil {
			return nil, err
		}
	}
	req := toCollectionSaveRackReq(r.Msg)
	result, err := h.svc.SaveRack(ctx, req)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.SaveRackResponse{
		DeviceSet:           toDeviceSet(result.Collection),
		AssignedCount:       result.AssignedCount,
		SiteReassignedCount: result.SiteReassignedCount,
	}), nil
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
