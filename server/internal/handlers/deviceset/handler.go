package deviceset

import (
	"context"

	"connectrpc.com/connect"
	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	dspb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	"github.com/block/proto-fleet/server/generated/grpc/device_set/v1/device_setv1connect"
	"github.com/block/proto-fleet/server/internal/domain/collection"
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
	_, err := h.svc.DeleteCollection(ctx, &collectionpb.DeleteCollectionRequest{
		CollectionId: r.Msg.DeviceSetId,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.DeleteDeviceSetResponse{}), nil
}

func (h *Handler) ListDeviceSets(ctx context.Context, r *connect.Request[dspb.ListDeviceSetsRequest]) (*connect.Response[dspb.ListDeviceSetsResponse], error) {
	req := toCollectionListReq(r.Msg)
	result, err := h.svc.ListCollections(ctx, req)
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

func (h *Handler) AddDevicesToDeviceSet(ctx context.Context, r *connect.Request[dspb.AddDevicesToDeviceSetRequest]) (*connect.Response[dspb.AddDevicesToDeviceSetResponse], error) {
	result, err := h.svc.AddDevicesToCollection(ctx, &collectionpb.AddDevicesToCollectionRequest{
		CollectionId:   r.Msg.DeviceSetId,
		DeviceSelector: r.Msg.DeviceSelector,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.AddDevicesToDeviceSetResponse{
		DeviceSetId: result.CollectionId,
		AddedCount:  result.AddedCount,
	}), nil
}

func (h *Handler) RemoveDevicesFromDeviceSet(ctx context.Context, r *connect.Request[dspb.RemoveDevicesFromDeviceSetRequest]) (*connect.Response[dspb.RemoveDevicesFromDeviceSetResponse], error) {
	result, err := h.svc.RemoveDevicesFromCollection(ctx, &collectionpb.RemoveDevicesFromCollectionRequest{
		CollectionId:   r.Msg.DeviceSetId,
		DeviceSelector: r.Msg.DeviceSelector,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.RemoveDevicesFromDeviceSetResponse{
		RemovedCount: result.RemovedCount,
	}), nil
}

func (h *Handler) ListDeviceSetMembers(ctx context.Context, r *connect.Request[dspb.ListDeviceSetMembersRequest]) (*connect.Response[dspb.ListDeviceSetMembersResponse], error) {
	result, err := h.svc.ListCollectionMembers(ctx, &collectionpb.ListCollectionMembersRequest{
		CollectionId: r.Msg.DeviceSetId,
		PageSize:     r.Msg.PageSize,
		PageToken:    r.Msg.PageToken,
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

func (h *Handler) GetDeviceDeviceSets(ctx context.Context, r *connect.Request[dspb.GetDeviceDeviceSetsRequest]) (*connect.Response[dspb.GetDeviceDeviceSetsResponse], error) {
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
	result, err := h.svc.ListRackZones(ctx, &collectionpb.ListRackZonesRequest{})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.ListRackZonesResponse{
		Zones: result.Zones,
	}), nil
}

func (h *Handler) ListRackTypes(ctx context.Context, r *connect.Request[dspb.ListRackTypesRequest]) (*connect.Response[dspb.ListRackTypesResponse], error) {
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
	req := toCollectionSaveRackReq(r.Msg)
	result, err := h.svc.SaveRack(ctx, req)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&dspb.SaveRackResponse{
		DeviceSet:     toDeviceSet(result.Collection),
		AssignedCount: result.AssignedCount,
	}), nil
}
