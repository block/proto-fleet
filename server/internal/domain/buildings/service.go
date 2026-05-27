// Package buildings is the domain layer for the BuildingService RPC
// surface. CRUD + cascade-unassign-on-delete; site assignment lives on
// SiteService.AssignBuildingToSite where the cross-collection
// invariant is enforced.
package buildings

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
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
	transactor      interfaces.Transactor
	activitySvc     *activity.Service
}

// NewService wires a BuildingStore, SiteStore (for site existence
// validation), CollectionStore (for the rack placement write path
// shared with SaveRack), Transactor (for the delete cascade), and the
// activity Service used for fire-and-forget audit logs. activitySvc
// may be nil in tests or environments where activity logging is
// disabled.
func NewService(
	store interfaces.BuildingStore,
	siteStore interfaces.SiteStore,
	collectionStore interfaces.CollectionStore,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:           store,
		siteStore:       siteStore,
		collectionStore: collectionStore,
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
		// AssignRackToBuilding can't race us into orphaned-position
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

// ListBuildingRacksPageSizeCap matches the buf.validate cap on
// ListBuildingRacksRequest.page_size. Mirrors the proto contract for
// non-proto callers and is also the default when the caller passes
// page_size == 0. Set to the maximum possible placed-rack count
// given the layout cap (aisles ≤ 100) × (racks_per_aisle ≤ 100) so
// ManageBuildingModal's single-page seed read always returns the
// complete working set.
const ListBuildingRacksPageSizeCap = int32(10000)

// ListBuildingRacks returns racks currently assigned to a building
// with their grid placement. Verifies the building exists in the org
// before returning so a stale building_id surfaces as NotFound rather
// than an empty list (which would look identical to "no racks yet").
// `pageSize` is clamped to [1, ListBuildingRacksPageSizeCap]; a value
// of 0 defaults to the cap, mirroring the proto contract.
func (s *Service) ListBuildingRacks(ctx context.Context, orgID, buildingID int64, pageSize int32) ([]models.BuildingRack, error) {
	if pageSize <= 0 || pageSize > ListBuildingRacksPageSizeCap {
		pageSize = ListBuildingRacksPageSizeCap
	}
	if _, err := s.store.GetBuilding(ctx, orgID, buildingID); err != nil {
		return nil, err
	}
	return s.store.ListBuildingRacks(ctx, orgID, buildingID, pageSize)
}

// AssignRackToBuilding sets a rack's building_id and, optionally, its
// grid placement (aisle_index, position_in_aisle). Runs in a single
// transaction:
//
//  1. Lock the target building (when assigning) so a concurrent
//     DeleteBuilding can't race the placement write.
//  2. Lock the rack row and read current placement.
//  3. Resolve the new site_id from the target building (or NULL when
//     unassigning).
//  4. Validate the optional grid cell against the target building's
//     aisles / racks_per_aisle.
//  5. Call collectionStore.UpdateRackPlacement to write site_id +
//     building_id + zone atomically (zone is cleared on cross/leave
//     building, mirroring the existing SaveRack cascade rule).
//  6. Cascade descendant device.site_id when the rack's site changes.
//  7. When the request includes a grid cell, write it via
//     SetRackBuildingPosition.
func (s *Service) AssignRackToBuilding(ctx context.Context, params models.AssignRackToBuildingParams) (*models.AssignRackToBuildingResult, error) {
	// Position fields must be paired. The proto CEL rule enforces
	// this at the wire boundary; this is the defense-in-depth check
	// for non-proto callers.
	if (params.AisleIndex == nil) != (params.PositionInAisle == nil) {
		return nil, fleeterror.NewInvalidArgumentError("aisle_index and position_in_aisle must both be set or both unset")
	}
	if params.AisleIndex != nil && params.BuildingID == nil {
		return nil, fleeterror.NewInvalidArgumentError("a grid cell (aisle_index, position_in_aisle) requires a building_id")
	}
	if params.AisleIndex != nil && *params.AisleIndex < 0 {
		return nil, fleeterror.NewInvalidArgumentError("aisle_index must be >= 0")
	}
	if params.PositionInAisle != nil && *params.PositionInAisle < 0 {
		return nil, fleeterror.NewInvalidArgumentError("position_in_aisle must be >= 0")
	}

	var (
		out        models.AssignRackToBuildingResult
		newSiteID  *int64
		cascadeRan bool
	)
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock the target building first (canonical lock order:
		// building -> rack). Skip when unassigning — there is no
		// building row to lock — but we still lock the rack below.
		var targetBuilding *models.Building
		if params.BuildingID != nil {
			if err := s.siteStore.LockBuildingForWrite(txCtx, params.OrgID, *params.BuildingID); err != nil {
				return err
			}
			b, err := s.store.GetBuilding(txCtx, params.OrgID, *params.BuildingID)
			if err != nil {
				return err
			}
			targetBuilding = b
			newSiteID = b.SiteID
		}

		// Grid-cell upper-bound validation has to run after we know
		// the target building's layout dimensions.
		if params.AisleIndex != nil && targetBuilding != nil {
			if targetBuilding.Aisles <= 0 || *params.AisleIndex >= targetBuilding.Aisles {
				return fleeterror.NewInvalidArgumentErrorf("aisle_index %d is out of bounds (building has %d aisles)", *params.AisleIndex, targetBuilding.Aisles)
			}
			if targetBuilding.RacksPerAisle <= 0 || *params.PositionInAisle >= targetBuilding.RacksPerAisle {
				return fleeterror.NewInvalidArgumentErrorf("position_in_aisle %d is out of bounds (building allows %d racks per aisle)", *params.PositionInAisle, targetBuilding.RacksPerAisle)
			}
		}

		// Lock the rack row and read its current placement so we can
		// decide whether the cascade needs to run + what zone value
		// to persist.
		current, err := s.collectionStore.LockRackPlacementForWrite(txCtx, params.RackID, params.OrgID)
		if err != nil {
			return err
		}

		// Building-only unassign must NOT cascade-clear the rack's site
		// (and, transitively, every descendant device.site_id). Removing
		// a rack from a building is a building-membership change; the
		// rack and its devices stay in their current site until an
		// explicit site-level unassign happens elsewhere. Preserve
		// current.SiteID in that branch so the siteChanged check below
		// reads false and the cascade stays inert.
		if params.BuildingID == nil {
			newSiteID = current.SiteID
		}

		// Mirror SaveRack's zone-clear cascade: clear zone when the
		// rack leaves a building or crosses to a different one.
		// Preserve the current zone on a no-op building transition
		// so legacy callers don't strip zone unintentionally.
		finalZone := current.Zone
		leavingBuilding := current.BuildingID != nil && params.BuildingID == nil
		crossingBuildings := current.BuildingID != nil && params.BuildingID != nil && *current.BuildingID != *params.BuildingID
		if leavingBuilding || crossingBuildings {
			finalZone = ""
		}

		// Persist site_id + building_id + zone in one write. The
		// query also clears the grid position on building transition
		// via a CASE expression, so a stale (aisle_index,
		// position_in_aisle) never outlives its parent building.
		if err := s.collectionStore.UpdateRackPlacement(txCtx, params.RackID, params.OrgID, newSiteID, params.BuildingID, finalZone); err != nil {
			return err
		}

		// Cascade descendant device.site_id when the rack's site
		// changed. CascadeRackDeviceSites returns the row count.
		siteChanged := !int64PtrEqual(current.SiteID, newSiteID)
		if siteChanged {
			count, err := s.collectionStore.CascadeRackDeviceSites(txCtx, params.RackID, params.OrgID, newSiteID)
			if err != nil {
				return err
			}
			out.SiteReassignedDeviceCount = count
			cascadeRan = true
		}

		// Grid-cell write. Two cases land here:
		//
		//   - Both fields set → write the explicit (aisle, position).
		//   - Both fields nil + building_id is set → operator is
		//     unplacing the rack within the same building (or moving
		//     it across with no chosen cell yet). Write NULL/NULL so
		//     the cell on the rack row matches the operator's intent.
		//     UpdateRackPlacement's CASE only clears when building_id
		//     changes, so without this explicit write a same-building
		//     unplace would silently no-op and the old position would
		//     survive.
		//
		// When BuildingID is nil (full unassign) we skip this call —
		// UpdateRackPlacement's CASE already nulls the position via
		// the building-id-changed branch.
		if params.BuildingID != nil {
			if err := s.store.SetRackBuildingPosition(txCtx, params.OrgID, params.RackID, params.AisleIndex, params.PositionInAisle); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER tx commits. SiteID is the rack's final
	// site after the write — same as newSiteID, which now equals
	// current.SiteID on building-only unassign (so we don't lose the
	// site filter on building-removal events) and the target
	// building's site otherwise. Using cascadeSite here would only
	// populate when CascadeRackDeviceSites ran, hiding same-site
	// assigns from site-scoped activity queries.
	orgIDVal := params.OrgID
	// Dereference the building id stored in metadata so JSON shape
	// matches DeleteBuilding (int64, not *int64). Downstream consumers
	// doing `.(int64)` on the metadata field would crash on the
	// pointer variant.
	var buildingIDMeta any
	if params.BuildingID != nil {
		buildingIDMeta = *params.BuildingID
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventRackAssignedBuilding,
		OrganizationID: &orgIDVal,
		SiteID:         newSiteID,
		Description: fmt.Sprintf(
			"Assigned rack %d to building %v",
			params.RackID, derefInt64(params.BuildingID),
		),
		Metadata: map[string]any{
			"rack_id":     params.RackID,
			"building_id": buildingIDMeta,
		},
	}
	if cascadeRan {
		event.Metadata["site_cascade"] = true
		event.Metadata["site_reassigned_device_count"] = out.SiteReassignedDeviceCount
	}
	if params.AisleIndex != nil {
		event.Metadata["aisle_index"] = *params.AisleIndex
		event.Metadata["position_in_aisle"] = *params.PositionInAisle
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
