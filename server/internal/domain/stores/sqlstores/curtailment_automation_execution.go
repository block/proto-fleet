package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

func (s *SQLCurtailmentStore) CountEnabledAutomationRulesByResponseProfile(ctx context.Context, orgID, profileID int64) (int64, error) {
	return s.GetQueries(ctx).CountEnabledCurtailmentAutomationRulesByResponseProfile(ctx, sqlc.CountEnabledCurtailmentAutomationRulesByResponseProfileParams{OrgID: orgID, ResponseProfileID: profileID})
}

func automationNullTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: !value.IsZero()}
}
func (s *SQLCurtailmentStore) QueueAutomationExecution(ctx context.Context, work models.AutomationExecution) error {
	return s.GetQueries(ctx).QueueCurtailmentAutomationExecution(ctx, sqlc.QueueCurtailmentAutomationExecutionParams{RuleID: work.RuleID, MqttSourceID: work.MQTTSourceID, Direction: work.Direction, Target: string(work.Target), TargetAt: automationNullTime(work.TargetAt), ReceivedAt: work.ReceivedAt, ReceivedBroker: work.ReceivedBroker, PriorEdgeAt: automationNullTime(work.PriorEdgeAt)})
}
func (s *SQLCurtailmentStore) ClaimAutomationExecution(ctx context.Context) (*models.AutomationExecution, error) {
	row, err := s.GetQueries(ctx).ClaimCurtailmentAutomationExecution(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.AutomationExecution{RuleID: row.RuleID, OrgID: row.OrgID, MQTTSourceID: row.MqttSourceID, Generation: row.Generation, Direction: row.Direction, Target: models.AutomationSignal(row.Target), TargetAt: row.TargetAt.Time, ReceivedAt: row.ReceivedAt, ReceivedBroker: row.ReceivedBroker, PriorEdgeAt: row.PriorEdgeAt.Time, LeaseExpiresAt: row.LeaseExpiresAt.Time}, nil
}
func (s *SQLCurtailmentStore) CompleteAutomationExecution(ctx context.Context, ruleID, generation int64, leaseExpiresAt time.Time) error {
	return s.GetQueries(ctx).CompleteCurtailmentAutomationExecution(ctx, sqlc.CompleteCurtailmentAutomationExecutionParams{RuleID: ruleID, Generation: generation, LeaseExpiresAt: leaseExpiresAt})
}
func (s *SQLCurtailmentStore) RetryAutomationExecution(ctx context.Context, ruleID, generation int64, leaseExpiresAt time.Time, message string) error {
	return s.GetQueries(ctx).RetryCurtailmentAutomationExecution(ctx, sqlc.RetryCurtailmentAutomationExecutionParams{RuleID: ruleID, Generation: generation, LeaseExpiresAt: leaseExpiresAt, LastError: sql.NullString{String: message, Valid: true}})
}
