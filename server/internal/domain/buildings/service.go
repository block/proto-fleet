// Package buildings is the domain layer for the BuildingService RPC
// surface. CRUD + cascade-unassign-on-delete; site assignment lives on
// SiteService.AssignBuildingsToSite where the cross-collection
// invariant is enforced.
package buildings

import (
	"context"
	"fmt"
	"sort"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/devicerollup"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for buildings activity logs.
const (
	eventBuildingCreated      = "building.created"
	eventBuildingUpdated      = "building.updated"
	eventBuildingDeleted      = "building.deleted"
	eventRackAssignedBuilding = "building.rack_assigned"
)

// Service is the domain entry point for building CRUD.
type Service struct {
	store           interfaces.BuildingStore
	siteStore       interfaces.SiteStore
	collectionStore interfaces.CollectionStore
	deviceQueryer   devicerollup.DeviceQueryer
	telemetry       devicerollup.TelemetryCollector
	transactor      interfaces.Transactor
	activitySvc     *activity.Service
}

// NewService wires a BuildingStore, SiteStore (for site existence
// validation), CollectionStore (for the rack placement write path
// shared with SaveRack), Transactor (for the delete cascade), and the
// activity Service used for fire-and-forget audit logs. activitySvc
// may be nil in tests or environments where activity logging is
// disabled.
//
// deviceQueryer and telemetry power GetBuildingStats only. Either may
// be nil in test setups that don't exercise the stats RPC;
// GetBuildingStats returns an internal error in that case.
func NewService(
	store interfaces.BuildingStore,
	siteStore interfaces.SiteStore,
	collectionStore interfaces.CollectionStore,
	deviceQueryer devicerollup.DeviceQueryer,
	telemetry devicerollup.TelemetryCollector,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:           store,
		siteStore:       siteStore,
		collectionStore: collectionStore,
		deviceQueryer:   deviceQueryer,
		telemetry:       telemetry,
		transactor:      transactor,
		activitySvc:     activitySvc,
	}
}

// CreateBuilding inserts a new building. If site_id is set, validates
// the site exists in the org.
func (s *Service) CreateBuilding(ctx context.Context, params models.CreateParams) (*models.Building, error) {
	if !params.DefaultRackOrderIndex.Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid default_rack_order_index")
	}
	if err := validateLayoutBounds(params.Aisles, params.RacksPerAisle); err != nil {
		return nil, err
	}

	var b *models.Building
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock the parent site row when specified so a concurrent
		// DeleteSite can't soft-delete it between the live-site check
		// and the building insert. LockSiteForWrite returns NotFound
		// when the site is missing/already soft-deleted, which we
		// surface directly.
		if params.SiteID != nil && *params.SiteID > 0 {
			if err := s.siteStore.LockSiteForWrite(txCtx, params.OrgID, *params.SiteID); err != nil {
				return err
			}
		}
		created, err := s.store.CreateBuilding(txCtx, params)
		if err != nil {
			return err
		}
		b = created
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER tx commits — RunInTx may retry the closure
	// on serialization failures, so an in-closure Log would duplicate.
	orgID := params.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingCreated,
		OrganizationID: &orgID,
		SiteID:         b.SiteID,
		Description:    fmt.Sprintf("Created building %q (id=%d)", b.Name, b.ID),
		Metadata: map[string]any{
			"building_id":   b.ID,
			"building_name": b.Name,
			"site_id":       b.SiteID,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return b, nil
}

// GetBuilding returns the live building or NotFound.
func (s *Service) GetBuilding(ctx context.Context, orgID, id int64) (*models.Building, error) {
	return s.store.GetBuilding(ctx, orgID, id)
}

// ListBuildings returns the filtered building list with rack counts.
func (s *Service) ListBuildings(ctx context.Context, filter models.ListFilter) ([]models.BuildingWithCounts, error) {
	// The proto oneof enforces mutual exclusion structurally; this is
	// a defense-in-depth guard for any non-proto caller.
	if filter.SiteID != nil && *filter.SiteID > 0 && filter.UnassignedOnly {
		return nil, fleeterror.NewInvalidArgumentError("site_id and unassigned_only are mutually exclusive")
	}
	return s.store.ListBuildings(ctx, filter)
}

// UpdateBuilding mutates the building's mutable fields. Site
// assignment is intentionally not handled here.
//
// Layout shrinks (decreasing aisles or racks_per_aisle below current)
// are validated against existing rack placements inside the same tx:
// any positioned rack whose (aisle, position) would fall outside the
// new bounds aborts the update with InvalidArgument. Without this
// guard, the FE silently drops out-of-bounds entries during render and
// the stale rows persist indefinitely.
func (s *Service) UpdateBuilding(ctx context.Context, params models.UpdateParams) (*models.Building, error) {
	if !params.DefaultRackOrderIndex.Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid default_rack_order_index")
	}
	if err := validateLayoutBounds(params.Aisles, params.RacksPerAisle); err != nil {
		return nil, err
	}
	var b *models.Building
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock the building row first so a concurrent
		// AssignRacksToBuilding can't race us into orphaned-position
		// state between the bounds check and the update.
		if err := s.siteStore.LockBuildingForWrite(txCtx, params.OrgID, params.ID); err != nil {
			return err
		}
		current, err := s.store.GetBuilding(txCtx, params.OrgID, params.ID)
		if err != nil {
			return err
		}
		// Bounds-shrink validation only runs when at least one
		// dimension is being reduced; growth never orphans rows.
		// Uses ListRacksOutsideBuildingBounds (unbounded by design)
		// instead of the paged ListBuildingRacks so a tail row past
		// the page-size cap can't silently bypass the guard.
		if params.Aisles < current.Aisles || params.RacksPerAisle < current.RacksPerAisle {
			orphans, err := s.store.ListRacksOutsideBuildingBounds(txCtx, params.OrgID, params.ID, params.Aisles, params.RacksPerAisle)
			if err != nil {
				return err
			}
			if len(orphans) > 0 {
				r := orphans[0]
				return fleeterror.NewInvalidArgumentErrorf(
					"cannot shrink layout: rack %q is at aisle %d, position %d which is outside the new %d aisles × %d racks-per-aisle bounds; unplace it first",
					r.RackLabel, *r.AisleIndex+1, *r.PositionInAisle+1, params.Aisles, params.RacksPerAisle,
				)
			}
		}
		updated, err := s.store.UpdateBuilding(txCtx, params)
		if err != nil {
			return err
		}
		b = updated
		return nil
	})
	if err != nil {
		return nil, err
	}

	orgID := params.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingUpdated,
		OrganizationID: &orgID,
		SiteID:         b.SiteID,
		Description:    fmt.Sprintf("Updated building %q (id=%d)", b.Name, b.ID),
		Metadata: map[string]any{
			"building_id":   b.ID,
			"building_name": b.Name,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return b, nil
}

// ListBuildingRacksDefaultPageSize / ListBuildingRacksMaxPageSize
// mirror the collection-service constants. Default matches the
// device-list ergonomics (50 rows/page); max bounds the buf.validate
// cap on ListBuildingRacksRequest.page_size. Callers that need the
// full working set (e.g. ManageBuildingModal seeding) loop through
// pages client-side.
const (
	ListBuildingRacksDefaultPageSize = int32(50)
	ListBuildingRacksMaxPageSize     = int32(1000)
	// MaxRacksPerStatsRequest caps the total number of racks GetBuildingStats
	// will walk before bailing. 10k racks ≈ 100×100 layout (the schema
	// validation ceiling) — anything higher signals a runaway. Without the
	// cap, a corrupted page cursor or unintended unbounded data could spin
	// GetBuildingStats indefinitely at every 60s poll tick.
	MaxRacksPerStatsRequest = 10_000
	// MaxDevicesPerStatsResponse caps the number of device identifiers
	// echoed in GetBuildingStats responses. The FE uses this list to scope
	// downstream telemetry + component-error fetches, so we ship every ID
	// for normal buildings; the cap is a defensive ceiling against
	// pathological orgs where a single building has hundreds of thousands
	// of miners (response payload + FE memory blow-up). 50k devices ≈ 5×
	// the largest expected building.
	MaxDevicesPerStatsResponse = 50_000
)

// ListBuildingRacks returns one page of racks currently assigned to a
// building with their grid placement. Verifies the building exists in
// the org before returning so a stale building_id surfaces as NotFound
// rather than an empty list (which would look identical to "no racks
// yet").
//
// `pageSize` clamps to (0, ListBuildingRacksMaxPageSize]; 0 defaults
// to ListBuildingRacksDefaultPageSize. `pageToken` is an opaque
// cursor from a prior response — empty string starts at the first
// page. Returns the next page token (empty when the caller has
// reached the last page).
func (s *Service) ListBuildingRacks(ctx context.Context, orgID, buildingID int64, pageSize int32, pageToken string) ([]models.BuildingRack, string, error) {
	if pageSize <= 0 {
		pageSize = ListBuildingRacksDefaultPageSize
	}
	if pageSize > ListBuildingRacksMaxPageSize {
		pageSize = ListBuildingRacksMaxPageSize
	}
	if _, err := s.store.GetBuilding(ctx, orgID, buildingID); err != nil {
		return nil, "", err
	}
	return s.store.ListBuildingRacks(ctx, orgID, buildingID, pageSize, pageToken)
}

// assignRacksToBuildingTx carries the per-attempt counters, resolved
// site ids, and cascaded/positioned rack-id slices out of the
// RunInTxWithResult closure. Declared at package scope so a tx retry
// (SQLTransactor serialization / deadlock failure) starts each attempt
// from zero — the closure constructs a fresh struct on every call.
type assignRacksToBuildingTx struct {
	siteReassignedDeviceCount int64
	targetSiteID              *int64
	cascadeRackIDs            []int64
	positionedRackIDs         []int64
	fallbackSiteID            *int64
}

// AssignRacksToBuilding sets the building_id (and optional grid
// placement) of every rack in the batch. Runs in a single transaction:
//
//  1. Lock the target building once (when assigning), canonical lock
//     order is building -> rack(s).
//  2. Validate every entry up-front (paired position fields, in-bounds
//     aisle/position). The whole batch rejects on any invalid entry.
//  3. For each rack (sorted by id for deadlock-safe lock order):
//     a. Lock the rack row and read current placement.
//     b. Resolve the new site_id from the target building (or preserve
//     current.SiteID on building-only unassign).
//     c. Compute final zone (clear on leave/cross building).
//     d. Persist site_id + building_id + zone via UpdateRackPlacement.
//     e. Cascade descendant device.site_id when the site changes; sum
//     the per-rack counts into the aggregate result.
//     f. Write the grid cell via SetRackBuildingPosition when the rack
//     is assigned to a building (placed or explicitly cleared).
//
// If any rack fails, the whole tx rolls back and no row is touched.
func (s *Service) AssignRacksToBuilding(ctx context.Context, params models.AssignRacksToBuildingParams) (*models.AssignRacksToBuildingResult, error) {
	if len(params.Racks) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("racks must not be empty")
	}

	// Reject duplicate rack_ids up front. The handler / proto layers
	// don't enforce uniqueness, and the per-entry grid-cell write would
	// silently clobber an earlier same-rack entry inside the tx —
	// surface the inconsistency to the caller instead.
	seenRackIDs := make(map[int64]struct{}, len(params.Racks))
	for _, rp := range params.Racks {
		if _, dup := seenRackIDs[rp.RackID]; dup {
			return nil, fleeterror.NewInvalidArgumentErrorf("duplicate rack_id %d in racks", rp.RackID)
		}
		seenRackIDs[rp.RackID] = struct{}{}
	}

	// Per-entry validation runs before any I/O so a bad request fails
	// fast without partial work. Defense-in-depth — the proto CEL rule
	// also enforces position pairing.
	for _, rp := range params.Racks {
		if rp.RackID <= 0 {
			return nil, fleeterror.NewInvalidArgumentError("rack_id must be > 0")
		}
		if (rp.AisleIndex == nil) != (rp.PositionInAisle == nil) {
			return nil, fleeterror.NewInvalidArgumentError("aisle_index and position_in_aisle must both be set or both unset")
		}
		if rp.AisleIndex != nil && params.TargetBuildingID == nil {
			return nil, fleeterror.NewInvalidArgumentError("a grid cell (aisle_index, position_in_aisle) requires a target_building_id")
		}
		if rp.AisleIndex != nil && *rp.AisleIndex < 0 {
			return nil, fleeterror.NewInvalidArgumentError("aisle_index must be >= 0")
		}
		if rp.PositionInAisle != nil && *rp.PositionInAisle < 0 {
			return nil, fleeterror.NewInvalidArgumentError("position_in_aisle must be >= 0")
		}
	}

	// Sort rack entries by id for stable lock order so two concurrent
	// AssignRacksToBuilding calls overlapping on a rack set can't
	// deadlock.
	racks := make([]models.RackPlacementParam, len(params.Racks))
	copy(racks, params.Racks)
	sort.Slice(racks, func(i, j int) bool { return racks[i].RackID < racks[j].RackID })

	// Counters, cascaded/positioned id slices, and the resolved
	// target/fallback SiteID all live inside the RunInTxWithResult
	// closure so a SQLTransactor retry (serialization / deadlock
	// failure on the first attempt) starts from zero on every attempt.
	// The returned struct reflects only the COMMITTED attempt.
	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		var (
			siteReassignedDeviceCount int64
			targetSiteID              *int64
			cascadeRackIDs            []int64
			positionedRackIDs         []int64
			// For a building-only unassign (TargetBuildingID == nil) of a
			// single rack, capture the rack's current SiteID so the activity
			// log preserves "the site this rack lives in" instead of nil
			// (which would make the event look site-less).
			fallbackSiteID *int64
		)
		// Lock the target building first (canonical lock order:
		// building -> rack). Skip when unassigning — there is no
		// building row to lock — but each rack still gets row-locked
		// below.
		var targetBuilding *models.Building
		if params.TargetBuildingID != nil {
			if err := s.siteStore.LockBuildingForWrite(txCtx, params.OrgID, *params.TargetBuildingID); err != nil {
				return nil, err
			}
			b, err := s.store.GetBuilding(txCtx, params.OrgID, *params.TargetBuildingID)
			if err != nil {
				return nil, err
			}
			targetBuilding = b
			targetSiteID = b.SiteID
		}

		// Grid-cell upper-bound validation has to run after we know
		// the target building's layout dimensions.
		if targetBuilding != nil {
			for _, rp := range racks {
				if rp.AisleIndex == nil {
					continue
				}
				if targetBuilding.Aisles <= 0 || *rp.AisleIndex >= targetBuilding.Aisles {
					return nil, fleeterror.NewInvalidArgumentErrorf("aisle_index %d is out of bounds (building has %d aisles)", *rp.AisleIndex, targetBuilding.Aisles)
				}
				if targetBuilding.RacksPerAisle <= 0 || *rp.PositionInAisle >= targetBuilding.RacksPerAisle {
					return nil, fleeterror.NewInvalidArgumentErrorf("position_in_aisle %d is out of bounds (building allows %d racks per aisle)", *rp.PositionInAisle, targetBuilding.RacksPerAisle)
				}
			}
		}

		for _, rp := range racks {
			// Lock the rack row and read its current placement so we
			// can decide whether the cascade needs to run + what zone
			// value to persist.
			current, err := s.collectionStore.LockRackPlacementForWrite(txCtx, rp.RackID, params.OrgID)
			if err != nil {
				return nil, err
			}

			// Capture the source SiteID for a single-rack building-only
			// unassign so the activity log carries the rack's site
			// instead of nil. Only meaningful when the batch is exactly
			// one rack — multi-rack batches may straddle sites and
			// surfacing the first rack's site would be misleading.
			if params.TargetBuildingID == nil && len(racks) == 1 {
				fallbackSiteID = current.SiteID
			}

			// Building-only unassign must NOT cascade-clear the rack's
			// site (and, transitively, every descendant device.site_id).
			// Removing a rack from a building is a building-membership
			// change; the rack and its devices stay in their current
			// site until an explicit site-level unassign happens
			// elsewhere. Preserve current.SiteID in that branch so the
			// siteChanged check below reads false and the cascade stays
			// inert.
			newSiteID := targetSiteID
			if params.TargetBuildingID == nil {
				newSiteID = current.SiteID
			}

			// Mirror SaveRack's zone-clear cascade: clear zone when the
			// rack leaves a building or crosses to a different one.
			// Preserve the current zone on a no-op building transition
			// so legacy callers don't strip zone unintentionally.
			finalZone := current.Zone
			leavingBuilding := current.BuildingID != nil && params.TargetBuildingID == nil
			crossingBuildings := current.BuildingID != nil && params.TargetBuildingID != nil && *current.BuildingID != *params.TargetBuildingID
			if leavingBuilding || crossingBuildings {
				finalZone = ""
			}

			// Persist site_id + building_id + zone in one write. The
			// query also clears the grid position on building
			// transition via a CASE expression, so a stale
			// (aisle_index, position_in_aisle) never outlives its
			// parent building.
			if err := s.collectionStore.UpdateRackPlacement(txCtx, rp.RackID, params.OrgID, newSiteID, params.TargetBuildingID, finalZone); err != nil {
				return nil, err
			}

			// Cascade descendant device.site_id when the rack's site
			// changed. CascadeRackDeviceSites returns the row count.
			siteChanged := !int64PtrEqual(current.SiteID, newSiteID)
			if siteChanged {
				count, err := s.collectionStore.CascadeRackDeviceSites(txCtx, rp.RackID, params.OrgID, newSiteID)
				if err != nil {
					return nil, err
				}
				siteReassignedDeviceCount += count
				cascadeRackIDs = append(cascadeRackIDs, rp.RackID)
			}

			// Pass-1 grid-cell vacate: write (NULL, NULL) for every
			// rack in the batch when TargetBuildingID is set. Pass-2
			// below writes the real (aisle, position) for racks that
			// have one. Splitting the writes into two passes lets a
			// swap or move-into-occupied-cell request commit without
			// tripping the partial unique index uk_device_set_rack
			// _building_position mid-loop — every cell touched by the
			// batch is NULL before any real position is written.
			//
			// When TargetBuildingID is nil (full unassign) we skip
			// this call — UpdateRackPlacement's CASE already nulls
			// the position via the building-id-changed branch.
			if params.TargetBuildingID != nil {
				if err := s.store.SetRackBuildingPosition(txCtx, params.OrgID, rp.RackID, nil, nil); err != nil {
					return nil, err
				}
				positionedRackIDs = append(positionedRackIDs, rp.RackID)
			}
		}

		// Pass-2 grid-cell place: write the real (aisle, position) for
		// racks that supplied one. By this point every cell touched by
		// the batch is NULL (pass-1) so no two writes can collide on
		// the partial unique index.
		if params.TargetBuildingID != nil {
			for _, rp := range racks {
				if rp.AisleIndex == nil || rp.PositionInAisle == nil {
					continue
				}
				if err := s.store.SetRackBuildingPosition(txCtx, params.OrgID, rp.RackID, rp.AisleIndex, rp.PositionInAisle); err != nil {
					return nil, err
				}
			}
		}
		return assignRacksToBuildingTx{
			siteReassignedDeviceCount: siteReassignedDeviceCount,
			targetSiteID:              targetSiteID,
			cascadeRackIDs:            cascadeRackIDs,
			positionedRackIDs:         positionedRackIDs,
			fallbackSiteID:            fallbackSiteID,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	txResult, ok := result.(assignRacksToBuildingTx)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	out := models.AssignRacksToBuildingResult{
		SiteReassignedDeviceCount: txResult.siteReassignedDeviceCount,
	}
	targetSiteID := txResult.targetSiteID
	cascadeRackIDs := txResult.cascadeRackIDs
	positionedRackIDs := txResult.positionedRackIDs
	fallbackSiteID := txResult.fallbackSiteID

	// Activity log fires AFTER tx commits.
	orgIDVal := params.OrgID
	var buildingIDMeta any
	if params.TargetBuildingID != nil {
		buildingIDMeta = *params.TargetBuildingID
	}
	rackIDs := make([]int64, len(racks))
	for i, rp := range racks {
		rackIDs[i] = rp.RackID
	}
	// For a single-rack building-only unassign, fall back to the rack's
	// own SiteID captured during the lock so the event still records
	// which site the operator was working in.
	eventSiteID := targetSiteID
	if eventSiteID == nil && fallbackSiteID != nil {
		eventSiteID = fallbackSiteID
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventRackAssignedBuilding,
		OrganizationID: &orgIDVal,
		SiteID:         eventSiteID,
		Description: fmt.Sprintf(
			"Assigned %d rack(s) to building %v",
			len(racks), derefInt64(params.TargetBuildingID),
		),
		Metadata: map[string]any{
			"rack_ids":    rackIDs,
			"building_id": buildingIDMeta,
		},
	}
	if len(cascadeRackIDs) > 0 {
		event.Metadata["site_cascade"] = true
		event.Metadata["site_cascaded_rack_ids"] = cascadeRackIDs
		event.Metadata["site_reassigned_device_count"] = out.SiteReassignedDeviceCount
	}
	if len(positionedRackIDs) > 0 {
		event.Metadata["positioned_rack_ids"] = positionedRackIDs
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &out, nil
}

// layoutDimensionMax caps aisles and racks_per_aisle on Create /
// UpdateBuilding. Mirrors the buf.validate int32.lte on
// CreateBuildingRequest + UpdateBuildingRequest — defense-in-depth for
// non-proto callers (sdk / agent-native paths) that bypass the wire
// validator.
const layoutDimensionMax = int32(100)

func validateLayoutBounds(aisles, racksPerAisle int32) error {
	if aisles > layoutDimensionMax {
		return fleeterror.NewInvalidArgumentErrorf("aisles must be ≤ %d (got %d)", layoutDimensionMax, aisles)
	}
	if racksPerAisle > layoutDimensionMax {
		return fleeterror.NewInvalidArgumentErrorf("racks_per_aisle must be ≤ %d (got %d)", layoutDimensionMax, racksPerAisle)
	}
	return nil
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

func derefInt64(v *int64) any {
	if v == nil {
		return "(unassigned)"
	}
	return *v
}

// DeleteBuilding soft-deletes the building and cascade-unassigns its
// racks in one transaction. Returns the impact count.
func (s *Service) DeleteBuilding(ctx context.Context, orgID, id int64) (*models.DeleteResult, error) {
	var out models.DeleteResult
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		rowsAffected, err := s.store.SoftDeleteBuilding(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return fleeterror.NewNotFoundErrorf("building %d not found", id)
		}
		rackCount, err := s.store.UnassignRacksFromBuilding(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.UnassignedRackCount = rackCount
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Fire AFTER tx commits; RunInTx may retry the closure.
	orgIDVal := orgID
	buildingIDVal := id
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingDeleted,
		OrganizationID: &orgIDVal,
		Description: fmt.Sprintf(
			"Deleted building %d (%d racks unassigned)",
			buildingIDVal, out.UnassignedRackCount,
		),
		Metadata: map[string]any{
			"building_id":           buildingIDVal,
			"unassigned_rack_count": out.UnassignedRackCount,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &out, nil
}

// GetBuildingStats returns a server-rolled telemetry + state-count
// snapshot for the building, plus a per-rack BuildingRackHealth entry
// for each placed rack. NotFound when the building doesn't exist in
// the org.
//
// `expectedSiteID` carries the site the handler resolved at authz time:
// if a concurrent AssignBuildingsToSite moves the building between the
// handler's pre-authz lookup and this read, the building's current
// site will diverge from what the caller was authorized for. We
// surface that as NotFound rather than leaking telemetry into the
// wrong site-scope. nil means "the handler saw an unassigned
// building"; nil/nil and equal int64 pointers compare as a match.
func (s *Service) GetBuildingStats(ctx context.Context, orgID, buildingID int64, expectedSiteID *int64) (*models.BuildingStats, error) {
	if s.deviceQueryer == nil || s.telemetry == nil {
		return nil, fleeterror.NewInternalErrorf("buildings.GetBuildingStats requires deviceQueryer and telemetry")
	}

	exists, err := s.store.BuildingBelongsToOrg(ctx, orgID, buildingID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fleeterror.NewNotFoundErrorf("building %d not found", buildingID)
	}

	// Pull every rack placement, paging at the store-clamp ceiling so a
	// building with hundreds of racks doesn't take dozens of round-trips.
	// `MaxRacksPerStatsRequest` is a defensive ceiling — the layout
	// validation already caps real buildings well below it.
	var racks []models.BuildingRack
	var pageToken string
	for {
		page, next, listErr := s.store.ListBuildingRacks(ctx, orgID, buildingID, ListBuildingRacksMaxPageSize, pageToken)
		if listErr != nil {
			return nil, listErr
		}
		racks = append(racks, page...)
		// Strict `>` so a building at the exact layout-validation ceiling
		// (100×100 = 10,000 racks) returns stats; the cap only trips when
		// pagination produced more rows than that. Checked BEFORE the
		// `next == ""` break so a runaway final page can't slip through
		// (a page-1 of 10,000 + final page of 1,000 with next="" would
		// otherwise bypass the cap entirely).
		if len(racks) > MaxRacksPerStatsRequest {
			return nil, fleeterror.NewInternalErrorf("building %d exceeded the %d rack scan cap", buildingID, MaxRacksPerStatsRequest)
		}
		if next == "" {
			break
		}
		pageToken = next
	}

	// Resolve floor-plan bounds for the out-of-range filter below. A rack
	// with aisle_index >= aisles or position_in_aisle >= racks_per_aisle
	// shouldn't normally exist (AssignRacksToBuilding + UpdateBuilding both
	// validate), but the FE silently drops cells outside the rendered
	// grid, so we clear the position fields server-side here for defense
	// in depth — the rack still appears in rack_health[] without a cell.
	building, err := s.store.GetBuilding(ctx, orgID, buildingID)
	if err != nil {
		return nil, err
	}
	// Guard against the AssignBuildingsToSite race: if the building has
	// moved to a different site since the handler's pre-authz lookup,
	// the permission grant we ran against doesn't match the current
	// scope. NotFound is the safe surface here — the caller was never
	// authorized for the building at its new site.
	if !int64PtrEqual(expectedSiteID, building.SiteID) {
		return nil, fleeterror.NewNotFoundErrorf("building %d not found", buildingID)
	}
	aisles := building.Aisles
	racksPerAisle := building.RacksPerAisle

	stats := &models.BuildingStats{
		BuildingID: buildingID,
		RackCount:  int32(len(racks)), //nolint:gosec // bounded by org capacity
		RackHealth: make([]models.BuildingRackHealth, 0, len(racks)),
	}

	// Per-rack state counts via the existing collection-membership query.
	//
	// Residual race window (intentionally not guarded): if
	// AssignRacksToBuilding moves a rack out of this building between
	// the ListBuildingRacks above and this counts read, the response
	// still includes per-rack state counts (hashing/broken/offline/
	// sleeping totals) for that rack. The post-read building.SiteID
	// check at the bottom catches building-level moves; rack-level
	// moves within a building that stays in the caller's site slip
	// through. The leaked surface is four aggregate ints per rack
	// (no device identifiers, no telemetry — those are scoped by site
	// above), and the window is the gap between two adjacent queries
	// in the same RPC. If operator workflows ever start moving racks
	// frequently enough that this matters, the fix is a post-counts
	// re-list with set comparison; today the noise:value ratio
	// doesn't justify the extra query on every poll tick.
	rackIDs := make([]int64, 0, len(racks))
	for _, r := range racks {
		rackIDs = append(rackIDs, r.RackID)
	}
	rackCounts := map[int64]interfaces.MinerStateCounts{}
	if len(rackIDs) > 0 {
		rackCounts, err = s.deviceQueryer.GetMinerStateCountsByCollections(ctx, orgID, rackIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, r := range racks {
		counts := rackCounts[r.RackID]
		// Clear out-of-bounds positions so the cell stays out of the FE
		// floor plan but the rack still surfaces in the rack_health list
		// (operator can spot it via a future "unplaced racks" affordance).
		aisleIdx := r.AisleIndex
		posIdx := r.PositionInAisle
		if aisleIdx != nil && posIdx != nil {
			if *aisleIdx < 0 || *aisleIdx >= aisles || *posIdx < 0 || *posIdx >= racksPerAisle {
				aisleIdx = nil
				posIdx = nil
			}
		}
		stats.RackHealth = append(stats.RackHealth, models.BuildingRackHealth{
			RackID:          r.RackID,
			RackLabel:       r.RackLabel,
			AisleIndex:      aisleIdx,
			PositionInAisle: posIdx,
			HashingCount:    counts.HashingCount,
			BrokenCount:     counts.BrokenCount,
			OfflineCount:    counts.OfflineCount,
			SleepingCount:   counts.SleepingCount,
		})
	}

	// Building-scoped device identifiers via the existing MinerFilter.
	// BuildingIDs joins rack → building_id; un-racked devices at the
	// site without a building aren't visible here, which is the right
	// scope (this is a building roll-up, not a site roll-up).
	// Pass PAIRED + AUTHENTICATION_NEEDED explicitly so the stats roll-up
	// counts AUTH_NEEDED devices the same way the miner list does.
	//
	// Also constrain by expectedSiteID so a concurrent AssignBuildingsToSite
	// that commits between the building re-read and the device fetch can't
	// leak the new site's device set: the cascade stamps device.site_id
	// onto every device under the moved building, so requiring
	// device.site_id == expectedSiteID returns an empty set the moment the
	// move commits. Pairs with the post-read re-check below as belt-and-
	// braces.
	// Limit = cap + 1 lets us detect over-cap from one bounded SQL query
	// rather than materializing the entire matching identifier set first.
	// We never hold (or fan out to state/telemetry queries with) more
	// than cap+1 rows even for a pathological building.
	devFilter := &interfaces.MinerFilter{
		BuildingIDs: []int64{buildingID},
		PairingStatuses: []fm.PairingStatus{
			fm.PairingStatus_PAIRING_STATUS_PAIRED,
			fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
		},
		Limit: MaxDevicesPerStatsResponse + 1,
	}
	if expectedSiteID != nil {
		devFilter.SiteIDs = []int64{*expectedSiteID}
	} else {
		devFilter.IncludeUnassigned = true
	}
	deviceIDs, err := s.deviceQueryer.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, devFilter)
	if err != nil {
		return nil, err
	}
	if len(deviceIDs) > MaxDevicesPerStatsResponse {
		return nil, fleeterror.NewInternalErrorf("building %d exceeded the %d device cap", buildingID, MaxDevicesPerStatsResponse)
	}
	stats.DeviceCount = int32(len(deviceIDs)) //nolint:gosec // bounded by cap above
	stats.DeviceIdentifiers = deviceIDs

	// State counts + telemetry only run when there's at least one
	// device; we still fall through to the post-read site re-check
	// below either way, so an empty-device path can't skip the race
	// guard.
	if len(deviceIDs) > 0 {
		counts, err := s.deviceQueryer.GetMinerStateCountsByDeviceIDs(ctx, orgID, deviceIDs)
		if err != nil {
			return nil, err
		}
		stats.HashingCount = counts.HashingCount
		stats.BrokenCount = counts.BrokenCount
		stats.OfflineCount = counts.OfflineCount
		stats.SleepingCount = counts.SleepingCount

		telemetryIDs := devicerollup.ToDeviceIdentifiers(deviceIDs)
		metrics, err := s.telemetry.GetLatestDeviceMetrics(ctx, telemetryIDs)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to fetch building telemetry: %v", err)
		}
		rollup := devicerollup.AggregateLatestMetrics(metrics, telemetryIDs)
		stats.ReportingCount = rollup.ReportingCount
		stats.HashrateReportingCount = rollup.HashrateReportingCount
		stats.EfficiencyReportingCount = rollup.EfficiencyReportingCount
		stats.PowerReportingCount = rollup.PowerReportingCount
		stats.TotalHashrateThs = rollup.TotalHashrateThs
		stats.TotalPowerKw = rollup.TotalPowerKw
		stats.AvgEfficiencyJth = rollup.AvgEfficiencyJth
	}

	// Belt-and-braces: re-read the building after all the rollup queries.
	// The device fetch is already scoped to expectedSiteID, but the rack
	// and per-rack state queries join on building_id alone — if
	// AssignBuildingsToSite committed between the initial GetBuilding check
	// and these reads, the rack/state data would still be that of the
	// moved building (which now belongs to a site the caller wasn't
	// authorized for). Catch that here and surface NotFound rather than
	// return a snapshot that mixes pre-move authz with post-move data.
	// Runs in both the with-devices and zero-devices paths so a moved
	// building that no longer has any site-A devices still trips here.
	postReadBuilding, err := s.store.GetBuilding(ctx, orgID, buildingID)
	if err != nil {
		return nil, err
	}
	if !int64PtrEqual(expectedSiteID, postReadBuilding.SiteID) {
		return nil, fleeterror.NewNotFoundErrorf("building %d not found", buildingID)
	}

	return stats, nil
}
