package collection

import (
	"context"
	"fmt"
	"math"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

const (
	defaultPageSize  int32 = 50
	maxPageSize      int32 = 1000
	maxRackDimension int32 = 12
)

const (
	hashToTeraHashConversion                   = 1e12
	wattsToKilowattsConversion                 = 1000.0
	joulesPerHashToJoulesPerTeraHashMultiplier = 1e12
)

// TelemetryCollector fetches latest device metrics for telemetry aggregation.
type TelemetryCollector interface {
	GetLatestDeviceMetrics(ctx context.Context, deviceIDs []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error)
}

// DeviceQueryer provides device-level queries needed by collection stats.
type DeviceQueryer interface {
	GetDeviceIdentifiersByOrgWithFilter(ctx context.Context, orgID int64, filter *interfaces.MinerFilter) ([]string, error)
	GetMinerStateCountsByCollections(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]interfaces.MinerStateCounts, error)
	GetComponentErrorCountsByCollections(ctx context.Context, orgID int64, collectionIDs []int64) ([]interfaces.ComponentErrorCount, error)
}

// DeviceIdentifierResolver resolves a DeviceSelector into device identifiers for an org.
type DeviceIdentifierResolver func(ctx context.Context, selector *commonpb.DeviceSelector, orgID int64) ([]string, error)

// Service provides business logic for device collections (groups).
type Service struct {
	collectionStore          interfaces.CollectionStore
	deviceQueryer            DeviceQueryer
	transactor               interfaces.Transactor
	resolveDeviceIdentifiers DeviceIdentifierResolver
	telemetry                TelemetryCollector
	activitySvc              *activity.Service
}

// NewService creates a new collection service.
func NewService(
	collectionStore interfaces.CollectionStore,
	deviceQueryer DeviceQueryer,
	transactor interfaces.Transactor,
	resolveDeviceIdentifiers DeviceIdentifierResolver,
	telemetry TelemetryCollector,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		collectionStore:          collectionStore,
		deviceQueryer:            deviceQueryer,
		transactor:               transactor,
		resolveDeviceIdentifiers: resolveDeviceIdentifiers,
		telemetry:                telemetry,
		activitySvc:              activitySvc,
	}
}

func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc != nil {
		s.activitySvc.Log(ctx, event)
	}
}

func collectionScopeType(collType pb.CollectionType) string {
	if collType == pb.CollectionType_COLLECTION_TYPE_RACK {
		return "rack"
	}
	return "group"
}

// createCollectionResult holds the result of the CreateCollection transaction.
type createCollectionResult struct {
	collection *pb.DeviceCollection
	addedCount int64
}

// CreateCollection creates a new collection, optionally adding devices atomically.
func (s *Service) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	rackInfo := req.GetRackInfo()
	if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK && rackInfo == nil {
		return nil, fleeterror.NewInvalidArgumentError("rack_info is required for rack collections")
	}
	if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK && rackInfo != nil && rackInfo.GetZone() == "" {
		return nil, fleeterror.NewInvalidArgumentError("zone is required for rack collections")
	}
	if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK && rackInfo != nil {
		if rackInfo.Rows < 1 || rackInfo.Rows > maxRackDimension {
			return nil, fleeterror.NewInvalidArgumentErrorf("rows must be between 1 and %d", maxRackDimension)
		}
		if rackInfo.Columns < 1 || rackInfo.Columns > maxRackDimension {
			return nil, fleeterror.NewInvalidArgumentErrorf("columns must be between 1 and %d", maxRackDimension)
		}
		if rackInfo.OrderIndex == pb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED {
			return nil, fleeterror.NewInvalidArgumentError("order_index is required for rack collections")
		}
		if _, ok := pb.RackOrderIndex_name[int32(rackInfo.OrderIndex)]; !ok {
			return nil, fleeterror.NewInvalidArgumentError("invalid order_index value")
		}
		if rackInfo.CoolingType == pb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED {
			return nil, fleeterror.NewInvalidArgumentError("cooling_type is required for rack collections")
		}
		if _, ok := pb.RackCoolingType_name[int32(rackInfo.CoolingType)]; !ok {
			return nil, fleeterror.NewInvalidArgumentError("invalid cooling_type value")
		}
	}

	// Resolve device identifiers outside the transaction if device_selector is provided.
	var deviceIdentifiers []string
	if req.DeviceSelector != nil {
		deviceIdentifiers, err = s.resolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
		if err != nil {
			return nil, err
		}
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		collection, err := s.collectionStore.CreateCollection(ctx, info.OrganizationID, req.Type, req.Label, req.Description)
		if err != nil {
			return nil, err
		}

		if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
			err = s.collectionStore.CreateRackExtension(ctx, collection.Id, rackInfo.GetZone(), rackInfo.Rows, rackInfo.Columns, int32(rackInfo.OrderIndex), int32(rackInfo.CoolingType), info.OrganizationID)
			if err != nil {
				return nil, err
			}
			collection.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: rackInfo}
		}

		// Add devices to the collection if device_selector was provided.
		var addedCount int64
		if len(deviceIdentifiers) > 0 {
			addedCount, err = s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, collection.Id, deviceIdentifiers)
			if err != nil {
				return nil, err
			}
			// Update device count to reflect added devices.
			// #nosec G115 -- addedCount bounded by request size which is limited by gRPC message size
			collection.DeviceCount = int32(addedCount)
		}

		return &createCollectionResult{collection: collection, addedCount: addedCount}, nil
	})
	if err != nil {
		return nil, err
	}

	txResult, ok := result.(*createCollectionResult)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	scopeType := collectionScopeType(req.Type)
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "create_collection",
		Description:    fmt.Sprintf("Create %s: %s", scopeType, req.Label),
		ScopeType:      &scopeType,
		ScopeLabel:     &req.Label,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	// #nosec G115 -- addedCount bounded by request size which is limited by gRPC message size
	return &pb.CreateCollectionResponse{Collection: txResult.collection, AddedCount: int32(txResult.addedCount)}, nil
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

	if collection.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
		rackInfo, err := s.collectionStore.GetRackInfo(ctx, collection.Id, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		if rackInfo != nil {
			collection.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: rackInfo}
		}
	}

	return &pb.GetCollectionResponse{Collection: collection}, nil
}

// UpdateCollection updates a collection's label, description, and/or membership.
func (s *Service) UpdateCollection(ctx context.Context, req *pb.UpdateCollectionRequest) (*pb.UpdateCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Resolve device identifiers outside the transaction if device_selector is provided.
	var deviceIdentifiers []string
	hasDeviceSelector := req.DeviceSelector != nil
	if hasDeviceSelector {
		deviceIdentifiers, err = s.resolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
		if err != nil {
			return nil, err
		}
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

		// Replace membership atomically if device_selector was provided.
		if hasDeviceSelector {
			_, err = s.collectionStore.RemoveAllDevicesFromCollection(ctx, info.OrganizationID, req.CollectionId)
			if err != nil {
				return nil, err
			}

			if len(deviceIdentifiers) > 0 {
				_, err = s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
				if err != nil {
					return nil, err
				}
			}
		}

		collection, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return nil, err
		}

		if collection.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
			rackInfo, err := s.collectionStore.GetRackInfo(ctx, collection.Id, info.OrganizationID)
			if err != nil {
				return nil, err
			}
			if rackInfo != nil {
				collection.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: rackInfo}
			}
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

	scopeType := collectionScopeType(collection.Type)
	label := collection.Label
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "update_collection",
		Description:    fmt.Sprintf("Update %s: %s", scopeType, label),
		ScopeType:      &scopeType,
		ScopeLabel:     &label,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	return &pb.UpdateCollectionResponse{Collection: collection}, nil
}

// DeleteCollection soft-deletes a collection.
func (s *Service) DeleteCollection(ctx context.Context, req *pb.DeleteCollectionRequest) (*pb.DeleteCollectionResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	collection, prefetchErr := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)

	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Remove memberships first so the idx_one_rack_per_device unique index
		// doesn't prevent the device from being added to another rack after soft-delete.
		if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, info.OrganizationID, req.CollectionId); err != nil {
			return err
		}
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

	if prefetchErr == nil {
		scopeType := collectionScopeType(collection.Type)
		label := collection.Label
		s.logActivity(ctx, activitymodels.Event{
			Category:       activitymodels.CategoryCollection,
			Type:           "delete_collection",
			Description:    fmt.Sprintf("Delete %s: %s", scopeType, label),
			ScopeType:      &scopeType,
			ScopeLabel:     &label,
			UserID:         &info.ExternalUserID,
			Username:       &info.Username,
			OrganizationID: &info.OrganizationID,
		})
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

	var sort *interfaces.SortConfig
	if req.Sort != nil {
		sort = &interfaces.SortConfig{
			Field:     interfaces.SortField(req.Sort.Field),
			Direction: interfaces.SortDirection(req.Sort.Direction),
		}
	}

	errorComponentTypes := make([]int32, len(req.ErrorComponentTypes))
	for i, ct := range req.ErrorComponentTypes {
		errorComponentTypes[i] = int32(ct)
	}

	// Validate that zone filter and zone sort are only used with rack collections
	isZoneSort := sort != nil && sort.Field == interfaces.SortFieldLocation
	if (len(req.Zones) > 0 || isZoneSort) && req.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
		return nil, fleeterror.NewInvalidArgumentErrorf("zone filter and sort are only supported for rack collections")
	}

	collections, nextPageToken, totalCount, err := s.collectionStore.ListCollections(ctx, info.OrganizationID, req.Type, pageSize, req.PageToken, sort, errorComponentTypes, req.Zones)
	if err != nil {
		return nil, err
	}

	return &pb.ListCollectionsResponse{Collections: collections, NextPageToken: nextPageToken, TotalCount: totalCount}, nil
}

type membershipChangeResult struct {
	collection *pb.DeviceCollection
	count      int64
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
		coll, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return nil, err
		}

		addedCount, err := s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
		if err != nil {
			return nil, err
		}

		return &membershipChangeResult{collection: coll, count: addedCount}, nil
	})
	if err != nil {
		return nil, err
	}

	txResult, ok := result.(*membershipChangeResult)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	addedCountInt := int(txResult.count)
	scopeType := collectionScopeType(txResult.collection.Type)
	label := txResult.collection.Label
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "add_devices",
		Description:    fmt.Sprintf("Add devices to %s: %s", scopeType, label),
		ScopeType:      &scopeType,
		ScopeLabel:     &label,
		ScopeCount:     &addedCountInt,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	// #nosec G115 -- addedCount is bounded by request size which is limited by gRPC message size
	return &pb.AddDevicesToCollectionResponse{CollectionId: req.CollectionId, AddedCount: int32(txResult.count)}, nil
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
		coll, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return nil, err
		}

		removedCount, err := s.collectionStore.RemoveDevicesFromCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
		if err != nil {
			return nil, err
		}

		return &membershipChangeResult{collection: coll, count: removedCount}, nil
	})
	if err != nil {
		return nil, err
	}

	txResult, ok := result.(*membershipChangeResult)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	removedCountInt := int(txResult.count)
	scopeType := collectionScopeType(txResult.collection.Type)
	label := txResult.collection.Label
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "remove_devices",
		Description:    fmt.Sprintf("Remove devices from %s: %s", scopeType, label),
		ScopeType:      &scopeType,
		ScopeLabel:     &label,
		ScopeCount:     &removedCountInt,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	// #nosec G115 -- removedCount is bounded by request size which is limited by gRPC message size
	return &pb.RemoveDevicesFromCollectionResponse{RemovedCount: int32(txResult.count)}, nil
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

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		coll, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return nil, err
		}
		if coll.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
			return nil, fleeterror.NewInvalidArgumentError("slot positions can only be set on rack collections")
		}

		// Device membership is enforced by the store query joining on device_set_membership.
		if err := s.collectionStore.SetRackSlotPosition(ctx, req.CollectionId, req.DeviceIdentifier, req.Position.Row, req.Position.Column, info.OrganizationID); err != nil {
			return nil, err
		}

		return coll, nil
	})
	if err != nil {
		return nil, err
	}

	coll, ok := result.(*pb.DeviceCollection)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	scopeType := "rack"
	label := coll.Label
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "set_rack_slot",
		Description:    "Set rack slot position",
		ScopeType:      &scopeType,
		ScopeLabel:     &label,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

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

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		coll, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return nil, err
		}
		if coll.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
			return nil, fleeterror.NewInvalidArgumentError("slot positions can only be cleared on rack collections")
		}
		if err := s.collectionStore.ClearRackSlotPosition(ctx, req.CollectionId, req.DeviceIdentifier, info.OrganizationID); err != nil {
			return nil, err
		}
		return coll, nil
	})
	if err != nil {
		return nil, err
	}

	coll, ok := result.(*pb.DeviceCollection)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	scopeType := "rack"
	label := coll.Label
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "clear_rack_slot",
		Description:    "Clear rack slot position",
		ScopeType:      &scopeType,
		ScopeLabel:     &label,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

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

	slots, err := s.collectionStore.GetRackSlots(ctx, req.CollectionId, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	return &pb.GetRackSlotsResponse{Slots: slots}, nil
}

// GetCollectionStats returns aggregated telemetry stats for a list of collections.
func (s *Service) GetCollectionStats(ctx context.Context, req *pb.GetCollectionStatsRequest) (*pb.GetCollectionStatsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if len(req.CollectionIds) == 0 {
		return &pb.GetCollectionStatsResponse{}, nil
	}

	// Batch-fetch collection types to avoid per-ID lookups.
	collectionTypes, err := s.collectionStore.GetCollectionTypes(ctx, info.OrganizationID, req.CollectionIds)
	if err != nil {
		return nil, err
	}

	// Get device identifiers per collection using existing device store filter.
	devicesByCollection := make(map[int64][]string, len(req.CollectionIds))
	uniqueDeviceIDs := make(map[string]struct{})
	for _, collectionID := range req.CollectionIds {
		collectionType, ok := collectionTypes[collectionID]
		if !ok {
			// Collection was deleted between list and stats call; skip it.
			continue
		}
		filter := &interfaces.MinerFilter{
			PairingStatuses: []fm.PairingStatus{
				fm.PairingStatus_PAIRING_STATUS_PAIRED,
				fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			},
		}
		if collectionType == pb.CollectionType_COLLECTION_TYPE_RACK {
			filter.RackIDs = []int64{collectionID}
		} else {
			filter.GroupIDs = []int64{collectionID}
		}
		ids, err := s.deviceQueryer.GetDeviceIdentifiersByOrgWithFilter(ctx, info.OrganizationID, filter)
		if err != nil {
			return nil, err
		}
		devicesByCollection[collectionID] = ids
		for _, id := range ids {
			uniqueDeviceIDs[id] = struct{}{}
		}
	}

	// Batch-fetch telemetry for all devices
	var telemetryData map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics
	if len(uniqueDeviceIDs) > 0 && s.telemetry != nil {
		deviceIDs := make([]minerModels.DeviceIdentifier, 0, len(uniqueDeviceIDs))
		for id := range uniqueDeviceIDs {
			deviceIDs = append(deviceIDs, minerModels.DeviceIdentifier(id))
		}

		telemetryData, err = s.telemetry.GetLatestDeviceMetrics(ctx, deviceIDs)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to fetch telemetry: %v", err)
		}
	}

	// Fetch miner state counts per collection using device store
	stateCounts, err := s.deviceQueryer.GetMinerStateCountsByCollections(ctx, info.OrganizationID, req.CollectionIds)
	if err != nil {
		return nil, err
	}

	// Fetch component error counts per collection
	componentErrors, err := s.deviceQueryer.GetComponentErrorCountsByCollections(ctx, info.OrganizationID, req.CollectionIds)
	if err != nil {
		return nil, err
	}
	// Build a map of (collectionID, componentType) -> deviceCount
	type componentKey struct {
		collectionID  int64
		componentType int32
	}
	componentErrorMap := make(map[componentKey]int32, len(componentErrors))
	for _, ce := range componentErrors {
		componentErrorMap[componentKey{ce.CollectionID, ce.ComponentType}] = ce.DeviceCount
	}

	// Fetch per-slot device statuses for rack-type collections
	rackCollectionIDs := make([]int64, 0)
	for _, id := range req.CollectionIds {
		if ct, ok := collectionTypes[id]; ok && ct == pb.CollectionType_COLLECTION_TYPE_RACK {
			rackCollectionIDs = append(rackCollectionIDs, id)
		}
	}
	var slotStatusesByCollection map[int64][]*pb.RackSlotStatus
	if len(rackCollectionIDs) > 0 {
		slotStatusesByCollection, err = s.collectionStore.GetRackSlotStatuses(ctx, info.OrganizationID, rackCollectionIDs)
		if err != nil {
			return nil, err
		}
	}

	// Aggregate per collection
	stats := make([]*pb.CollectionStats, 0, len(req.CollectionIds))
	for _, collectionID := range req.CollectionIds {
		deviceIDs := devicesByCollection[collectionID]
		counts := stateCounts[collectionID]
		// #nosec G115 -- len(deviceIDs) bounded by org device count which fits in int32
		cs := &pb.CollectionStats{
			CollectionId:  collectionID,
			DeviceCount:   int32(len(deviceIDs)),
			HashingCount:  counts.HashingCount,
			BrokenCount:   counts.BrokenCount,
			OfflineCount:  counts.OfflineCount,
			SleepingCount: counts.SleepingCount,
		}

		var (
			reportingCount    int32
			hashrateReporting int32
			powerReporting    int32
			efficiencyN       int32
			tempReporting     int32
			totalHashrate     float64
			totalPower        float64
			efficiencySum     float64
			minTemp           = math.MaxFloat64
			maxTemp           = -math.MaxFloat64
		)

		for _, devID := range deviceIDs {
			metrics, ok := telemetryData[minerModels.DeviceIdentifier(devID)]
			if !ok {
				continue
			}
			reportingCount++

			if metrics.HashrateHS != nil {
				totalHashrate += metrics.HashrateHS.Value
				hashrateReporting++
			}
			if metrics.PowerW != nil {
				totalPower += metrics.PowerW.Value
				powerReporting++
			}
			if metrics.EfficiencyJH != nil {
				efficiencySum += metrics.EfficiencyJH.Value
				efficiencyN++
			}
			if metrics.TempC != nil {
				if metrics.TempC.Value < minTemp {
					minTemp = metrics.TempC.Value
				}
				if metrics.TempC.Value > maxTemp {
					maxTemp = metrics.TempC.Value
				}
				tempReporting++
			}
		}

		cs.ReportingCount = reportingCount
		cs.HashrateReportingCount = hashrateReporting
		cs.PowerReportingCount = powerReporting
		cs.EfficiencyReportingCount = efficiencyN
		cs.TemperatureReportingCount = tempReporting
		if reportingCount > 0 {
			cs.TotalHashrateThs = totalHashrate / hashToTeraHashConversion
			cs.TotalPowerKw = totalPower / wattsToKilowattsConversion
			if efficiencyN > 0 {
				cs.AvgEfficiencyJth = (efficiencySum / float64(efficiencyN)) * joulesPerHashToJoulesPerTeraHashMultiplier
			}
			if minTemp != math.MaxFloat64 {
				cs.MinTemperatureC = minTemp
			}
			if maxTemp != -math.MaxFloat64 {
				cs.MaxTemperatureC = maxTemp
			}
		}

		// Populate component issue counts
		cs.ControlBoardIssueCount = componentErrorMap[componentKey{collectionID, 4}]
		cs.FanIssueCount = componentErrorMap[componentKey{collectionID, 3}]
		cs.HashBoardIssueCount = componentErrorMap[componentKey{collectionID, 2}]
		cs.PsuIssueCount = componentErrorMap[componentKey{collectionID, 1}]

		// Attach per-slot statuses for rack collections
		if slots, ok := slotStatusesByCollection[collectionID]; ok {
			cs.SlotStatuses = slots
		}

		stats = append(stats, cs)
	}

	return &pb.GetCollectionStatsResponse{Stats: stats}, nil
}

// ListRackTypes returns all distinct rack types for the organization.
func (s *Service) ListRackTypes(ctx context.Context, _ *pb.ListRackTypesRequest) (*pb.ListRackTypesResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	rackTypes, err := s.collectionStore.ListRackTypes(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	return &pb.ListRackTypesResponse{RackTypes: rackTypes}, nil
}

// ListRackZones returns all distinct rack zones for the organization.
func (s *Service) ListRackZones(ctx context.Context, _ *pb.ListRackZonesRequest) (*pb.ListRackZonesResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	zones, err := s.collectionStore.ListRackZones(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	return &pb.ListRackZonesResponse{Zones: zones}, nil
}

// saveRackResult holds the result of the SaveRack transaction.
type saveRackResult struct {
	collection    *pb.DeviceCollection
	assignedCount int32
}

// SaveRack atomically creates or updates a rack with its membership and slot assignments.
func (s *Service) SaveRack(ctx context.Context, req *pb.SaveRackRequest) (*pb.SaveRackResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	rackInfo := req.GetRackInfo()
	if rackInfo == nil {
		return nil, fleeterror.NewInvalidArgumentError("rack_info is required")
	}
	if rackInfo.GetZone() == "" {
		return nil, fleeterror.NewInvalidArgumentError("zone is required for rack collections")
	}
	if rackInfo.Rows < 1 || rackInfo.Rows > maxRackDimension {
		return nil, fleeterror.NewInvalidArgumentErrorf("rows must be between 1 and %d", maxRackDimension)
	}
	if rackInfo.Columns < 1 || rackInfo.Columns > maxRackDimension {
		return nil, fleeterror.NewInvalidArgumentErrorf("columns must be between 1 and %d", maxRackDimension)
	}
	if rackInfo.OrderIndex == pb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED {
		return nil, fleeterror.NewInvalidArgumentError("order_index is required for rack collections")
	}
	if _, ok := pb.RackOrderIndex_name[int32(rackInfo.OrderIndex)]; !ok {
		return nil, fleeterror.NewInvalidArgumentError("invalid order_index value")
	}
	if rackInfo.CoolingType == pb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED {
		return nil, fleeterror.NewInvalidArgumentError("cooling_type is required for rack collections")
	}
	if _, ok := pb.RackCoolingType_name[int32(rackInfo.CoolingType)]; !ok {
		return nil, fleeterror.NewInvalidArgumentError("invalid cooling_type value")
	}

	// Validate slot assignments are within rack bounds.
	for _, slot := range req.SlotAssignments {
		if slot.Position == nil {
			return nil, fleeterror.NewInvalidArgumentError("slot assignment must have a position")
		}
		if slot.Position.Row < 0 || slot.Position.Row >= rackInfo.Rows {
			return nil, fleeterror.NewInvalidArgumentErrorf("slot row %d is out of bounds (rack has %d rows)", slot.Position.Row, rackInfo.Rows)
		}
		if slot.Position.Column < 0 || slot.Position.Column >= rackInfo.Columns {
			return nil, fleeterror.NewInvalidArgumentErrorf("slot column %d is out of bounds (rack has %d columns)", slot.Position.Column, rackInfo.Columns)
		}
	}

	// Resolve device identifiers outside the transaction.
	// An empty device list is valid for SaveRack (removes all members).
	var deviceIdentifiers []string
	if req.DeviceSelector != nil {
		if dl, ok := req.DeviceSelector.SelectionType.(*commonpb.DeviceSelector_DeviceList); ok && (dl.DeviceList == nil || len(dl.DeviceList.DeviceIdentifiers) == 0) {
			// Empty device list — no members to add, skip resolver.
			deviceIdentifiers = nil
		} else {
			deviceIdentifiers, err = s.resolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
			if err != nil {
				return nil, err
			}
		}
	}

	// Build a set of resolved device IDs for slot assignment validation.
	deviceSet := make(map[string]struct{}, len(deviceIdentifiers))
	for _, id := range deviceIdentifiers {
		deviceSet[id] = struct{}{}
	}
	for _, slot := range req.SlotAssignments {
		if _, ok := deviceSet[slot.DeviceIdentifier]; !ok {
			return nil, fleeterror.NewInvalidArgumentErrorf("slot assignment references device %q which is not in the device selector", slot.DeviceIdentifier)
		}
	}

	isUpdate := req.CollectionId != nil

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		var collectionID int64

		if isUpdate {
			collectionID = *req.CollectionId

			// Verify the collection exists and belongs to the org.
			belongs, err := s.collectionStore.CollectionBelongsToOrg(ctx, collectionID, info.OrganizationID)
			if err != nil {
				return nil, err
			}
			if !belongs {
				return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", collectionID)
			}

			// Verify the collection is a rack (not a group).
			collectionType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, collectionID)
			if err != nil {
				return nil, err
			}
			if collectionType != pb.CollectionType_COLLECTION_TYPE_RACK {
				return nil, fleeterror.NewInvalidArgumentErrorf("collection %d is not a rack", collectionID)
			}

			// Update collection metadata.
			err = s.collectionStore.UpdateCollection(ctx, info.OrganizationID, collectionID, &req.Label, nil)
			if err != nil {
				return nil, err
			}

			// Update rack-specific info.
			err = s.collectionStore.UpdateRackInfo(ctx, collectionID, rackInfo.GetZone(), rackInfo.Rows, rackInfo.Columns, int32(rackInfo.OrderIndex), int32(rackInfo.CoolingType), info.OrganizationID)
			if err != nil {
				return nil, err
			}
		} else {
			// Create new rack.
			collection, err := s.collectionStore.CreateCollection(ctx, info.OrganizationID, pb.CollectionType_COLLECTION_TYPE_RACK, req.Label, "")
			if err != nil {
				return nil, err
			}
			collectionID = collection.Id

			err = s.collectionStore.CreateRackExtension(ctx, collectionID, rackInfo.GetZone(), rackInfo.Rows, rackInfo.Columns, int32(rackInfo.OrderIndex), int32(rackInfo.CoolingType), info.OrganizationID)
			if err != nil {
				return nil, err
			}
		}

		// Replace membership: remove all existing, then add the new set.
		_, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, info.OrganizationID, collectionID)
		if err != nil {
			return nil, err
		}

		if len(deviceIdentifiers) > 0 {
			_, err = s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, collectionID, deviceIdentifiers)
			if err != nil {
				return nil, err
			}
		}

		// Clear all existing slot positions.
		existingSlots, err := s.collectionStore.GetRackSlots(ctx, collectionID, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		for _, slot := range existingSlots {
			err = s.collectionStore.ClearRackSlotPosition(ctx, collectionID, slot.DeviceIdentifier, info.OrganizationID)
			if err != nil {
				return nil, err
			}
		}

		// Set new slot positions.
		for _, slot := range req.SlotAssignments {
			err = s.collectionStore.SetRackSlotPosition(ctx, collectionID, slot.DeviceIdentifier, slot.Position.Row, slot.Position.Column, info.OrganizationID)
			if err != nil {
				return nil, err
			}
		}

		// Fetch the final collection state.
		collection, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, collectionID)
		if err != nil {
			return nil, err
		}
		collection.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: rackInfo}

		// #nosec G115 -- slot count bounded by rack dimensions (max 12x12 = 144)
		return &saveRackResult{collection: collection, assignedCount: int32(len(req.SlotAssignments))}, nil
	})
	if err != nil {
		return nil, err
	}

	txResult, ok := result.(*saveRackResult)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	scopeType := "rack"
	deviceCount := len(deviceIdentifiers)
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "save_rack",
		Description:    fmt.Sprintf("Save rack: %s", req.Label),
		ScopeType:      &scopeType,
		ScopeLabel:     &req.Label,
		ScopeCount:     &deviceCount,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	return &pb.SaveRackResponse{Collection: txResult.collection, AssignedCount: txResult.assignedCount}, nil
}
