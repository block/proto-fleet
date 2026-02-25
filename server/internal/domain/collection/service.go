package collection

import (
	"context"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	defaultPageSize int32 = 50
	maxPageSize     int32 = 1000
)

// DeviceIdentifierResolver resolves a DeviceSelector into device identifiers for an org.
type DeviceIdentifierResolver func(ctx context.Context, selector *commonpb.DeviceSelector, orgID int64) ([]string, error)

// Service provides business logic for device collections (groups).
type Service struct {
	collectionStore          interfaces.CollectionStore
	transactor               interfaces.Transactor
	resolveDeviceIdentifiers DeviceIdentifierResolver
}

// NewService creates a new collection service.
func NewService(
	collectionStore interfaces.CollectionStore,
	transactor interfaces.Transactor,
	resolveDeviceIdentifiers DeviceIdentifierResolver,
) *Service {
	return &Service{
		collectionStore:          collectionStore,
		transactor:               transactor,
		resolveDeviceIdentifiers: resolveDeviceIdentifiers,
	}
}

// CreateCollection creates a new collection.
func (s *Service) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	rackInfo := req.GetRackInfo()
	if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK && rackInfo == nil {
		return nil, fleeterror.NewInvalidArgumentError("rack_info is required for rack collections")
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		collection, err := s.collectionStore.CreateCollection(ctx, info.OrganizationID, req.Type, req.Label, req.Description)
		if err != nil {
			return nil, err
		}

		if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
			err = s.collectionStore.CreateRackExtension(ctx, collection.Id, rackInfo.GetLocation(), rackInfo.Rows, rackInfo.Columns)
			if err != nil {
				return nil, err
			}
			collection.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: rackInfo}
		}

		return collection, nil
	})
	if err != nil {
		return nil, err
	}

	collection, ok := result.(*pb.DeviceCollection)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	return &pb.CreateCollectionResponse{Collection: collection}, nil
}

// GetCollection retrieves a collection by ID.
func (s *Service) GetCollection(ctx context.Context, req *pb.GetCollectionRequest) (*pb.GetCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	collection, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)
	if err != nil {
		return nil, err
	}

	return &pb.GetCollectionResponse{Collection: collection}, nil
}

// UpdateCollection updates a collection's label and/or description.
func (s *Service) UpdateCollection(ctx context.Context, req *pb.UpdateCollectionRequest) (*pb.UpdateCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		var label, description *string
		if req.Label != nil {
			label = req.Label
		}
		if req.Description != nil {
			description = req.Description
		}

		err := s.collectionStore.UpdateCollection(ctx, info.OrganizationID, req.CollectionId, label, description)
		if err != nil {
			return nil, err
		}

		collection, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return nil, err
		}

		return collection, nil
	})
	if err != nil {
		return nil, err
	}

	collection, ok := result.(*pb.DeviceCollection)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	return &pb.UpdateCollectionResponse{Collection: collection}, nil
}

// DeleteCollection soft-deletes a collection.
func (s *Service) DeleteCollection(ctx context.Context, req *pb.DeleteCollectionRequest) (*pb.DeleteCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		rowsAffected, err := s.collectionStore.SoftDeleteCollection(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return fleeterror.NewNotFoundErrorf("collection not found: %d", req.CollectionId)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &pb.DeleteCollectionResponse{}, nil
}

func validatePageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return defaultPageSize
	}
	if pageSize > maxPageSize {
		return maxPageSize
	}
	return pageSize
}

// ListCollections returns a paginated list of collections for the organization.
func (s *Service) ListCollections(ctx context.Context, req *pb.ListCollectionsRequest) (*pb.ListCollectionsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	pageSize := validatePageSize(req.PageSize)

	collections, nextPageToken, err := s.collectionStore.ListCollections(ctx, info.OrganizationID, req.Type, pageSize, req.PageToken)
	if err != nil {
		return nil, err
	}

	return &pb.ListCollectionsResponse{Collections: collections, NextPageToken: nextPageToken}, nil
}

// AddDevicesToCollection adds devices to a collection.
func (s *Service) AddDevicesToCollection(ctx context.Context, req *pb.AddDevicesToCollectionRequest) (*pb.AddDevicesToCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.resolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		belongs, err := s.collectionStore.CollectionBelongsToOrg(ctx, req.CollectionId, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		if !belongs {
			return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", req.CollectionId)
		}

		addedCount, err := s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
		if err != nil {
			return nil, err
		}

		return addedCount, nil
	})
	if err != nil {
		return nil, err
	}

	addedCount, ok := result.(int64)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	// #nosec G115 -- addedCount is bounded by request size which is limited by gRPC message size
	return &pb.AddDevicesToCollectionResponse{CollectionId: req.CollectionId, AddedCount: int32(addedCount)}, nil
}

// RemoveDevicesFromCollection removes devices from a collection.
func (s *Service) RemoveDevicesFromCollection(ctx context.Context, req *pb.RemoveDevicesFromCollectionRequest) (*pb.RemoveDevicesFromCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.resolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		belongs, err := s.collectionStore.CollectionBelongsToOrg(ctx, req.CollectionId, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		if !belongs {
			return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", req.CollectionId)
		}

		removedCount, err := s.collectionStore.RemoveDevicesFromCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
		if err != nil {
			return nil, err
		}

		return removedCount, nil
	})
	if err != nil {
		return nil, err
	}

	removedCount, ok := result.(int64)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	// #nosec G115 -- removedCount is bounded by request size which is limited by gRPC message size
	return &pb.RemoveDevicesFromCollectionResponse{RemovedCount: int32(removedCount)}, nil
}

// ListCollectionMembers returns all members of a collection.
func (s *Service) ListCollectionMembers(ctx context.Context, req *pb.ListCollectionMembersRequest) (*pb.ListCollectionMembersResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Verify collection exists and belongs to org
	belongs, err := s.collectionStore.CollectionBelongsToOrg(ctx, req.CollectionId, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", req.CollectionId)
	}

	pageSize := validatePageSize(req.PageSize)

	members, nextPageToken, err := s.collectionStore.ListCollectionMembers(ctx, info.OrganizationID, req.CollectionId, pageSize, req.PageToken)
	if err != nil {
		return nil, err
	}

	return &pb.ListCollectionMembersResponse{Members: members, NextPageToken: nextPageToken}, nil
}

// GetDeviceCollections returns all collections a device belongs to.
func (s *Service) GetDeviceCollections(ctx context.Context, req *pb.GetDeviceCollectionsRequest) (*pb.GetDeviceCollectionsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	collections, err := s.collectionStore.GetDeviceCollections(ctx, info.OrganizationID, req.DeviceIdentifier, req.Type)
	if err != nil {
		return nil, err
	}

	return &pb.GetDeviceCollectionsResponse{Collections: collections}, nil
}

// SetRackSlotPosition sets a device's slot position within a rack.
func (s *Service) SetRackSlotPosition(ctx context.Context, req *pb.SetRackSlotPositionRequest) (*pb.SetRackSlotPositionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if req.Position == nil {
		return nil, fleeterror.NewInvalidArgumentError("position is required")
	}

	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		collectionType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return err
		}
		if collectionType != pb.CollectionType_COLLECTION_TYPE_RACK {
			return fleeterror.NewInvalidArgumentError("slot positions can only be set on rack collections")
		}

		// Device membership is enforced by the store query joining on device_collection_membership.
		return s.collectionStore.SetRackSlotPosition(ctx, req.CollectionId, req.DeviceIdentifier, req.Position.Row, req.Position.Column)
	})
	if err != nil {
		return nil, err
	}

	return &pb.SetRackSlotPositionResponse{
		CollectionId: req.CollectionId,
		Slot: &pb.RackSlot{
			DeviceIdentifier: req.DeviceIdentifier,
			Position:         req.Position,
		},
	}, nil
}

// ClearRackSlotPosition clears a device's slot position within a rack.
func (s *Service) ClearRackSlotPosition(ctx context.Context, req *pb.ClearRackSlotPositionRequest) (*pb.ClearRackSlotPositionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		collectionType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return err
		}
		if collectionType != pb.CollectionType_COLLECTION_TYPE_RACK {
			return fleeterror.NewInvalidArgumentError("slot positions can only be cleared on rack collections")
		}
		return s.collectionStore.ClearRackSlotPosition(ctx, req.CollectionId, req.DeviceIdentifier)
	})
	if err != nil {
		return nil, err
	}

	return &pb.ClearRackSlotPositionResponse{}, nil
}

// GetRackSlots lists all occupied slot positions in a rack.
func (s *Service) GetRackSlots(ctx context.Context, req *pb.GetRackSlotsRequest) (*pb.GetRackSlotsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	collectionType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, req.CollectionId)
	if err != nil {
		return nil, err
	}
	if collectionType != pb.CollectionType_COLLECTION_TYPE_RACK {
		return nil, fleeterror.NewInvalidArgumentError("slot positions can only be retrieved from rack collections")
	}

	slots, err := s.collectionStore.GetRackSlots(ctx, req.CollectionId)
	if err != nil {
		return nil, err
	}

	return &pb.GetRackSlotsResponse{Slots: slots}, nil
}
