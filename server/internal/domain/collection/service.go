package collection

import (
	"context"
	"fmt"
	"math"

	"connectrpc.com/connect"

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
	// maxCascadeAuditEntries caps the per-device cascade audit array
	// stored on activity_log.metadata. A bulk add/move can touch
	// thousands of devices; storing one JSON entry per affected device
	// inflates the activity row to dangerous sizes (latency, planner
	// pressure, potential row-size limits). When the affected set
	// exceeds this cap the metadata records `truncated: true` plus
	// `total_affected: <N>` so consumers see the real count without
	// the full per-device list.
	maxCascadeAuditEntries = 100
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
	transactor               interfaces.Transactor
	resolveDeviceIdentifiers DeviceIdentifierResolver
	telemetry                TelemetryCollector
	activitySvc              *activity.Service
}

// NewService creates a new collection service. siteStore may be nil for
// callers that do not need rack site/building cascade (e.g. legacy
// site-less installs); the rack edit/move flow guards on a nil store and
// rejects placement changes when sites are not wired.
func NewService(
	collectionStore interfaces.CollectionStore,
	deviceQueryer DeviceQueryer,
	siteStore interfaces.SiteStore,
	transactor interfaces.Transactor,
	resolveDeviceIdentifiers DeviceIdentifierResolver,
	telemetry TelemetryCollector,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		collectionStore:          collectionStore,
		deviceQueryer:            deviceQueryer,
		siteStore:                siteStore,
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

// resolveAndLockRackPlacement validates the rack_info site/building
// fields, derives the authoritative site for the rack, and locks the
// relevant rows in the canonical multi-site order **site -> building**
// so the rack-edit cascade tx serializes against SiteService writers
// (AssignBuildingToSite, DeleteSite) without deadlocking. When
// building_id is set the server derives site_id from the building and
// rejects a client-supplied site_id that disagrees. Explicit zero IDs
// are normalized to nil to match the AssignBuildingToSite convention
// (target=nil or target=0 both mean "Unassigned"). When both ids are
// nil the rack is treated as fully unassigned. Must be called from
// inside a transaction, BEFORE the rack row itself is locked.
func (s *Service) resolveAndLockRackPlacement(ctx context.Context, orgID int64, rackInfo *pb.RackInfo) (siteID, buildingID *int64, err error) {
	if rackInfo == nil {
		return nil, nil, nil
	}
	// Normalize explicit zero IDs to nil so a misformed RackInfo with
	// SiteId=&0 / BuildingId=&0 doesn't fall through to LockSiteForWrite(0)
	// and surface a misleading "site 0 not found" error.
	if rackInfo.SiteId != nil && *rackInfo.SiteId == 0 {
		rackInfo.SiteId = nil
	}
	if rackInfo.BuildingId != nil && *rackInfo.BuildingId == 0 {
		rackInfo.BuildingId = nil
	}
	if rackInfo.SiteId == nil && rackInfo.BuildingId == nil {
		return nil, nil, nil
	}
	if s.siteStore == nil {
		return nil, nil, fleeterror.NewFailedPreconditionError("site assignment unavailable: site service not configured")
	}

	if rackInfo.BuildingId != nil {
		bID := *rackInfo.BuildingId
		// Peek the building's site WITHOUT a lock so we can acquire the
		// site lock first per canonical site -> building order. The peek
		// can race with a concurrent AssignBuildingToSite, so we re-read
		// the building's site UNDER the building lock below and abort on
		// mismatch — the WithTransaction retry loop reruns the closure
		// with fresh state.
		peekedSiteID, err := s.collectionStore.GetBuildingSite(ctx, orgID, bID)
		if err != nil {
			return nil, nil, err
		}
		if rackInfo.SiteId != nil && (peekedSiteID == nil || *peekedSiteID != *rackInfo.SiteId) {
			return nil, nil, fleeterror.NewInvalidArgumentErrorf(
				"rack site_id %d does not match building %d site", *rackInfo.SiteId, bID)
		}
		if peekedSiteID != nil {
			if err := s.siteStore.LockSiteForWrite(ctx, orgID, *peekedSiteID); err != nil {
				return nil, nil, err
			}
		}
		if err := s.siteStore.LockBuildingForWrite(ctx, orgID, bID); err != nil {
			return nil, nil, err
		}
		// Re-read building.site_id under the building lock. If it
		// changed between the peek and the lock acquisition (a
		// concurrent AssignBuildingToSite committed in between), abort
		// with Aborted so the tx retries; the next attempt sees the new
		// site and locks it correctly per the canonical order.
		lockedSiteID, err := s.collectionStore.GetBuildingSite(ctx, orgID, bID)
		if err != nil {
			return nil, nil, err
		}
		if !int64PtrEqual(peekedSiteID, lockedSiteID) {
			return nil, nil, fleeterror.NewPlainError(
				"building site changed concurrently; tx will retry",
				connect.CodeAborted)
		}
		buildingID = &bID
		siteID = lockedSiteID
		return siteID, buildingID, nil
	}

	// site_id only — direct-under-site rack with no building.
	sID := *rackInfo.SiteId
	if err := s.siteStore.LockSiteForWrite(ctx, orgID, sID); err != nil {
		return nil, nil, err
	}
	siteID = &sID
	return siteID, buildingID, nil
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
	// Cascade audit fields populated when a site-stamped rack is
	// created with an initial device_selector. Mirrors the shape used
	// by SaveRack / AddDevicesToCollection so the activity log records
	// implicit device-site reassignments uniformly across the three
	// rack write paths. Empty/zero for groups and for site-less rack
	// creates.
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
	// TODO(#226): CreateCollection unconditionally requires zone here, but
	// SaveRack makes zone conditional on building_id (zone required only
	// when the rack belongs to a building). The asymmetry is intentional
	// pre-Phase-2: today's UI mental model is "racks have zones, sites/
	// buildings don't exist yet." Align the two rules after Phase 2 UI
	// lands — see https://github.com/block/proto-fleet/issues/226 for
	// the broader "zone semantics without building" decision.
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

		// Add devices to the collection if device_selector was provided.
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
			// Update device count to reflect added devices.
			// #nosec G115 -- addedCount bounded by request size which is limited by gRPC message size
			collection.DeviceCount = int32(addedCount)

			// New rack with a site stamped: capture priors + cascade
			// rack.site_id onto every added device. CascadeRackDeviceSites
			// is a no-op when the rack has no site_id, so this also covers
			// group creation and site-less rack creation. Per-device
			// priors are captured for the activity-log audit, matching
			// the shape SaveRack / AddDevicesToCollection produce.
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
		// Site-stamped rack created with an initial device_selector
		// implicitly rewrote some device.site_id values. Mirror the
		// audit shape SaveRack / AddDevicesToCollection produce so
		// downstream consumers parse all three paths uniformly.
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
			// Lock rack placement BEFORE the cascade so the site_id we
			// read is stable for the duration of this tx. No-op for
			// group-type collections (rack lock isn't needed when there
			// is no site invariant to enforce).
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
				// Cascade rack site onto the freshly-replaced membership
				// so every member's device.site_id matches the rack
				// (including NULL when the rack has no site stamped).
				// CascadeRackDeviceSites uses IS DISTINCT FROM, so it's
				// a no-op for devices already aligned. Same path
				// AddDevicesToCollection / SaveRack use — closes the
				// invariant gap on this third write path.
				if isRack {
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

	// Prefetch the collection out-of-tx for the activity log description
	// only — its result MUST NOT decide whether the in-tx site cascade
	// runs (a transient prefetch error would otherwise skip the
	// device.site_id cleanup while still soft-deleting the rack). The
	// in-tx flow re-reads the collection type via GetCollectionType to
	// make the cascade decision under correct locking semantics.
	collection, prefetchErr := s.collectionStore.GetCollection(ctx, info.OrganizationID, req.CollectionId)

	var siteUnassignedCount int64
	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Re-read the collection type inside the tx so the cascade
		// decision doesn't rely on the out-of-tx prefetch. Plan
		// §"Cross-collection consistency rule": rack-type collections
		// cascade-null device.site_id on delete; group-type collections
		// don't stamp device.site_id and skip the cascade entirely.
		collType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return err
		}
		// For rack-type collections, null device.site_id for every paired
		// member before dropping membership. Devices that entered a site
		// via this rack should not keep pointing at the site after the
		// rack disappears — they land in the Unassigned bucket and the
		// operator can explicitly reassign. Mirrors
		// AssignBuildingToSite(target=NULL) cascade semantics.
		// No-op when the collection is a group (groups are org-scoped
		// and never stamp device.site_id) or when the rack has no
		// stamped site_id (handled by the SQL guard).
		if collType == pb.CollectionType_COLLECTION_TYPE_RACK {
			n, err := s.collectionStore.UnassignDeviceSitesByRack(ctx, req.CollectionId, info.OrganizationID)
			if err != nil {
				return err
			}
			siteUnassignedCount = n
		}
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

	// Activity-log description uses the out-of-tx prefetch (label +
	// scope), which is best-effort. The tx-internal cascade above does
	// not depend on it.
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
			// Record the cascade impact so the audit log reflects the
			// implicit device site reassignment.
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

		// For rack targets, lock the rack extension row FOR UPDATE so
		// the cascade reads rack.site_id under a write lock that
		// serializes against SiteService writers. SiteService writers
		// (AssignBuildingToSite step 1, DeleteSite step 1+3, SaveRack
		// update) all UPDATE device_set_rack rows under their site lock;
		// our rack FOR UPDATE blocks them until our cascade finishes.
		// Since AddDevicesToCollection itself never mutates the site
		// row, we don't need a site lock here — taking one would invert
		// canonical lock order (SiteService locks site before rack) and
		// deadlock against concurrent site moves. Group targets remain
		// org-scoped per plan §"Cross-collection consistency rule" and
		// skip the lock + cascade entirely.
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

		// Cascade the rack's site_id onto added devices whose current
		// site differs. No-op for group targets and for racks without a
		// stamped site_id.
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
		// Capture prior site_id per device so the activity log row
		// reconstructs the implicit cascade per plan §"Add devices to
		// rack". Stored on metadata rather than scope_count so callers
		// can list them out. The activity log fires AFTER the tx commits
		// (PR B convention) because the WithTransaction retry loop would
		// otherwise duplicate the row on serialization-failure retries.
		//
		// Bound the per-device list at maxCascadeAuditEntries to keep
		// the activity_log JSON payload bounded — a bulk add of
		// thousands of devices would otherwise inflate the row size.
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
	collection          *pb.DeviceCollection
	assignedCount       int32
	cascadeApplied      bool
	finalSiteID         *int64
	siteReassignedCount int64
	// deviceSiteChanges captures the per-device prior site_id for every
	// rack member whose site_id was rewritten by the cascade. One entry
	// per rewritten device, suitable for activity_log.metadata. Capped
	// at maxCascadeAuditEntries; totalAffected carries the un-truncated
	// count so the audit row records the real cascade scope.
	deviceSiteChanges []map[string]any
	totalAffected     int
}

// SaveRack atomically creates or updates a rack with its membership and
// slot assignments. Lock order across the transaction is the canonical
// multi-site order **site -> building -> rack -> devices**: site/building
// locks are taken by resolveAndLockRackPlacement, then the rack
// extension row, then membership writes. Inverting this order would
// deadlock with concurrent SiteService writers (AssignBuildingToSite,
// DeleteSite). Rack edit/move cascade rewrites descendant
// device.site_id when the rack's site changes; per-device prior site_ids
// are captured for the activity-log row.
func (s *Service) SaveRack(ctx context.Context, req *pb.SaveRackRequest) (*pb.SaveRackResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	rackInfo := req.GetRackInfo()
	if err := validateSaveRackRequest(req, rackInfo); err != nil {
		return nil, err
	}

	// Resolve device identifiers outside the transaction.
	// An empty device list is valid for SaveRack (removes all members).
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
			// On the create path every member is "newly added", so the
			// cascade in replaceRackMembershipAndSlots aligns them with
			// the rack's site. The "siteChanged" semantic doesn't apply
			// (no prior state) — record the cascade as applied if a
			// stamped site is being applied to anyone.
			siteChanged = finalSiteID != nil
		}

		// Membership + slot replacement is identical for create and
		// update. The cascade runs here so it touches the FINAL member
		// set only — devices being removed by membership replace keep
		// their previous site_id, and devices being added get aligned
		// to the rack's site (or nulled when the rack is unassigned).
		cascade, err := s.replaceRackMembershipAndSlots(ctx, info.OrganizationID, collectionID, deviceIdentifiers, req.SlotAssignments, finalSiteID)
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
		// Implicit device site rewrite triggered by the rack edit/move.
		// Stamp the per-device priors on the activity-log metadata so
		// the audit reflects the cascade per plan §"Rack edit / move"
		// and the cross-cutting consistency rule.
		// The activity log is fired AFTER the transaction commits (PR B
		// convention) because the WithTransaction retry loop would
		// otherwise duplicate the row on serialization-failure retries.
		// Failure to insert is logged but not surfaced (best-effort
		// audit consistent with sites/buildings services).
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

// validateSaveRackRequest enforces the SaveRack input contract: rack_info
// shape, slot bounds, zone-required-when-building-set rule. Pulled out
// of SaveRack so the orchestration stays focused on the tx flow.
func validateSaveRackRequest(req *pb.SaveRackRequest, rackInfo *pb.RackInfo) error {
	if rackInfo == nil {
		return fleeterror.NewInvalidArgumentError("rack_info is required")
	}
	// Normalize explicit zero IDs to nil up front so validation rules
	// that key off "building set" / "site set" agree with the
	// downstream resolveAndLockRackPlacement convention (mirrors
	// AssignBuildingToSite's target=nil / target=0 equivalence). Without
	// this step, a client using building_id=0 to mean "unassigned"
	// would hit the zone-required check below.
	if rackInfo.SiteId != nil && *rackInfo.SiteId == 0 {
		rackInfo.SiteId = nil
	}
	if rackInfo.BuildingId != nil && *rackInfo.BuildingId == 0 {
		rackInfo.BuildingId = nil
	}
	// Zone is required when the rack lives inside a building (zones are
	// the sub-building organizer). For direct-under-site or fully
	// unassigned racks, zone may be empty.
	if rackInfo.BuildingId != nil && rackInfo.GetZone() == "" {
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

// resolveSaveRackDevices pulls the device identifier list off the
// request's device_selector. An empty DeviceList is valid (removes all
// members); other selector shapes go through the resolver.
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

// saveRackCreate runs the SaveRack create branch: resolve placement,
// insert the device_set + device_set_rack rows. Must run inside a
// transaction; site/building locks are acquired by
// resolveAndLockRackPlacement before any writes.
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
// siteChanged signals that the rack's site moved between current and new;
// the cascade itself + per-device prior capture happen in
// replaceRackMembershipAndSlots so they reflect the final member set.
type saveRackUpdatePathResult struct {
	collectionID    int64
	finalSiteID     *int64
	finalBuildingID *int64
	finalZone       string
	siteChanged     bool
}

// saveRackUpdate runs the SaveRack update branch: validate ownership,
// lock site/building/rack in canonical order, derive the final zone
// per the building-boundary rule, persist placement, and cascade
// device.site_id when the rack's site actually changed. Per-device
// prior site_ids are captured before the cascade fires so the
// activity-log row reflects the exact reassignments.
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

	// Canonical lock order: site/building first via
	// resolveAndLockRackPlacement, then the rack row via
	// LockRackPlacementForWrite. Reverse ordering would deadlock with
	// SiteService writers that lock site -> building -> rack-descendants.
	newSiteID, newBuildingID, err := s.resolveAndLockRackPlacement(ctx, info.OrganizationID, rackInfo)
	if err != nil {
		return nil, err
	}
	current, err := s.collectionStore.LockRackPlacementForWrite(ctx, collectionID, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	// Zone clears only when the building changes between two buildings
	// or when leaving a building to direct-under-site placement; the
	// rack's zone belonged to a specific building-scoped namespace and
	// shouldn't be carried into another. When entering a building from
	// no-building (current nil -> new non-nil), the operator's supplied
	// zone is honored — there's no prior building scope to leak from.
	finalZone := rackInfo.GetZone()
	leavingBuilding := current.BuildingID != nil && newBuildingID == nil
	crossingBuildings := current.BuildingID != nil && newBuildingID != nil && !int64PtrEqual(current.BuildingID, newBuildingID)
	if leavingBuilding || crossingBuildings {
		finalZone = ""
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

	// Mark whether the rack's site changed; the cascade itself runs
	// after membership replacement (see replaceRackMembershipAndSlots)
	// so it only touches devices that REMAIN in the rack. Cascading
	// before the membership replace would rewrite devices the operator
	// is removing in this same call, leaving them orphaned at the new
	// site with no rack. Capturing per-device priors also has to wait
	// until after membership replace so they reflect the final-member
	// set (stayers + new additions), not the pre-replace set.
	out := &saveRackUpdatePathResult{
		collectionID:    collectionID,
		finalSiteID:     newSiteID,
		finalBuildingID: newBuildingID,
		finalZone:       finalZone,
		siteChanged:     !int64PtrEqual(current.SiteID, newSiteID),
	}
	return out, nil
}

// rackCascadeOutcome holds the per-call cascade results: number of
// device rows rewritten, per-device prior site_ids for the activity
// audit, and the total affected count (which may exceed
// len(deviceSiteChanges) when the audit list was truncated).
type rackCascadeOutcome struct {
	cascadeCount      int64
	deviceSiteChanges []map[string]any
	totalAffected     int
}

// replaceRackMembershipAndSlots removes existing membership + slot
// positions and writes the new set. Runs the rack site cascade AFTER the
// membership replace so:
//   - Devices being removed from the rack keep their previous site_id
//     (the invariant only enforces while a device is in a rack).
//   - Per-device priors captured for the audit reflect the final member
//     set (stayers + new additions), not the pre-replace set.
//
// Cascade fires unconditionally when membership is non-empty so that:
//   - A rack moved to fully-unassigned (finalSiteID == nil) still nulls
//     newly-added members' site_id (CascadeRackDeviceSites uses IS
//     DISTINCT FROM, which accepts NULL as a distinct target).
//   - A rack with no site stamped is still a no-op (cascade matches no
//     rows because every member already aligns with NULL).
//
// Returns the cascade outcome so the caller can fold it into the
// activity-log totals + response.
func (s *Service) replaceRackMembershipAndSlots(ctx context.Context, orgID, collectionID int64, deviceIdentifiers []string, slotAssignments []*pb.RackSlot, finalSiteID *int64) (rackCascadeOutcome, error) {
	var out rackCascadeOutcome
	if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, orgID, collectionID); err != nil {
		return out, err
	}

	if len(deviceIdentifiers) > 0 {
		if _, err := s.collectionStore.AddDevicesToCollection(ctx, orgID, collectionID, deviceIdentifiers); err != nil {
			return out, err
		}
		// Capture per-device priors AFTER membership replace so the
		// audit reflects the final member set. Cascade rewrites only
		// devices whose current site_id differs from finalSiteID
		// (IS DISTINCT FROM); the audit lists only those.
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

	// Clear all existing slot positions, then set the new ones. Slot
	// row count is bounded by rack dimensions (max 12x12 = 144).
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

// buildDeviceSiteChanges turns the pre-cascade prior-site map into the
// activity-log metadata shape: one entry per device whose prior site_id
// differs from the cascade target. Devices already at the target site
// are omitted (the cascade UPDATE skips them via IS DISTINCT FROM, and
// the audit log shouldn't list no-op changes). Mirrors the shape used by
// AddDevicesToCollection's cascade audit so consumers can parse both
// uniformly. The returned slice is capped at maxCascadeAuditEntries so
// a multi-thousand-device cascade does not blow up the activity_log
// JSON payload; the caller annotates the metadata with total_affected
// + truncated when applicable.
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
