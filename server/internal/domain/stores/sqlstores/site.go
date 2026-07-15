package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesdomain "github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.SiteStore = &SQLSiteStore{}

type SQLSiteStore struct {
	SQLConnectionManager
}

func NewSQLSiteStore(conn *sql.DB, opts ...ConnectionManagerOption) *SQLSiteStore {
	return &SQLSiteStore{SQLConnectionManager: NewSQLConnectionManager(conn, opts...)}
}

func (s *SQLSiteStore) CreateSite(ctx context.Context, params models.CreateSiteParams) (*models.Site, error) {
	if strings.TrimSpace(params.Slug) == "" {
		usedSlugs, err := s.ListSiteSlugs(ctx, params.OrgID)
		if err != nil {
			return nil, err
		}
		params.Slug = sitesdomain.GenerateSiteSlug(params.Name, usedSlugs)
	}
	row, err := s.GetQueries(ctx).CreateSite(ctx, sqlc.CreateSiteParams{
		OrgID:           params.OrgID,
		Name:            params.Name,
		Slug:            params.Slug,
		LocationCity:    emptyToNullString(params.LocationCity),
		LocationState:   emptyToNullString(params.LocationState),
		Timezone:        emptyToNullString(params.Timezone),
		PowerCapacityMw: numericFromFloat(params.PowerCapacityMw),
		NetworkConfig:   emptyToNullString(params.NetworkConfig),
		Address:         emptyToNullString(params.Address),
		PostalCode:      emptyToNullString(params.PostalCode),
		Country:         emptyToNullString(params.Country),
		Notes:           emptyToNullString(params.Notes),
	})
	if err != nil {
		if isUniqueViolationOn(err, "uk_site_org_slug") {
			return nil, models.ErrSiteSlugCollision
		}
		if isUniqueViolation(err) {
			return nil, fleeterror.NewPlainError("a site with this name already exists", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return nil, fleeterror.NewInternalErrorf("failed to create site: %v", err)
	}
	out := siteFromRow(row)
	return &out, nil
}

func (s *SQLSiteStore) GetSite(ctx context.Context, orgID, id int64) (*models.Site, error) {
	row, err := s.GetQueries(ctx).GetSite(ctx, sqlc.GetSiteParams{ID: id, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("site %d not found", id)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get site: %v", err)
	}
	out := siteFromRow(row)
	return &out, nil
}

func (s *SQLSiteStore) GetSiteBySlug(ctx context.Context, orgID int64, slug string) (*models.Site, error) {
	row, err := s.GetQueries(ctx).GetSiteBySlug(ctx, sqlc.GetSiteBySlugParams{Slug: slug, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("site %q not found", slug)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get site by slug: %v", err)
	}
	out := siteFromRow(row)
	return &out, nil
}

func (s *SQLSiteStore) ListSites(ctx context.Context, orgID int64) ([]models.SiteWithCounts, error) {
	rows, err := s.GetQueries(ctx).ListSites(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list sites: %v", err)
	}
	out := make([]models.SiteWithCounts, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.SiteWithCounts{
			Site: models.Site{
				ID:              row.ID,
				OrgID:           row.OrgID,
				Name:            row.Name,
				Slug:            row.Slug,
				LocationCity:    row.LocationCity.String,
				LocationState:   row.LocationState.String,
				Timezone:        row.Timezone.String,
				PowerCapacityMw: floatFromNumeric(row.PowerCapacityMw),
				NetworkConfig:   row.NetworkConfig.String,
				Address:         row.Address.String,
				PostalCode:      row.PostalCode.String,
				Country:         row.Country,
				Notes:           row.Notes.String,
				CreatedAt:       row.CreatedAt,
				UpdatedAt:       row.UpdatedAt,
			},
			DeviceCount:               row.DeviceCount,
			BuildingCount:             row.BuildingCount,
			RackCount:                 row.RackCount,
			InfrastructureDeviceCount: row.InfrastructureDeviceCount,
		})
	}
	return out, nil
}

func (s *SQLSiteStore) ListSiteSlugs(ctx context.Context, orgID int64) ([]string, error) {
	slugs, err := s.GetQueries(ctx).ListSiteSlugs(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list site slugs: %v", err)
	}
	return slugs, nil
}

func (s *SQLSiteStore) CountRacksBySite(ctx context.Context, orgID, siteID int64) (int64, error) {
	count, err := s.GetQueries(ctx).CountRacksBySite(ctx, sqlc.CountRacksBySiteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count racks by site: %v", err)
	}
	return count, nil
}

func (s *SQLSiteStore) CountBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error) {
	count, err := s.GetQueries(ctx).CountBuildingsBySite(ctx, sqlc.CountBuildingsBySiteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count buildings by site: %v", err)
	}
	return count, nil
}

func (s *SQLSiteStore) UpdateSite(ctx context.Context, params models.UpdateSiteParams) (*models.Site, error) {
	q := s.GetQueries(ctx)
	if err := q.UpdateSite(ctx, sqlc.UpdateSiteParams{
		Name:            params.Name,
		Slug:            params.Slug,
		LocationCity:    emptyToNullString(params.LocationCity),
		LocationState:   emptyToNullString(params.LocationState),
		Timezone:        emptyToNullString(params.Timezone),
		PowerCapacityMw: numericFromFloat(params.PowerCapacityMw),
		NetworkConfig:   emptyToNullString(params.NetworkConfig),
		Address:         emptyToNullString(params.Address),
		PostalCode:      emptyToNullString(params.PostalCode),
		Country:         emptyToNullString(params.Country),
		Notes:           emptyToNullString(params.Notes),
		ID:              params.ID,
		OrgID:           params.OrgID,
	}); err != nil {
		if isUniqueViolationOn(err, "uk_site_org_slug") {
			return nil, models.ErrSiteSlugCollision
		}
		if isUniqueViolation(err) {
			return nil, fleeterror.NewPlainError("a site with this name already exists", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return nil, fleeterror.NewInternalErrorf("failed to update site: %v", err)
	}
	return s.GetSite(ctx, params.OrgID, params.ID)
}

func (s *SQLSiteStore) SoftDeleteSite(ctx context.Context, orgID, id int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).SoftDeleteSite(ctx, sqlc.SoftDeleteSiteParams{ID: id, OrgID: orgID})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to soft-delete site: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) UnassignDevicesFromSite(ctx context.Context, orgID, siteID int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).UnassignDevicesFromSite(ctx, sqlc.UnassignDevicesFromSiteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to unassign devices: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) DeleteCurtailmentResponseProfilesBySite(ctx context.Context, orgID, siteID int64) (int64, error) {
	row, err := s.GetQueries(ctx).DeleteCurtailmentResponseProfilesBySite(ctx, sqlc.DeleteCurtailmentResponseProfilesBySiteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	})
	if err != nil {
		if isForeignKeyViolationOn(err, "fk_curtailment_automation_rule_response_profile") {
			return 0, fleeterror.NewFailedPreconditionError("site has curtailment response profiles referenced by automation rules")
		}
		return 0, fleeterror.NewInternalErrorf("failed to delete curtailment response profiles by site: %v", err)
	}
	if row.BlockingRuleCount > 0 {
		return 0, fleeterror.NewFailedPreconditionError("site has curtailment response profiles referenced by automation rules")
	}
	return row.DeletedCount, nil
}

func (s *SQLSiteStore) SoftDeleteBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).SoftDeleteBuildingsBySite(ctx, sqlc.SoftDeleteBuildingsBySiteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to soft-delete buildings: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) SoftDeleteInfrastructureDevicesBySite(ctx context.Context, orgID, siteID int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).SoftDeleteInfrastructureDevicesBySite(ctx, sqlc.SoftDeleteInfrastructureDevicesBySiteParams{
		OrgID:  orgID,
		SiteID: siteID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to soft-delete infrastructure devices: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) UnassignRacksFromSite(ctx context.Context, orgID, siteID int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).UnassignRacksFromSite(ctx, sqlc.UnassignRacksFromSiteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to unassign racks from site: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) UnassignRacksFromBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).UnassignRacksFromBuildingsBySite(ctx, sqlc.UnassignRacksFromBuildingsBySiteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to unassign racks from buildings: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) SiteBelongsToOrg(ctx context.Context, orgID, id int64) (bool, error) {
	belongs, err := s.GetQueries(ctx).SiteBelongsToOrg(ctx, sqlc.SiteBelongsToOrgParams{ID: id, OrgID: orgID})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to check site ownership: %v", err)
	}
	return belongs, nil
}

func (s *SQLSiteStore) SitesByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.GetQueries(ctx).SitesByIDs(ctx, sqlc.SitesByIDsParams{
		OrgID: orgID,
		Ids:   ids,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to look up sites by ID: %v", err)
	}
	return rows, nil
}

func (s *SQLSiteStore) LockSiteForWrite(ctx context.Context, orgID, siteID int64) error {
	if _, err := s.GetQueries(ctx).LockSiteForWrite(ctx, sqlc.LockSiteForWriteParams{ID: siteID, OrgID: orgID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("site %d not found", siteID)
		}
		return fleeterror.NewInternalErrorf("failed to lock site for write: %v", err)
	}
	return nil
}

func (s *SQLSiteStore) LockBuildingForWrite(ctx context.Context, orgID, buildingID int64) error {
	if _, err := s.GetQueries(ctx).LockBuildingForWrite(ctx, sqlc.LockBuildingForWriteParams{ID: buildingID, OrgID: orgID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("building %d not found", buildingID)
		}
		return fleeterror.NewInternalErrorf("failed to lock building for write: %v", err)
	}
	return nil
}

func (s *SQLSiteStore) LockBuildingsBySiteForWrite(ctx context.Context, orgID, siteID int64) error {
	// Empty result is not an error — no live building under the site means
	// no row to lock and no conflict to serialize against.
	if _, err := s.GetQueries(ctx).LockBuildingsBySiteForWrite(ctx, sqlc.LockBuildingsBySiteForWriteParams{
		OrgID:  orgID,
		SiteID: zeroToNullInt64(siteID),
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to lock buildings by site for write: %v", err)
	}
	return nil
}

func (s *SQLSiteStore) LockDevicesForReassign(ctx context.Context, orgID int64, deviceIdentifiers []string) error {
	if _, err := s.GetQueries(ctx).LockDevicesForReassign(ctx, sqlc.LockDevicesForReassignParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to lock devices for reassign: %w", err)
	}
	return nil
}

func (s *SQLSiteStore) AssignDevicesToSite(ctx context.Context, orgID int64, targetSiteID *int64, deviceIdentifiers []string) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).AssignDevicesToSite(ctx, sqlc.AssignDevicesToSiteParams{
		OrgID:             orgID,
		TargetSiteID:      ptrToNullInt64(targetSiteID),
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to reassign devices: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) GetDistinctDeviceSiteIDs(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]*int64, error) {
	rows, err := s.GetQueries(ctx).GetDistinctDeviceSiteIDs(ctx, sqlc.GetDistinctDeviceSiteIDsParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to query distinct device site_ids: %v", err)
	}
	return nullInt64sToPtrs(rows), nil
}

func (s *SQLSiteStore) FindDeviceSiteConflicts(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]int64, error) {
	rows, err := s.GetQueries(ctx).FindDeviceSiteConflicts(ctx, sqlc.FindDeviceSiteConflictsParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to find device site conflicts: %v", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.DeviceIdentifier] = r.ConflictingSiteID
	}
	return out, nil
}

func (s *SQLSiteStore) FindDevicesInSiteLessRacks(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]string, error) {
	rows, err := s.GetQueries(ctx).FindDevicesInSiteLessRacks(ctx, sqlc.FindDevicesInSiteLessRacksParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to find devices in site-less racks: %v", err)
	}
	return rows, nil
}

func (s *SQLSiteStore) ListExistingDeviceIdentifiers(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]string, error) {
	rows, err := s.GetQueries(ctx).ListExistingDeviceIdentifiers(ctx, sqlc.ListExistingDeviceIdentifiersParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list existing device identifiers: %v", err)
	}
	return rows, nil
}

func (s *SQLSiteStore) AssignBuildingToSite(ctx context.Context, orgID, buildingID int64, targetSiteID *int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).AssignBuildingToSite(ctx, sqlc.AssignBuildingToSiteParams{
		SiteID: ptrToNullInt64(targetSiteID),
		ID:     buildingID,
		OrgID:  orgID,
	})
	if err != nil {
		// uk_building_site_name (partial unique on site_id + name) rejects
		// a move when the target site already has a live building with the
		// same name. Mirror Create/UpdateBuilding's mapping so the operator
		// gets an actionable AlreadyExists rather than a 500.
		if isUniqueViolation(err) {
			return 0, fleeterror.NewPlainError("a building with this name already exists in the target site", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return 0, fleeterror.NewInternalErrorf("failed to assign building to site: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) AssignBuildingsToSiteBulk(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error) {
	if len(buildingIDs) == 0 {
		return 0, nil
	}
	rowsAffected, err := s.GetQueries(ctx).AssignBuildingsToSiteBulk(ctx, sqlc.AssignBuildingsToSiteBulkParams{
		SiteID:      ptrToNullInt64(targetSiteID),
		BuildingIds: buildingIDs,
		OrgID:       orgID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fleeterror.NewPlainError("a building with this name already exists in the target site", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return 0, fleeterror.NewInternalErrorf("failed to bulk-assign buildings to site: %w", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) ReassignRacksUnderBuilding(ctx context.Context, orgID, buildingID int64, targetSiteID *int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).ReassignRacksUnderBuilding(ctx, sqlc.ReassignRacksUnderBuildingParams{
		TargetSiteID: ptrToNullInt64(targetSiteID),
		OrgID:        orgID,
		BuildingID:   zeroToNullInt64(buildingID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to reassign racks under building: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) ReassignRacksUnderBuildingsBulk(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error) {
	if len(buildingIDs) == 0 {
		return 0, nil
	}
	rowsAffected, err := s.GetQueries(ctx).ReassignRacksUnderBuildingsBulk(ctx, sqlc.ReassignRacksUnderBuildingsBulkParams{
		TargetSiteID: ptrToNullInt64(targetSiteID),
		OrgID:        orgID,
		BuildingIds:  buildingIDs,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to bulk-reassign racks under buildings: %w", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) ReassignDevicesUnderBuilding(ctx context.Context, orgID, buildingID int64, targetSiteID *int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).ReassignDevicesUnderBuilding(ctx, sqlc.ReassignDevicesUnderBuildingParams{
		TargetSiteID: ptrToNullInt64(targetSiteID),
		OrgID:        orgID,
		BuildingID:   zeroToNullInt64(buildingID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to reassign devices under building: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) ReassignDevicesUnderBuildingsBulk(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error) {
	if len(buildingIDs) == 0 {
		return 0, nil
	}
	rowsAffected, err := s.GetQueries(ctx).ReassignDevicesUnderBuildingsBulk(ctx, sqlc.ReassignDevicesUnderBuildingsBulkParams{
		TargetSiteID: ptrToNullInt64(targetSiteID),
		OrgID:        orgID,
		BuildingIds:  buildingIDs,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to bulk-reassign devices under buildings: %w", err)
	}
	return rowsAffected, nil
}

func (s *SQLSiteStore) ListAllSiteNetworkConfigs(ctx context.Context, orgID, excludeID int64) ([]models.SiteNetworkConfigEntry, error) {
	rows, err := s.GetQueries(ctx).ListSiteNetworkConfigsForOverlap(ctx, sqlc.ListSiteNetworkConfigsForOverlapParams{
		OrgID:     orgID,
		ExcludeID: excludeID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list site network configs: %v", err)
	}
	out := make([]models.SiteNetworkConfigEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.SiteNetworkConfigEntry{
			ID:            r.ID,
			Name:          r.Name,
			NetworkConfig: r.NetworkConfig.String,
		})
	}
	return out, nil
}

func siteFromRow(row sqlc.Site) models.Site {
	return models.Site{
		ID:              row.ID,
		OrgID:           row.OrgID,
		Name:            row.Name,
		Slug:            row.Slug,
		LocationCity:    row.LocationCity.String,
		LocationState:   row.LocationState.String,
		Timezone:        row.Timezone.String,
		PowerCapacityMw: floatFromNumeric(row.PowerCapacityMw),
		NetworkConfig:   row.NetworkConfig.String,
		Address:         row.Address.String,
		PostalCode:      row.PostalCode.String,
		Country:         row.Country,
		Notes:           row.Notes.String,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}
