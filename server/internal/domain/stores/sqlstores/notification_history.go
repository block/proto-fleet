package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
)

// > Grafana repeat_interval (1h, notification-policies.yaml) with margin for one missed re-notify; keep in sync.
const activeAlertStaleAfter = 135 * time.Minute

type SQLNotificationHistoryStore struct {
	SQLConnectionManager
}

func NewSQLNotificationHistoryStore(conn *sql.DB) *SQLNotificationHistoryStore {
	return &SQLNotificationHistoryStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

var _ notificationhistory.Store = (*SQLNotificationHistoryStore)(nil)
var _ notificationhistory.Lister = (*SQLNotificationHistoryStore)(nil)

func marshalNotificationJSON(m map[string]string) (json.RawMessage, error) {
	if m == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal notification json: %w", err)
	}
	return b, nil
}

func (s *SQLNotificationHistoryStore) Insert(ctx context.Context, n *notificationhistory.Notification) error {
	params, err := insertNotificationParams(n)
	if err != nil {
		return err
	}
	return s.GetQueries(ctx).InsertNotificationHistory(ctx, params)
}

// InsertBatch persists many notifications atomically in one transaction so a large outage (one
// org-grouped notification with thousands of alerts) lands or rolls back as a unit; the caller can
// then treat success as "every alert persisted". Each row goes through the generated sqlc query
// (AGENTS.md: all DB access through sqlc), bound to the transaction.
func (s *SQLNotificationHistoryStore) InsertBatch(ctx context.Context, notifs []*notificationhistory.Notification) error {
	if len(notifs) == 0 {
		return nil
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin notification batch tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	q := sqlc.New(tx)
	for _, n := range notifs {
		params, err := insertNotificationParams(n)
		if err != nil {
			return err
		}
		if err := q.InsertNotificationHistory(ctx, params); err != nil {
			return fmt.Errorf("insert notification batch row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notification batch: %w", err)
	}
	committed = true
	return nil
}

func insertNotificationParams(n *notificationhistory.Notification) (sqlc.InsertNotificationHistoryParams, error) {
	labels, err := marshalNotificationJSON(n.Labels)
	if err != nil {
		return sqlc.InsertNotificationHistoryParams{}, fmt.Errorf("marshal notification labels: %w", err)
	}
	annotations, err := marshalNotificationJSON(n.Annotations)
	if err != nil {
		return sqlc.InsertNotificationHistoryParams{}, fmt.Errorf("marshal notification annotations: %w", err)
	}
	return sqlc.InsertNotificationHistoryParams{
		AlertName:      n.AlertName,
		Status:         n.Status,
		Severity:       n.Severity,
		RuleGroup:      n.RuleGroup,
		Fingerprint:    n.Fingerprint,
		OrganizationID: ptrToNullInt64(n.OrganizationID),
		DeviceID:       n.DeviceID,
		Template:       n.Template,
		Summary:        n.Summary,
		StartsAt:       ptrToNullTime(n.StartsAt),
		EndsAt:         ptrToNullTime(n.EndsAt),
		Labels:         labels,
		Annotations:    annotations,
	}, nil
}

func (s *SQLNotificationHistoryStore) List(ctx context.Context, organizationID int64, beforeID *int64, limit int32) ([]notificationhistory.StoredNotification, error) {
	rows, err := s.GetQueries(ctx).ListNotificationHistory(ctx, sqlc.ListNotificationHistoryParams{
		OrganizationID: sql.NullInt64{Int64: organizationID, Valid: true},
		BeforeID:       ptrToNullInt64(beforeID),
		PageLimit:      limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list notification history: %w", err)
	}
	out := make([]notificationhistory.StoredNotification, 0, len(rows))
	for _, row := range rows {
		out = append(out, notificationhistory.StoredNotification{
			ID:         row.ID,
			ReceivedAt: row.ReceivedAt,
			DeviceName: row.DeviceName,
			DeviceMAC:  row.DeviceMac,
			Notification: notificationhistory.Notification{
				AlertName:      row.AlertName,
				Status:         row.Status,
				Severity:       row.Severity,
				RuleGroup:      row.RuleGroup,
				Fingerprint:    row.Fingerprint,
				OrganizationID: nullInt64ToPtr(row.OrganizationID),
				DeviceID:       row.DeviceID,
				Template:       row.Template,
				Summary:        row.Summary,
				StartsAt:       nullTimeToPtr(row.StartsAt),
				EndsAt:         nullTimeToPtr(row.EndsAt),
			},
		})
	}
	return out, nil
}

func (s *SQLNotificationHistoryStore) ListActive(ctx context.Context, organizationID int64, limit int32) ([]notificationhistory.StoredNotification, error) {
	rows, err := s.GetQueries(ctx).ListActiveNotifications(ctx, sqlc.ListActiveNotificationsParams{
		OrganizationID: organizationID,
		PageLimit:      limit,
		ActiveSince:    time.Now().Add(-activeAlertStaleAfter),
	})
	if err != nil {
		return nil, fmt.Errorf("list active notifications: %w", err)
	}
	out := make([]notificationhistory.StoredNotification, 0, len(rows))
	for _, row := range rows {
		org := row.OrganizationID
		out = append(out, notificationhistory.StoredNotification{
			ID:         row.HistoryID,
			ReceivedAt: row.ReceivedAt,
			DeviceName: row.DeviceName,
			DeviceMAC:  row.DeviceMac,
			Notification: notificationhistory.Notification{
				AlertName: row.AlertName,
				// ListActiveNotifications filters to status = 'firing', so every returned row is firing.
				Status:         "firing",
				Severity:       row.Severity,
				RuleGroup:      row.RuleGroup,
				Fingerprint:    row.Fingerprint,
				OrganizationID: &org,
				DeviceID:       row.DeviceID,
				Template:       row.Template,
				Summary:        row.Summary,
				StartsAt:       nullTimeToPtr(row.StartsAt),
				EndsAt:         nullTimeToPtr(row.EndsAt),
			},
		})
	}
	return out, nil
}
