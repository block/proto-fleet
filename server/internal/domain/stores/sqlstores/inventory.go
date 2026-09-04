package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.InventoryStore = (*SQLInventoryStore)(nil)

// SQLInventoryStore persists organization-scoped inventory through prepared
// sqlc queries. It honors transaction-bound queries carried by ctx.
type SQLInventoryStore struct {
	SQLConnectionManager
}

func NewSQLInventoryStore(conn *sql.DB) *SQLInventoryStore {
	return &SQLInventoryStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLInventoryStore) Create(ctx context.Context, params models.CreateParams) (*models.InventoryPart, error) {
	id, err := s.GetQueries(ctx).CreateInventoryPart(ctx, sqlc.CreateInventoryPartParams{
		OrgID:        params.OrgID,
		Name:         params.Name,
		Type:         params.Type,
		Manufacturer: ptrToNullString(params.Manufacturer),
		PartNumber:   ptrToNullString(params.PartNumber),
		SiteID:       ptrToNullInt64(params.SiteID),
		OnHand:       params.OnHand,
		ReorderPoint: params.ReorderPoint,
		BinLocation:  ptrToNullString(params.BinLocation),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fleeterror.NewAlreadyExistsErrorf("inventory part %q already exists at this site", params.Name)
		}
		if isForeignKeyViolationOn(err, "fk_inventory_part_site") {
			return nil, fleeterror.NewNotFoundError("inventory site not found")
		}
		return nil, fleeterror.NewInternalErrorf("failed to create inventory part: %v", err)
	}
	return s.Get(ctx, params.OrgID, id)
}

func (s *SQLInventoryStore) Get(ctx context.Context, orgID, id int64) (*models.InventoryPart, error) {
	row, err := s.GetQueries(ctx).GetInventoryPart(ctx, sqlc.GetInventoryPartParams{ID: id, OrgID: orgID})
	if err != nil {
		return nil, inventoryGetError(err, id)
	}
	part := inventoryPartFromGetRow(row)
	return &part, nil
}

func (s *SQLInventoryStore) GetForUpdate(ctx context.Context, orgID, id int64) (*models.InventoryPart, error) {
	row, err := s.GetQueries(ctx).GetInventoryPartForUpdate(ctx, sqlc.GetInventoryPartForUpdateParams{ID: id, OrgID: orgID})
	if err != nil {
		return nil, inventoryGetError(err, id)
	}
	part := inventoryPartFromForUpdateRow(row)
	return &part, nil
}

func (s *SQLInventoryStore) List(ctx context.Context, filter models.ListFilter) ([]models.InventoryPart, error) {
	rows, err := s.GetQueries(ctx).ListInventoryParts(ctx, sqlc.ListInventoryPartsParams{
		OrgID:          filter.OrgID,
		FilterSiteIds:  inventoryNilIfEmpty(filter.SiteIDs),
		FilterTypes:    inventoryNilIfEmpty(filter.Types),
		FilterLowStock: filter.LowStockOnly,
		CursorID:       ptrToNullInt64(filter.CursorID),
		LimitN:         filter.Limit,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list inventory parts: %v", err)
	}
	parts := make([]models.InventoryPart, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, inventoryPartFromListRow(row))
	}
	return parts, nil
}

func (s *SQLInventoryStore) Count(ctx context.Context, filter models.ListFilter) (int32, error) {
	count, err := s.GetQueries(ctx).CountInventoryParts(ctx, sqlc.CountInventoryPartsParams{
		OrgID:          filter.OrgID,
		FilterSiteIds:  inventoryNilIfEmpty(filter.SiteIDs),
		FilterTypes:    inventoryNilIfEmpty(filter.Types),
		FilterLowStock: filter.LowStockOnly,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count inventory parts: %v", err)
	}
	return count, nil
}

func (s *SQLInventoryStore) Update(ctx context.Context, params models.UpdateParams) (*models.InventoryPart, error) {
	rows, err := s.GetQueries(ctx).UpdateInventoryPart(ctx, sqlc.UpdateInventoryPartParams{
		OnHand:       ptrToNullInt32(params.OnHand),
		ReorderPoint: ptrToNullInt32(params.ReorderPoint),
		BinLocation:  ptrToNullString(params.BinLocation),
		ID:           params.ID,
		OrgID:        params.OrgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to update inventory part: %v", err)
	}
	if rows == 0 {
		if _, getErr := s.Get(ctx, params.OrgID, params.ID); getErr != nil {
			return nil, getErr
		}
		return nil, fleeterror.NewFailedPreconditionError("on_hand cannot be less than allocated stock")
	}
	return s.Get(ctx, params.OrgID, params.ID)
}

func (s *SQLInventoryStore) SoftDelete(ctx context.Context, orgID, id int64) (int64, error) {
	rows, err := s.GetQueries(ctx).SoftDeleteInventoryPart(ctx, sqlc.SoftDeleteInventoryPartParams{ID: id, OrgID: orgID})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to delete inventory part: %v", err)
	}
	if rows == 0 {
		if _, getErr := s.Get(ctx, orgID, id); getErr != nil {
			return 0, getErr
		}
		return 0, fleeterror.NewFailedPreconditionError("allocated inventory part cannot be deleted")
	}
	return rows, nil
}

func (s *SQLInventoryStore) GetInsights(ctx context.Context, orgID int64) (*models.InventoryInsights, error) {
	row, err := s.GetQueries(ctx).GetInventoryInsights(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get inventory insights: %v", err)
	}
	return &models.InventoryInsights{
		TotalOnHand:    row.TotalOnHand,
		TotalAllocated: row.TotalAllocated,
		LowStockCount:  row.LowStockCount,
		SitesCount:     row.SitesCount,
		PartTypes:      row.PartTypes,
	}, nil
}

func (s *SQLInventoryStore) ListPartsBySite(ctx context.Context, orgID, siteID int64) ([]models.InventoryPart, error) {
	rows, err := s.GetQueries(ctx).ListPartsBySite(ctx, sqlc.ListPartsBySiteParams{
		OrgID: orgID, SiteID: sql.NullInt64{Int64: siteID, Valid: true},
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list inventory parts by site: %v", err)
	}
	parts := make([]models.InventoryPart, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, inventoryPartFromSiteRow(row))
	}
	return parts, nil
}

func (s *SQLInventoryStore) Reserve(ctx context.Context, orgID, id int64, quantity int32) error {
	if quantity <= 0 {
		return fleeterror.NewInvalidArgumentError("reservation quantity must be greater than zero")
	}
	rows, err := s.GetQueries(ctx).ReserveInventoryPart(ctx, sqlc.ReserveInventoryPartParams{
		Quantity: quantity, ID: id, OrgID: orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to reserve inventory part: %v", err)
	}
	return s.stockMutationResult(ctx, orgID, id, rows, "insufficient available stock")
}

func (s *SQLInventoryStore) Release(ctx context.Context, orgID, id int64, quantity int32) error {
	if quantity <= 0 {
		return fleeterror.NewInvalidArgumentError("release quantity must be greater than zero")
	}
	rows, err := s.GetQueries(ctx).ReleaseInventoryPart(ctx, sqlc.ReleaseInventoryPartParams{
		Quantity: quantity, ID: id, OrgID: orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to release inventory part: %v", err)
	}
	return s.stockMutationResult(ctx, orgID, id, rows, "insufficient reserved stock")
}

func (s *SQLInventoryStore) ConsumeReserved(ctx context.Context, orgID, id int64, quantity int32) error {
	if quantity <= 0 {
		return fleeterror.NewInvalidArgumentError("consumption quantity must be greater than zero")
	}
	rows, err := s.GetQueries(ctx).ConsumeReservedInventoryPart(ctx, sqlc.ConsumeReservedInventoryPartParams{
		Quantity: quantity, ID: id, OrgID: orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to consume reserved inventory part: %v", err)
	}
	return s.stockMutationResult(ctx, orgID, id, rows, "insufficient reserved stock")
}

func (s *SQLInventoryStore) ResolveSiteByName(ctx context.Context, orgID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	id, err := s.GetQueries(ctx).ResolveInventorySiteByName(ctx, sqlc.ResolveInventorySiteByNameParams{OrgID: orgID, Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fleeterror.NewNotFoundErrorf("inventory site %q not found", name)
		}
		return 0, fleeterror.NewInternalErrorf("failed to resolve inventory site: %v", err)
	}
	return id, nil
}

func (s *SQLInventoryStore) BulkCreate(ctx context.Context, orgID int64, rows []models.ResolvedCsvRow) (int32, error) {
	var created int32
	for _, row := range rows {
		_, err := s.Create(ctx, models.CreateParams{
			OrgID:        orgID,
			Name:         row.Name,
			Type:         row.Type,
			Manufacturer: row.Manufacturer,
			PartNumber:   row.PartNumber,
			SiteID:       row.SiteID,
			OnHand:       row.OnHand,
			ReorderPoint: row.ReorderPoint,
			BinLocation:  row.BinLocation,
		})
		if err != nil {
			return 0, fmt.Errorf("CSV row %d: %w", row.RowNumber, err)
		}
		created++
	}
	return created, nil
}

func (s *SQLInventoryStore) stockMutationResult(ctx context.Context, orgID, id, rows int64, message string) error {
	if rows > 0 {
		return nil
	}
	if _, err := s.Get(ctx, orgID, id); err != nil {
		return err
	}
	return fleeterror.NewFailedPreconditionError(message)
}

func inventoryGetError(err error, id int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundErrorf("inventory part %d not found", id)
	}
	return fleeterror.NewInternalErrorf("failed to get inventory part: %v", err)
}

func inventoryPart(
	id, orgID int64,
	name, partType string,
	manufacturer, partNumber sql.NullString,
	siteID sql.NullInt64,
	siteName string,
	onHand, allocated, reorderPoint int32,
	binLocation sql.NullString,
	createdAt, updatedAt sql.NullTime,
	deletedAt sql.NullTime,
) models.InventoryPart {
	return models.InventoryPart{
		ID:           id,
		OrgID:        orgID,
		Name:         name,
		Type:         partType,
		Manufacturer: inventoryStringPtr(manufacturer),
		PartNumber:   inventoryStringPtr(partNumber),
		SiteID:       nullInt64ToPtr(siteID),
		SiteName:     siteName,
		OnHand:       onHand,
		Allocated:    allocated,
		ReorderPoint: reorderPoint,
		BinLocation:  inventoryStringPtr(binLocation),
		CreatedAt:    createdAt.Time,
		UpdatedAt:    updatedAt.Time,
		DeletedAt:    inventoryTimePtr(deletedAt),
	}
}

func inventoryPartFromGetRow(row sqlc.GetInventoryPartRow) models.InventoryPart {
	return inventoryPart(row.ID, row.OrgID, row.Name, row.Type, row.Manufacturer, row.PartNumber, row.SiteID, row.SiteName,
		row.OnHand, row.Allocated, row.ReorderPoint, row.BinLocation,
		sql.NullTime{Time: row.CreatedAt, Valid: true}, sql.NullTime{Time: row.UpdatedAt, Valid: true}, row.DeletedAt)
}

func inventoryPartFromForUpdateRow(row sqlc.GetInventoryPartForUpdateRow) models.InventoryPart {
	return inventoryPart(row.ID, row.OrgID, row.Name, row.Type, row.Manufacturer, row.PartNumber, row.SiteID, row.SiteName,
		row.OnHand, row.Allocated, row.ReorderPoint, row.BinLocation,
		sql.NullTime{Time: row.CreatedAt, Valid: true}, sql.NullTime{Time: row.UpdatedAt, Valid: true}, row.DeletedAt)
}

func inventoryPartFromListRow(row sqlc.ListInventoryPartsRow) models.InventoryPart {
	return inventoryPart(row.ID, row.OrgID, row.Name, row.Type, row.Manufacturer, row.PartNumber, row.SiteID, row.SiteName,
		row.OnHand, row.Allocated, row.ReorderPoint, row.BinLocation,
		sql.NullTime{Time: row.CreatedAt, Valid: true}, sql.NullTime{Time: row.UpdatedAt, Valid: true}, row.DeletedAt)
}

func inventoryPartFromSiteRow(row sqlc.ListPartsBySiteRow) models.InventoryPart {
	return inventoryPart(row.ID, row.OrgID, row.Name, row.Type, row.Manufacturer, row.PartNumber, row.SiteID, row.SiteName,
		row.OnHand, row.Allocated, row.ReorderPoint, row.BinLocation,
		sql.NullTime{Time: row.CreatedAt, Valid: true}, sql.NullTime{Time: row.UpdatedAt, Valid: true}, row.DeletedAt)
}

func inventoryStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func inventoryTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}

func inventoryNilIfEmpty[T any](values []T) []T {
	if len(values) == 0 {
		return nil
	}
	return values
}
