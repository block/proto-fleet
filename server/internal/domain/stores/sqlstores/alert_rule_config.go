package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
)

type SQLAlertRuleConfigStore struct {
	SQLTransactor
}

func NewSQLAlertRuleConfigStore(conn *sql.DB) *SQLAlertRuleConfigStore {
	return &SQLAlertRuleConfigStore{SQLTransactor: *NewSQLTransactor(conn)}
}

var _ alerts.RuleConfigStore = (*SQLAlertRuleConfigStore)(nil)

func (s *SQLAlertRuleConfigStore) UpsertConfig(ctx context.Context, orgID int64, ruleUID string, cfg alerts.RuleConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal rule config: %w", err)
	}
	return s.GetQueries(ctx).UpsertAlertRuleConfig(ctx, sqlc.UpsertAlertRuleConfigParams{
		OrgID:   orgID,
		RuleUid: ruleUID,
		Config:  raw,
	})
}

func (s *SQLAlertRuleConfigStore) GetConfig(ctx context.Context, orgID int64, ruleUID string) (*alerts.RuleConfig, error) {
	raw, err := s.GetQueries(ctx).GetAlertRuleConfig(ctx, sqlc.GetAlertRuleConfigParams{
		OrgID:   orgID,
		RuleUid: ruleUID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg alerts.RuleConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal rule config for %s: %w", ruleUID, err)
	}
	return &cfg, nil
}

func (s *SQLAlertRuleConfigStore) ListConfigs(ctx context.Context, orgID int64, ruleUIDs []string) (map[string]alerts.RuleConfig, error) {
	rows, err := s.GetQueries(ctx).ListAlertRuleConfigs(ctx, sqlc.ListAlertRuleConfigsParams{
		OrgID:    orgID,
		RuleUids: ruleUIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]alerts.RuleConfig, len(rows))
	for _, row := range rows {
		var cfg alerts.RuleConfig
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal rule config for %s: %w", row.RuleUid, err)
		}
		out[row.RuleUid] = cfg
	}
	return out, nil
}

func (s *SQLAlertRuleConfigStore) DeleteConfig(ctx context.Context, orgID int64, ruleUID string) error {
	return s.GetQueries(ctx).DeleteAlertRuleConfig(ctx, sqlc.DeleteAlertRuleConfigParams{
		OrgID:   orgID,
		RuleUid: ruleUID,
	})
}

func (s *SQLAlertRuleConfigStore) SweepConfigs(ctx context.Context, orgID int64, liveRuleUIDs []string) (int64, error) {
	return s.GetQueries(ctx).SweepAlertRuleConfigs(ctx, sqlc.SweepAlertRuleConfigsParams{
		OrgID:        orgID,
		LiveRuleUids: liveRuleUIDs,
	})
}
