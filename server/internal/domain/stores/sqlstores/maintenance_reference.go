package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.MaintenanceReferenceStore = (*SQLMaintenanceStore)(nil)

// NewSQLMaintenanceReferenceStore returns the same transaction-aware store
// used for repair tickets; it is retained as an explicit wiring constructor.
func NewSQLMaintenanceReferenceStore(conn *sql.DB) *SQLMaintenanceStore {
	return NewSQLMaintenanceStore(conn)
}

func (s *SQLMaintenanceStore) LockSiteForTicket(ctx context.Context, orgID, siteID int64) error {
	_, err := s.GetQueries(ctx).LockMaintenanceSiteForTicket(ctx, sqlc.LockMaintenanceSiteForTicketParams{
		OrgID: orgID, SiteID: siteID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("site %d not found", siteID)
		}
		return fleeterror.NewInternalErrorf("failed to lock maintenance site: %v", err)
	}
	return nil
}

func (s *SQLMaintenanceStore) ResolveMinerContext(ctx context.Context, orgID int64, minerIdentifier string) (*models.AssetContext, error) {
	row, err := s.GetQueries(ctx).ResolveMaintenanceMinerContext(ctx, sqlc.ResolveMaintenanceMinerContextParams{
		OrgID: orgID, MinerIdentifier: minerIdentifier,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("miner %q not found", minerIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve maintenance miner: %v", err)
	}
	return &models.AssetContext{
		MinerIdentifier: row.MinerIdentifier,
		SiteID:          nullInt64ToPtr(row.SiteID),
		SiteName:        row.SiteName,
		BuildingID:      nullInt64ToPtr(row.BuildingID),
		BuildingName:    row.BuildingName,
		Zone:            stringPtr(row.Zone),
		RackID:          nullInt64ToPtr(row.RackID),
		RackLabel:       stringPtr(row.RackLabel),
		GroupLabel:      nonEmptyStringPtr(row.GroupLabel),
	}, nil
}

func (s *SQLMaintenanceStore) ResolveLocationContext(ctx context.Context, orgID int64, siteID, buildingID *int64) (*models.AssetContext, error) {
	row, err := s.GetQueries(ctx).ResolveMaintenanceLocationContext(ctx, sqlc.ResolveMaintenanceLocationContextParams{
		OrgID: orgID, SiteID: ptrToNullInt64(siteID), BuildingID: ptrToNullInt64(buildingID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("maintenance site or building not found")
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve maintenance location: %v", err)
	}
	return &models.AssetContext{
		SiteID: nullInt64ToPtr(row.SiteID), SiteName: row.SiteName,
		BuildingID: nullInt64ToPtr(row.BuildingID), BuildingName: row.BuildingName,
	}, nil
}

func (s *SQLMaintenanceStore) ResolveAssignee(ctx context.Context, orgID, userID int64) (*models.Assignee, error) {
	row, err := s.GetQueries(ctx).ResolveMaintenanceAssignee(ctx, sqlc.ResolveMaintenanceAssigneeParams{OrgID: orgID, UserID: userID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("maintenance assignee %d not found", userID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve maintenance assignee: %v", err)
	}
	return &models.Assignee{UserID: row.UserID, Username: row.Username, RoleName: row.RoleName}, nil
}

func (s *SQLMaintenanceStore) ListAssignees(ctx context.Context, orgID int64) ([]models.Assignee, error) {
	rows, err := s.GetQueries(ctx).ListMaintenanceAssignees(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list maintenance assignees: %v", err)
	}
	assignees := make([]models.Assignee, 0, len(rows))
	for _, row := range rows {
		assignees = append(assignees, models.Assignee{UserID: row.UserID, Username: row.Username, RoleName: row.RoleName})
	}
	return assignees, nil
}

func nonEmptyStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
