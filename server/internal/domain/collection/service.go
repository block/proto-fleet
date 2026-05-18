package collection

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgconn"

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
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	defaultPageSize  int32 = 50
	maxPageSize      int32 = 1000
	maxRackDimension int32 = 12
	// maxCascadeAuditEntries bounds the per-device cascade audit list in
	// activity_log.metadata; overflow is signaled via the truncated flag.
	maxCascadeAuditEntries = 100
	// maxDeviceSetFilterValues caps the size of free-form repeated filter
	// arrays in the legacy collection.v1 list path. Mirrors the cap in
	// fleetmanagement.parseFilter and the handler-level cap in
	// deviceset.convert so all three surfaces stay aligned.
	maxDeviceSetFilterValues = 1024
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
	siteStore                interfaces.SiteStore
	buildingStore            interfaces.BuildingStore
	transactor               interfaces.Transactor
	resolveDeviceIdentifiers DeviceIdentifierResolver
	telemetry                TelemetryCollector
	activitySvc              *activity.Service
}

// NewService creates a new collection service. A nil siteStore disables
// rack site/building placement. A nil buildingStore disables the
// cross-org check on building_ids / zone_keys filters.
func NewService(
	collectionStore interfaces.CollectionStore,
	deviceQueryer DeviceQueryer,
	siteStore interfaces.SiteStore,
	buildingStore interfaces.BuildingStore,
	transactor interfaces.Transactor,
	resolveDeviceIdentifiers DeviceIdentifierResolver,
	telemetry TelemetryCollector,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		collectionStore:          collectionStore,
		deviceQueryer:            deviceQueryer,
		siteStore:                siteStore,
		buildingStore:            buildingStore,
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

// resolveAndLockRackPlacement derives the authoritative site for the rack
// and locks the relevant rows in site -> building order. Building_id, when
// set, dictates site_id; a disagreeing client site_id is rejected.
// Placement encoding: both nil = no intent, *id == 0 = explicit unassign,
// *id > 0 = assign. Must run in-tx, before the rack row is locked.
func (s *Service) resolveAndLockRackPlacement(ctx context.Context, orgID int64, rackInfo *pb.RackInfo) (siteID, buildingID *int64, err error) {
	if rackInfo == nil {
		return nil, nil, nil
	}
	effectiveSiteID := rackInfo.SiteId
	if effectiveSiteID != nil && *effectiveSiteID == 0 {
		effectiveSiteID = nil
	}
	effectiveBuildingID := rackInfo.BuildingId
	if effectiveBuildingID != nil && *effectiveBuildingID == 0 {
		effectiveBuildingID = nil
	}
	if effectiveSiteID == nil && effectiveBuildingID == nil {
		return nil, nil, nil
	}
	if s.siteStore == nil {
		return nil, nil, fleeterror.NewFailedPreconditionError("site assignment unavailable: site service not configured")
	}

	if effectiveBuildingID != nil {
		bID := *effectiveBuildingID
		// Peek before locking so we can acquire site first (canonical
		// lock order); re-read under the building lock and retry on mismatch.
		peekedSiteID, err := s.collectionStore.GetBuildingSite(ctx, orgID, bID)
		if err != nil {
			return nil, nil, err
		}
		if effectiveSiteID != nil && (peekedSiteID == nil || *peekedSiteID != *effectiveSiteID) {
			return nil, nil, fleeterror.NewInvalidArgumentErrorf(
				"rack site_id %d does not match building %d site", *effectiveSiteID, bID)
		}
		if peekedSiteID != nil {
			if err := s.siteStore.LockSiteForWrite(ctx, orgID, *peekedSiteID); err != nil {
				return nil, nil, err
			}
		}
		if err := s.siteStore.LockBuildingForWrite(ctx, orgID, bID); err != nil {
			return nil, nil, err
		}
		lockedSiteID, err := s.collectionStore.GetBuildingSite(ctx, orgID, bID)
		if err != nil {
			return nil, nil, err
		}
		if !int64PtrEqual(peekedSiteID, lockedSiteID) {
			// Synthetic serialization failure so WithTransaction retries.
			return nil, nil, &pgconn.PgError{
				Code:    db.PGSerializationFailure,
				Message: fmt.Sprintf("building %d site changed during rack placement resolution; retrying", bID),
			}
		}
		buildingID = &bID
		siteID = lockedSiteID
		return siteID, buildingID, nil
	}

	sID := *effectiveSiteID
	if err := s.siteStore.LockSiteForWrite(ctx, orgID, sID); err != nil {
		return nil, nil, err
	}
	siteID = &sID
	return siteID, buildingID, nil
}

// rackPlacementOmitted reports whether the caller omitted placement intent
// (both ids nil). Explicit zero (unassign) returns false.
func rackPlacementOmitted(rackInfo *pb.RackInfo) bool {
	return rackInfo != nil && rackInfo.SiteId == nil && rackInfo.BuildingId == nil
}

func int64PtrEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// createCollectionResult holds the result of the CreateCollection transaction.
type createCollectionResult struct {
	collection *pb.DeviceCollection
	addedCount int64
	// Cascade audit; set only for site-stamped racks with devices.
	finalSiteID          *int64
	cascadeCount         int64
	deviceSiteChanges    []map[string]any
	cascadeTotalAffected int
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
	// TODO(#226): align with SaveRack's conditional zone rule once site/building UI lands.
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

	var deviceIdentifiers []string
	if req.DeviceSelector != nil {
		deviceIdentifiers, err = s.resolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
		if err != nil {
			return nil, err
		}
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		var siteID, buildingID *int64
		if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
			var err error
			siteID, buildingID, err = s.resolveAndLockRackPlacement(ctx, info.OrganizationID, rackInfo)
			if err != nil {
				return nil, err
			}
		}

		collection, err := s.collectionStore.CreateCollection(ctx, info.OrganizationID, req.Type, req.Label, req.Description)
		if err != nil {
			return nil, err
		}

		if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
			err = s.collectionStore.CreateRackExtension(ctx, interfaces.CreateRackExtensionParams{
				OrgID:        info.OrganizationID,
				CollectionID: collection.Id,
				Rows:         rackInfo.Rows,
				Columns:      rackInfo.Columns,
				OrderIndex:   int32(rackInfo.OrderIndex),
				CoolingType:  int32(rackInfo.CoolingType),
				Zone:         rackInfo.GetZone(),
				SiteID:       siteID,
				BuildingID:   buildingID,
			})
			if err != nil {
				return nil, err
			}
			rackInfo.SiteId = siteID
			rackInfo.BuildingId = buildingID
			collection.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: rackInfo}
		}

		var (
			addedCount        int64
			cascadeCount      int64
			deviceSiteChanges []map[string]any
			totalAffected     int
		)
		if len(deviceIdentifiers) > 0 {
			addedCount, err = s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, collection.Id, deviceIdentifiers)
			if err != nil {
				return nil, err
			}
			// #nosec G115 -- addedCount bounded by request size which is limited by gRPC message size
			collection.DeviceCount = int32(addedCount)

			if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK && siteID != nil {
				priors, err := s.collectionStore.GetDeviceSiteIDsByMembership(ctx, collection.Id, info.OrganizationID)
				if err != nil {
					return nil, err
				}
				deviceSiteChanges, totalAffected = buildDeviceSiteChanges(priors, siteID)
				n, err := s.collectionStore.CascadeRackDeviceSites(ctx, collection.Id, info.OrganizationID, siteID)
				if err != nil {
					return nil, err
				}
				cascadeCount = n
			}
		}

		return &createCollectionResult{
			collection:           collection,
			addedCount:           addedCount,
			finalSiteID:          siteID,
			cascadeCount:         cascadeCount,
			deviceSiteChanges:    deviceSiteChanges,
			cascadeTotalAffected: totalAffected,
		}, nil
	})
	if err != nil {
		return nil, err
	}

	txResult, ok := result.(*createCollectionResult)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	scopeType := collectionScopeType(req.Type)
	createEvent := activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "create_collection",
		Description:    fmt.Sprintf("Create %s: %s", scopeType, req.Label),
		ScopeType:      &scopeType,
		ScopeLabel:     &req.Label,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		SiteID:         txResult.finalSiteID,
	}
	if txResult.cascadeCount > 0 || txResult.cascadeTotalAffected > 0 {
		meta := map[string]any{
			"site_cascade":          true,
			"final_site_id":         txResult.finalSiteID,
			"site_reassigned_count": txResult.cascadeCount,
		}
		if len(txResult.deviceSiteChanges) > 0 {
			meta["device_site_changes"] = txResult.deviceSiteChanges
		}
		if txResult.cascadeTotalAffected > 0 {
			meta["total_affected"] = txResult.cascadeTotalAffected
			if txResult.cascadeTotalAffected > maxCascadeAuditEntries {
				meta["truncated"] = true
			}
		}
		createEvent.Metadata = meta
	}
	s.logActivity(ctx, createEvent)

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

		if hasDeviceSelector {
			collType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, req.CollectionId)
			if err != nil {
				return nil, err
			}
			var rackSiteID *int64
			isRack := collType == pb.CollectionType_COLLECTION_TYPE_RACK
			if isRack {
				placement, err := s.collectionStore.LockRackPlacementForWrite(ctx, req.CollectionId, info.OrganizationID)
				if err != nil {
					return nil, err
				}
				rackSiteID = placement.SiteID
			}
			if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, info.OrganizationID, req.CollectionId); err != nil {
				return nil, err
			}
			if len(deviceIdentifiers) > 0 {
				if _, err := s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers); err != nil {
					return nil, err
				}
				// Skip when site-less: cascading NULL would wipe direct assignments.
				if isRack && rackSiteID != nil {
					if _, err := s.collectionStore.CascadeRackDeviceSites(ctx, req.CollectionId, info.OrganizationID, rackSiteID); err != nil {
						return nil, err
					}
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

	// Best-effort prefetch for the activity log; cascade re-reads in-tx.
	collection, prefetchErr := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)

	var siteUnassignedCount int64
	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		collType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return err
		}
		if collType == pb.CollectionType_COLLECTION_TYPE_RACK {
			// Lock the rack FOR UPDATE so concurrent AddDevicesToCollection
			// / SaveRack can't slip a new member or cascade in between our
			// unassign + membership-drop + soft-delete steps.
			if _, err := s.collectionStore.LockRackPlacementForWrite(ctx, req.CollectionId, info.OrganizationID); err != nil {
				return err
			}
			n, err := s.collectionStore.UnassignDeviceSitesByRack(ctx, req.CollectionId, info.OrganizationID)
			if err != nil {
				return err
			}
			siteUnassignedCount = n
		}
		// Drop membership before soft-delete so idx_one_rack_per_device
		// allows re-adding devices to another rack.
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
		event := activitymodels.Event{
			Category:       activitymodels.CategoryCollection,
			Type:           "delete_collection",
			Description:    fmt.Sprintf("Delete %s: %s", scopeType, label),
			ScopeType:      &scopeType,
			ScopeLabel:     &label,
			UserID:         &info.ExternalUserID,
			Username:       &info.Username,
			OrganizationID: &info.OrganizationID,
		}
		if siteUnassignedCount > 0 {
			event.Metadata = map[string]any{
				"site_unassigned_count": siteUnassignedCount,
			}
		}
		s.logActivity(ctx, event)
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

// ListCollectionsParams is the domain-level input for listing collections.
// Used by the device_set.v1 handler so it can pass the new filter shape
// (building_ids, include_no_building, zone_keys) without round-tripping
// through the deprecated collection.v1 proto request type.
type ListCollectionsParams struct {
	Type      pb.CollectionType
	PageSize  int32
	PageToken string
	Sort      *interfaces.SortConfig
	Filter    *interfaces.DeviceSetFilter
}

// ListCollectionsDomain is the domain-level entry point for the
// collection list. Validates the rack-only filter constraints, runs
// the cross-org building check, and delegates to the store. The
// proto-shaped ListCollections wraps this.
func (s *Service) ListCollectionsDomain(ctx context.Context, params ListCollectionsParams) (*pb.ListCollectionsResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	pageSize := validatePageSize(params.PageSize)

	isZoneSort := params.Sort != nil && params.Sort.Field == interfaces.SortFieldLocation
	if isZoneSort && params.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
		return nil, fleeterror.NewInvalidArgumentErrorf("zone sort is only supported for rack collections")
	}
	if params.Filter != nil && params.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
		if len(params.Filter.BuildingIDs) > 0 || params.Filter.IncludeNoBuilding || len(params.Filter.ZoneKeys) > 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf("building / zone filters are only supported for rack collections")
		}
	}

	if err := s.validateFilterBuildings(ctx, info.OrganizationID, params.Filter); err != nil {
		return nil, err
	}

	collections, nextPageToken, totalCount, err := s.collectionStore.ListCollections(ctx, info.OrganizationID, params.Type, pageSize, params.PageToken, params.Sort, params.Filter)
	if err != nil {
		return nil, err
	}

	return &pb.ListCollectionsResponse{Collections: collections, NextPageToken: nextPageToken, TotalCount: totalCount}, nil
}

// validateFilterBuildings wraps the shared
// interfaces.ValidateFilterBuildings helper. Kept as a method for
// brevity at the call site; logic lives in
// interfaces/filtervalidation.go so the fleetmanagement and
// collection paths can't drift.
func (s *Service) validateFilterBuildings(ctx context.Context, orgID int64, filter *interfaces.DeviceSetFilter) error {
	if filter == nil {
		return nil
	}
	return interfaces.ValidateFilterBuildings(ctx, orgID, filter.BuildingIDs, filter.ZoneKeys, s.buildingStore)
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

	// Zone sort + zone filter are rack-only. The legacy `zones` field is
	// preserved here as a transitional shim — translate to wildcard
	// ZoneKey entries so existing collection.v1 callers keep working
	// until the wire contract retires (#255).
	isZoneSort := sort != nil && sort.Field == interfaces.SortFieldLocation
	if isZoneSort && req.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
		return nil, fleeterror.NewInvalidArgumentErrorf("zone sort is only supported for rack collections")
	}
	if len(req.Zones) > 0 && req.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
		return nil, fleeterror.NewInvalidArgumentErrorf("zone filter is only supported for rack collections")
	}
	if len(req.Zones) > maxDeviceSetFilterValues {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"zones exceeds maximum of %d values", maxDeviceSetFilterValues)
	}
	zoneKeys := make([]interfaces.ZoneKey, 0, len(req.Zones))
	for i, z := range req.Zones {
		if z == "" {
			return nil, fleeterror.NewInvalidArgumentErrorf("zones[%d] must be non-empty", i)
		}
		zoneKeys = append(zoneKeys, interfaces.ZoneKey{BuildingID: 0, Zone: z})
	}

	filter := &interfaces.DeviceSetFilter{
		ErrorComponentTypes: errorComponentTypes,
		ZoneKeys:            zoneKeys,
	}

	collections, nextPageToken, totalCount, err := s.collectionStore.ListCollections(ctx, info.OrganizationID, req.Type, pageSize, req.PageToken, sort, filter)
	if err != nil {
		return nil, err
	}

	return &pb.ListCollectionsResponse{Collections: collections, NextPageToken: nextPageToken, TotalCount: totalCount}, nil
}

type membershipChangeResult struct {
	collection   *pb.DeviceCollection
	count        int64
	conflicts    []interfaces.AddedDeviceSiteConflict
	finalSiteID  *int64
	cascadeCount int64
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

		// Lock rack FOR UPDATE so the cascade reads rack.site_id under a
		// write lock that serializes against SiteService writers. Skip the
		// site lock — that would invert canonical lock order and deadlock
		// against concurrent site moves. Groups skip lock and cascade.
		var (
			conflicts    []interfaces.AddedDeviceSiteConflict
			finalSiteID  *int64
			cascadeCount int64
		)
		if coll.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
			placement, err := s.collectionStore.LockRackPlacementForWrite(ctx, req.CollectionId, info.OrganizationID)
			if err != nil {
				return nil, err
			}
			finalSiteID = placement.SiteID
			if placement.SiteID != nil {
				conflicts, err = s.collectionStore.GetAddedDeviceSiteConflicts(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
				if err != nil {
					return nil, err
				}
			}
		}

		addedCount, err := s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
		if err != nil {
			return nil, err
		}

		if coll.Type == pb.CollectionType_COLLECTION_TYPE_RACK && finalSiteID != nil {
			n, err := s.collectionStore.CascadeAddedDeviceSites(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers)
			if err != nil {
				return nil, err
			}
			cascadeCount = n
		}

		return &membershipChangeResult{
			collection:   coll,
			count:        addedCount,
			conflicts:    conflicts,
			finalSiteID:  finalSiteID,
			cascadeCount: cascadeCount,
		}, nil
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
	addEvent := activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "add_devices",
		Description:    fmt.Sprintf("Add devices to %s: %s", scopeType, label),
		ScopeType:      &scopeType,
		ScopeLabel:     &label,
		ScopeCount:     &addedCountInt,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		SiteID:         txResult.finalSiteID,
	}
	if len(txResult.conflicts) > 0 {
		total := len(txResult.conflicts)
		capacity := total
		if capacity > maxCascadeAuditEntries {
			capacity = maxCascadeAuditEntries
		}
		priors := make([]map[string]any, 0, capacity)
		for i, c := range txResult.conflicts {
			if i >= maxCascadeAuditEntries {
				break
			}
			row := map[string]any{
				"device_identifier": c.DeviceIdentifier,
				"target_site_id":    c.TargetSiteID,
			}
			if c.PriorSiteID != nil {
				row["prior_site_id"] = *c.PriorSiteID
			}
			priors = append(priors, row)
		}
		meta := map[string]any{
			"site_cascade":          true,
			"final_site_id":         txResult.finalSiteID,
			"site_reassigned_count": txResult.cascadeCount,
			"device_site_changes":   priors,
			"total_affected":        total,
		}
		if total > maxCascadeAuditEntries {
			meta["truncated"] = true
		}
		addEvent.Metadata = meta
	}
	s.logActivity(ctx, addEvent)

	// #nosec G115 -- addedCount is bounded by request size which is limited by gRPC message size
	return &pb.AddDevicesToCollectionResponse{
		CollectionId: req.CollectionId,
		AddedCount:   int32(txResult.count),
		// #nosec G115 -- cascadeCount bounded by added member count
		SiteReassignedCount: int32(txResult.cascadeCount),
	}, nil
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
//
// Deprecated: this RPC still backs the legacy collection.v1 surface; new
// callers (notably device_set.v1.ListRackZones) use ListRackZoneRefs to
// receive (building_id, zone) tuples with denormalized labels. See
// docs/plans/2026-05-14-229-miner-zone-building-filter-plan.md.
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

// ListRackZoneRefs returns all distinct (building_id, zone) pairs in
// the org with denormalized building and site labels. Backs
// device_set.v1.ListRackZones, bypassing the deprecated collection.v1
// flat-string shape.
func (s *Service) ListRackZoneRefs(ctx context.Context) ([]interfaces.ZoneRefRow, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return s.collectionStore.ListRackZoneRefs(ctx, info.OrganizationID)
}

// saveRackResult holds the result of the SaveRack transaction.
type saveRackResult struct {
	collection          *pb.DeviceCollection
	assignedCount       int32
	cascadeApplied      bool
	finalSiteID         *int64
	siteReassignedCount int64
	// Per-device prior site_id for cascade-rewritten members, capped at
	// maxCascadeAuditEntries; totalAffected holds the un-truncated count.
	deviceSiteChanges []map[string]any
	totalAffected     int
}

// SaveRack atomically creates or updates a rack with its membership and slot
// assignments. Lock order is the canonical site -> building -> rack -> devices.
// On site change, the cascade rewrites descendant device.site_id.
func (s *Service) SaveRack(ctx context.Context, req *pb.SaveRackRequest) (*pb.SaveRackResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	rackInfo := req.GetRackInfo()
	if err := validateSaveRackRequest(req, rackInfo); err != nil {
		return nil, err
	}

	// Empty device list is valid; it removes all members.
	deviceIdentifiers, err := s.resolveSaveRackDevices(ctx, req, info.OrganizationID)
	if err != nil {
		return nil, err
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
		var (
			collectionID    int64
			finalSiteID     *int64
			finalBuildingID *int64
			finalZone       string
			siteChanged     bool
		)

		if isUpdate {
			res, err := s.saveRackUpdate(ctx, info, req, rackInfo)
			if err != nil {
				return nil, err
			}
			collectionID = res.collectionID
			finalSiteID = res.finalSiteID
			finalBuildingID = res.finalBuildingID
			finalZone = res.finalZone
			siteChanged = res.siteChanged
		} else {
			res, err := s.saveRackCreate(ctx, info, req, rackInfo)
			if err != nil {
				return nil, err
			}
			collectionID = res.collectionID
			finalSiteID = res.finalSiteID
			finalBuildingID = res.finalBuildingID
			finalZone = res.finalZone
			// Create path: every member is new, so cascade aligns them
			// with the rack's site when one is stamped.
			siteChanged = finalSiteID != nil
		}

		// Cascade runs after membership replace so it touches only the
		// final member set; removed devices keep their prior site_id.
		cascade, err := s.replaceRackMembershipAndSlots(ctx, info.OrganizationID, collectionID, deviceIdentifiers, req.SlotAssignments, finalSiteID, siteChanged)
		if err != nil {
			return nil, err
		}
		cascadeApplied := siteChanged || cascade.cascadeCount > 0
		cascadeCount := cascade.cascadeCount
		deviceSiteChanges := cascade.deviceSiteChanges
		totalAffected := cascade.totalAffected

		// Fetch the final collection state.
		collection, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, collectionID)
		if err != nil {
			return nil, err
		}
		rackInfo.SiteId = finalSiteID
		rackInfo.BuildingId = finalBuildingID
		rackInfo.Zone = finalZone
		collection.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: rackInfo}

		// #nosec G115 -- slot count bounded by rack dimensions (max 12x12 = 144)
		return &saveRackResult{
			collection:          collection,
			assignedCount:       int32(len(req.SlotAssignments)),
			cascadeApplied:      cascadeApplied,
			finalSiteID:         finalSiteID,
			siteReassignedCount: cascadeCount,
			deviceSiteChanges:   deviceSiteChanges,
			totalAffected:       totalAffected,
		}, nil
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
	saveEvent := activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "save_rack",
		Description:    fmt.Sprintf("Save rack: %s", req.Label),
		ScopeType:      &scopeType,
		ScopeLabel:     &req.Label,
		ScopeCount:     &deviceCount,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		SiteID:         txResult.finalSiteID,
	}
	if txResult.cascadeApplied || txResult.siteReassignedCount > 0 {
		meta := map[string]any{
			"site_cascade":          true,
			"final_site_id":         txResult.finalSiteID,
			"site_reassigned_count": txResult.siteReassignedCount,
		}
		if len(txResult.deviceSiteChanges) > 0 {
			meta["device_site_changes"] = txResult.deviceSiteChanges
		}
		if txResult.totalAffected > 0 {
			meta["total_affected"] = txResult.totalAffected
			if txResult.totalAffected > maxCascadeAuditEntries {
				meta["truncated"] = true
			}
		}
		saveEvent.Metadata = meta
	}
	s.logActivity(ctx, saveEvent)

	return &pb.SaveRackResponse{
		Collection:    txResult.collection,
		AssignedCount: txResult.assignedCount,
		// #nosec G115 -- cascadeCount bounded by rack member count (~144)
		SiteReassignedCount: int32(txResult.siteReassignedCount),
	}, nil
}

// validateSaveRackRequest enforces SaveRack input contract: rack_info
// shape, slot bounds, and zone-required-when-building-set.
func validateSaveRackRequest(req *pb.SaveRackRequest, rackInfo *pb.RackInfo) error {
	if rackInfo == nil {
		return fleeterror.NewInvalidArgumentError("rack_info is required")
	}
	// Building_id=0 means "no building" (zero-as-unassign convention).
	// Don't mutate rackInfo.BuildingId — nil-vs-&0 distinguishes
	// "preserve placement" from "explicit unassign" downstream.
	buildingPresent := rackInfo.BuildingId != nil && *rackInfo.BuildingId != 0
	if buildingPresent && rackInfo.GetZone() == "" {
		return fleeterror.NewInvalidArgumentError("zone is required when the rack belongs to a building")
	}
	if rackInfo.Rows < 1 || rackInfo.Rows > maxRackDimension {
		return fleeterror.NewInvalidArgumentErrorf("rows must be between 1 and %d", maxRackDimension)
	}
	if rackInfo.Columns < 1 || rackInfo.Columns > maxRackDimension {
		return fleeterror.NewInvalidArgumentErrorf("columns must be between 1 and %d", maxRackDimension)
	}
	if rackInfo.OrderIndex == pb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED {
		return fleeterror.NewInvalidArgumentError("order_index is required for rack collections")
	}
	if _, ok := pb.RackOrderIndex_name[int32(rackInfo.OrderIndex)]; !ok {
		return fleeterror.NewInvalidArgumentError("invalid order_index value")
	}
	if rackInfo.CoolingType == pb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED {
		return fleeterror.NewInvalidArgumentError("cooling_type is required for rack collections")
	}
	if _, ok := pb.RackCoolingType_name[int32(rackInfo.CoolingType)]; !ok {
		return fleeterror.NewInvalidArgumentError("invalid cooling_type value")
	}
	for _, slot := range req.SlotAssignments {
		if slot.Position == nil {
			return fleeterror.NewInvalidArgumentError("slot assignment must have a position")
		}
		if slot.Position.Row < 0 || slot.Position.Row >= rackInfo.Rows {
			return fleeterror.NewInvalidArgumentErrorf("slot row %d is out of bounds (rack has %d rows)", slot.Position.Row, rackInfo.Rows)
		}
		if slot.Position.Column < 0 || slot.Position.Column >= rackInfo.Columns {
			return fleeterror.NewInvalidArgumentErrorf("slot column %d is out of bounds (rack has %d columns)", slot.Position.Column, rackInfo.Columns)
		}
	}
	return nil
}

// resolveSaveRackDevices resolves device_selector to identifiers; an
// empty DeviceList is valid and removes all members.
func (s *Service) resolveSaveRackDevices(ctx context.Context, req *pb.SaveRackRequest, orgID int64) ([]string, error) {
	if req.DeviceSelector == nil {
		return nil, nil
	}
	if dl, ok := req.DeviceSelector.SelectionType.(*commonpb.DeviceSelector_DeviceList); ok && (dl.DeviceList == nil || len(dl.DeviceList.DeviceIdentifiers) == 0) {
		return nil, nil
	}
	return s.resolveDeviceIdentifiers(ctx, req.DeviceSelector, orgID)
}

// saveRackCreatePathResult holds the outputs of the SaveRack create branch.
type saveRackCreatePathResult struct {
	collectionID    int64
	finalSiteID     *int64
	finalBuildingID *int64
	finalZone       string
}

// saveRackCreate runs the SaveRack create branch in-tx: resolve placement,
// then insert device_set + device_set_rack rows.
func (s *Service) saveRackCreate(ctx context.Context, info *session.Info, req *pb.SaveRackRequest, rackInfo *pb.RackInfo) (*saveRackCreatePathResult, error) {
	newSiteID, newBuildingID, err := s.resolveAndLockRackPlacement(ctx, info.OrganizationID, rackInfo)
	if err != nil {
		return nil, err
	}

	collection, err := s.collectionStore.CreateCollection(ctx, info.OrganizationID, pb.CollectionType_COLLECTION_TYPE_RACK, req.Label, "")
	if err != nil {
		return nil, err
	}

	finalZone := rackInfo.GetZone()
	err = s.collectionStore.CreateRackExtension(ctx, interfaces.CreateRackExtensionParams{
		OrgID:        info.OrganizationID,
		CollectionID: collection.Id,
		Rows:         rackInfo.Rows,
		Columns:      rackInfo.Columns,
		OrderIndex:   int32(rackInfo.OrderIndex),
		CoolingType:  int32(rackInfo.CoolingType),
		Zone:         finalZone,
		SiteID:       newSiteID,
		BuildingID:   newBuildingID,
	})
	if err != nil {
		return nil, err
	}

	return &saveRackCreatePathResult{
		collectionID:    collection.Id,
		finalSiteID:     newSiteID,
		finalBuildingID: newBuildingID,
		finalZone:       finalZone,
	}, nil
}

// saveRackUpdatePathResult holds the outputs of the SaveRack update branch.
// siteChanged signals the rack moved; cascade + prior capture run in
// replaceRackMembershipAndSlots against the final member set.
type saveRackUpdatePathResult struct {
	collectionID    int64
	finalSiteID     *int64
	finalBuildingID *int64
	finalZone       string
	siteChanged     bool
}

// saveRackUpdate runs the SaveRack update branch: validate ownership,
// lock site/building/rack in canonical order, derive the final zone,
// persist placement, and flag siteChanged for the downstream cascade.
func (s *Service) saveRackUpdate(ctx context.Context, info *session.Info, req *pb.SaveRackRequest, rackInfo *pb.RackInfo) (*saveRackUpdatePathResult, error) {
	collectionID := *req.CollectionId

	belongs, err := s.collectionStore.CollectionBelongsToOrg(ctx, collectionID, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", collectionID)
	}
	collectionType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, collectionID)
	if err != nil {
		return nil, err
	}
	if collectionType != pb.CollectionType_COLLECTION_TYPE_RACK {
		return nil, fleeterror.NewInvalidArgumentErrorf("collection %d is not a rack", collectionID)
	}

	var (
		current       interfaces.RackPlacement
		newSiteID     *int64
		newBuildingID *int64
	)
	if rackPlacementOmitted(rackInfo) {
		// Preserve current placement; skip site/building locks since the
		// rack lock alone serializes the no-op cascade.
		current, err = s.collectionStore.LockRackPlacementForWrite(ctx, collectionID, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		newSiteID = current.SiteID
		newBuildingID = current.BuildingID
	} else {
		// Placement intent supplied; resolve and lock site/building
		// before the rack lock (canonical order).
		newSiteID, newBuildingID, err = s.resolveAndLockRackPlacement(ctx, info.OrganizationID, rackInfo)
		if err != nil {
			return nil, err
		}
		current, err = s.collectionStore.LockRackPlacementForWrite(ctx, collectionID, info.OrganizationID)
		if err != nil {
			return nil, err
		}
	}

	// Zone is building-scoped: clear it when leaving or crossing buildings,
	// and preserve the current zone when the caller omitted it but the rack
	// stays in a building (legacy clients don't send zone — validation only
	// requires it when the request itself sets a non-zero building_id).
	finalZone := rackInfo.GetZone()
	leavingBuilding := current.BuildingID != nil && newBuildingID == nil
	crossingBuildings := current.BuildingID != nil && newBuildingID != nil && !int64PtrEqual(current.BuildingID, newBuildingID)
	switch {
	case leavingBuilding || crossingBuildings:
		finalZone = ""
	case finalZone == "" && newBuildingID != nil:
		finalZone = current.Zone
	}

	err = s.collectionStore.UpdateCollection(ctx, info.OrganizationID, collectionID, &req.Label, nil)
	if err != nil {
		return nil, err
	}
	err = s.collectionStore.UpdateRackInfo(ctx, collectionID, finalZone, rackInfo.Rows, rackInfo.Columns, int32(rackInfo.OrderIndex), int32(rackInfo.CoolingType), info.OrganizationID)
	if err != nil {
		return nil, err
	}
	err = s.collectionStore.UpdateRackPlacement(ctx, collectionID, info.OrganizationID, newSiteID, newBuildingID, finalZone)
	if err != nil {
		return nil, err
	}

	out := &saveRackUpdatePathResult{
		collectionID:    collectionID,
		finalSiteID:     newSiteID,
		finalBuildingID: newBuildingID,
		finalZone:       finalZone,
		siteChanged:     !int64PtrEqual(current.SiteID, newSiteID),
	}
	return out, nil
}

// rackCascadeOutcome holds per-call cascade results; totalAffected may
// exceed len(deviceSiteChanges) when the audit list was truncated.
type rackCascadeOutcome struct {
	cascadeCount      int64
	deviceSiteChanges []map[string]any
	totalAffected     int
}

// replaceRackMembershipAndSlots removes existing membership + slots and
// writes the new set. Cascade runs AFTER membership replace so removed
// devices keep their prior site_id and the per-device priors captured
// here reflect the final member set.
func (s *Service) replaceRackMembershipAndSlots(ctx context.Context, orgID, collectionID int64, deviceIdentifiers []string, slotAssignments []*pb.RackSlot, finalSiteID *int64, siteChanged bool) (rackCascadeOutcome, error) {
	var out rackCascadeOutcome
	if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, orgID, collectionID); err != nil {
		return out, err
	}

	// Cascade fires when the rack has a stamped site OR its site just
	// transitioned. Both false means the rack stayed site-less; cascading
	// NULL there would clobber direct device.site_id assignments.
	cascadeFires := finalSiteID != nil || siteChanged

	if len(deviceIdentifiers) > 0 {
		if _, err := s.collectionStore.AddDevicesToCollection(ctx, orgID, collectionID, deviceIdentifiers); err != nil {
			return out, err
		}
		if cascadeFires {
			priors, err := s.collectionStore.GetDeviceSiteIDsByMembership(ctx, collectionID, orgID)
			if err != nil {
				return out, err
			}
			out.deviceSiteChanges, out.totalAffected = buildDeviceSiteChanges(priors, finalSiteID)
			n, err := s.collectionStore.CascadeRackDeviceSites(ctx, collectionID, orgID, finalSiteID)
			if err != nil {
				return out, err
			}
			out.cascadeCount = n
		}
	}

	existingSlots, err := s.collectionStore.GetRackSlots(ctx, collectionID, orgID)
	if err != nil {
		return out, err
	}
	for _, slot := range existingSlots {
		if err := s.collectionStore.ClearRackSlotPosition(ctx, collectionID, slot.DeviceIdentifier, orgID); err != nil {
			return out, err
		}
	}
	for _, slot := range slotAssignments {
		if err := s.collectionStore.SetRackSlotPosition(ctx, collectionID, slot.DeviceIdentifier, slot.Position.Row, slot.Position.Column, orgID); err != nil {
			return out, err
		}
	}

	return out, nil
}

// buildDeviceSiteChanges produces the activity-log metadata: one entry
// per device whose prior site_id differs from target, capped at
// maxCascadeAuditEntries. Devices already at target are omitted.
func buildDeviceSiteChanges(priors map[string]*int64, target *int64) (changes []map[string]any, totalAffected int) {
	changes = make([]map[string]any, 0, len(priors))
	for deviceIdentifier, prior := range priors {
		if int64PtrEqual(prior, target) {
			continue
		}
		totalAffected++
		if len(changes) >= maxCascadeAuditEntries {
			continue
		}
		row := map[string]any{
			"device_identifier": deviceIdentifier,
		}
		if prior != nil {
			row["prior_site_id"] = *prior
		}
		if target != nil {
			row["target_site_id"] = *target
		}
		changes = append(changes, row)
	}
	return changes, totalAffected
}
