package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
)

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

func (s *SQLNotificationHistoryStore) Insert(ctx context.Context, n *notificationhistory.Notification) error {
	marshalJSONMap := func(m map[string]string) (json.RawMessage, error) {
		if m == nil {
			return json.RawMessage("{}"), nil
		}
		return json.Marshal(m)
	}

	labels, err := marshalJSONMap(n.Labels)
	if err != nil {
		return fmt.Errorf("marshal notification labels: %w", err)
	}
	annotations, err := marshalJSONMap(n.Annotations)
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
