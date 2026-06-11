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
