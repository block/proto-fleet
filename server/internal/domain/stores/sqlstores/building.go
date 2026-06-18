package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.BuildingStore = &SQLBuildingStore{}

type SQLBuildingStore struct {
	SQLConnectionManager
}

func NewSQLBuildingStore(conn *sql.DB) *SQLBuildingStore {
	return &SQLBuildingStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLBuildingStore) CreateBuilding(ctx context.Context, params models.CreateParams) (*models.Building, error) {
	row, err := s.GetQueries(ctx).CreateBuilding(ctx, sqlc.CreateBuildingParams{
		OrgID:                 params.OrgID,
		SiteID:                ptrToNullInt64(params.SiteID),
		Name:                  params.Name,
		Description:           emptyToNullString(params.Description),
		PowerKw:               numericFromFloat(params.PowerKw),
		OverheadKw:            numericFromFloat(params.OverheadKw),
		Aisles:                zeroToNullInt32(params.Aisles),
		PhysicalRackCount:     zeroToNullInt32(params.PhysicalRackCount),
		RacksPerAisle:         zeroToNullInt32(params.RacksPerAisle),
		DefaultRackRows:       zeroToNullInt32(params.DefaultRackRows),
		DefaultRackColumns:    zeroToNullInt32(params.DefaultRackColumns),
		DefaultRackOrderIndex: int16(params.DefaultRackOrderIndex),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fleeterror.NewPlainError("a building with this name already exists in the site", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return nil, fleeterror.NewInternalErrorf("failed to create building: %v", err)
	}
	return s.GetBuilding(ctx, params.OrgID, row.ID)
}

func (s *SQLBuildingStore) GetBuilding(ctx context.Context, orgID, id int64) (*models.Building, error) {
	row, err := s.GetQueries(ctx).GetBuilding(ctx, sqlc.GetBuildingParams{ID: id, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("building %d not found", id)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get building: %v", err)
	}
	out := buildingFromGetRow(row)
	return &out, nil
}

func (s *SQLBuildingStore) ListBuildings(ctx context.Context, filter models.ListFilter) ([]models.BuildingWithCounts, error) {
	siteIDs := filter.SiteIDs
	if siteIDs == nil {
		siteIDs = []int64{}
	}
	rows, err := s.GetQueries(ctx).ListBuildingsByOrg(ctx, sqlc.ListBuildingsByOrgParams{
		OrgID:             filter.OrgID,
		SiteIds:           siteIDs,
		IncludeUnassigned: filter.IncludeUnassigned,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list buildings: %v", err)
	}
	out := make([]models.BuildingWithCounts, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.BuildingWithCounts{
			Building: models.Building{
				ID:                    row.ID,
				OrgID:                 row.OrgID,
				SiteID:                nullInt64ToPtr(row.SiteID),
				SiteLabel:             row.SiteLabel,
				Name:                  row.Name,
				Description:           row.Description.String,
				PowerKw:               floatFromNumeric(row.PowerKw),
				OverheadKw:            floatFromNumeric(row.OverheadKw),
				Aisles:                row.Aisles.Int32,
				PhysicalRackCount:     row.PhysicalRackCount.Int32,
				RacksPerAisle:         row.RacksPerAisle.Int32,
				DefaultRackRows:       row.DefaultRackRows.Int32,
				DefaultRackColumns:    row.DefaultRackColumns.Int32,
				DefaultRackOrderIndex: models.RackOrderIndex(row.DefaultRackOrderIndex),
				CreatedAt:             row.CreatedAt,
				UpdatedAt:             row.UpdatedAt,
			},
			RackCount:   row.RackCount,
			DeviceCount: row.DeviceCount,
		})
	}
	return out, nil
}

func (s *SQLBuildingStore) UpdateBuilding(ctx context.Context, params models.UpdateParams) (*models.Building, error) {
	if err := s.GetQueries(ctx).UpdateBuilding(ctx, sqlc.UpdateBuildingParams{
		Name:                  params.Name,
		Description:           emptyToNullString(params.Description),
		PowerKw:               numericFromFloat(params.PowerKw),
		OverheadKw:            numericFromFloat(params.OverheadKw),
		Aisles:                zeroToNullInt32(params.Aisles),
		PhysicalRackCount:     zeroToNullInt32(params.PhysicalRackCount),
		RacksPerAisle:         zeroToNullInt32(params.RacksPerAisle),
		DefaultRackRows:       zeroToNullInt32(params.DefaultRackRows),
		DefaultRackColumns:    zeroToNullInt32(params.DefaultRackColumns),
		DefaultRackOrderIndex: int16(params.DefaultRackOrderIndex),
		ID:                    params.ID,
		OrgID:                 params.OrgID,
	}); err != nil {
		if isUniqueViolation(err) {
			return nil, fleeterror.NewPlainError("a building with this name already exists in the site", connect.CodeAlreadyExists).WithCallerStackTrace()
		}
		return nil, fleeterror.NewInternalErrorf("failed to update building: %v", err)
	}
	return s.GetBuilding(ctx, params.OrgID, params.ID)
}

func (s *SQLBuildingStore) SoftDeleteBuilding(ctx context.Context, orgID, id int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).SoftDeleteBuilding(ctx, sqlc.SoftDeleteBuildingParams{ID: id, OrgID: orgID})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to soft-delete building: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLBuildingStore) UnassignRacksFromBuilding(ctx context.Context, orgID, buildingID int64) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).UnassignRacksFromBuilding(ctx, sqlc.UnassignRacksFromBuildingParams{
		OrgID:      orgID,
		BuildingID: zeroToNullInt64(buildingID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to unassign racks from building: %v", err)
	}
	return rowsAffected, nil
}

func (s *SQLBuildingStore) BuildingBelongsToOrg(ctx context.Context, orgID, id int64) (bool, error) {
	belongs, err := s.GetQueries(ctx).BuildingBelongsToOrg(ctx, sqlc.BuildingBelongsToOrgParams{ID: id, OrgID: orgID})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to check building ownership: %v", err)
	}
	return belongs, nil
}

func (s *SQLBuildingStore) BuildingsByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.GetQueries(ctx).BuildingsByIDs(ctx, sqlc.BuildingsByIDsParams{
		OrgID: orgID,
		Ids:   ids,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to look up buildings by ID: %v", err)
	}
	return rows, nil
}

func (s *SQLBuildingStore) ListBuildingRacks(ctx context.Context, orgID, buildingID int64, pageSize int32, pageToken string) ([]models.BuildingRack, string, error) {
	cursor, err := decodeBuildingRackCursor(pageToken)
	if err != nil {
		return nil, "", err
	}
	params := sqlc.ListBuildingRacksParams{
		OrgID:      orgID,
		BuildingID: zeroToNullInt64(buildingID),
		// Fetch one extra row so we can detect whether the next page
		// exists without an additional COUNT query.
		LimitN: pageSize + 1,
	}
	if cursor != nil {
		params.CursorLabel = sql.NullString{String: cursor.Label, Valid: true}
		params.CursorID = sql.NullInt64{Int64: cursor.ID, Valid: true}
	}
	rows, err := s.GetQueries(ctx).ListBuildingRacks(ctx, params)
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("failed to list building racks: %v", err)
	}
	var nextPageToken string
	// Compare in int space — pageSize is service-clamped to
	// ListBuildingRacksMaxPageSize (1000), so the conversion is
	// always safe. Cast pageSize → int rather than len(rows) → int32
	// to keep gosec G115 happy.
	if len(rows) > int(pageSize) {
		// Trim the probe row and encode a cursor at the last in-page row.
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		nextPageToken = encodeBuildingRackCursor(&buildingRackCursor{Label: last.RackLabel, ID: last.RackID})
	}
	out := make([]models.BuildingRack, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.BuildingRack{
			RackID:          row.RackID,
			RackLabel:       row.RackLabel,
			AisleIndex:      nullInt32ToPtr(row.AisleIndex),
			PositionInAisle: nullInt32ToPtr(row.PositionInAisle),
		})
	}
	return out, nextPageToken, nil
}

func (s *SQLBuildingStore) ListRacksOutsideBuildingBounds(ctx context.Context, orgID, buildingID int64, newAisles, newRacksPerAisle int32) ([]models.BuildingRack, error) {
	rows, err := s.GetQueries(ctx).ListRacksOutsideBuildingBounds(ctx, sqlc.ListRacksOutsideBuildingBoundsParams{
		OrgID:            orgID,
		BuildingID:       zeroToNullInt64(buildingID),
		NewAisles:        newAisles,
		NewRacksPerAisle: newRacksPerAisle,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to scan out-of-bounds racks: %v", err)
	}
	out := make([]models.BuildingRack, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.BuildingRack{
			RackID:          row.RackID,
			RackLabel:       row.RackLabel,
			AisleIndex:      nullInt32ToPtr(row.AisleIndex),
			PositionInAisle: nullInt32ToPtr(row.PositionInAisle),
		})
	}
	return out, nil
}

func (s *SQLBuildingStore) SetRackBuildingPosition(ctx context.Context, orgID, rackID int64, aisleIndex, positionInAisle *int32) error {
	err := s.GetQueries(ctx).SetRackBuildingPosition(ctx, sqlc.SetRackBuildingPositionParams{
		RackID:          rackID,
		OrgID:           orgID,
		AisleIndex:      ptrToNullInt32(aisleIndex),
		PositionInAisle: ptrToNullInt32(positionInAisle),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to set rack building position: %v", err)
	}
	return nil
}

func (s *SQLBuildingStore) SetRackBuildingPositionBulkClear(ctx context.Context, orgID int64, rackIDs []int64) error {
	if len(rackIDs) == 0 {
		return nil
	}
	if err := s.GetQueries(ctx).SetRackBuildingPositionBulkClear(ctx, sqlc.SetRackBuildingPositionBulkClearParams{
		RackIds: rackIDs,
		OrgID:   orgID,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to bulk-clear rack building positions: %w", err)
	}
	return nil
}

func (s *SQLBuildingStore) SetRackBuildingPositionBulkPlace(ctx context.Context, orgID int64, rackIDs []int64, aisleIndexes, positionInAisles []int32) error {
	if len(rackIDs) == 0 {
		return nil
	}
	if len(rackIDs) != len(aisleIndexes) || len(rackIDs) != len(positionInAisles) {
		return fleeterror.NewInternalErrorf("SetRackBuildingPositionBulkPlace: array length mismatch (rackIDs=%d aisles=%d positions=%d)", len(rackIDs), len(aisleIndexes), len(positionInAisles))
	}
	if err := s.GetQueries(ctx).SetRackBuildingPositionBulkPlace(ctx, sqlc.SetRackBuildingPositionBulkPlaceParams{
		OrgID:            orgID,
		RackIds:          rackIDs,
		AisleIndexes:     aisleIndexes,
		PositionInAisles: positionInAisles,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to bulk-place rack building positions: %w", err)
	}
	return nil
}

func (s *SQLBuildingStore) AssignDevicesToBuilding(ctx context.Context, orgID int64, targetBuildingID *int64, deviceIdentifiers []string) (int64, error) {
	rowsAffected, err := s.GetQueries(ctx).AssignDevicesToBuilding(ctx, sqlc.AssignDevicesToBuildingParams{
		OrgID:             orgID,
		TargetBuildingID:  ptrToNullInt64(targetBuildingID),
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to reassign devices to building: %w", err)
	}
	return rowsAffected, nil
}

func (s *SQLBuildingStore) CascadeDevicesSiteForBuilding(ctx context.Context, orgID int64, deviceIdentifiers []string, targetSiteID *int64) (int64, error) {
	if len(deviceIdentifiers) == 0 {
		return 0, nil
	}
	rowsAffected, err := s.GetQueries(ctx).CascadeDevicesSiteForBuilding(ctx, sqlc.CascadeDevicesSiteForBuildingParams{
		OrgID:             orgID,
		TargetSiteID:      ptrToNullInt64(targetSiteID),
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to cascade device sites for building: %w", err)
	}
	return rowsAffected, nil
}

func (s *SQLBuildingStore) FindDeviceBuildingConflicts(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]int64, error) {
	rows, err := s.GetQueries(ctx).FindDeviceBuildingConflicts(ctx, sqlc.FindDeviceBuildingConflictsParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to find device building conflicts: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.DeviceIdentifier] = r.ConflictingBuildingID
	}
	return out, nil
}

func (s *SQLBuildingStore) FindDevicesInBuildingLessPlacedRacks(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]string, error) {
	if len(deviceIdentifiers) == 0 {
		return nil, nil
	}
	rows, err := s.GetQueries(ctx).FindDevicesInBuildingLessPlacedRacks(ctx, sqlc.FindDevicesInBuildingLessPlacedRacksParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to find devices in building-less placed racks: %w", err)
	}
	return rows, nil
}

func (s *SQLBuildingStore) GetBuildingSiteID(ctx context.Context, orgID, buildingID int64) (*int64, error) {
	siteID, err := s.GetQueries(ctx).GetBuildingSiteID(ctx, sqlc.GetBuildingSiteIDParams{
		ID:    buildingID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("building %d not found", buildingID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to read building site id: %w", err)
	}
	return nullInt64ToPtr(siteID), nil
}

func (s *SQLBuildingStore) ClearDeviceBuildingsByBuilding(ctx context.Context, orgID, buildingID int64) (int64, error) {
	n, err := s.GetQueries(ctx).ClearDeviceBuildingsByBuilding(ctx, sqlc.ClearDeviceBuildingsByBuildingParams{
		OrgID:      orgID,
		BuildingID: buildingID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to clear device buildings by building: %w", err)
	}
	return n, nil
}

func (s *SQLBuildingStore) ClearDeviceBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error) {
	n, err := s.GetQueries(ctx).ClearDeviceBuildingsBySite(ctx, sqlc.ClearDeviceBuildingsBySiteParams{
		OrgID:  orgID,
		SiteID: siteID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to clear device buildings by site: %w", err)
	}
	return n, nil
}

func (s *SQLBuildingStore) CascadeDirectDeviceSitesByBuildings(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error) {
	if len(buildingIDs) == 0 {
		return 0, nil
	}
	n, err := s.GetQueries(ctx).CascadeDirectDeviceSitesByBuildings(ctx, sqlc.CascadeDirectDeviceSitesByBuildingsParams{
		OrgID:        orgID,
		BuildingIds:  buildingIDs,
		TargetSiteID: ptrToNullInt64(targetSiteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to cascade direct device sites by buildings: %w", err)
	}
	return n, nil
}

func (s *SQLBuildingStore) ClearDeviceBuildingsOnSiteMismatch(ctx context.Context, orgID int64, deviceIdentifiers []string, targetSiteID *int64) (int64, error) {
	if len(deviceIdentifiers) == 0 {
		return 0, nil
	}
	n, err := s.GetQueries(ctx).ClearDeviceBuildingsOnSiteMismatch(ctx, sqlc.ClearDeviceBuildingsOnSiteMismatchParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
		TargetSiteID:      ptrToNullInt64(targetSiteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to clear device buildings on site mismatch: %w", err)
	}
	return n, nil
}

func buildingFromGetRow(row sqlc.GetBuildingRow) models.Building {
	return models.Building{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		SiteID:                nullInt64ToPtr(row.SiteID),
		SiteLabel:             row.SiteLabel,
		Name:                  row.Name,
		Description:           row.Description.String,
		PowerKw:               floatFromNumeric(row.PowerKw),
		OverheadKw:            floatFromNumeric(row.OverheadKw),
		Aisles:                row.Aisles.Int32,
		PhysicalRackCount:     row.PhysicalRackCount.Int32,
		RacksPerAisle:         row.RacksPerAisle.Int32,
		DefaultRackRows:       row.DefaultRackRows.Int32,
		DefaultRackColumns:    row.DefaultRackColumns.Int32,
		DefaultRackOrderIndex: models.RackOrderIndex(row.DefaultRackOrderIndex),
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
}
