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
	out := buildingFromRow(row)
	return &out, nil
}

func (s *SQLBuildingStore) GetBuilding(ctx context.Context, orgID, id int64) (*models.Building, error) {
	row, err := s.GetQueries(ctx).GetBuilding(ctx, sqlc.GetBuildingParams{ID: id, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("building %d not found", id)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get building: %v", err)
	}
	out := buildingFromRow(row)
	return &out, nil
}

func (s *SQLBuildingStore) ListBuildings(ctx context.Context, filter models.ListFilter) ([]models.BuildingWithCounts, error) {
	rows, err := s.GetQueries(ctx).ListBuildingsByOrg(ctx, sqlc.ListBuildingsByOrgParams{
		OrgID:          filter.OrgID,
		SiteID:         ptrToNullInt64(filter.SiteID),
		UnassignedOnly: sql.NullBool{Bool: filter.UnassignedOnly, Valid: filter.UnassignedOnly},
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
			RackCount: row.RackCount,
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

func buildingFromRow(row sqlc.Building) models.Building {
	return models.Building{
		ID:                    row.ID,
		OrgID:                 row.OrgID,
		SiteID:                nullInt64ToPtr(row.SiteID),
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
