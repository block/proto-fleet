package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
	labels, err := marshalNotificationJSON(n.Labels)
	if err != nil {
		return fmt.Errorf("marshal notification labels: %w", err)
	}
	annotations, err := marshalNotificationJSON(n.Annotations)
	if err != nil {
		return fmt.Errorf("marshal notification annotations: %w", err)
	}

	return s.GetQueries(ctx).InsertNotificationHistory(ctx, sqlc.InsertNotificationHistoryParams{
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
	})
}

// maxBatchRows keeps each multi-row INSERT under PostgreSQL's 65535-parameter limit
// (notificationHistoryColumns params per row); larger batches are chunked.
const maxBatchRows = 4000

const notificationHistoryColumns = 13

// InsertBatch persists many notifications in one transaction using chunked multi-row INSERTs,
// so a large outage (one org-grouped notification with thousands of alerts) lands quickly and
// atomically instead of via thousands of sequential round trips. All-or-nothing: on any error
// the whole batch rolls back, so the caller can treat success as "every alert persisted".
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

	for start := 0; start < len(notifs); start += maxBatchRows {
		end := min(start+maxBatchRows, len(notifs))
		query, args, err := buildNotificationHistoryInsert(notifs[start:end])
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert notification batch: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notification batch: %w", err)
	}
	committed = true
	return nil
}

func buildNotificationHistoryInsert(notifs []*notificationhistory.Notification) (string, []any, error) {
	var b strings.Builder
	b.WriteString(`INSERT INTO notification_history (alert_name, status, severity, rule_group, fingerprint, organization_id, device_id, template, summary, starts_at, ends_at, labels, annotations) VALUES `)
	args := make([]any, 0, len(notifs)*notificationHistoryColumns)
	p := 1
	for i, n := range notifs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		for j := range notificationHistoryColumns {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "$%d", p)
			p++
		}
		b.WriteByte(')')

		labels, err := marshalNotificationJSON(n.Labels)
		if err != nil {
			return "", nil, fmt.Errorf("marshal notification labels: %w", err)
		}
		annotations, err := marshalNotificationJSON(n.Annotations)
		if err != nil {
			return "", nil, fmt.Errorf("marshal notification annotations: %w", err)
		}
		args = append(args,
			n.AlertName, n.Status, n.Severity, n.RuleGroup, n.Fingerprint,
			ptrToNullInt64(n.OrganizationID), n.DeviceID, n.Template, n.Summary,
			ptrToNullTime(n.StartsAt), ptrToNullTime(n.EndsAt), labels, annotations,
		)
	}
	return b.String(), args, nil
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
