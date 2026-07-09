package collection

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/devicerollup"
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

// TelemetryCollector fetches latest device metrics for telemetry aggregation.
type TelemetryCollector interface {
	GetLatestDeviceMetrics(ctx context.Context, deviceIDs []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error)
}

// DeviceQueryer provides device-level queries needed by collection stats.
type DeviceQueryer interface {
	GetDeviceIdentifiersByOrgWithFilter(ctx context.Context, orgID int64, filter *interfaces.MinerFilter) ([]string, error)
	GetMinerStateCountsByCollections(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]interfaces.MinerStateCounts, error)
	GetComponentErrorCounts(ctx context.Context, orgID int64, scope interfaces.ComponentErrorScope) ([]interfaces.ComponentErrorCount, error)
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

// resolveDeviceSetSiteScope derives the (site_id, multi_site) scope of a
// multi-device group event (#538) from the touched identifiers: a single
// shared site is stamped so the event surfaces under /{site}/activity; a
// set spanning sites (or mixing sited + site-less devices) is marked
// multi_site so it stays out of the unassigned bucket. Best-effort — a
// resolution error leaves the event org-scoped (nil/false) rather than
// failing the action's fire-and-forget audit log.
func (s *Service) resolveDeviceSetSiteScope(ctx context.Context, orgID int64, identifiers []string) activitymodels.SiteScope {
	// No activity sink, or a group-only service constructed without a
	// siteStore (NewService documents it as optional): the scope would only
	// feed an event we never write, AND this runs AFTER the membership tx has
	// committed — so degrade to an org-scoped event rather than panicking on a
	// nil siteStore for an operation that already succeeded.
	if s.activitySvc == nil || s.siteStore == nil {
		return activitymodels.SiteScope{}
	}
	sites, err := s.siteStore.GetDistinctDeviceSiteIDs(ctx, orgID, identifiers)
	if err != nil {
		slog.Warn("failed to resolve device-set site scope for activity log", "error", err)
		return activitymodels.SiteScope{}
	}
	return activitymodels.ResolveSiteScope(sites)
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

// enforceBuildingRackCapacity rejects placing a rack into buildingID when
// the building's grid (aisles×racks_per_aisle) is already full. netNew is
// the count of racks newly joining the building: 1 for a create or a
// move-in, 0 for a re-save that keeps the rack in the same building.
//
// A building has no "floating beyond capacity" state, so SaveRack — which
// also writes device_set_rack.building_id via rack_info.building_id — must
// honor the same cap that buildings.AssignRacksToBuilding enforces;
// otherwise the assign-side guard is bypassable through the rack save path.
// Skipped when buildingStore is unwired (placement-less test harnesses) or
// the grid is unconfigured (capacity 0). Must run in-tx after the building
// is locked (resolveAndLockRackPlacement) so the count can't race.
func (s *Service) enforceBuildingRackCapacity(ctx context.Context, orgID, buildingID int64, netNew int) error {
	if s.buildingStore == nil || netNew <= 0 {
		return nil
	}
	b, err := s.buildingStore.GetBuilding(ctx, orgID, buildingID)
	if err != nil {
		return err
	}
	capacity := int64(b.Aisles) * int64(b.RacksPerAisle)
	if capacity <= 0 {
		return nil
	}
	existing, err := s.buildingStore.CountRacksInBuilding(ctx, orgID, buildingID)
	if err != nil {
		return err
	}
	if resulting := existing + int64(netNew); resulting > capacity {
		return fleeterror.NewInvalidArgumentErrorf(
			"cannot assign racks: building has %d positions (%d aisles × %d racks per aisle) but %d racks would be assigned",
			capacity, b.Aisles, b.RacksPerAisle, resulting,
		)
	}
	return nil
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

// cascadeRackMembersToPlacement re-stamps device.site_id AND
// device.building_id for every current member of the rack so both stay
// in lockstep with the rack's placement. Callers decide *whether* to
// cascade (a fully-unassigned rack dictates nothing); once they do, this
// fires BOTH columns together so a caller can't update one and forget
// the other — the site-cascaded/building-forgotten defect class that
// recurred across the reparent write paths (see #495 and
// device_placement_invariant_integration_test.go).
//
// nil arguments are meaningful: a site-level rack (site set, building
// NULL) passes building=nil to clear members' stale building_id; an
// unassign transition passes the cleared column as nil. Each underlying
// query is IS DISTINCT FROM guarded, so a column that doesn't actually
// change is a no-op. Returns the number of members whose site row was
// rewritten, for the activity audit.
func (s *Service) cascadeRackMembersToPlacement(ctx context.Context, orgID, collectionID int64, siteID, buildingID *int64) (int64, error) {
	siteCount, err := s.collectionStore.CascadeRackDeviceSites(ctx, collectionID, orgID, siteID)
	if err != nil {
		return 0, err
	}
	if _, err := s.collectionStore.CascadeRackDeviceBuildings(ctx, collectionID, orgID, buildingID); err != nil {
		return 0, err
	}
	return siteCount, nil
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

			if req.Type == pb.CollectionType_COLLECTION_TYPE_RACK {
				// A rack ALWAYS dictates its members' placement — including a
				// fully-unassigned rack (site + building NULL). Members can't
				// keep a direct site/building the rack doesn't have, or the
				// membership tree diverges (a sited miner in a site-less
				// rack). The helper cascades both columns in lockstep; nil
				// placement strips them to match. IS DISTINCT FROM makes it a
				// no-op when a member already matches.
				{
					priors, err := s.collectionStore.GetDeviceSiteIDsByMembership(ctx, collection.Id, info.OrganizationID)
					if err != nil {
						return nil, err
					}
					deviceSiteChanges, totalAffected = buildDeviceSiteChanges(priors, siteID)
					n, err := s.cascadeRackMembersToPlacement(ctx, info.OrganizationID, collection.Id, siteID, buildingID)
					if err != nil {
						return nil, err
					}
					cascadeCount = n
				}
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
			var (
				rackSiteID     *int64
				rackBuildingID *int64
			)
			isRack := collType == pb.CollectionType_COLLECTION_TYPE_RACK
			if isRack {
				placement, err := s.collectionStore.LockRackPlacementForWrite(ctx, req.CollectionId, info.OrganizationID)
				if err != nil {
					return nil, err
				}
				rackSiteID = placement.SiteID
				rackBuildingID = placement.BuildingID
			}
			if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, info.OrganizationID, req.CollectionId); err != nil {
				return nil, err
			}
			if len(deviceIdentifiers) > 0 {
				if _, err := s.collectionStore.AddDevicesToCollection(ctx, info.OrganizationID, req.CollectionId, deviceIdentifiers); err != nil {
					return nil, err
				}
				// A rack ALWAYS dictates its members' placement, including a
				// fully-unassigned rack (site + building NULL) — members
				// can't keep a direct site/building the rack lacks, or the
				// membership tree diverges. nil placement strips members;
				// IS DISTINCT FROM no-ops members that already match.
				if isRack {
					if _, err := s.cascadeRackMembersToPlacement(ctx, info.OrganizationID, req.CollectionId, rackSiteID, rackBuildingID); err != nil {
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
		// Stamp the collection's site (racks under a site/building) so the
		// update lands in /{site}/activity; nil for site-less groups.
		SiteID: collectionSiteID(collection),
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
	// Site for the delete audit row, captured from the in-tx locked placement
	// (not the best-effort prefetch) so a rack move racing the delete can't
	// file the event under the old site. Nil for non-rack collections (groups
	// have no site) and unassigned racks.
	var lockedRackSiteID *int64
	err = s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		lockedRackSiteID = nil // reset in case RunInTx retries the closure
		collType, err := s.collectionStore.GetCollectionType(ctx, info.OrganizationID, req.CollectionId)
		if err != nil {
			return err
		}
		if collType == pb.CollectionType_COLLECTION_TYPE_RACK {
			// Lock the rack FOR UPDATE so concurrent AddDevicesToCollection
			// / SaveRack can't slip a new member or cascade in between our
			// unassign + membership-drop + soft-delete steps.
			placement, err := s.collectionStore.LockRackPlacementForWrite(ctx, req.CollectionId, info.OrganizationID)
			if err != nil {
				return err
			}
			if placement.SiteID != nil {
				sid := *placement.SiteID
				lockedRackSiteID = &sid
			}
			n, err := s.collectionStore.UnassignDeviceSitesByRack(ctx, req.CollectionId, info.OrganizationID)
			if err != nil {
				return err
			}
			siteUnassignedCount = n
			// Building peer of the site unassign: drops device.building_id
			// for members whose value still matched the rack's stamped
			// building. Preserves direct AssignDevicesToBuilding
			// assignments that diverged from the rack.
			if _, err := s.collectionStore.UnassignDeviceBuildingsByRack(ctx, req.CollectionId, info.OrganizationID); err != nil {
				return err
			}
			// Clear device_set_rack placement BEFORE soft-deleting the
			// device_set row so the partial unique index
			// uk_device_set_rack_building_position releases the cell
			// atomically. Without this, the orphan row would keep the
			// cell occupied while ListBuildingRacks hid it — operators
			// would see an empty cell that fails on assign. Scoped to
			// the rack branch because non-rack collection types don't
			// have device_set_rack rows.
			if err := s.collectionStore.ClearRackPlacementForSoftDelete(ctx, info.OrganizationID, req.CollectionId); err != nil {
				return err
			}
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
			// Stamp the site from the in-tx locked placement so a site-scoped
			// rack delete lands in /{site}/activity; nil for site-less groups
			// and unassigned racks.
			SiteID: lockedRackSiteID,
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
		if len(params.Filter.BuildingIDs) > 0 || params.Filter.IncludeNoBuilding ||
			len(params.Filter.ZoneKeys) > 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf("building / zone filters are only supported for rack collections")
		}
		if params.Type != pb.CollectionType_COLLECTION_TYPE_GROUP &&
			(len(params.Filter.SiteIDs) > 0 || params.Filter.IncludeUnassigned) {
			return nil, fleeterror.NewInvalidArgumentErrorf("site filters are only supported for rack or group collections")
		}
	}

	if err := s.validateFilterSites(ctx, info.OrganizationID, params.Filter); err != nil {
		return nil, err
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

// validateFilterSites wraps the shared interfaces.ValidateFilterSites
// helper for the rack-list site_ids filter. Same rationale as
// validateFilterBuildings: enforce per-org ownership so cross-org IDs
// error instead of silently returning empty.
func (s *Service) validateFilterSites(ctx context.Context, orgID int64, filter *interfaces.DeviceSetFilter) error {
	if filter == nil {
		return nil
	}
	return interfaces.ValidateFilterSites(ctx, orgID, filter.SiteIDs, s.siteStore)
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

// AddDevicesToGroupParams is the domain-layer input shape for adding
// devices to a group device set. TargetGroupID must point at a group;
// rack adds must go through AssignDevicesToRack to get atomic prior-rack
// removal + site cascade.
type AddDevicesToGroupParams struct {
	TargetGroupID  int64
	DeviceSelector *commonpb.DeviceSelector
}

// AddDevicesToGroupResult carries the added-row count for the activity
// log + handler response surface.
type AddDevicesToGroupResult struct {
	AddedCount int64
}

// AddDevicesToGroup adds devices to a group device set. Groups are
// org-scoped (devices may span sites) so this skips the rack site
// cascade entirely. Rack targets are rejected with InvalidArgument.
func (s *Service) AddDevicesToGroup(ctx context.Context, params AddDevicesToGroupParams) (*AddDevicesToGroupResult, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.resolveDeviceIdentifiers(ctx, params.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	type txOut struct {
		added []string
		label string
	}
	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		coll, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, params.TargetGroupID)
		if err != nil {
			return nil, err
		}
		if coll.Type != pb.CollectionType_COLLECTION_TYPE_GROUP {
			return nil, fleeterror.NewInvalidArgumentErrorf("target_group_id %d is not a group", params.TargetGroupID)
		}

		added, err := s.collectionStore.AddDevicesToCollectionReturningAdded(ctx, info.OrganizationID, params.TargetGroupID, deviceIdentifiers)
		if err != nil {
			return nil, err
		}

		return &txOut{added: added, label: coll.Label}, nil
	})
	if err != nil {
		return nil, err
	}
	out, ok := result.(*txOut)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	addedCountInt := len(out.added)
	scopeType := collectionScopeType(pb.CollectionType_COLLECTION_TYPE_GROUP)
	addEvent := activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "add_devices",
		Description:    fmt.Sprintf("Add devices to %s: %s", scopeType, out.label),
		ScopeType:      &scopeType,
		ScopeLabel:     &out.label,
		ScopeCount:     &addedCountInt,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	}
	// Scope to the devices whose membership actually changed (not the full
	// requested set): a no-op identifier in another site must not pull the
	// event into that site's feed (#538).
	addEvent.ApplySiteScope(s.resolveDeviceSetSiteScope(ctx, info.OrganizationID, out.added))
	s.logActivity(ctx, addEvent)

	return &AddDevicesToGroupResult{AddedCount: int64(len(out.added))}, nil
}

// RemoveDevicesFromGroupParams is the domain-layer input shape for
// removing devices from a group device set. Rack targets are rejected
// with InvalidArgument; use AssignDevicesToRack (target_rack_id unset)
// to clear rack membership.
type RemoveDevicesFromGroupParams struct {
	TargetGroupID  int64
	DeviceSelector *commonpb.DeviceSelector
}

// RemoveDevicesFromGroupResult carries the removed-row count for the
// activity log + handler response surface.
type RemoveDevicesFromGroupResult struct {
	RemovedCount int64
}

// RemoveDevicesFromGroup removes devices from a group device set.
func (s *Service) RemoveDevicesFromGroup(ctx context.Context, params RemoveDevicesFromGroupParams) (*RemoveDevicesFromGroupResult, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.resolveDeviceIdentifiers(ctx, params.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	type txOut struct {
		removed []string
		label   string
	}
	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		coll, err := s.collectionStore.GetCollection(ctx, info.OrganizationID, params.TargetGroupID)
		if err != nil {
			return nil, err
		}
		if coll.Type != pb.CollectionType_COLLECTION_TYPE_GROUP {
			return nil, fleeterror.NewInvalidArgumentErrorf("target_group_id %d is not a group", params.TargetGroupID)
		}

		removed, err := s.collectionStore.RemoveDevicesFromCollectionReturningRemoved(ctx, info.OrganizationID, params.TargetGroupID, deviceIdentifiers)
		if err != nil {
			return nil, err
		}

		return &txOut{removed: removed, label: coll.Label}, nil
	})
	if err != nil {
		return nil, err
	}
	out, ok := result.(*txOut)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	removedCountInt := len(out.removed)
	scopeType := collectionScopeType(pb.CollectionType_COLLECTION_TYPE_GROUP)
	removeEvent := activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "remove_devices",
		Description:    fmt.Sprintf("Remove devices from %s: %s", scopeType, out.label),
		ScopeType:      &scopeType,
		ScopeLabel:     &out.label,
		ScopeCount:     &removedCountInt,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	}
	// Scope to the devices whose membership actually changed: identifiers that
	// were not members must not pull the event into their sites' feeds (#538).
	removeEvent.ApplySiteScope(s.resolveDeviceSetSiteScope(ctx, info.OrganizationID, out.removed))
	s.logActivity(ctx, removeEvent)

	return &RemoveDevicesFromGroupResult{RemovedCount: int64(len(out.removed))}, nil
}

// uniqueIdentifiers returns the unique entries of ids, preserving no
// particular order. Used to dedupe AssignDevicesToRack response counts
// and the activity-log scope count: the store ops fold duplicates via
// ANY()/ON CONFLICT DO NOTHING, so reporting len(ids) inflates when
// the caller hands us repeats.
func uniqueIdentifiers(ids []string) map[string]struct{} {
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		unique[id] = struct{}{}
	}
	return unique
}

// AssignDevicesToRackParams is the domain-layer input shape for the
// atomic rack reassignment flow. TargetRackID == nil means "clear
// rack membership without re-assigning" (site/building stay intact).
type AssignDevicesToRackParams struct {
	OrgID             int64
	TargetRackID      *int64
	DeviceIdentifiers []string
	// ForceClearConflictingSite proceeds with an add to a site-less
	// (fully-unassigned) rack even when some miners currently have a
	// site, stripping their site/building to match the rack. When false
	// (default) such an add returns Conflicts and writes nothing.
	ForceClearConflictingSite bool
}

// PerDeviceRackConflictReason enumerates why a device blocked an
// AssignDevicesToRack batch.
type PerDeviceRackConflictReason int

const (
	RackConflictReasonUnspecified PerDeviceRackConflictReason = 0
	// RackConflictReasonDeviceLosesSite — the target rack has no site,
	// but the device currently has one, so joining the rack would strip
	// the device's site (and building).
	RackConflictReasonDeviceLosesSite PerDeviceRackConflictReason = 1
)

// PerDeviceRackConflict reports a single device that blocked the batch.
type PerDeviceRackConflict struct {
	DeviceIdentifier string
	Reason           PerDeviceRackConflictReason
}

// AssignDevicesToRackResult carries the per-step row counts the
// activity log + handler response surface.
//
// AssignedCount is "devices whose membership now points at the target
// rack" — includes devices that were already in the target before this
// call. NewlyAssignedCount is the subset that were newly inserted on
// this call (i.e. excludes prior-target membership rows preserved by the
// excludeRackID predicate in RemoveDevicesFromAnyRack). Callers that
// surface user-facing "how many were added" metrics — e.g. the bulk
// importer — must use NewlyAssignedCount; re-imports over already-
// assigned devices would otherwise overstate the count by the size of
// the overlap.
type AssignDevicesToRackResult struct {
	AssignedCount       int64
	NewlyAssignedCount  int64
	RemovedCount        int64
	SiteReassignedCount int64
	// Conflicts is non-empty only when adding to a site-less rack would
	// strip a miner's site and the caller didn't pass
	// ForceClearConflictingSite. When set, no write happened.
	Conflicts []PerDeviceRackConflict
}

// AssignDevicesToRack atomically moves devices into target_rack_id (or
// clears their rack membership when target is unset) inside one
// transaction. The two-step "remove from prior rack, insert into new
// rack" happens under a single tx so a server error / network blip
// can't leave devices without rack membership (the orphan window the
// client-side orchestration had). Cascades device.site_id when the
// target rack's site differs from the device's current site, matching
// AddDevicesToCollection's cascade.
//
// Empty DeviceIdentifiers rejects with InvalidArgument so the caller
// learns up-front instead of getting a 0-row response.
func (s *Service) AssignDevicesToRack(ctx context.Context, params AssignDevicesToRackParams) (*AssignDevicesToRackResult, error) {
	if len(params.DeviceIdentifiers) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("device_identifiers must not be empty")
	}

	type txOut struct {
		assigned          int64
		newlyAssigned     int64
		removed           int64
		siteReassigned    int64
		targetLabel       string
		finalSiteID       *int64
		deviceSiteChanges []map[string]any
		totalAffected     int
		conflicts         []PerDeviceRackConflict
	}
	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		var (
			targetSiteID     *int64
			targetBuildingID *int64
			targetLabel      string
		)
		// Canonical lock order: lock every rack involved in the
		// reparent -- sources + target -- together in ascending
		// device_set.id order via LockRacksForReparent. Locking source
		// and target as one globally sorted set is what keeps two
		// concurrent AssignDevicesToRack calls moving devices in
		// opposite directions between the same rack pair from
		// deadlocking: each tx acquires {rack1, rack2} in the same
		// {1, 2} order regardless of which side is source vs target.
		// Without the pre-pass, the calls race the
		// device_set_membership unique constraint and the loser trips
		// uk_device_set_membership during the INSERT. The subsequent
		// LockRackPlacementForWrite call below still fires for its
		// device_set_rack row + placement read; the parent device_set
		// row it locks is already held by this tx from the pre-pass.
		// Pass 0 in the clear-rack path so the UNION contributes no
		// target row.
		var targetRackID int64
		if params.TargetRackID != nil {
			targetRackID = *params.TargetRackID
		}
		if _, err := s.collectionStore.LockRacksForReparent(ctx, params.OrgID, params.DeviceIdentifiers, targetRackID); err != nil {
			return nil, err
		}

		// Lock + verify target rack so concurrent SaveRack /
		// DeleteCollection on the target can't race us. Site/building
		// locks would invert against AddDevicesToCollection's cascade,
		// so we stay rack-only here.
		//
		// Order: take the row lock BEFORE the label/type read so a
		// concurrent rename can't slip a stale label into the activity
		// log we emit downstream.
		if params.TargetRackID != nil {
			placement, err := s.collectionStore.LockRackPlacementForWrite(ctx, *params.TargetRackID, params.OrgID)
			if err != nil {
				return nil, err
			}
			coll, err := s.collectionStore.GetCollection(ctx, params.OrgID, *params.TargetRackID)
			if err != nil {
				return nil, err
			}
			if coll.Type != pb.CollectionType_COLLECTION_TYPE_RACK {
				return nil, fleeterror.NewInvalidArgumentErrorf("target_rack_id %d is not a rack", *params.TargetRackID)
			}
			targetSiteID = placement.SiteID
			targetBuildingID = placement.BuildingID
			targetLabel = coll.Label
		}

		// Placement-consistency guard for a site-less (fully-unassigned)
		// target rack. Such a rack dictates "no placement", so any added
		// miner that currently has a site OR a building would have it
		// stripped to NULL. Detect those up-front: without force, reject
		// with the conflict list and write NOTHING; with force, the strip
		// happens after the add below. A rack WITH a site/building doesn't
		// need this — the CascadeAdded* queries already bring members into
		// line with the rack's placement.
		if params.TargetRackID != nil && targetSiteID == nil && targetBuildingID == nil {
			withPlacement, err := s.collectionStore.FindDevicesWithSiteOrBuilding(ctx, params.OrgID, params.DeviceIdentifiers)
			if err != nil {
				return nil, err
			}
			if len(withPlacement) > 0 && !params.ForceClearConflictingSite {
				sort.Strings(withPlacement)
				conflicts := make([]PerDeviceRackConflict, 0, len(withPlacement))
				for _, id := range withPlacement {
					conflicts = append(conflicts, PerDeviceRackConflict{
						DeviceIdentifier: id,
						Reason:           RackConflictReasonDeviceLosesSite,
					})
				}
				return &txOut{conflicts: conflicts}, nil
			}
		}

		// Clear existing rack membership for the given devices regardless
		// of which rack they sit in EXCEPT the target rack. Excluding
		// the target preserves the membership row + rack_slot child for
		// devices that are already in the target rack (a same-rack
		// re-add would otherwise cascade-drop the rack_slot row). This
		// is the half of the operation that previously lived in the
		// client-side RemoveDevicesFromDeviceSet call; bundling it into
		// the tx is what closes the orphan window described in #420.
		removed, err := s.collectionStore.RemoveDevicesFromAnyRack(ctx, params.OrgID, params.DeviceIdentifiers, targetRackID)
		if err != nil {
			return nil, err
		}

		var (
			assigned          int64
			newlyAssigned     int64
			siteReassigned    int64
			deviceSiteChanges []map[string]any
			totalAffected     int
		)
		if params.TargetRackID != nil {
			// AddDevicesToCollection uses ON CONFLICT DO NOTHING, so its
			// return is only newly-inserted rows. The documented
			// AssignedCount contract is "devices whose membership now
			// points at target_rack_id" — which includes devices that
			// were already in the target before this call (target-rack
			// rows are preserved by RemoveDevicesFromAnyRack's
			// excludeRackID predicate). Missing devices (not present in
			// our DB at all) are silently skipped by the store layer,
			// matching pre-PR behavior; we count the unique requested
			// identifiers as the defensible approximation. The store's
			// return value — only newly-inserted rows — is the
			// "newly added" half of the contract, exposed separately for
			// callers that need to match the old AddDevicesToCollection
			// semantics (e.g. the bulk importer's devices_assigned
			// counter, which would otherwise inflate on re-imports).
			added, err := s.collectionStore.AddDevicesToCollection(ctx, params.OrgID, *params.TargetRackID, params.DeviceIdentifiers)
			if err != nil {
				return nil, err
			}
			newlyAssigned = added
			assigned = int64(len(uniqueIdentifiers(params.DeviceIdentifiers)))
			// Site cascade fires when the rack has a site OR a building.
			// A rack in a building inherits that building's site (NULL
			// for an unassigned building), so added devices must follow
			// even when targetSiteID is nil — otherwise device.site_id
			// would disagree with the building_id stamped below.
			// CascadeAddedDeviceSites sets device.site_id to the rack's
			// site (NULL included) and self-guards against
			// fully-unassigned racks. Fully-unassigned racks (no site,
			// no building) skip the cascade to preserve direct
			// device.site_id assignments.
			if targetSiteID != nil || targetBuildingID != nil {
				// Capture per-device priors BEFORE the cascade rewrites
				// device.site_id, so the activity audit reflects the
				// implicit site reassignment. Mirrors the CreateCollection
				// cascade-audit path so audit consumers can treat both
				// event types uniformly.
				priors, err := s.collectionStore.GetDeviceSiteIDsByMembership(ctx, *params.TargetRackID, params.OrgID)
				if err != nil {
					return nil, err
				}
				deviceSiteChanges, totalAffected = buildDeviceSiteChanges(priors, targetSiteID)
				c, err := s.collectionStore.CascadeAddedDeviceSites(ctx, params.OrgID, *params.TargetRackID, params.DeviceIdentifiers)
				if err != nil {
					return nil, err
				}
				siteReassigned = c
			}
			// Building cascade — paired with the site cascade above so
			// device.building_id stays in lockstep with the rack's
			// building. Fires independently of targetSiteID because a
			// rack can have a building without a stamped site
			// (building.site_id is denormalized onto the rack, not
			// required). No-ops when the rack has no building.
			if _, err := s.collectionStore.CascadeAddedDeviceBuildings(ctx, params.OrgID, *params.TargetRackID, params.DeviceIdentifiers); err != nil {
				return nil, err
			}

			// Site-less target rack: the CascadeAdded* gate above skips
			// fully-unassigned racks to preserve direct assignments, so we
			// strip the added members' site/building here instead. Reached
			// only past the conflict pre-check — i.e. nothing had a site,
			// or the caller forced it. Idempotent (no-op when already
			// clear); count the stripped rows as the site reassignment.
			if targetSiteID == nil && targetBuildingID == nil {
				stripped, err := s.collectionStore.ClearDeviceSitesAndBuildings(ctx, params.OrgID, params.DeviceIdentifiers)
				if err != nil {
					return nil, err
				}
				siteReassigned = stripped
			}
		}

		return &txOut{
			assigned:          assigned,
			newlyAssigned:     newlyAssigned,
			removed:           removed,
			siteReassigned:    siteReassigned,
			targetLabel:       targetLabel,
			finalSiteID:       targetSiteID,
			deviceSiteChanges: deviceSiteChanges,
			totalAffected:     totalAffected,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	out, ok := result.(*txOut)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	// Conflict short-circuit: the tx wrote nothing (it returned before
	// the remove/add), so surface the conflicts without logging an
	// activity event. The caller confirms and retries with force.
	if len(out.conflicts) > 0 {
		return &AssignDevicesToRackResult{Conflicts: out.conflicts}, nil
	}

	info, _ := session.GetInfo(ctx)
	var (
		userID, username                *string
		orgIDPtr                        *int64
		eventDescription, eventScopeStr string
	)
	if info != nil {
		userID = &info.ExternalUserID
		username = &info.Username
		orgIDPtr = &info.OrganizationID
	}
	if params.TargetRackID != nil {
		eventScopeStr = "rack"
		eventDescription = fmt.Sprintf("Assigned devices to rack: %s", out.targetLabel)
	} else {
		eventDescription = "Cleared devices from rack"
	}
	// ScopeCount is the number of unique devices the caller asked to
	// touch, not the sum of per-step row counts (assigned + removed
	// can double-count devices that were both removed from a prior
	// rack and added to a new one). Dedupe identifiers because the
	// store ops collapse duplicates via ANY()/ON CONFLICT but the
	// activity scope count would otherwise inflate.
	assignedInt := len(uniqueIdentifiers(params.DeviceIdentifiers))
	event := activitymodels.Event{
		Category:       activitymodels.CategoryCollection,
		Type:           "assign_devices_to_rack",
		Description:    eventDescription,
		ScopeCount:     &assignedInt,
		UserID:         userID,
		Username:       username,
		OrganizationID: orgIDPtr,
		SiteID:         out.finalSiteID,
	}
	if eventScopeStr != "" {
		event.ScopeType = &eventScopeStr
		event.ScopeLabel = &out.targetLabel
	}
	// Cascade-audit metadata mirrors the CreateCollection path so audit
	// consumers don't need to special-case this event when reconstructing
	// implicit device.site_id reassignments.
	if out.siteReassigned > 0 || out.totalAffected > 0 {
		meta := map[string]any{
			"site_cascade":          true,
			"final_site_id":         out.finalSiteID,
			"site_reassigned_count": out.siteReassigned,
		}
		if len(out.deviceSiteChanges) > 0 {
			meta["device_site_changes"] = out.deviceSiteChanges
		}
		if out.totalAffected > 0 {
			meta["total_affected"] = out.totalAffected
			if out.totalAffected > maxCascadeAuditEntries {
				meta["truncated"] = true
			}
		}
		event.Metadata = meta
	}
	s.logActivity(ctx, event)

	return &AssignDevicesToRackResult{
		AssignedCount:       out.assigned,
		NewlyAssignedCount:  out.newlyAssigned,
		RemovedCount:        out.removed,
		SiteReassignedCount: out.siteReassigned,
	}, nil
}

type ListCollectionMembersParams struct {
	CollectionID int64
	PageSize     int32
	PageToken    string
	Filter       *interfaces.DeviceSetFilter
}

func (s *Service) ListCollectionMembersDomain(ctx context.Context, params ListCollectionMembersParams) (*pb.ListCollectionMembersResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Verify collection exists and belongs to org
	belongs, err := s.collectionStore.CollectionBelongsToOrg(ctx, params.CollectionID, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", params.CollectionID)
	}

	pageSize := validatePageSize(params.PageSize)

	if err := s.validateFilterSites(ctx, info.OrganizationID, params.Filter); err != nil {
		return nil, err
	}

	members, nextPageToken, err := s.collectionStore.ListCollectionMembers(ctx, info.OrganizationID, params.CollectionID, pageSize, params.PageToken, params.Filter)
	if err != nil {
		return nil, err
	}

	return &pb.ListCollectionMembersResponse{Members: members, NextPageToken: nextPageToken}, nil
}

// ListCollectionMembers returns all members of a collection.
func (s *Service) ListCollectionMembers(ctx context.Context, req *pb.ListCollectionMembersRequest) (*pb.ListCollectionMembersResponse, error) {
	return s.ListCollectionMembersDomain(ctx, ListCollectionMembersParams{
		CollectionID: req.CollectionId,
		PageSize:     req.PageSize,
		PageToken:    req.PageToken,
	})
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
		// Stamp the rack's site so this single-site event lands in the
		// site's activity feed rather than the unassigned bucket. Nil when
		// the rack itself is unassigned (genuinely no site).
		SiteID: collectionSiteID(coll),
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
		// See SetRackSlotPosition: stamp the rack's site (nil if unassigned).
		SiteID: collectionSiteID(coll),
	})

	return &pb.ClearRackSlotPositionResponse{}, nil
}

// collectionSiteID returns the collection's effective site id as a nil-able
// pointer, preserving the distinction between "assigned to site N" and
// "unassigned" (nil). It reads from Placement, which GetCollection populates
// (and which reflects the building-derived site); TypeDetails.RackInfo is NOT
// filled by GetCollection, so it must not be relied on here.
func collectionSiteID(coll *pb.DeviceCollection) *int64 {
	site := coll.GetPlacement().GetSite()
	if site == nil {
		return nil
	}
	id := site.GetId()
	return &id
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
				fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
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
	componentErrors, err := s.deviceQueryer.GetComponentErrorCounts(ctx, info.OrganizationID, interfaces.ComponentErrorScope{
		Kind: interfaces.ComponentErrorScopeCollections,
		IDs:  req.CollectionIds,
	})
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
		componentErrorMap[componentKey{ce.ScopeID, ce.ComponentType}] = ce.DeviceCount
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

		telemetryIDs := devicerollup.ToDeviceIdentifiers(deviceIDs)
		rollup := devicerollup.AggregateLatestMetrics(telemetryData, telemetryIDs)
		cs.ReportingCount = rollup.ReportingCount
		cs.HashrateReportingCount = rollup.HashrateReportingCount
		cs.PowerReportingCount = rollup.PowerReportingCount
		cs.EfficiencyReportingCount = rollup.EfficiencyReportingCount
		cs.TemperatureReportingCount = rollup.TemperatureReportingCount
		cs.TotalHashrateThs = rollup.TotalHashrateThs
		cs.TotalPowerKw = rollup.TotalPowerKw
		cs.AvgEfficiencyJth = rollup.AvgEfficiencyJth
		cs.MinTemperatureC = rollup.MinTemperatureC
		cs.MaxTemperatureC = rollup.MaxTemperatureC

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

	// Capacity guard. A rack has no "floating" members — every miner is
	// expected to occupy a slot — so membership is bounded by the grid
	// (rows×columns, each 1..maxRackDimension, validated above). Reject an
	// over-capacity selection here, after the selector resolves (an
	// all-mode selector can resolve to the full fleet), so the membership
	// write never persists more miners than the rack can ever hold.
	if capacity := int(rackInfo.Rows) * int(rackInfo.Columns); len(deviceIdentifiers) > capacity {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"cannot assign %d miners to a rack with %d slot(s) (%d×%d)",
			len(deviceIdentifiers), capacity, rackInfo.Rows, rackInfo.Columns,
		)
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
			buildingChanged bool
		)

		// The rack-locking pre-pass (LockRacksForReparent) lives INSIDE the
		// path helpers below rather than at the top of the tx. On a placement
		// path it runs AFTER the target site/building lock, preserving the
		// canonical site/building -> rack order (see resolveAndLockRackPlacement
		// and AssignRacksToBuilding); on the omitted-placement update path there
		// is no site/building lock and it is simply the first lock taken. Either
		// way it acquires every source + target rack in one ascending
		// device_set.id set ahead of the per-target LockRackPlacementForWrite
		// and the replaceRackMembershipAndSlots deletes — which is what keeps
		// the RemoveDevicesFromAnyRack delete from deadlocking against a
		// concurrent rack save moving devices the opposite way.
		if isUpdate {
			res, err := s.saveRackUpdate(ctx, info, req, rackInfo, deviceIdentifiers)
			if err != nil {
				return nil, err
			}
			collectionID = res.collectionID
			finalSiteID = res.finalSiteID
			finalBuildingID = res.finalBuildingID
			finalZone = res.finalZone
			siteChanged = res.siteChanged
			buildingChanged = res.buildingChanged
		} else {
			res, err := s.saveRackCreate(ctx, info, req, rackInfo, deviceIdentifiers)
			if err != nil {
				return nil, err
			}
			collectionID = res.collectionID
			finalSiteID = res.finalSiteID
			finalBuildingID = res.finalBuildingID
			finalZone = res.finalZone
			// Create path: every member is new, so cascade aligns them
			// with the rack's site/building when one is stamped.
			siteChanged = finalSiteID != nil
			buildingChanged = finalBuildingID != nil
		}

		// Cascade runs after membership replace so it touches only the
		// final member set; removed devices keep their prior site_id.
		cascade, err := s.replaceRackMembershipAndSlots(ctx, info.OrganizationID, collectionID, deviceIdentifiers, req.SlotAssignments, finalSiteID, finalBuildingID)
		if err != nil {
			return nil, err
		}
		// A placement transition (site OR building) or a non-zero cascade
		// count means the move touched member placement → record it.
		cascadeApplied := siteChanged || buildingChanged || cascade.cascadeCount > 0
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
// shape and slot bounds. Zone is an optional free-text sub-building label
// (nullable column, no placement dependency), so it is never required —
// including for a rack that belongs to a building.
func validateSaveRackRequest(req *pb.SaveRackRequest, rackInfo *pb.RackInfo) error {
	if rackInfo == nil {
		return fleeterror.NewInvalidArgumentError("rack_info is required")
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

// lockSourceRacksForReparent locks every source rack the members currently
// sit in -- and, when targetRackID > 0, the target rack too -- in one
// ascending device_set.id FOR UPDATE acquisition. It must run BEFORE the
// per-target LockRackPlacementForWrite and the membership deletes; that
// ordering is what keeps the RemoveDevicesFromAnyRack delete from deadlocking
// against a concurrent rack save moving devices the opposite way. On a
// placement path the caller takes the site/building lock first and calls this
// after it, preserving the canonical site/building -> rack order; the
// omitted-placement path takes no site/building lock, so this is simply its
// first lock. targetRackID 0 (the create path, where the rack doesn't exist
// yet) contributes no target row. No-ops on an empty member set, which
// touches no source racks.
func (s *Service) lockSourceRacksForReparent(ctx context.Context, orgID int64, deviceIdentifiers []string, targetRackID int64) error {
	if len(deviceIdentifiers) == 0 {
		return nil
	}
	_, err := s.collectionStore.LockRacksForReparent(ctx, orgID, deviceIdentifiers, targetRackID)
	return err
}

// saveRackCreate runs the SaveRack create branch in-tx: resolve placement,
// then insert device_set + device_set_rack rows.
func (s *Service) saveRackCreate(ctx context.Context, info *session.Info, req *pb.SaveRackRequest, rackInfo *pb.RackInfo, deviceIdentifiers []string) (*saveRackCreatePathResult, error) {
	newSiteID, newBuildingID, err := s.resolveAndLockRackPlacement(ctx, info.OrganizationID, rackInfo)
	if err != nil {
		return nil, err
	}

	// A brand-new rack placed into a building is always a net-new member.
	if newBuildingID != nil {
		if err := s.enforceBuildingRackCapacity(ctx, info.OrganizationID, *newBuildingID, 1); err != nil {
			return nil, err
		}
	}

	// targetRackID 0: the rack doesn't exist yet, so the pre-pass locks only
	// the source racks the members currently sit in. Runs after placement
	// resolution above and before the membership writes below.
	if err := s.lockSourceRacksForReparent(ctx, info.OrganizationID, deviceIdentifiers, 0); err != nil {
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
	buildingChanged bool
}

// saveRackUpdate runs the SaveRack update branch: validate ownership,
// lock site/building/rack in canonical order, derive the final zone,
// persist placement, and flag siteChanged for the downstream cascade.
func (s *Service) saveRackUpdate(ctx context.Context, info *session.Info, req *pb.SaveRackRequest, rackInfo *pb.RackInfo, deviceIdentifiers []string) (*saveRackUpdatePathResult, error) {
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
		// rack lock alone serializes the no-op cascade. The source-rack
		// pre-pass still runs (canonical: racks before the target placement
		// row) so a member moved here from another rack can't deadlock.
		if err := s.lockSourceRacksForReparent(ctx, info.OrganizationID, deviceIdentifiers, collectionID); err != nil {
			return nil, err
		}
		current, err = s.collectionStore.LockRackPlacementForWrite(ctx, collectionID, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		newSiteID = current.SiteID
		newBuildingID = current.BuildingID
	} else {
		// Placement intent supplied; resolve and lock site/building first,
		// then every source + target rack in ascending device_set.id order,
		// then the target placement row (canonical site/building -> rack).
		newSiteID, newBuildingID, err = s.resolveAndLockRackPlacement(ctx, info.OrganizationID, rackInfo)
		if err != nil {
			return nil, err
		}
		if err := s.lockSourceRacksForReparent(ctx, info.OrganizationID, deviceIdentifiers, collectionID); err != nil {
			return nil, err
		}
		current, err = s.collectionStore.LockRackPlacementForWrite(ctx, collectionID, info.OrganizationID)
		if err != nil {
			return nil, err
		}
	}

	// Capacity guard: only a move INTO a different building is a net-new
	// member. A re-save that keeps the rack in its current building (or the
	// omitted-placement branch, where newBuildingID == current.BuildingID)
	// is net-zero and must not be rejected just because the building is
	// already at capacity.
	if newBuildingID != nil && !int64PtrEqual(current.BuildingID, newBuildingID) {
		if err := s.enforceBuildingRackCapacity(ctx, info.OrganizationID, *newBuildingID, 1); err != nil {
			return nil, err
		}
	}

	// Zone is building-scoped: clear it when the rack leaves or crosses
	// buildings. Otherwise the submitted zone is authoritative — a client that
	// manages placement (sends an explicit site/building, including 0 to
	// unassign) seeds the current zone into its edit form, so an unedited save
	// keeps it and an explicit blank clears it (zone is optional). Only a
	// caller that OMITS placement entirely (legacy: no site_id/building_id)
	// keeps the preserve-on-empty behavior, so a metadata-only update can't
	// silently wipe the zone of a rack that stays in its building.
	finalZone := rackInfo.GetZone()
	leavingBuilding := current.BuildingID != nil && newBuildingID == nil
	crossingBuildings := current.BuildingID != nil && newBuildingID != nil && !int64PtrEqual(current.BuildingID, newBuildingID)
	switch {
	case leavingBuilding || crossingBuildings:
		finalZone = ""
	case rackPlacementOmitted(rackInfo) && finalZone == "" && newBuildingID != nil:
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
		buildingChanged: !int64PtrEqual(current.BuildingID, newBuildingID),
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
func (s *Service) replaceRackMembershipAndSlots(ctx context.Context, orgID, collectionID int64, deviceIdentifiers []string, slotAssignments []*pb.RackSlot, finalSiteID, finalBuildingID *int64) (rackCascadeOutcome, error) {
	var out rackCascadeOutcome
	if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, orgID, collectionID); err != nil {
		return out, err
	}

	// A rack ALWAYS dictates its members' placement — a placed rack stamps
	// its site/building, a fully-unassigned rack strips members to NULL.
	// Members can't keep a direct site/building the rack lacks, or the
	// membership tree diverges. The helper cascades both columns in
	// lockstep; its IS-DISTINCT-FROM-guarded queries no-op the column that
	// didn't change, so cascadeCount stays accurate.
	if len(deviceIdentifiers) > 0 {
		// Clear any prior rack membership for these devices (excluding this
		// rack) before adding them, so a device seeded from another rack
		// MOVES here instead of tripping idx_one_rack_per_device. Mirrors
		// AssignDevicesToRack's move semantics within the same transaction —
		// no orphan window. A no-op for the edit flow, which never sends
		// devices already sitting in a different rack.
		if _, err := s.collectionStore.RemoveDevicesFromAnyRack(ctx, orgID, deviceIdentifiers, collectionID); err != nil {
			return out, err
		}
		if _, err := s.collectionStore.AddDevicesToCollection(ctx, orgID, collectionID, deviceIdentifiers); err != nil {
			return out, err
		}
		priors, err := s.collectionStore.GetDeviceSiteIDsByMembership(ctx, collectionID, orgID)
		if err != nil {
			return out, err
		}
		out.deviceSiteChanges, out.totalAffected = buildDeviceSiteChanges(priors, finalSiteID)
		n, err := s.cascadeRackMembersToPlacement(ctx, orgID, collectionID, finalSiteID, finalBuildingID)
		if err != nil {
			return out, err
		}
		out.cascadeCount = n
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
