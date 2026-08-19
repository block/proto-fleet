package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
)

type SQLAlertMaintenanceWindowStore struct {
	SQLTransactor
}

func NewSQLAlertMaintenanceWindowStore(conn *sql.DB) *SQLAlertMaintenanceWindowStore {
	return &SQLAlertMaintenanceWindowStore{SQLTransactor: *NewSQLTransactor(conn)}
}

var _ alerts.MaintenanceWindowStore = (*SQLAlertMaintenanceWindowStore)(nil)

// pq.Array encodes a nil slice as SQL NULL rather than '{}'. Use for array args whose SQL
// needs a real (possibly empty) array: NOT NULL columns like this store's all-targets
// ("every rule/channel") scopes, and cardinality(...) = 0 all-values branches like the
// activity log's site filter (cardinality NULL ≠ 0 would silently match nothing).
func emptyIfNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (s *SQLAlertMaintenanceWindowStore) Insert(ctx context.Context, rec alerts.MaintenanceWindowRecord) (alerts.MaintenanceWindowRecord, error) {
	row, err := s.GetQueries(ctx).InsertAlertMaintenanceWindow(ctx, sqlc.InsertAlertMaintenanceWindowParams{
		OrgID:      rec.OrganizationID,
		RuleUids:   emptyIfNil(rec.RuleUIDs),
		ChannelIds: emptyIfNil(rec.ChannelIDs),
		StartsAt:   rec.StartsAt,
		EndsAt:     rec.EndsAt,
		Comment:    rec.Comment,
		CreatedBy:  rec.CreatedBy,
	})
	if err != nil {
		return alerts.MaintenanceWindowRecord{}, err
	}
	return maintenanceWindowRecordFromRow(row), nil
}

func (s *SQLAlertMaintenanceWindowStore) Update(ctx context.Context, rec alerts.MaintenanceWindowRecord) (alerts.MaintenanceWindowRecord, error) {
	row, err := s.GetQueries(ctx).UpdateAlertMaintenanceWindow(ctx, sqlc.UpdateAlertMaintenanceWindowParams{
		RuleUids:   emptyIfNil(rec.RuleUIDs),
		ChannelIds: emptyIfNil(rec.ChannelIDs),
		StartsAt:   rec.StartsAt,
		EndsAt:     rec.EndsAt,
		Comment:    rec.Comment,
		ID:         rec.ID,
		OrgID:      rec.OrganizationID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return alerts.MaintenanceWindowRecord{}, alerts.ErrNotFound
	}
	if err != nil {
		return alerts.MaintenanceWindowRecord{}, err
	}
	return maintenanceWindowRecordFromRow(row), nil
}

func (s *SQLAlertMaintenanceWindowStore) List(ctx context.Context, orgID int64) ([]alerts.MaintenanceWindowRecord, error) {
	rows, err := s.GetQueries(ctx).ListAlertMaintenanceWindows(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return maintenanceWindowRecordsFromRows(rows), nil
}

func (s *SQLAlertMaintenanceWindowStore) ListActive(ctx context.Context, orgID int64, now time.Time) ([]alerts.MaintenanceWindowRecord, error) {
	rows, err := s.GetQueries(ctx).ListActiveAlertMaintenanceWindows(ctx, sqlc.ListActiveAlertMaintenanceWindowsParams{
		OrgID: orgID,
		Now:   now,
	})
	if err != nil {
		return nil, err
	}
	return maintenanceWindowRecordsFromRows(rows), nil
}

func (s *SQLAlertMaintenanceWindowStore) CountUnexpired(ctx context.Context, orgID int64, now time.Time, excludingID int64) (int64, error) {
	return s.GetQueries(ctx).CountUnexpiredAlertMaintenanceWindows(ctx, sqlc.CountUnexpiredAlertMaintenanceWindowsParams{
		OrgID:       orgID,
		Now:         now,
		ExcludingID: excludingID,
	})
}

func (s *SQLAlertMaintenanceWindowStore) PruneExpired(ctx context.Context, orgID int64, now time.Time, retention time.Duration, keepNewest int64) (int64, error) {
	return s.GetQueries(ctx).PruneExpiredAlertMaintenanceWindows(ctx, sqlc.PruneExpiredAlertMaintenanceWindowsParams{
		OrgID:      orgID,
		Now:        now,
		Before:     now.Add(-retention),
		KeepNewest: keepNewest,
	})
}

func (s *SQLAlertMaintenanceWindowStore) Delete(ctx context.Context, orgID, id int64) error {
	n, err := s.GetQueries(ctx).DeleteAlertMaintenanceWindow(ctx, sqlc.DeleteAlertMaintenanceWindowParams{
		ID:    id,
		OrgID: orgID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return alerts.ErrNotFound
	}
	return nil
}

func maintenanceWindowRecordsFromRows(rows []sqlc.AlertMaintenanceWindow) []alerts.MaintenanceWindowRecord {
	out := make([]alerts.MaintenanceWindowRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, maintenanceWindowRecordFromRow(row))
	}
	return out
}

func maintenanceWindowRecordFromRow(row sqlc.AlertMaintenanceWindow) alerts.MaintenanceWindowRecord {
	return alerts.MaintenanceWindowRecord{
		ID:             row.ID,
		OrganizationID: row.OrgID,
		RuleUIDs:       row.RuleUids,
		ChannelIDs:     row.ChannelIds,
		StartsAt:       row.StartsAt,
		EndsAt:         row.EndsAt,
		Comment:        row.Comment,
		CreatedBy:      row.CreatedBy,
		CreatedAt:      row.CreatedAt,
	}
}
