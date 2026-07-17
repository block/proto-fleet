// Package sites is the domain layer for the SiteService RPC surface.
// It owns network_config validation, the cross-collection invariant
// enforced on bulk reassignments and building site moves, and the
// site delete cascade.
package sites

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/devicerollup"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetlistfilter"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// Event type constants for sites activity logs.
const (
	eventSiteCreated             = "site.created"
	eventSiteUpdated             = "site.updated"
	eventSiteDeleted             = "site.deleted"
	eventDevicesReassignedToSite = "devices.reassigned_to_site"
	eventBuildingAssignedToSite  = "building.assigned_to_site"
	eventRacksAssignedToSite     = "racks.assigned_to_site"
)

// maxDeviceIdentifiersInMetadata bounds how many identifiers we keep in
// the activity row's metadata for a single reassign event. We log the
// total separately; the truncated list is just a debugging affordance.
const maxDeviceIdentifiersInMetadata = 50

// MaxDevicesPerSiteStatsRequest caps the device list GetSiteStats will
// materialize in-memory before bailing. Unlike GetBuildingStats this
// list is never echoed in the response — the ceiling guards against
// runaway memory + giant Postgres/Timescale ANY() queries on a site
// that's been pathologically misconfigured. Production single-site
// fleets cap around 30k devices; 100k is generous headroom and still
// well below the point a single rollup would stall a 60s poll tick.
const MaxDevicesPerSiteStatsRequest = 100_000

// Service is the domain entry point for site CRUD, device reassignment,
// and building site reassignment. The transactor is required: the
// site delete cascade and the bulk-reassign all-or-nothing semantics
// both depend on it.
type Service struct {
	store           interfaces.SiteStore
	buildingStore   interfaces.BuildingStore
	collectionStore interfaces.CollectionStore
	deviceQueryer   devicerollup.DeviceQueryer
	telemetry       devicerollup.TelemetryCollector
	transactor      interfaces.Transactor
	activitySvc     *activity.Service
}

// NewService wires a SiteStore, Transactor, and the activity Service.
// Most site activity is fire-and-forget and tolerates a nil activitySvc;
// infrastructure control-subnet commissioning requires both dependencies so
// its mutation and security audit can commit atomically.
//
// buildingStore, deviceQueryer, and telemetry power GetSiteStats only.
// Any of them may be nil in test setups where the stats RPC isn't
// exercised; GetSiteStats returns an internal error in that case.
//
// collectionStore powers AssignRacksToSite (it owns the rack
// placement read/write path shared with SaveRack). Nil collectionStore
// causes AssignRacksToSite to return an internal error.
func NewService(
	store interfaces.SiteStore,
	buildingStore interfaces.BuildingStore,
	collectionStore interfaces.CollectionStore,
	deviceQueryer devicerollup.DeviceQueryer,
	telemetry devicerollup.TelemetryCollector,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:           store,
		buildingStore:   buildingStore,
		collectionStore: collectionStore,
		deviceQueryer:   deviceQueryer,
		telemetry:       telemetry,
		transactor:      transactor,
		activitySvc:     activitySvc,
	}
}

// CreateResult is the output of CreateSite, carrying both the saved
// site and any non-blocking warnings (cross-site overlap, etc.).
type CreateResult struct {
	Site                  *models.Site
	NetworkConfigWarnings []string
}

// CreateSite validates network_config, computes cross-site overlap
// warnings, and inserts the row.
func (s *Service) CreateSite(ctx context.Context, params models.CreateSiteParams) (*CreateResult, error) {
	canon, err := CanonicalizeNetworkConfig(params.NetworkConfig)
	if err != nil {
		return nil, err
	}
	params.NetworkConfig = canon.Canonical

	warnings, err := s.computeCrossSiteOverlapWarnings(ctx, params.OrgID, 0, canon.Prefixes)
	if err != nil {
		return nil, err
	}

	usedSlugs, err := s.store.ListSiteSlugs(ctx, params.OrgID)
	if err != nil {
		return nil, err
	}
	used := make(map[string]struct{}, len(usedSlugs))
	for _, slug := range usedSlugs {
		used[slug] = struct{}{}
	}

	var site *models.Site
	for {
		params.Slug = generateSiteSlug(params.Name, used)
		site, err = s.store.CreateSite(ctx, params)
		if errors.Is(err, models.ErrSiteSlugCollision) {
			used[params.Slug] = struct{}{}
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}

	orgID := params.OrgID
	siteID := site.ID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventSiteCreated,
		OrganizationID: &orgID,
		SiteID:         &siteID,
		Description:    fmt.Sprintf("Created site %q (id=%d)", site.Name, site.ID),
		Metadata: map[string]any{
			"site_id":   site.ID,
			"site_name": site.Name,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &CreateResult{Site: site, NetworkConfigWarnings: warnings}, nil
}

// UpdateResult mirrors CreateResult for the update flow.
type UpdateResult struct {
	Site                  *models.Site
	NetworkConfigWarnings []string
}

// UpdateSite validates network_config, computes cross-site overlap
// warnings excluding the row being saved, and updates.
func (s *Service) UpdateSite(ctx context.Context, params models.UpdateSiteParams) (*UpdateResult, error) {
	canon, err := CanonicalizeNetworkConfig(params.NetworkConfig)
	if err != nil {
		return nil, err
	}
	params.NetworkConfig = canon.Canonical

	warnings, err := s.computeCrossSiteOverlapWarnings(ctx, params.OrgID, params.ID, canon.Prefixes)
	if err != nil {
		return nil, err
	}

	// The slug is not user-editable, but it tracks the site name so scoped
	// URLs stay legible. Regenerate it only when the name actually changes;
	// an edit that leaves the name untouched keeps the existing slug stable
	// so unrelated field edits never churn the site's URL.
	current, err := s.store.GetSite(ctx, params.OrgID, params.ID)
	if err != nil {
		return nil, err
	}

	var site *models.Site
	if params.Name == current.Name {
		params.Slug = current.Slug
		site, err = s.store.UpdateSite(ctx, params)
		if err != nil {
			return nil, err
		}
	} else {
		usedSlugs, err := s.store.ListSiteSlugs(ctx, params.OrgID)
		if err != nil {
			return nil, err
		}
		// Exclude our own current slug so a rename can re-derive the same
		// base (or a shorter one) without colliding with itself.
		used := make(map[string]struct{}, len(usedSlugs))
		for _, slug := range usedSlugs {
			if slug == current.Slug {
				continue
			}
			used[slug] = struct{}{}
		}
		for {
			params.Slug = generateSiteSlug(params.Name, used)
			site, err = s.store.UpdateSite(ctx, params)
			if errors.Is(err, models.ErrSiteSlugCollision) {
				used[params.Slug] = struct{}{}
				continue
			}
			if err != nil {
				return nil, err
			}
			break
		}
	}

	orgID := params.OrgID
	siteID := site.ID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventSiteUpdated,
		OrganizationID: &orgID,
		SiteID:         &siteID,
		Description:    fmt.Sprintf("Updated site %q (id=%d)", site.Name, site.ID),
		Metadata: map[string]any{
			"site_id":   site.ID,
			"site_name": site.Name,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &UpdateResult{Site: site, NetworkConfigWarnings: warnings}, nil
}

// ListStatsAuthorizer reports whether list-row telemetry stats may be
// populated for a site. Nil means list stats are disabled.
type ListStatsAuthorizer func(siteID int64) bool

// ListSites returns sites with attachment counts for the delete-confirm
// dialog impact numbers.
func (s *Service) ListSites(ctx context.Context, orgID int64, statsFilter fleetlistfilter.Filter, includeStatsForSite ListStatsAuthorizer) ([]models.SiteWithCounts, error) {
	rows, err := s.store.ListSites(ctx, orgID)
	if err != nil {
		return rows, err
	}
	hasStatsFilter := fleetlistfilter.HasFilters(statsFilter)
	if includeStatsForSite == nil {
		if hasStatsFilter {
			return nil, fleeterror.NewInternalErrorf("sites.ListSites filters require stats authorization")
		}
		return rows, nil
	}
	hasStatsRow := false
	for _, row := range rows {
		if includeStatsForSite(row.Site.ID) {
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
		return nil, fleeterror.NewInternalErrorf("sites.ListSites stats requires deviceQueryer and telemetry")
	}
	if err := s.populateListStats(ctx, orgID, rows, includeStatsForSite, len(statsFilter.TelemetryRanges) > 0); err != nil {
		return nil, err
	}
	if hasStatsFilter {
		rows = filterSiteRowsByListStats(rows, statsFilter)
	}
	return rows, nil
}

// GetSiteBySlug returns a live site in the org by its URL slug. The slug is
// not user-editable but is regenerated from the name on a rename.
func (s *Service) GetSiteBySlug(ctx context.Context, orgID int64, slug string) (*models.Site, error) {
	return s.store.GetSiteBySlug(ctx, orgID, slug)
}

// DeleteSite soft-deletes the site and cascade-unassigns or deletes its
// devices, racks, buildings, and response profiles in one transaction. Returns
// the impact counts.
func (s *Service) DeleteSite(ctx context.Context, orgID, id int64) (*models.DeleteSiteResult, error) {
	var out models.DeleteSiteResult
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// 0. Lock the site row first so two concurrent DeleteSite calls
		// can't both cascade. If the row is already soft-deleted/gone,
		// LockSiteForWrite returns NotFound and we bail.
		if err := s.store.LockSiteForWrite(txCtx, orgID, id); err != nil {
			return err
		}
		// 0b. Lock every live building under this site so a concurrent
		// AssignBuildingToSite can't move one out from under the
		// rack-clear step below. Site-first-then-buildings lock order
		// matches AssignBuildingToSite to avoid deadlock.
		if err := s.store.LockBuildingsBySiteForWrite(txCtx, orgID, id); err != nil {
			return err
		}
		infrastructureDeviceIDs, err := s.store.LockInfrastructureDevicesBySiteForWrite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		activeEventCount, err := s.store.CountActiveCurtailmentEventsByInfrastructureDevices(
			txCtx,
			orgID,
			infrastructureDeviceIDs,
		)
		if err != nil {
			return err
		}
		if activeEventCount > 0 {
			return fleeterror.NewFailedPreconditionError(
				"infrastructure devices at this site are claimed by active curtailment events; wait for those events to finish before deleting the site",
			)
		}
		// Clear rack→building linkage + zone for racks under any
		// building of this site, BEFORE the buildings disappear.
		if _, err := s.store.UnassignRacksFromBuildingsBySite(txCtx, orgID, id); err != nil {
			return err
		}
		// Clear direct-FK device.building_id for any device whose
		// building lives under this site, BEFORE the buildings get
		// soft-deleted. Rack-membership devices are handled by the
		// UnassignRacksFromBuildingsBySite call above; this covers
		// the direct-assignment branch added in migration 000091.
		if _, err := s.buildingStore.ClearDeviceBuildingsBySite(txCtx, orgID, id); err != nil {
			return err
		}
		// Soft-delete buildings under the site.
		deletedBuildings, err := s.store.SoftDeleteBuildingsBySite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.DeletedBuildingCount = deletedBuildings
		// Unassign racks directly under the site.
		rackCount, err := s.store.UnassignRacksFromSite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.UnassignedRackCount = rackCount
		// Unassign devices.
		deviceCount, err := s.store.UnassignDevicesFromSite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.UnassignedDeviceCount = deviceCount
		// Delete response profiles scoped to this site so reusable
		// curtailment behavior cannot outlive the site row.
		profileCount, err := s.store.DeleteCurtailmentResponseProfilesBySite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.DeletedResponseProfileCount = profileCount
		referencingProfileCount, err := s.store.CountResponseProfilesByInfrastructureDevices(
			txCtx,
			orgID,
			infrastructureDeviceIDs,
		)
		if err != nil {
			return err
		}
		if referencingProfileCount > 0 {
			return fleeterror.NewFailedPreconditionError(
				"infrastructure devices at this site are referenced by curtailment response profiles; update those profiles first",
			)
		}
		// Soft-delete infrastructure devices (facility fans) under the
		// site so controllable devices cannot outlive the site row.
		infraCount, err := s.store.SoftDeleteInfrastructureDevicesBySite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.DeletedInfrastructureDeviceCount = infraCount
		// Soft-delete the site row last.
		n, err := s.store.SoftDeleteSite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return fleeterror.NewNotFoundErrorf("site %d not found", id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Fire the audit row only after the tx commits — db.WithTransaction
	// can retry the closure on serialization failures, so an in-closure
	// Log would duplicate the row across retries.
	orgIDVal := orgID
	siteIDVal := id
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventSiteDeleted,
		OrganizationID: &orgIDVal,
		SiteID:         &siteIDVal,
		Description: fmt.Sprintf(
			"Deleted site %d (%d buildings, %d racks, %d devices unassigned, %d response profiles deleted, %d infrastructure devices deleted)",
			id,
			out.DeletedBuildingCount,
			out.UnassignedRackCount,
			out.UnassignedDeviceCount,
			out.DeletedResponseProfileCount,
			out.DeletedInfrastructureDeviceCount,
		),
		Metadata: map[string]any{
			"deleted_building_count":              out.DeletedBuildingCount,
			"unassigned_rack_count":               out.UnassignedRackCount,
			"unassigned_device_count":             out.UnassignedDeviceCount,
			"deleted_response_profile_count":      out.DeletedResponseProfileCount,
			"deleted_infrastructure_device_count": out.DeletedInfrastructureDeviceCount,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &out, nil
}

// assignDevicesToSiteTx carries the per-attempt counters out of the
// RunInTxWithResult closure. Declared at package scope so a tx retry
// (SQLTransactor serialization / deadlock failure) starts each attempt
// from zero — the closure constructs a fresh struct on every call.
// forceClearedIDs is populated when the force-clear branch fired so
// the post-commit audit log can record the rack-detachment side
// effect; the regular conflicts surface via txConflicts.
type assignDevicesToSiteTx struct {
	rowsAffected    int64
	txConflicts     []models.PerDeviceConflict
	forceClearedIDs []string
}

// AssignDevicesToSite enforces the cross-collection invariant and,
// on success, bulk-updates device.site_id for every identifier in one
// transaction. Per the plan, the entire batch rejects if *any* device
// fails the check; no partial writes. The conflict check and the
// UPDATE run inside the same row-locked transaction so a concurrent
// assign can't slip between them.
func (s *Service) AssignDevicesToSite(ctx context.Context, params models.AssignDevicesToSiteParams) (int64, []models.PerDeviceConflict, error) {
	identifiers := dedupeStrings(params.DeviceIdentifiers)
	if len(identifiers) == 0 {
		return 0, nil, fleeterror.NewInvalidArgumentError("device_identifiers must not be empty")
	}

	targetSiteID := params.TargetSiteID
	// Per-attempt state lives inside the RunInTxWithResult closure so a
	// SQLTransactor retry (serialization / deadlock failure on the first
	// attempt) starts from zero on every attempt. The returned struct
	// reflects only the COMMITTED attempt's totals.
	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		attempt := assignDevicesToSiteTx{}
		// Lock the target site BEFORE the device rows so this flow uses
		// the same site→device order as AssignBuildingToSite and
		// DeleteSite. Inverting the order can deadlock when a concurrent
		// AssignBuildingToSite into the same target holds the site lock
		// and waits on a device row this tx already locked.
		// target=nil/0 (Unassigned) needs no site lock.
		if targetSiteID != nil && *targetSiteID > 0 {
			if err := s.store.LockSiteForWrite(txCtx, params.OrgID, *targetSiteID); err != nil {
				return attempt, err
			}
		}
		// Row-lock the devices so the conflict check sees a stable snapshot.
		if err := s.store.LockDevicesForReassign(txCtx, params.OrgID, identifiers); err != nil {
			return attempt, err
		}
		conflicts, err := s.computeReassignConflicts(txCtx, params.OrgID, targetSiteID, identifiers)
		if err != nil {
			return attempt, err
		}
		// When the caller opted into the force-clear branch, treat
		// DEVICE_IN_RACK_AT_OTHER_SITE conflicts as a cascade-clear
		// signal instead of a rejection. DEVICE_NOT_FOUND still aborts
		// — the caller can't move what doesn't exist. Partition
		// conflicts: only identifiers whose blocker is rack-at-other-
		// site get their rack memberships cleared. Devices already at
		// the target site (no conflict) keep their rack rows, so we
		// don't cascade-delete unrelated rack_slot rows.
		if params.ForceClearConflictingRackMembership && len(conflicts) > 0 {
			var (
				clearableIDs []string
				residual     []models.PerDeviceConflict
			)
			for _, c := range conflicts {
				if c.Reason == models.ReasonDeviceInRackAtOtherSite {
					clearableIDs = append(clearableIDs, c.DeviceIdentifier)
					continue
				}
				residual = append(residual, c)
			}
			// Abort BEFORE any deletion when residual non-clearable
			// conflicts remain. Otherwise the tx would commit the
			// rack-membership delete for clearable devices and then
			// return without the site move, leaving rack-stripped
			// devices on their old site.
			if len(residual) > 0 {
				attempt.txConflicts = residual
				return attempt, nil
			}
			if len(clearableIDs) > 0 {
				if s.collectionStore == nil {
					return attempt, fleeterror.NewInternalErrorf("force-clear branch requires a collection store")
				}
				// Clear only the rack memberships for devices that
				// actually had the cross-site conflict. targetRackID=0
				// means "exclude nothing", i.e. drop every rack row
				// for the listed devices.
				if _, err := s.collectionStore.RemoveDevicesFromAnyRack(txCtx, params.OrgID, clearableIDs, 0); err != nil {
					return attempt, err
				}
				attempt.forceClearedIDs = clearableIDs
				conflicts = nil
			}
		}
		if len(conflicts) > 0 {
			// Don't return a sentinel error — SQLTransactor wraps non-
			// FleetError errors as Internal, which would surface as a
			// 500 in prod. Stash conflicts and commit the lock+reads
			// tx without writes.
			attempt.txConflicts = conflicts
			return attempt, nil
		}
		n, txErr := s.store.AssignDevicesToSite(txCtx, params.OrgID, targetSiteID, identifiers)
		if txErr != nil {
			return attempt, txErr
		}
		attempt.rowsAffected = n
		// A direct site move only writes device.site_id; a device with a
		// direct-FK device.building_id pointing at a building in the old
		// site would otherwise be left referencing a building in the
		// wrong site. Clear building_id for any moved device whose
		// building isn't in the new target site (devices already in a
		// target-site building, or with no building, are untouched).
		if _, err := s.buildingStore.ClearDeviceBuildingsOnSiteMismatch(txCtx, params.OrgID, identifiers, targetSiteID); err != nil {
			return attempt, err
		}
		return attempt, nil
	})
	if err != nil {
		return 0, nil, err
	}
	committed, _ := result.(assignDevicesToSiteTx)
	if len(committed.txConflicts) > 0 {
		return 0, committed.txConflicts, nil
	}

	// Only fire when the write happened (no conflicts; rowsAffected
	// reflects the SQL UPDATE row count).
	if committed.rowsAffected > 0 {
		orgIDVal := params.OrgID
		idents := identifiers
		if len(idents) > maxDeviceIdentifiersInMetadata {
			idents = idents[:maxDeviceIdentifiersInMetadata]
		}
		metadata := map[string]any{
			"target_site_id":     targetSiteID,
			"device_count":       committed.rowsAffected,
			"device_identifiers": idents,
		}
		description := fmt.Sprintf(
			"Reassigned %d device(s) to site %s",
			committed.rowsAffected, formatSiteIDForDescription(targetSiteID),
		)
		// Record the rack-detachment side effect alongside the site move
		// so the audit log makes the cascade visible. Truncate the
		// identifier list to the same cap the regular field uses.
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
			Type:           eventDevicesReassignedToSite,
			OrganizationID: &orgIDVal,
			SiteID:         targetSiteID,
			Description:    description,
			Metadata:       metadata,
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}
	return committed.rowsAffected, nil, nil
}

// assignBuildingsTx carries the per-attempt counters out of the
// RunInTxWithResult closure. Declared at package scope so a tx retry
// (SQLTransactor serialization / deadlock failure) starts each attempt
// from zero — the closure constructs a fresh struct on every call.
type assignBuildingsTx struct {
	rackCount   int64
	deviceCount int64
}

// AssignBuildingsToSite moves one or more buildings to a target site
// (or to "Unassigned" when TargetSiteID is nil) and cascades site_id
// down to each building's racks and their devices. Everything runs in
// one transaction; if any building fails, the batch rolls back.
// Returns the aggregate cascade counts across every building.
func (s *Service) AssignBuildingsToSite(ctx context.Context, params models.AssignBuildingsToSiteParams) (*models.AssignBuildingsToSiteResult, error) {
	buildingIDs := dedupeInt64s(params.BuildingIDs)
	if len(buildingIDs) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("building_ids must not be empty")
	}
	// Reject an explicit target_site_id == 0 so callers don't confuse
	// "Unassigned" (TargetSiteID == nil) with a zero-valued site row
	// they forgot to populate. nil stays the sentinel for unassign.
	if params.TargetSiteID != nil && *params.TargetSiteID == 0 {
		return nil, fleeterror.NewInvalidArgumentError("target_site_id must be > 0 (use nil for Unassigned)")
	}
	// Sort for stable lock order: deadlock-safe against concurrent
	// AssignBuildingsToSite touching an overlapping building set.
	sort.Slice(buildingIDs, func(i, j int) bool { return buildingIDs[i] < buildingIDs[j] })

	// Counters live inside the RunInTxWithResult closure so a
	// SQLTransactor retry (serialization / deadlock failure on the
	// first attempt) starts from zero on every attempt. The returned
	// struct reflects only the COMMITTED attempt's totals.
	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		var (
			rackCount   int64
			deviceCount int64
		)
		// Lock target site (if any) inside the tx so a concurrent
		// DeleteSite can't soft-delete it between the check and the
		// cascade writes. target=nil/0 (Unassigned) needs no lock.
		// Site-first-then-building lock order matches DeleteSite to
		// avoid deadlock.
		if params.TargetSiteID != nil && *params.TargetSiteID > 0 {
			if err := s.store.LockSiteForWrite(txCtx, params.OrgID, *params.TargetSiteID); err != nil {
				return nil, err
			}
		}
		// Phase A: sequential per-building lock acquisition in sorted
		// order so a concurrent DeleteSite owning the source site can't
		// clear racks under any of these buildings between our reads
		// and writes. Locks must be acquired one-by-one — bulk lock
		// acquisition would risk a deadlock against another
		// AssignBuildingsToSite touching an overlapping building set.
		for _, buildingID := range buildingIDs {
			if err := s.store.LockBuildingForWrite(txCtx, params.OrgID, buildingID); err != nil {
				return nil, err
			}
		}

		// Phase B1: single bulk write moving every locked building to
		// the target site. The row count tells us whether every
		// requested building is live; mismatch surfaces as NotFound
		// (matches the per-row check the old loop performed).
		rowsAffected, err := s.store.AssignBuildingsToSiteBulk(txCtx, params.OrgID, buildingIDs, params.TargetSiteID)
		if err != nil {
			return nil, err
		}
		if rowsAffected != int64(len(buildingIDs)) {
			return nil, fleeterror.NewNotFoundErrorf("one or more buildings not found (expected %d, updated %d)", len(buildingIDs), rowsAffected)
		}

		// Phase B2: single bulk rack cascade across every building in
		// the batch.
		racks, err := s.store.ReassignRacksUnderBuildingsBulk(txCtx, params.OrgID, buildingIDs, params.TargetSiteID)
		if err != nil {
			return nil, err
		}
		rackCount = racks

		// Phase B3: single bulk device cascade across every building
		// in the batch. Reaches devices via rack membership.
		devices, err := s.store.ReassignDevicesUnderBuildingsBulk(txCtx, params.OrgID, buildingIDs, params.TargetSiteID)
		if err != nil {
			return nil, err
		}
		deviceCount = devices
		// Phase B4: direct-FK device cascade. Devices with
		// device.building_id pointing at any of the moved buildings
		// (and no rack at all) wouldn't get touched by Phase B3's
		// rack-path cascade — keep them in lockstep too.
		directDevices, err := s.buildingStore.CascadeDirectDeviceSitesByBuildings(txCtx, params.OrgID, buildingIDs, params.TargetSiteID)
		if err != nil {
			return nil, err
		}
		deviceCount += directDevices
		return assignBuildingsTx{rackCount: rackCount, deviceCount: deviceCount}, nil
	})
	if err != nil {
		return nil, err
	}
	txResult, ok := result.(assignBuildingsTx)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	rackCount := txResult.rackCount
	deviceCount := txResult.deviceCount

	orgIDVal := params.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingAssignedToSite,
		OrganizationID: &orgIDVal,
		SiteID:         params.TargetSiteID,
		Description: fmt.Sprintf(
			"Assigned %d building(s) to site %s (%d racks, %d devices cascaded)",
			len(buildingIDs), formatSiteIDForDescription(params.TargetSiteID), rackCount, deviceCount,
		),
		Metadata: map[string]any{
			"building_ids":            buildingIDs,
			"target_site_id":          params.TargetSiteID,
			"reassigned_rack_count":   rackCount,
			"reassigned_device_count": deviceCount,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &models.AssignBuildingsToSiteResult{
		ReassignedRackCount:   rackCount,
		ReassignedDeviceCount: deviceCount,
	}, nil
}

// assignRacksToSiteTx carries the per-attempt counters and cascaded
// rack ids out of the RunInTxWithResult closure. Declared at package
// scope so a tx retry starts each attempt from zero — the closure
// constructs a fresh struct on every call.
type assignRacksToSiteTx struct {
	deviceCount     int64
	clearedCount    int64
	cascadedRackIDs []int64
}

// AssignRacksToSite moves one or more racks to a target site (or to
// "Unassigned" when TargetSiteID is nil) as a partial update — label,
// layout, members, and slot assignments stay untouched. building_id is
// auto-cleared on any site transition because a building belongs to a
// single site; the response carries the count of racks whose building
// was cleared so the UI can prompt the operator to pick a building in
// the new site. Same transaction cascades device.site_id for every
// rack member.
//
// Lock order: target site → each rack id ascending. Matches
// AssignBuildingsToSite + AssignDevicesToSite so concurrent site-scope
// writers can't deadlock against an overlapping rack set.
func (s *Service) AssignRacksToSite(ctx context.Context, params models.AssignRacksToSiteParams) (*models.AssignRacksToSiteResult, error) {
	if s.collectionStore == nil {
		return nil, fleeterror.NewInternalErrorf("collection store not configured")
	}
	rackIDs := dedupeInt64s(params.RackIDs)
	if len(rackIDs) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("rack_ids must not be empty")
	}
	// Reject an explicit target_site_id == 0 so callers don't confuse
	// "Unassigned" (TargetSiteID == nil) with a zero-valued site row
	// they forgot to populate. nil stays the sentinel for unassign.
	if params.TargetSiteID != nil && *params.TargetSiteID == 0 {
		return nil, fleeterror.NewInvalidArgumentError("target_site_id must be > 0 (use nil for Unassigned)")
	}
	sort.Slice(rackIDs, func(i, j int) bool { return rackIDs[i] < rackIDs[j] })

	// Counters + cascaded-id slice live inside the RunInTxWithResult
	// closure so a SQLTransactor retry (serialization / deadlock
	// failure on the first attempt) starts from zero on every attempt.
	// The returned struct reflects only the COMMITTED attempt.
	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		var (
			deviceCount     int64
			clearedCount    int64
			cascadedRackIDs []int64
		)
		// Lock target site first if assigning so a concurrent
		// DeleteSite can't soft-delete it between the check and the
		// cascade writes. target=nil/0 (Unassigned) needs no lock.
		if params.TargetSiteID != nil && *params.TargetSiteID > 0 {
			if err := s.store.LockSiteForWrite(txCtx, params.OrgID, *params.TargetSiteID); err != nil {
				return nil, err
			}
		}
		// Phase A: sequential per-rack lock acquisition in sorted order
		// (deadlock-safe). Per-rack reads classify each rack into the
		// "site changed" bucket (which drives both the SQL write set
		// and the activity-log metadata) or the same-site no-op
		// bucket.
		var changedRackIDs []int64
		for _, rackID := range rackIDs {
			current, err := s.collectionStore.LockRackPlacementForWrite(txCtx, rackID, params.OrgID)
			if err != nil {
				return nil, err
			}
			siteChanged := !int64PtrEqual(current.SiteID, params.TargetSiteID)
			if !siteChanged {
				continue
			}
			changedRackIDs = append(changedRackIDs, rackID)
			cascadedRackIDs = append(cascadedRackIDs, rackID)
			if current.BuildingID != nil {
				clearedCount++
			}
		}

		// Phase B1: single bulk update for every rack whose site
		// actually changes. building_id, zone, and grid placement
		// clear together because a building is bound to one site and
		// the partial unique index would otherwise leave stale cells
		// behind. Skip the round-trip when no rack changes site.
		if len(changedRackIDs) > 0 {
			if err := s.collectionStore.UpdateRackPlacementBulkForSite(
				txCtx, params.OrgID, changedRackIDs, params.TargetSiteID,
			); err != nil {
				return nil, err
			}

			// Phase B2: single bulk cascade for the same set.
			n, err := s.collectionStore.CascadeRackDeviceSitesBulk(
				txCtx, params.OrgID, changedRackIDs, params.TargetSiteID,
			)
			if err != nil {
				return nil, err
			}
			deviceCount += n

			// Phase B3: building cascade. UpdateRackPlacementBulkForSite
			// cleared rack.building_id for every cross-site move; the
			// devices' building_id has to follow so they don't reference
			// a building the device is no longer in. NOT routed through
			// collection.cascadeRackMembersToPlacement: a cross-site move
			// always pins building to nil regardless of its prior value, so
			// this bulk path's gate genuinely differs from the single-rack
			// paired helper — don't try to unify them.
			if _, err := s.collectionStore.CascadeRackDeviceBuildingsBulk(
				txCtx, params.OrgID, changedRackIDs, nil,
			); err != nil {
				return nil, err
			}
		}
		return assignRacksToSiteTx{
			deviceCount:     deviceCount,
			clearedCount:    clearedCount,
			cascadedRackIDs: cascadedRackIDs,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	txResult, ok := result.(assignRacksToSiteTx)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	deviceCount := txResult.deviceCount
	clearedCount := txResult.clearedCount
	cascadedRackIDs := txResult.cascadedRackIDs

	orgIDVal := params.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventRacksAssignedToSite,
		OrganizationID: &orgIDVal,
		SiteID:         params.TargetSiteID,
		Description: fmt.Sprintf(
			"Assigned %d rack(s) to site %s (%d devices cascaded, %d building(s) cleared)",
			len(rackIDs), formatSiteIDForDescription(params.TargetSiteID), deviceCount, clearedCount,
		),
		Metadata: map[string]any{
			"rack_ids":                rackIDs,
			"target_site_id":          params.TargetSiteID,
			"reassigned_device_count": deviceCount,
			"cleared_building_count":  clearedCount,
		},
	}
	if len(cascadedRackIDs) > 0 {
		event.Metadata["site_cascaded_rack_ids"] = cascadedRackIDs
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &models.AssignRacksToSiteResult{
		ReassignedDeviceCount: deviceCount,
		ClearedBuildingCount:  clearedCount,
	}, nil
}

// int64PtrEqual treats two *int64 as equal when both are nil or both
// dereference to the same value. Local helper since this is the only
// caller in the sites domain; collection.Service has its own copy.
func int64PtrEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// --- helpers ---

func (s *Service) computeReassignConflicts(ctx context.Context, orgID int64, targetSiteID *int64, identifiers []string) ([]models.PerDeviceConflict, error) {
	existingList, err := s.store.ListExistingDeviceIdentifiers(ctx, orgID, identifiers)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(existingList))
	for _, ident := range existingList {
		existing[ident] = struct{}{}
	}

	var conflicts []models.PerDeviceConflict
	for _, ident := range identifiers {
		if _, ok := existing[ident]; !ok {
			conflicts = append(conflicts, models.PerDeviceConflict{
				DeviceIdentifier: ident,
				Reason:           models.ReasonDeviceNotFound,
			})
		}
	}

	siteByDevice, err := s.store.FindDeviceSiteConflicts(ctx, orgID, identifiers)
	if err != nil {
		return nil, err
	}
	var target int64
	if targetSiteID != nil {
		target = *targetSiteID
	}
	for ident, siteID := range siteByDevice {
		if siteID == target {
			continue
		}
		conflicts = append(conflicts, models.PerDeviceConflict{
			DeviceIdentifier:  ident,
			Reason:            models.ReasonDeviceInRackAtOtherSite,
			ConflictingSiteID: siteID,
		})
	}

	// Site-less rack guard. A device in a fully-unassigned rack (no
	// site) isn't returned by FindDeviceSiteConflicts (it filters
	// dsr.site_id IS NOT NULL), yet it can't take a direct site while
	// remaining in that rack without diverging from its rack's site.
	// Flag those (clearable — force-clear drops the rack membership)
	// whenever assigning to a real site. Skipped on unassign (target
	// nil): a site-less rack member moving to Unassigned ends at site
	// nil == rack site nil, already consistent. ConflictingSiteID stays
	// 0 — the rack has no site, only the divergence is the conflict.
	if targetSiteID != nil {
		siteLess, err := s.store.FindDevicesInSiteLessRacks(ctx, orgID, identifiers)
		if err != nil {
			return nil, err
		}
		for _, ident := range siteLess {
			conflicts = append(conflicts, models.PerDeviceConflict{
				DeviceIdentifier: ident,
				Reason:           models.ReasonDeviceInRackAtOtherSite,
			})
		}
	}
	// Deterministic order — siteByDevice is a map, so the
	// rack-conflict branch above would otherwise emit conflicts
	// in random order, which makes API responses non-reproducible.
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].DeviceIdentifier < conflicts[j].DeviceIdentifier
	})
	return conflicts, nil
}

func (s *Service) computeCrossSiteOverlapWarnings(ctx context.Context, orgID, excludeID int64, prefixes []netip.Prefix) ([]string, error) {
	if len(prefixes) == 0 {
		return nil, nil
	}
	others, err := s.store.ListAllSiteNetworkConfigs(ctx, orgID, excludeID)
	if err != nil {
		return nil, err
	}
	var warnings []string
	for _, other := range others {
		if strings.TrimSpace(other.NetworkConfig) == "" {
			continue
		}
		// Re-canonicalize the other site's stored config; if canonical
		// validation now rejects it (shouldn't happen since we
		// canonicalize on save), log + surface a generic warning so we
		// don't silently drop the comparison.
		canon, cerr := CanonicalizeNetworkConfig(other.NetworkConfig)
		if cerr != nil {
			slog.WarnContext(ctx,
				"sites: failed to canonicalize stored network_config for overlap comparison",
				"site_id", other.ID, "site_name", other.Name, "error", cerr)
			warnings = append(warnings, "could not check overlap against site "+other.Name+" (stored config invalid)")
			continue
		}
		warnings = append(warnings, CrossSiteOverlapWarnings(prefixes, canon.Prefixes, other.Name)...)
	}
	return warnings, nil
}

// dedupeStrings collapses duplicates while preserving first-occurrence
// order so per-device error reporting matches the operator's input.
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

func dedupeInt64s(in []int64) []int64 {
	seen := make(map[int64]struct{}, len(in))
	out := make([]int64, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func formatSiteIDForDescription(target *int64) string {
	if target == nil {
		return "Unassigned"
	}
	return fmt.Sprintf("%d", *target)
}

// GetSiteStats rolls up telemetry + miner-state counts for every device
// in the site (racked or directly site-attached) plus the live building
// count. NotFound when the site doesn't exist in the org.
func (s *Service) GetSiteStats(ctx context.Context, orgID, siteID int64) (*models.SiteStats, error) {
	if s.deviceQueryer == nil || s.telemetry == nil {
		return nil, fleeterror.NewInternalErrorf("sites.GetSiteStats requires deviceQueryer and telemetry")
	}

	// Existence check — NotFound if the site is gone or belongs to a
	// different org. Cheaper than fetching the full row.
	exists, err := s.store.SiteBelongsToOrg(ctx, orgID, siteID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fleeterror.NewNotFoundErrorf("site %d not found", siteID)
	}

	buildingCount, err := s.store.CountBuildingsBySite(ctx, orgID, siteID)
	if err != nil {
		return nil, err
	}
	rackCount, err := s.store.CountRacksBySite(ctx, orgID, siteID)
	if err != nil {
		return nil, err
	}

	// Device identifiers scoped to the site via the existing MinerFilter.
	// SiteIDs alone is enough — site-direct devices have device.site_id
	// set; racked devices inherit site_id through the rack cascade.
	// Pass PAIRED + AUTHENTICATION_NEEDED explicitly so the stats roll-up
	// counts AUTH_NEEDED devices the same way the miner list does — without
	// this, the default PAIRED-only filter would silently undercount.
	// Limit = cap + 1 lets us detect over-cap with a single bounded query
	// rather than materializing the full identifier list before bailing.
	// The cap-exceeded guard below trips when the SQL returns cap+1 rows;
	// we never hold a slice larger than that even for an unboundedly-sized
	// site.
	deviceIDs, err := s.deviceQueryer.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, &interfaces.MinerFilter{
		SiteIDs: []int64{siteID},
		PairingStatuses: []fm.PairingStatus{
			fm.PairingStatus_PAIRING_STATUS_PAIRED,
			fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
		},
		Limit: MaxDevicesPerSiteStatsRequest + 1,
	})
	if err != nil {
		return nil, err
	}
	if len(deviceIDs) > MaxDevicesPerSiteStatsRequest {
		return nil, fleeterror.NewInternalErrorf("site %d exceeded the %d device cap", siteID, MaxDevicesPerSiteStatsRequest)
	}

	stats := &models.SiteStats{
		SiteID:        siteID,
		BuildingCount: int32(buildingCount),  //nolint:gosec // building count bounded by org config
		RackCount:     int32(rackCount),      //nolint:gosec // rack count bounded by org config
		DeviceCount:   int32(len(deviceIDs)), //nolint:gosec // device count bounded by org fleet
	}

	if len(deviceIDs) == 0 {
		return stats, nil
	}

	// State counts via the flat by-device-ids query (covers un-racked devices
	// that wouldn't appear in a collection-membership join).
	counts, err := s.deviceQueryer.GetMinerStateCountsByDeviceIDs(ctx, orgID, deviceIDs)
	if err != nil {
		return nil, err
	}
	stats.HashingCount = counts.HashingCount
	stats.BrokenCount = counts.BrokenCount
	stats.OfflineCount = counts.OfflineCount
	stats.SleepingCount = counts.SleepingCount

	componentCounts, err := s.deviceQueryer.GetComponentErrorCounts(ctx, orgID, interfaces.ComponentErrorScope{
		Kind: interfaces.ComponentErrorScopeSites,
		IDs:  []int64{siteID},
	})
	if err != nil {
		return nil, err
	}
	issues := devicerollup.AggregateComponentIssueCounts(componentCounts, siteID)
	stats.ControlBoardIssueCount = issues.ControlBoardIssueCount
	stats.FanIssueCount = issues.FanIssueCount
	stats.HashBoardIssueCount = issues.HashBoardIssueCount
	stats.PsuIssueCount = issues.PsuIssueCount

	// Telemetry rollup runs through the shared aggregator so site +
	// building stats can't drift on unit conversions or NaN handling.
	telemetryIDs := devicerollup.ToDeviceIdentifiers(deviceIDs)
	metrics, err := s.telemetry.GetLatestDeviceMetrics(ctx, telemetryIDs)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to fetch site telemetry: %v", err)
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

	return stats, nil
}

func (s *Service) populateListStats(ctx context.Context, orgID int64, rows []models.SiteWithCounts, includeStatsForSite ListStatsAuthorizer, requireTelemetry bool) error {
	if len(rows) == 0 {
		return nil
	}

	siteIDs := make([]int64, 0, len(rows))
	deviceIDsBySite := make(map[int64][]string, len(rows))
	uniqueDeviceIDs := make(map[string]struct{})
	for i := range rows {
		siteID := rows[i].Site.ID
		if !includeStatsForSite(siteID) {
			continue
		}
		siteIDs = append(siteIDs, siteID)
		rows[i].ListStats = &models.FleetListStats{
			BuildingCount: int32(rows[i].BuildingCount), //nolint:gosec // bounded by org capacity
			RackCount:     int32(rows[i].RackCount),     //nolint:gosec // bounded by org capacity
		}
		deviceIDs, err := s.deviceQueryer.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, &interfaces.MinerFilter{
			SiteIDs: []int64{siteID},
			PairingStatuses: []fm.PairingStatus{
				fm.PairingStatus_PAIRING_STATUS_PAIRED,
				fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
				fm.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
			},
			Limit: MaxDevicesPerSiteStatsRequest + 1,
		})
		if err != nil {
			return err
		}
		if len(deviceIDs) > MaxDevicesPerSiteStatsRequest {
			return fleeterror.NewInternalErrorf("site %d exceeded the %d device cap", siteID, MaxDevicesPerSiteStatsRequest)
		}
		deviceIDsBySite[siteID] = deviceIDs
		for _, id := range deviceIDs {
			uniqueDeviceIDs[id] = struct{}{}
		}
	}
	if len(siteIDs) == 0 {
		return nil
	}

	componentCounts, err := s.deviceQueryer.GetComponentErrorCounts(ctx, orgID, interfaces.ComponentErrorScope{
		Kind: interfaces.ComponentErrorScopeSites,
		IDs:  siteIDs,
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
				return fleeterror.NewInternalErrorf("failed to fetch site list telemetry: %v", err)
			}
			slog.WarnContext(ctx, "failed to fetch site list telemetry", "error", err)
			metrics = nil
		}
	}

	for i := range rows {
		stats := rows[i].ListStats
		if stats == nil {
			continue
		}
		siteID := rows[i].Site.ID
		deviceIDs := deviceIDsBySite[siteID]
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
		issues := devicerollup.AggregateComponentIssueCounts(componentCounts, siteID)
		stats.ControlBoardIssueCount = issues.ControlBoardIssueCount
		stats.FanIssueCount = issues.FanIssueCount
		stats.HashBoardIssueCount = issues.HashBoardIssueCount
		stats.PsuIssueCount = issues.PsuIssueCount
	}
	return nil
}

func filterSiteRowsByListStats(rows []models.SiteWithCounts, filter fleetlistfilter.Filter) []models.SiteWithCounts {
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
