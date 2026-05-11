package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.SiteStore = &SQLSiteStore{}

type SQLSiteStore struct {
	SQLConnectionManager
}

func NewSQLSiteStore(conn *sql.DB) *SQLSiteStore {
	return &SQLSiteStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLSiteStore) CreateSite(ctx context.Context, params models.CreateSiteParams) (*models.Site, error) {
	row, err := s.GetQueries(ctx).CreateSite(ctx, sqlc.CreateSiteParams{
		OrgID:           params.OrgID,
		Name:            params.Name,
		Description:     emptyToNullString(params.Description),
		LocationCity:    emptyToNullString(params.LocationCity),
		LocationState:   emptyToNullString(params.LocationState),
		Timezone:        emptyToNullString(params.Timezone),
		PowerCapacityMw: numericFromFloat(params.PowerCapacityMw),
		NetworkConfig:   emptyToNullString(params.NetworkConfig),
	})
	if err != nil {
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
				Description:     row.Description.String,
				LocationCity:    row.LocationCity.String,
				LocationState:   row.LocationState.String,
				Timezone:        row.Timezone.String,
				PowerCapacityMw: floatFromNumeric(row.PowerCapacityMw),
				NetworkConfig:   row.NetworkConfig.String,
				CreatedAt:       row.CreatedAt,
				UpdatedAt:       row.UpdatedAt,
			},
			DeviceCount:   row.DeviceCount,
			BuildingCount: row.BuildingCount,
			RackCount:     row.RackCount,
		})
	}
	return out, nil
}

func (s *SQLSiteStore) UpdateSite(ctx context.Context, params models.UpdateSiteParams) (*models.Site, error) {
	q := s.GetQueries(ctx)
	if err := q.UpdateSite(ctx, sqlc.UpdateSiteParams{
		Name:            params.Name,
		Description:     emptyToNullString(params.Description),
		LocationCity:    emptyToNullString(params.LocationCity),
		LocationState:   emptyToNullString(params.LocationState),
		Timezone:        emptyToNullString(params.Timezone),
		PowerCapacityMw: numericFromFloat(params.PowerCapacityMw),
		NetworkConfig:   emptyToNullString(params.NetworkConfig),
		ID:              params.ID,
		OrgID:           params.OrgID,
	}); err != nil {
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

func (s *SQLSiteStore) LockDevicesForReassign(ctx context.Context, orgID int64, deviceIdentifiers []string) error {
	if _, err := s.GetQueries(ctx).LockDevicesForReassign(ctx, sqlc.LockDevicesForReassignParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to lock devices for reassign: %v", err)
	}
	return nil
}

func (s *SQLSiteStore) ReassignDevicesToSite(ctx context.Context, orgID int64, targetSiteID *int64, deviceIdentifiers []string) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).ReassignDevicesToSite(ctx, sqlc.ReassignDevicesToSiteParams{
		OrgID:             orgID,
		TargetSiteID:      ptrToNullInt64(targetSiteID),
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to reassign devices: %v", err)
	}
	return rowsAffected, nil
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
		return 0, fleeterror.NewInternalErrorf("failed to assign building to site: %v", err)
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
		Description:     row.Description.String,
		LocationCity:    row.LocationCity.String,
		LocationState:   row.LocationState.String,
		Timezone:        row.Timezone.String,
		PowerCapacityMw: floatFromNumeric(row.PowerCapacityMw),
		NetworkConfig:   row.NetworkConfig.String,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}
