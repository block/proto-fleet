// Package buildings is the domain layer for the BuildingService RPC
// surface. CRUD + cascade-unassign-on-delete; site assignment lives on
// SiteService.AssignBuildingsToSite where the cross-collection
// invariant is enforced.
package buildings

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/devicerollup"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// Event type constants for buildings activity logs.
const (
	eventBuildingCreated             = "building.created"
	eventBuildingUpdated             = "building.updated"
	eventBuildingDeleted             = "building.deleted"
	eventRackAssignedBuilding        = "building.rack_assigned"
	eventDevicesReassignedToBuilding = "devices.reassigned_to_building"
)

// maxDeviceIdentifiersInMetadata bounds how many identifiers we keep in
// the activity row's metadata for a single reassign event. We log the
// total separately; the truncated list is just a debugging affordance.
const maxDeviceIdentifiersInMetadata = 50

// maxListFilterValues caps the ListBuildings site_ids filter array.
// Mirrors the miner-list and rack-list repeated-filter caps so an
// oversized request can't inflate query planning cost.
const maxListFilterValues = 1024

// ListStatsAuthorizer reports whether list-row telemetry stats may be
// populated for a building at the supplied site. Nil site_id means the
// building is unassigned and must be authorized at org scope.
type ListStatsAuthorizer func(siteID *int64) bool

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
// SiteIDs and IncludeUnassigned compose additively; the store query
// treats "both empty" as "no filter".
func (s *Service) ListBuildings(ctx context.Context, filter models.ListFilter, statsFilter fleetlistfilter.Filter, includeStatsForSite ListStatsAuthorizer) ([]models.BuildingWithCounts, error) {
	// Cap the repeated filter to bound request size / query planning
	// cost, matching the miner-list (maxFreeFormFilterValues) and
	// rack-list (maxDeviceSetFilterValues) paths.
	if len(filter.SiteIDs) > maxListFilterValues {
		return nil, fleeterror.NewInvalidArgumentErrorf("site_ids exceeds maximum of %d values", maxListFilterValues)
	}
	for i, id := range filter.SiteIDs {
		if id <= 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf("site_ids[%d] must be positive", i)
		}
	}
	rows, err := s.store.ListBuildings(ctx, filter)
	if err != nil {
		return rows, err
	}
	hasStatsFilter := fleetlistfilter.HasFilters(statsFilter)
	if !filter.IncludeStats {
		if hasStatsFilter {
			return rows[:0], nil
		}
		return rows, nil
	}
	if includeStatsForSite == nil {
		if hasStatsFilter {
			return nil, fleeterror.NewInternalErrorf("buildings.ListBuildings filters require stats authorization")
		}
		return rows, nil
	}
	hasStatsRow := false
	for _, row := range rows {
		if includeStatsForSite(row.Building.SiteID) {
			hasStatsRow = true
			break
		}
	}
	if !hasStatsRow {
		if hasStatsFilter {
			return rows[:0], nil
		}
		return rows, nil
	}
	if s.deviceQueryer == nil || s.telemetry == nil {
		return nil, fleeterror.NewInternalErrorf("buildings.ListBuildings stats requires deviceQueryer and telemetry")
	}
	if err := s.populateListStats(ctx, filter.OrgID, rows, includeStatsForSite, len(statsFilter.TelemetryRanges) > 0); err != nil {
		return nil, err
	}
	if hasStatsFilter {
		rows = filterBuildingRowsByListStats(rows, statsFilter)
	}
	return rows, nil
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
//  3. Pass 1 — for each rack (sorted by id for deadlock-safe lock
//     order):
//     a. Lock the rack row and read current placement.
//     b. Resolve the new site_id from the target building (or preserve
//     current.SiteID on building-only unassign).
//     c. Compute final zone (clear on leave/cross building).
//     d. Persist site_id + building_id + zone via UpdateRackPlacement.
//     e. Cascade descendant device.site_id when the site changes; sum
//     the per-rack counts into the aggregate result.
//     f. When assigning (TargetBuildingID != nil), clear the rack's
//     grid cell to (NULL, NULL) via SetRackBuildingPosition.
//  4. Pass 2 — for each rack that carries an explicit (aisle, position),
//     write the final cell via SetRackBuildingPosition.
//
// The two-pass split is what lets a single batch contain heterogeneous
// position changes (swaps, "move into occupied cell", clear + reuse)
// without tripping uk_device_set_rack_building_position. By the time
// any rack tries to claim a cell in pass 2, every rack in the batch is
// guaranteed to hold NULL position — so no partial-unique-index
// collision can fire mid-batch.
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
			// cascadeBuildingRackIDs is the building peer of
			// cascadeRackIDs — racks whose building_id transitioned, so
			// member device.building_id has to follow. Independent of
			// the site-cascade set because a same-site building change
			// (move within one site) hits buildings but not sites.
			cascadeBuildingRackIDs []int64
			positionedRackIDs      []int64
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

		// Phase A: sequential per-rack lock acquisition in sorted order.
		// Locks must be acquired one-by-one to avoid the deadlock that
		// would happen if two concurrent calls grabbed an overlapping
		// rack set in different orders. Only locks + snapshot reads
		// run here — every write happens in Phase B as a bulk
		// statement once every row lock is held.
		allRackIDs := make([]int64, 0, len(racks))
		// cascadeRackIDs is populated in this phase so the bulk
		// CascadeRackDeviceSites call in Phase B knows which racks
		// actually transitioned sites. It's still appended in
		// sorted-rack order to keep the activity-log metadata stable.
		for _, rp := range racks {
			// Lock the rack row and read its current placement so we
			// can decide whether the cascade needs to run later and
			// capture per-rack state for the activity-log fallback.
			current, err := s.collectionStore.LockRackPlacementForWrite(txCtx, rp.RackID, params.OrgID)
			if err != nil {
				return nil, err
			}
			allRackIDs = append(allRackIDs, rp.RackID)

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
			// Preserve current.SiteID in that branch so siteChanged
			// reads false and the cascade stays inert.
			newSiteID := targetSiteID
			if params.TargetBuildingID == nil {
				newSiteID = current.SiteID
			}
			siteChanged := !int64PtrEqual(current.SiteID, newSiteID)
			if siteChanged {
				cascadeRackIDs = append(cascadeRackIDs, rp.RackID)
			}
			// Track racks whose building_id changes — distinct from the
			// site set because a same-site building move hits buildings
			// but not sites. params.TargetBuildingID is the new value
			// (nil on building-only unassign).
			if !int64PtrEqual(current.BuildingID, params.TargetBuildingID) {
				cascadeBuildingRackIDs = append(cascadeBuildingRackIDs, rp.RackID)
				// Placing a previously site-less rack into a site-less
				// building. The site gate above misses this (nil->nil,
				// siteChanged false), but the building cascade below stamps
				// the site-less building, and an unassigned rack's members
				// may carry a direct device.site_id — which must clear to
				// NULL to stay in lockstep with the site-less building.
				// Only this corner slips past the site gate: any target
				// building WITH a site, or a rack that already had a site,
				// is already covered by siteChanged. Skipped on building
				// unassign (target nil), which deliberately preserves site.
				if params.TargetBuildingID != nil && current.SiteID == nil && targetSiteID == nil {
					cascadeRackIDs = append(cascadeRackIDs, rp.RackID)
				}
			}
		}

		// Phase B1: single bulk write for site_id + building_id + zone
		// + grid-position-on-building-change across every rack. The
		// SQL CASE expressions mirror the per-row UpdateRackPlacement +
		// service-layer zone rules so the swap/mixed-clear-and-place
		// cases still behave like the F5 two-pass shape.
		//
		// The returned row count must match len(allRackIDs). Phase A's
		// LockRackPlacementForWrite pre-pass already errors on
		// missing/cross-org ids, but the count check locks the
		// contract in case the pre-pass is ever refactored: an UPDATE
		// that touches fewer rows than requested means one or more
		// rack ids didn't resolve to a row in this org and we'd
		// otherwise silently drop them.
		rowsAffected, err := s.collectionStore.UpdateRackPlacementBulkForBuilding(
			txCtx, params.OrgID, allRackIDs, targetSiteID, params.TargetBuildingID,
		)
		if err != nil {
			return nil, err
		}
		if rowsAffected != int64(len(allRackIDs)) {
			return nil, fleeterror.NewNotFoundErrorf(
				"one or more racks not found (expected %d, updated %d)",
				len(allRackIDs), rowsAffected,
			)
		}

		// Phase B2: single bulk cascade for the subset of racks whose
		// site actually changed. CascadeRackDeviceSitesBulk no-ops on
		// an empty rack set, but skip the call to keep the wire log
		// clean.
		if len(cascadeRackIDs) > 0 {
			count, err := s.collectionStore.CascadeRackDeviceSitesBulk(
				txCtx, params.OrgID, cascadeRackIDs, targetSiteID,
			)
			if err != nil {
				return nil, err
			}
			siteReassignedDeviceCount += count
		}
		// Building cascade — fires whenever any rack's building_id moved
		// (different value, or set/cleared). Keeps device.building_id in
		// lockstep with the rack the same way the site cascade above
		// keeps device.site_id in lockstep. Independent of the site
		// cascade because a same-site building move would fire here but
		// not there — which is exactly why this bulk path is NOT routed
		// through collection.cascadeRackMembersToPlacement: the two
		// columns cascade over different rack-id subsets here, so a single
		// paired helper can't express it. Don't try to unify.
		if len(cascadeBuildingRackIDs) > 0 {
			if _, err := s.collectionStore.CascadeRackDeviceBuildingsBulk(
				txCtx, params.OrgID, cascadeBuildingRackIDs, params.TargetBuildingID,
			); err != nil {
				return nil, err
			}
		}

		// Phase B3: single bulk pass-1 vacate. Force (aisle, position)
		// to (NULL, NULL) for every rack in the batch so pass-2 below
		// can reclaim any cell without colliding mid-batch on the
		// partial unique index uk_device_set_rack_building_position.
		// When TargetBuildingID is nil the UpdateRackPlacement bulk
		// already nulled positions via its CASE — skip here.
		if params.TargetBuildingID != nil {
			if err := s.store.SetRackBuildingPositionBulkClear(txCtx, params.OrgID, allRackIDs); err != nil {
				return nil, err
			}
			positionedRackIDs = append(positionedRackIDs, allRackIDs...)
		}

		// Phase B4: single bulk pass-2 place for racks carrying a
		// (aisle, position). Pass-1 vacated every cell touched by the
		// batch so no two writes can collide.
		if params.TargetBuildingID != nil {
			var (
				placeRackIDs []int64
				placeAisles  []int32
				placePos     []int32
			)
			for _, rp := range racks {
				if rp.AisleIndex == nil || rp.PositionInAisle == nil {
					continue
				}
				placeRackIDs = append(placeRackIDs, rp.RackID)
				placeAisles = append(placeAisles, *rp.AisleIndex)
				placePos = append(placePos, *rp.PositionInAisle)
			}
			if len(placeRackIDs) > 0 {
				if err := s.store.SetRackBuildingPositionBulkPlace(
					txCtx, params.OrgID, placeRackIDs, placeAisles, placePos,
				); err != nil {
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

// assignDevicesToBuildingTx carries the per-attempt counters out of the
// RunInTxWithResult closure. Declared at package scope so a tx retry
// (SQLTransactor serialization / deadlock failure) starts each attempt
// from zero — the closure constructs a fresh struct on every call.
type assignDevicesToBuildingTx struct {
	rowsAffected              int64
	siteReassignedDeviceCount int64
	targetSiteID              *int64
	txConflicts               []models.PerDeviceBuildingConflict
	forceClearedIDs           []string
}

// AssignDevicesToBuilding enforces the cross-building invariant and,
// on success, bulk-updates device.building_id for every identifier in
// one transaction. Mirrors SiteService.AssignDevicesToSite — the entire
// batch rejects if any device fails the check; no partial writes. When
// target_building_id is set, also cascades device.site_id to the
// building's site so site/building stay in lockstep.
func (s *Service) AssignDevicesToBuilding(ctx context.Context, params models.AssignDevicesToBuildingParams) (*models.AssignDevicesToBuildingResult, []models.PerDeviceBuildingConflict, error) {
	identifiers := dedupeStrings(params.DeviceIdentifiers)
	if len(identifiers) == 0 {
		return nil, nil, fleeterror.NewInvalidArgumentError("device_identifiers must not be empty")
	}
	if params.TargetBuildingID != nil && *params.TargetBuildingID == 0 {
		return nil, nil, fleeterror.NewInvalidArgumentError("target_building_id must be > 0 (use nil for Unassigned)")
	}
	targetBuildingID := params.TargetBuildingID
	// Sort identifiers for stable lock order so two concurrent calls
	// touching an overlapping identifier set can't deadlock against
	// each other on the device row lock acquisition path.
	sort.Strings(identifiers)

	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		attempt := assignDevicesToBuildingTx{}
		// Lock the target building first (canonical lock order:
		// building → device). target=nil/0 (Unassigned) needs no
		// building lock — device.building_id gets nulled instead.
		if targetBuildingID != nil && *targetBuildingID > 0 {
			if err := s.siteStore.LockBuildingForWrite(txCtx, params.OrgID, *targetBuildingID); err != nil {
				return attempt, err
			}
			// Read the building's site so we can cascade
			// device.site_id in the same tx — keeps building/site in
			// lockstep just like AssignRacksToBuilding's site cascade.
			siteID, err := s.store.GetBuildingSiteID(txCtx, params.OrgID, *targetBuildingID)
			if err != nil {
				return attempt, err
			}
			attempt.targetSiteID = siteID
		}
		// Row-lock the devices so the conflict check and the UPDATE
		// are atomic against concurrent reassigns.
		if err := s.siteStore.LockDevicesForReassign(txCtx, params.OrgID, identifiers); err != nil {
			return attempt, err
		}
		conflicts, err := s.computeReassignBuildingConflicts(txCtx, params.OrgID, targetBuildingID, attempt.targetSiteID, identifiers)
		if err != nil {
			return attempt, err
		}
		// Force-clear branch mirrors AssignDevicesToSite: when the
		// caller opted in, both DEVICE_IN_RACK_AT_OTHER_BUILDING and
		// DEVICE_IN_RACK_AT_OTHER_SITE become cascade-clear signals
		// (the fix is the same — drop the rack membership row).
		// DEVICE_NOT_FOUND still aborts.
		if params.ForceClearConflictingRackMembership && len(conflicts) > 0 {
			clearableIDs, residual := partitionClearableBuildingConflicts(conflicts)
			// Abort before any deletion when residual non-clearable
			// conflicts remain — otherwise the tx would commit the
			// rack-membership delete without the building move,
			// leaving rack-stripped devices on their old building.
			if len(residual) > 0 {
				attempt.txConflicts = residual
				return attempt, nil
			}
			if len(clearableIDs) > 0 {
				if s.collectionStore == nil {
					return attempt, fleeterror.NewInternalErrorf("force-clear branch requires a collection store")
				}
				// targetRackID=0 means "exclude nothing", i.e. drop
				// every rack row for the listed devices.
				if _, err := s.collectionStore.RemoveDevicesFromAnyRack(txCtx, params.OrgID, clearableIDs, 0); err != nil {
					return attempt, err
				}
				attempt.forceClearedIDs = clearableIDs
				conflicts = nil
			}
		}
		if len(conflicts) > 0 {
			attempt.txConflicts = conflicts
			return attempt, nil
		}
		n, err := s.store.AssignDevicesToBuilding(txCtx, params.OrgID, targetBuildingID, identifiers)
		if err != nil {
			return attempt, err
		}
		attempt.rowsAffected = n
		// Cascade device.site_id to match the target building's site so
		// "Add to building" keeps building/site in lockstep. Fires
		// whenever target_building_id is set — including when the
		// target building is itself unassigned (site_id IS NULL), in
		// which case the cascade resolves the device's site to NULL
		// too. Skipping it for site-less buildings would leave devices
		// pointing at an unassigned building while keeping their old
		// site, which breaks the invariant immediately after the move.
		// On building-unassign (target_building_id NULL), the cascade
		// is skipped entirely — building_id NULL is a deliberate
		// "Unassigned" state and shouldn't drag site_id along.
		if targetBuildingID != nil {
			count, err := s.store.CascadeDevicesSiteForBuilding(txCtx, params.OrgID, identifiers, attempt.targetSiteID)
			if err != nil {
				return attempt, err
			}
			attempt.siteReassignedDeviceCount = count
		}
		return attempt, nil
	})
	if err != nil {
		return nil, nil, err
	}
	committed, _ := result.(assignDevicesToBuildingTx)
	if len(committed.txConflicts) > 0 {
		return nil, committed.txConflicts, nil
	}

	if committed.rowsAffected > 0 {
		orgIDVal := params.OrgID
		idents := identifiers
		if len(idents) > maxDeviceIdentifiersInMetadata {
			idents = idents[:maxDeviceIdentifiersInMetadata]
		}
		metadata := map[string]any{
			"target_building_id":           targetBuildingID,
			"device_count":                 committed.rowsAffected,
			"device_identifiers":           idents,
			"site_reassigned_device_count": committed.siteReassignedDeviceCount,
		}
		description := fmt.Sprintf(
			"Reassigned %d device(s) to building %s",
			committed.rowsAffected, formatBuildingIDForDescription(targetBuildingID),
		)
		if len(committed.forceClearedIDs) > 0 {
			clearedCount := len(committed.forceClearedIDs)
			clearedIdents := committed.forceClearedIDs
			if len(clearedIdents) > maxDeviceIdentifiersInMetadata {
				clearedIdents = clearedIdents[:maxDeviceIdentifiersInMetadata]
			}
			metadata["force_cleared_rack_membership_count"] = clearedCount
			metadata["force_cleared_device_identifiers"] = clearedIdents
			description = fmt.Sprintf(
				"%s (%d rack membership(s) force-cleared)",
				description, clearedCount,
			)
		}
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventDevicesReassignedToBuilding,
			OrganizationID: &orgIDVal,
			SiteID:         committed.targetSiteID,
			Description:    description,
			Metadata:       metadata,
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return &models.AssignDevicesToBuildingResult{
		ReassignedCount:           committed.rowsAffected,
		SiteReassignedDeviceCount: committed.siteReassignedDeviceCount,
	}, nil, nil
}

// computeReassignBuildingConflicts mirrors sites.computeReassignConflicts
// but compares against the requested target building_id (and the target
// building's site_id) instead of just site_id. Returns conflicts sorted
// by device identifier so the API response is reproducible.
//
// Two layers of cross-collection check, because a building has both an
// id and a site:
//   - rack.building_id != target_building_id  → IN_RACK_AT_OTHER_BUILDING.
//     Covers the obvious case where the device's rack belongs to a
//     different building.
//   - rack.site_id != target_building.site_id → IN_RACK_AT_OTHER_SITE.
//     Catches the rack-without-building corner: a rack at Site A with
//     no building wouldn't trip the building-only check above, but
//     moving the device to a building at Site B would still leave
//     rack/site out of sync. Runs for site-less target buildings too
//     (target site nil): a device whose rack is at a real site can't
//     be cascaded site-less while staying in that rack, so it's
//     flagged for force-clear.
func (s *Service) computeReassignBuildingConflicts(ctx context.Context, orgID int64, targetBuildingID *int64, targetSiteID *int64, identifiers []string) ([]models.PerDeviceBuildingConflict, error) {
	existingList, err := s.siteStore.ListExistingDeviceIdentifiers(ctx, orgID, identifiers)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(existingList))
	for _, ident := range existingList {
		existing[ident] = struct{}{}
	}

	// De-dupe by device identifier so a device that's both at the
	// wrong building AND a stray site only surfaces once. Building
	// conflict wins because the more specific reason explains the
	// fix to the operator more clearly.
	flagged := make(map[string]models.PerDeviceBuildingConflict)
	for _, ident := range identifiers {
		if _, ok := existing[ident]; !ok {
			flagged[ident] = models.PerDeviceBuildingConflict{
				DeviceIdentifier: ident,
				Reason:           models.ReasonBuildingDeviceNotFound,
			}
		}
	}

	buildingByDevice, err := s.store.FindDeviceBuildingConflicts(ctx, orgID, identifiers)
	if err != nil {
		return nil, err
	}
	var target int64
	if targetBuildingID != nil {
		target = *targetBuildingID
	}
	for ident, buildingID := range buildingByDevice {
		if buildingID == target {
			continue
		}
		if _, ok := flagged[ident]; ok {
			continue
		}
		flagged[ident] = models.PerDeviceBuildingConflict{
			DeviceIdentifier:      ident,
			Reason:                models.ReasonBuildingDeviceInRackAtOtherBuilding,
			ConflictingBuildingID: buildingID,
		}
	}

	// Building-less placed-rack guard. A device in a rack that has a
	// site but no building (a site-level rack) is missed by the
	// building probe above (its building_id is NULL) and by the
	// site probe below when the target building is in the same site —
	// yet it can't take a direct building while staying in that rack
	// without breaking rack/device lockstep. Flag those (clearable)
	// whenever we're assigning to a building. Fully-unassigned racks
	// are excluded by the query: they dictate no placement.
	if targetBuildingID != nil {
		buildingLess, err := s.store.FindDevicesInBuildingLessPlacedRacks(ctx, orgID, identifiers)
		if err != nil {
			return nil, err
		}
		for _, ident := range buildingLess {
			if _, ok := flagged[ident]; ok {
				continue
			}
			flagged[ident] = models.PerDeviceBuildingConflict{
				DeviceIdentifier: ident,
				Reason:           models.ReasonBuildingDeviceInRackAtOtherBuilding,
			}
		}
	}

	// Cross-site rack guard. Reuses the existing site-conflict probe
	// from SiteStore so we don't carry a parallel query family.
	// FindDeviceSiteConflicts only returns devices whose rack has a
	// non-NULL site, so:
	//   - target building has a site S → flag devices whose rack site
	//     differs from S.
	//   - target building is site-less (targetSiteID nil) → every
	//     returned device is a mismatch (its rack is at a real site,
	//     the target has none). Without flagging these, the building
	//     write would cascade device.site_id to NULL while the device
	//     is still a member of a rack at a real site.
	// Runs whenever we're assigning to a building (targetBuildingID
	// != nil) — the case where the building write cascades site.
	// Skipped on building-unassign (targetBuildingID nil): no site
	// cascade fires there, so there's nothing to keep consistent.
	if targetBuildingID != nil {
		siteByDevice, err := s.siteStore.FindDeviceSiteConflicts(ctx, orgID, identifiers)
		if err != nil {
			return nil, err
		}
		for ident, siteID := range siteByDevice {
			if targetSiteID != nil && siteID == *targetSiteID {
				continue
			}
			if _, ok := flagged[ident]; ok {
				continue
			}
			flagged[ident] = models.PerDeviceBuildingConflict{
				DeviceIdentifier: ident,
				Reason:           models.ReasonBuildingDeviceInRackAtOtherSite,
				// ConflictingBuildingID intentionally 0 — the rack has
				// no building, only a site mismatch. The client uses
				// the reason enum to render the dialog, not the id.
			}
		}

		// Fully-unassigned rack guard. FindDeviceSiteConflicts (rack
		// site set) and FindDevicesInBuildingLessPlacedRacks (rack site
		// set, building null) both require a non-NULL rack site, so a
		// device in a rack with NEITHER site nor building slips past
		// both. Assigning it to a building cascades device.site_id to
		// the building's site, leaving the device with a site while
		// still in a site-less rack. Flag those (clearable) so the
		// force-clear path drops the rack membership first.
		//
		// FindDevicesInSiteLessRacks keys only on rack.site_id IS NULL,
		// which ALSO matches a rack in a site-less building (its site is
		// NULL but its building is set). Skip those: a device whose rack
		// has a building is already handled by the building-conflict
		// branch above (same building → no conflict, so it stays out of
		// `flagged`; different building → already flagged there). Only a
		// rack with no building at all (absent from buildingByDevice) is
		// the genuinely-unassigned case this guard targets.
		siteLess, err := s.siteStore.FindDevicesInSiteLessRacks(ctx, orgID, identifiers)
		if err != nil {
			return nil, err
		}
		for _, ident := range siteLess {
			if _, ok := flagged[ident]; ok {
				continue
			}
			if _, rackHasBuilding := buildingByDevice[ident]; rackHasBuilding {
				continue
			}
			flagged[ident] = models.PerDeviceBuildingConflict{
				DeviceIdentifier: ident,
				Reason:           models.ReasonBuildingDeviceInRackAtOtherSite,
			}
		}
	}

	conflicts := make([]models.PerDeviceBuildingConflict, 0, len(flagged))
	for _, c := range flagged {
		conflicts = append(conflicts, c)
	}
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].DeviceIdentifier < conflicts[j].DeviceIdentifier
	})
	return conflicts, nil
}

// partitionClearableBuildingConflicts splits force-clear conflicts into
// the device identifiers whose rack membership can be dropped to resolve
// the conflict (IN_RACK_AT_OTHER_BUILDING / IN_RACK_AT_OTHER_SITE — the
// fix is the same, drop the rack row) and the residual non-clearable
// conflicts (e.g. DEVICE_NOT_FOUND) that must still abort the batch.
func partitionClearableBuildingConflicts(conflicts []models.PerDeviceBuildingConflict) (clearableIDs []string, residual []models.PerDeviceBuildingConflict) {
	for _, c := range conflicts {
		if c.Reason == models.ReasonBuildingDeviceInRackAtOtherBuilding ||
			c.Reason == models.ReasonBuildingDeviceInRackAtOtherSite {
			clearableIDs = append(clearableIDs, c.DeviceIdentifier)
			continue
		}
		residual = append(residual, c)
	}
	return clearableIDs, residual
}

// dedupeStrings collapses duplicates from the operator's input list.
// Caller is responsible for any downstream ordering — AssignDevicesToBuilding
// sorts the result for deadlock-safe lock acquisition, and the conflict
// response is sorted by identifier separately.
func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func formatBuildingIDForDescription(target *int64) string {
	if target == nil {
		return "Unassigned"
	}
	return fmt.Sprintf("%d", *target)
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
		// Building soft-delete leaves device.building_id FK rows
		// pointing at the now-soft-deleted row (FK only fires on hard
		// delete). Clear them in the same tx so direct-FK devices
		// don't outlive their building. Rack-membership devices are
		// handled separately by the rack-level building cascade.
		if _, err := s.store.ClearDeviceBuildingsByBuilding(txCtx, orgID, id); err != nil {
			return err
		}
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
			fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
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

		componentCounts, err := s.deviceQueryer.GetComponentErrorCounts(ctx, orgID, interfaces.ComponentErrorScope{
			Kind: interfaces.ComponentErrorScopeBuildings,
			IDs:  []int64{buildingID},
		})
		if err != nil {
			return nil, err
		}
		issues := devicerollup.AggregateComponentIssueCounts(componentCounts, buildingID)
		stats.ControlBoardIssueCount = issues.ControlBoardIssueCount
		stats.FanIssueCount = issues.FanIssueCount
		stats.HashBoardIssueCount = issues.HashBoardIssueCount
		stats.PsuIssueCount = issues.PsuIssueCount

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
		stats.TemperatureReportingCount = rollup.TemperatureReportingCount
		stats.TotalHashrateThs = rollup.TotalHashrateThs
		stats.TotalPowerKw = rollup.TotalPowerKw
		stats.AvgEfficiencyJth = rollup.AvgEfficiencyJth
		stats.MinTemperatureC = rollup.MinTemperatureC
		stats.MaxTemperatureC = rollup.MaxTemperatureC
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

func (s *Service) populateListStats(ctx context.Context, orgID int64, rows []models.BuildingWithCounts, includeStatsForSite ListStatsAuthorizer, requireTelemetry bool) error {
	if len(rows) == 0 {
		return nil
	}

	buildingIDs := make([]int64, 0, len(rows))
	deviceIDsByBuilding := make(map[int64][]string, len(rows))
	uniqueDeviceIDs := make(map[string]struct{})
	for i := range rows {
		buildingID := rows[i].Building.ID
		if !includeStatsForSite(rows[i].Building.SiteID) {
			continue
		}
		buildingIDs = append(buildingIDs, buildingID)
		rows[i].ListStats = &models.FleetListStats{
			RackCount: int32(rows[i].RackCount), //nolint:gosec // bounded by org capacity
		}

		filter := &interfaces.MinerFilter{
			BuildingIDs: []int64{buildingID},
			PairingStatuses: []fm.PairingStatus{
				fm.PairingStatus_PAIRING_STATUS_PAIRED,
				fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
				fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
			},
			Limit: MaxDevicesPerStatsResponse + 1,
		}
		if rows[i].Building.SiteID != nil {
			filter.SiteIDs = []int64{*rows[i].Building.SiteID}
		} else {
			filter.IncludeUnassigned = true
		}

		deviceIDs, err := s.deviceQueryer.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, filter)
		if err != nil {
			return err
		}
		if len(deviceIDs) > MaxDevicesPerStatsResponse {
			return fleeterror.NewInternalErrorf("building %d exceeded the %d device cap", buildingID, MaxDevicesPerStatsResponse)
		}
		deviceIDsByBuilding[buildingID] = deviceIDs
		for _, id := range deviceIDs {
			uniqueDeviceIDs[id] = struct{}{}
		}
	}
	if len(buildingIDs) == 0 {
		return nil
	}

	componentCounts, err := s.deviceQueryer.GetComponentErrorCounts(ctx, orgID, interfaces.ComponentErrorScope{
		Kind: interfaces.ComponentErrorScopeBuildings,
		IDs:  buildingIDs,
	})
	if err != nil {
		return err
	}

	var metrics map[minerModels.DeviceIdentifier]telemetrymodels.DeviceMetrics
	if len(uniqueDeviceIDs) > 0 {
		uniqueTelemetryIDs := make([]string, 0, len(uniqueDeviceIDs))
		for id := range uniqueDeviceIDs {
			uniqueTelemetryIDs = append(uniqueTelemetryIDs, id)
		}
		metrics, err = s.telemetry.GetLatestDeviceMetrics(ctx, devicerollup.ToDeviceIdentifiers(uniqueTelemetryIDs))
		if err != nil {
			if requireTelemetry {
				return fleeterror.NewInternalErrorf("failed to fetch building list telemetry: %v", err)
			}
			slog.WarnContext(ctx, "failed to fetch building list telemetry", "error", err)
			metrics = nil
		}
	}

	for i := range rows {
		stats := rows[i].ListStats
		if stats == nil {
			continue
		}
		buildingID := rows[i].Building.ID
		deviceIDs := deviceIDsByBuilding[buildingID]
		stats.DeviceCount = int32(len(deviceIDs)) //nolint:gosec // bounded by cap above
		if len(deviceIDs) > 0 {
			counts, err := s.deviceQueryer.GetMinerStateCountsByDeviceIDs(ctx, orgID, deviceIDs)
			if err != nil {
				return err
			}
			stats.HashingCount = counts.HashingCount
			stats.BrokenCount = counts.BrokenCount
			stats.OfflineCount = counts.OfflineCount
			stats.SleepingCount = counts.SleepingCount

			telemetryIDs := devicerollup.ToDeviceIdentifiers(deviceIDs)
			rollup := devicerollup.AggregateLatestMetrics(metrics, telemetryIDs)
			stats.ReportingCount = rollup.ReportingCount
			stats.HashrateReportingCount = rollup.HashrateReportingCount
			stats.EfficiencyReportingCount = rollup.EfficiencyReportingCount
			stats.PowerReportingCount = rollup.PowerReportingCount
			stats.TemperatureReportingCount = rollup.TemperatureReportingCount
			stats.TotalHashrateThs = rollup.TotalHashrateThs
			stats.TotalPowerKw = rollup.TotalPowerKw
			stats.AvgEfficiencyJth = rollup.AvgEfficiencyJth
			stats.MinTemperatureC = rollup.MinTemperatureC
			stats.MaxTemperatureC = rollup.MaxTemperatureC
		}
		issues := devicerollup.AggregateComponentIssueCounts(componentCounts, buildingID)
		stats.ControlBoardIssueCount = issues.ControlBoardIssueCount
		stats.FanIssueCount = issues.FanIssueCount
		stats.HashBoardIssueCount = issues.HashBoardIssueCount
		stats.PsuIssueCount = issues.PsuIssueCount
	}
	return nil
}

func filterBuildingRowsByListStats(rows []models.BuildingWithCounts, filter fleetlistfilter.Filter) []models.BuildingWithCounts {
	out := rows[:0]
	for _, row := range rows {
		if row.ListStats == nil {
			continue
		}
		stats := row.ListStats
		if fleetlistfilter.Matches(fleetlistfilter.Stats{
			HashrateReportingCount:    stats.HashrateReportingCount,
			EfficiencyReportingCount:  stats.EfficiencyReportingCount,
			PowerReportingCount:       stats.PowerReportingCount,
			TemperatureReportingCount: stats.TemperatureReportingCount,
			TotalHashrateThs:          stats.TotalHashrateThs,
			AvgEfficiencyJth:          stats.AvgEfficiencyJth,
			TotalPowerKw:              stats.TotalPowerKw,
			MinTemperatureC:           stats.MinTemperatureC,
			MaxTemperatureC:           stats.MaxTemperatureC,
			ControlBoardIssueCount:    stats.ControlBoardIssueCount,
			FanIssueCount:             stats.FanIssueCount,
			HashBoardIssueCount:       stats.HashBoardIssueCount,
			PsuIssueCount:             stats.PsuIssueCount,
		}, filter) {
			out = append(out, row)
		}
	}
	return out
}
