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

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
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

// Service is the domain entry point for site CRUD, device reassignment,
// and building site reassignment. The transactor is required: the
// site delete cascade and the bulk-reassign all-or-nothing semantics
// both depend on it.
type Service struct {
	store       interfaces.SiteStore
	transactor  interfaces.Transactor
	activitySvc *activity.Service
}

// NewService wires a SiteStore, Transactor, and the activity Service
// used for fire-and-forget audit logs. activitySvc may be nil in tests
// or in environments where activity logging is disabled; all
// logActivity calls nil-check before firing.
func NewService(store interfaces.SiteStore, transactor interfaces.Transactor, activitySvc *activity.Service) *Service {
	return &Service{store: store, transactor: transactor, activitySvc: activitySvc}
}

// logActivity is the nil-safe fire-and-forget wrapper. Audit failures
// must not fail user RPCs, so we use Service.Log (not LogStrict).
func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc == nil {
		return
	}
	s.activitySvc.Log(ctx, event)
}

// stampActor populates the per-user fields on an activity event from
// session.Info when present. The activity service warns on a nil
// OrganizationID for non-auth categories, so we always set it from the
// session even if the service-side caller already populated it.
func stampActor(ctx context.Context, e *activitymodels.Event) {
	info, err := session.GetInfo(ctx)
	if err != nil || info == nil {
		return
	}
	if e.UserID == nil && info.ExternalUserID != "" {
		uid := info.ExternalUserID
		e.UserID = &uid
	}
	if e.Username == nil && info.Username != "" {
		uname := info.Username
		e.Username = &uname
	}
	if e.OrganizationID == nil && info.OrganizationID != 0 {
		oid := info.OrganizationID
		e.OrganizationID = &oid
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
	stampActor(ctx, &event)
	s.logActivity(ctx, event)

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
	stampActor(ctx, &event)
	s.logActivity(ctx, event)

	return &UpdateResult{Site: site, NetworkConfigWarnings: warnings}, nil
}

// ListSites returns sites with attachment counts for the delete-confirm
// dialog impact numbers.
func (s *Service) ListSites(ctx context.Context, orgID int64) ([]models.SiteWithCounts, error) {
	return s.store.ListSites(ctx, orgID)
}

// DeleteSite soft-deletes the site and cascade-unassigns its devices,
// racks, and buildings in one transaction. Returns the impact counts.
func (s *Service) DeleteSite(ctx context.Context, orgID, id int64) (*models.DeleteSiteResult, error) {
	var out models.DeleteSiteResult
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		// 0. Lock the site row first so two concurrent DeleteSite calls
		// can't both cascade. If the row is already soft-deleted/gone,
		// LockSiteForWrite returns NotFound and we bail.
		if err := s.store.LockSiteForWrite(txCtx, orgID, id); err != nil {
			return err
		}
		// 1. Clear rack→building linkage + zone for racks under any
		// building of this site, BEFORE the buildings disappear.
		if _, err := s.store.UnassignRacksFromBuildingsBySite(txCtx, orgID, id); err != nil {
			return err
		}
		// 2. Soft-delete buildings under the site.
		deletedBuildings, err := s.store.SoftDeleteBuildingsBySite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.DeletedBuildingCount = deletedBuildings
		// 3. Unassign racks directly under the site.
		rackCount, err := s.store.UnassignRacksFromSite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.UnassignedRackCount = rackCount
		// 4. Unassign devices.
		deviceCount, err := s.store.UnassignDevicesFromSite(txCtx, orgID, id)
		if err != nil {
			return err
		}
		out.UnassignedDeviceCount = deviceCount
		// 5. Soft-delete the site row last.
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
			"Deleted site %d (%d buildings, %d racks, %d devices unassigned)",
			id, out.DeletedBuildingCount, out.UnassignedRackCount, out.UnassignedDeviceCount,
		),
		Metadata: map[string]any{
			"deleted_building_count":  out.DeletedBuildingCount,
			"unassigned_rack_count":   out.UnassignedRackCount,
			"unassigned_device_count": out.UnassignedDeviceCount,
		},
	}
	stampActor(ctx, &event)
	s.logActivity(ctx, event)

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
		// Row-lock first so the conflict check sees a stable snapshot.
		if err := s.store.LockDevicesForReassign(txCtx, params.OrgID, identifiers); err != nil {
			return err
		}
		if targetSiteID != nil && *targetSiteID > 0 {
			if err := s.store.LockSiteForWrite(txCtx, params.OrgID, *targetSiteID); err != nil {
				return err
			}
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
		stampActor(ctx, &event)
		s.logActivity(ctx, event)
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
		if params.TargetSiteID != nil && *params.TargetSiteID > 0 {
			if err := s.store.LockSiteForWrite(txCtx, params.OrgID, *params.TargetSiteID); err != nil {
				return err
			}
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
	stampActor(ctx, &event)
	s.logActivity(ctx, event)

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
