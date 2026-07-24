package sqlstores

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
)

type SQLAlertRouteStore struct {
	SQLTransactor
}

func NewSQLAlertRouteStore(conn *sql.DB) *SQLAlertRouteStore {
	return &SQLAlertRouteStore{SQLTransactor: *NewSQLTransactor(conn)}
}

var _ alerts.RouteStore = (*SQLAlertRouteStore)(nil)

// SetPolicy upserts the policy row and replaces its channel set in one transaction, so a reader can never observe a half-replaced channel list.
func (s *SQLAlertRouteStore) SetPolicy(ctx context.Context, orgID int64, policy alerts.RoutePolicy) error {
	return s.RunInTx(ctx, func(ctx context.Context) error {
		q := s.GetQueries(ctx)
		row, err := q.UpsertAlertRoutePolicy(ctx, sqlc.UpsertAlertRoutePolicyParams{
			OrgID:   orgID,
			RuleUid: policy.RuleUID,
			Mode:    string(policy.Mode),
		})
		if err != nil {
			return err
		}
		if err := q.DeleteAlertRouteChannels(ctx, row.ID); err != nil {
			return err
		}
		if len(policy.ChannelIDs) == 0 {
			return nil
		}
		return q.InsertAlertRouteChannels(ctx, sqlc.InsertAlertRouteChannelsParams{
			PolicyID:   row.ID,
			ChannelIds: policy.ChannelIDs,
		})
	})
}

func (s *SQLAlertRouteStore) DeletePolicy(ctx context.Context, orgID int64, ruleUID string) error {
	_, err := s.GetQueries(ctx).DeleteAlertRoutePolicy(ctx, sqlc.DeleteAlertRoutePolicyParams{
		OrgID:   orgID,
		RuleUid: ruleUID,
	})
	return err
}

func (s *SQLAlertRouteStore) ListPolicies(ctx context.Context, orgID int64) ([]alerts.RoutePolicy, error) {
	rows, err := s.GetQueries(ctx).ListAlertRoutePolicies(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]alerts.RoutePolicy, 0, len(rows))
	for _, row := range rows {
		out = append(out, alerts.RoutePolicy{
			RuleUID:    row.RuleUid,
			Mode:       alerts.RouteMode(row.Mode),
			ChannelIDs: row.ChannelIds,
		})
	}
	return out, nil
}
