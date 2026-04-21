package collection

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/generated/grpc/collection/v1/collectionv1connect"
	"github.com/block/proto-fleet/server/internal/domain/collection"
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

// CreateCollection creates a new collection.
func (h *Handler) CreateCollection(ctx context.Context, r *connect.Request[pb.CreateCollectionRequest]) (*connect.Response[pb.CreateCollectionResponse], error) {
	result, err := h.collectionSvc.CreateCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetCollection retrieves a collection by ID.
func (h *Handler) GetCollection(ctx context.Context, r *connect.Request[pb.GetCollectionRequest]) (*connect.Response[pb.GetCollectionResponse], error) {
	result, err := h.collectionSvc.GetCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// UpdateCollection updates a collection's label and/or description.
func (h *Handler) UpdateCollection(ctx context.Context, r *connect.Request[pb.UpdateCollectionRequest]) (*connect.Response[pb.UpdateCollectionResponse], error) {
	result, err := h.collectionSvc.UpdateCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// DeleteCollection soft-deletes a collection.
func (h *Handler) DeleteCollection(ctx context.Context, r *connect.Request[pb.DeleteCollectionRequest]) (*connect.Response[pb.DeleteCollectionResponse], error) {
	result, err := h.collectionSvc.DeleteCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListCollections returns all collections for the organization.
func (h *Handler) ListCollections(ctx context.Context, r *connect.Request[pb.ListCollectionsRequest]) (*connect.Response[pb.ListCollectionsResponse], error) {
	result, err := h.collectionSvc.ListCollections(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// AddDevicesToCollection adds devices to a collection.
func (h *Handler) AddDevicesToCollection(ctx context.Context, r *connect.Request[pb.AddDevicesToCollectionRequest]) (*connect.Response[pb.AddDevicesToCollectionResponse], error) {
	result, err := h.collectionSvc.AddDevicesToCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// RemoveDevicesFromCollection removes devices from a collection.
func (h *Handler) RemoveDevicesFromCollection(ctx context.Context, r *connect.Request[pb.RemoveDevicesFromCollectionRequest]) (*connect.Response[pb.RemoveDevicesFromCollectionResponse], error) {
	result, err := h.collectionSvc.RemoveDevicesFromCollection(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListCollectionMembers returns all members of a collection.
func (h *Handler) ListCollectionMembers(ctx context.Context, r *connect.Request[pb.ListCollectionMembersRequest]) (*connect.Response[pb.ListCollectionMembersResponse], error) {
	result, err := h.collectionSvc.ListCollectionMembers(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetDeviceCollections returns all collections a device belongs to.
func (h *Handler) GetDeviceCollections(ctx context.Context, r *connect.Request[pb.GetDeviceCollectionsRequest]) (*connect.Response[pb.GetDeviceCollectionsResponse], error) {
	result, err := h.collectionSvc.GetDeviceCollections(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// SetRackSlotPosition sets a device's slot position within a rack.
func (h *Handler) SetRackSlotPosition(ctx context.Context, r *connect.Request[pb.SetRackSlotPositionRequest]) (*connect.Response[pb.SetRackSlotPositionResponse], error) {
	result, err := h.collectionSvc.SetRackSlotPosition(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ClearRackSlotPosition clears a device's slot position within a rack.
func (h *Handler) ClearRackSlotPosition(ctx context.Context, r *connect.Request[pb.ClearRackSlotPositionRequest]) (*connect.Response[pb.ClearRackSlotPositionResponse], error) {
	result, err := h.collectionSvc.ClearRackSlotPosition(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetRackSlots lists all occupied slot positions in a rack.
func (h *Handler) GetRackSlots(ctx context.Context, r *connect.Request[pb.GetRackSlotsRequest]) (*connect.Response[pb.GetRackSlotsResponse], error) {
	result, err := h.collectionSvc.GetRackSlots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// GetCollectionStats returns aggregated telemetry stats for collections.
func (h *Handler) GetCollectionStats(ctx context.Context, r *connect.Request[pb.GetCollectionStatsRequest]) (*connect.Response[pb.GetCollectionStatsResponse], error) {
	result, err := h.collectionSvc.GetCollectionStats(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListRackTypes returns all distinct rack types for the organization.
func (h *Handler) ListRackTypes(ctx context.Context, r *connect.Request[pb.ListRackTypesRequest]) (*connect.Response[pb.ListRackTypesResponse], error) {
	result, err := h.collectionSvc.ListRackTypes(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// ListRackZones returns all distinct rack zones for the organization.
func (h *Handler) ListRackZones(ctx context.Context, r *connect.Request[pb.ListRackZonesRequest]) (*connect.Response[pb.ListRackZonesResponse], error) {
	result, err := h.collectionSvc.ListRackZones(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

// SaveRack atomically creates or updates a rack with membership and slot assignments.
func (h *Handler) SaveRack(ctx context.Context, r *connect.Request[pb.SaveRackRequest]) (*connect.Response[pb.SaveRackResponse], error) {
	result, err := h.collectionSvc.SaveRack(ctx, r.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
