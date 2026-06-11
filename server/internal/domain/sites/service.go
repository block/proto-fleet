// Package sites is the domain layer for the SiteService RPC surface.
// It owns network_config validation, the cross-collection invariant
// enforced on bulk reassignments and building site moves, and the
// site delete cascade.
package sites

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/devicerollup"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for sites activity logs.
const (
	eventSiteCreated             = "site.created"
	eventSiteUpdated             = "site.updated"
	eventSiteDeleted             = "site.deleted"
	eventDevicesReassignedToSite = "devices.reassigned_to_site"
	eventBuildingAssignedToSite  = "building.assigned_to_site"
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
	store         interfaces.SiteStore
	buildingStore interfaces.BuildingStore
	deviceQueryer devicerollup.DeviceQueryer
	telemetry     devicerollup.TelemetryCollector
	transactor    interfaces.Transactor
	activitySvc   *activity.Service
}

// NewService wires a SiteStore, Transactor, and the activity Service
// used for fire-and-forget audit logs. activitySvc may be nil in tests
// or in environments where activity logging is disabled; activity.Log
// is nil-receiver-safe.
//
// buildingStore, deviceQueryer, and telemetry power GetSiteStats only.
// Any of them may be nil in test setups where the stats RPC isn't
// exercised; GetSiteStats returns an internal error in that case.
func NewService(
	store interfaces.SiteStore,
	buildingStore interfaces.BuildingStore,
	deviceQueryer devicerollup.DeviceQueryer,
	telemetry devicerollup.TelemetryCollector,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:         store,
		buildingStore: buildingStore,
		deviceQueryer: deviceQueryer,
		telemetry:     telemetry,
		transactor:    transactor,
		activitySvc:   activitySvc,
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

	site, err := s.store.CreateSite(ctx, params)
	if err != nil {
		return nil, err
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

	site, err := s.store.UpdateSite(ctx, params)
	if err != nil {
		return nil, err
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

// ListSites returns sites with attachment counts for the delete-confirm
// dialog impact numbers.
func (s *Service) ListSites(ctx context.Context, orgID int64) ([]models.SiteWithCounts, error) {
	return s.store.ListSites(ctx, orgID)
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
		// Clear rack→building linkage + zone for racks under any
		// building of this site, BEFORE the buildings disappear.
		if _, err := s.store.UnassignRacksFromBuildingsBySite(txCtx, orgID, id); err != nil {
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
			"Deleted site %d (%d buildings, %d racks, %d devices unassigned, %d response profiles deleted)",
			id,
			out.DeletedBuildingCount,
			out.UnassignedRackCount,
			out.UnassignedDeviceCount,
			out.DeletedResponseProfileCount,
		),
		Metadata: map[string]any{
			"deleted_building_count":         out.DeletedBuildingCount,
			"unassigned_rack_count":          out.UnassignedRackCount,
			"unassigned_device_count":        out.UnassignedDeviceCount,
			"deleted_response_profile_count": out.DeletedResponseProfileCount,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &out, nil
}

// ReassignDevicesToSite enforces the cross-collection invariant and,
// on success, bulk-updates device.site_id for every identifier in one
// transaction. Per the plan, the entire batch rejects if *any* device
// fails the check; no partial writes. The conflict check and the
// UPDATE run inside the same row-locked transaction so a concurrent
// reassign can't slip between them.
func (s *Service) ReassignDevicesToSite(ctx context.Context, params models.ReassignDevicesToSiteParams) (int64, []models.PerDeviceConflict, error) {
	identifiers := dedupeStrings(params.DeviceIdentifiers)
	if len(identifiers) == 0 {
		return 0, nil, fleeterror.NewInvalidArgumentError("device_identifiers must not be empty")
	}

	var (
		rowsAffected int64
		txConflicts  []models.PerDeviceConflict
		targetSiteID = params.TargetSiteID
	)
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock the target site BEFORE the device rows so this flow uses
		// the same site→device order as AssignBuildingToSite and
		// DeleteSite. Inverting the order can deadlock when a concurrent
		// AssignBuildingToSite into the same target holds the site lock
		// and waits on a device row this tx already locked.
		// target=nil/0 (Unassigned) needs no site lock.
		if targetSiteID != nil && *targetSiteID > 0 {
			if err := s.store.LockSiteForWrite(txCtx, params.OrgID, *targetSiteID); err != nil {
				return err
			}
		}
		// Row-lock the devices so the conflict check sees a stable snapshot.
		if err := s.store.LockDevicesForReassign(txCtx, params.OrgID, identifiers); err != nil {
			return err
		}
		conflicts, err := s.computeReassignConflicts(txCtx, params.OrgID, targetSiteID, identifiers)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			// Don't return a sentinel error — SQLTransactor wraps non-
			// FleetError errors as Internal, which would surface as a
			// 500 in prod. Stash conflicts and commit the lock+reads
			// tx without writes.
			txConflicts = conflicts
			return nil
		}
		n, txErr := s.store.ReassignDevicesToSite(txCtx, params.OrgID, targetSiteID, identifiers)
		if txErr != nil {
			return txErr
		}
		rowsAffected = n
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	if len(txConflicts) > 0 {
		return 0, txConflicts, nil
	}

	// Only fire when the write happened (no conflicts; rowsAffected
	// reflects the SQL UPDATE row count).
	if rowsAffected > 0 {
		orgIDVal := params.OrgID
		idents := identifiers
		if len(idents) > maxDeviceIdentifiersInMetadata {
			idents = idents[:maxDeviceIdentifiersInMetadata]
		}
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventDevicesReassignedToSite,
			OrganizationID: &orgIDVal,
			SiteID:         targetSiteID,
			Description: fmt.Sprintf(
				"Reassigned %d device(s) to site %s",
				rowsAffected, formatSiteIDForDescription(targetSiteID),
			),
			Metadata: map[string]any{
				"target_site_id":     targetSiteID,
				"device_count":       rowsAffected,
				"device_identifiers": idents,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}
	return rowsAffected, nil, nil
}

// AssignBuildingToSite moves a building to a different site (or to
// "Unassigned" when TargetSiteID is nil) and cascades site_id down to
// the building's racks and their devices in one transaction. Returns
// the cascade counts.
func (s *Service) AssignBuildingToSite(ctx context.Context, params models.AssignBuildingToSiteParams) (*models.AssignBuildingToSiteResult, error) {
	var (
		rackCount   int64
		deviceCount int64
	)
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// Lock target site (if any) inside the tx so a concurrent
		// DeleteSite can't soft-delete it between the check and the
		// cascade writes. target=nil/0 (Unassigned) needs no lock.
		// Site-first-then-building lock order matches DeleteSite to
		// avoid deadlock.
		if params.TargetSiteID != nil && *params.TargetSiteID > 0 {
			if err := s.store.LockSiteForWrite(txCtx, params.OrgID, *params.TargetSiteID); err != nil {
				return err
			}
		}
		// Lock the building so a concurrent DeleteSite that owns the
		// source site can't clear this building's racks while we
		// reassign them. Same site→building lock order DeleteSite uses.
		if err := s.store.LockBuildingForWrite(txCtx, params.OrgID, params.BuildingID); err != nil {
			return err
		}
		rowsAffected, err := s.store.AssignBuildingToSite(txCtx, params.OrgID, params.BuildingID, params.TargetSiteID)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return fleeterror.NewNotFoundErrorf("building %d not found", params.BuildingID)
		}
		rackCount, err = s.store.ReassignRacksUnderBuilding(txCtx, params.OrgID, params.BuildingID, params.TargetSiteID)
		if err != nil {
			return err
		}
		deviceCount, err = s.store.ReassignDevicesUnderBuilding(txCtx, params.OrgID, params.BuildingID, params.TargetSiteID)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	orgIDVal := params.OrgID
	buildingIDVal := params.BuildingID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventBuildingAssignedToSite,
		OrganizationID: &orgIDVal,
		SiteID:         params.TargetSiteID,
		Description: fmt.Sprintf(
			"Assigned building %d to site %s (%d racks, %d devices cascaded)",
			buildingIDVal, formatSiteIDForDescription(params.TargetSiteID), rackCount, deviceCount,
		),
		Metadata: map[string]any{
			"building_id":             buildingIDVal,
			"target_site_id":          params.TargetSiteID,
			"reassigned_rack_count":   rackCount,
			"reassigned_device_count": deviceCount,
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)

	return &models.AssignBuildingToSiteResult{
		ReassignedRackCount:   rackCount,
		ReassignedDeviceCount: deviceCount,
	}, nil
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
	if s.deviceQueryer == nil || s.telemetry == nil || s.buildingStore == nil {
		return nil, fleeterror.NewInternalErrorf("sites.GetSiteStats requires deviceQueryer, telemetry, and buildingStore")
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

	// Building count from the buildings store.
	bldgs, err := s.buildingStore.ListBuildings(ctx, buildingsmodels.ListFilter{OrgID: orgID, SiteID: &siteID})
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
		BuildingCount: int32(len(bldgs)),     //nolint:gosec // building count bounded by org config
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
	stats.TotalHashrateThs = rollup.TotalHashrateThs
	stats.TotalPowerKw = rollup.TotalPowerKw
	stats.AvgEfficiencyJth = rollup.AvgEfficiencyJth

	return stats, nil
}
