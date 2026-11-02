package curtailment

import (
	"context"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
)

// AutomationExecutionStore is the durable per-rule retry boundary. Production
// stores implement it; the source settles as soon as its assertion is queued.
type AutomationExecutionStore interface {
	QueueAutomationExecution(ctx context.Context, work models.AutomationExecution) error
	ClaimAutomationExecution(ctx context.Context) (*models.AutomationExecution, error)
	CompleteAutomationExecution(ctx context.Context, ruleID, generation int64, leaseExpiresAt time.Time) error
	RetryAutomationExecution(ctx context.Context, ruleID, generation int64, leaseExpiresAt time.Time, message string) error
}

// Run drains independent rule mailboxes until shutdown. Failed rules retain
// their latest assertion with backoff; another rule remains immediately due.
// Schedule rules assert their current window into the same mailboxes.
func (s *AutomationService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastScheduleProduce time.Time
	for {
		if now := s.clock(); now.Sub(lastScheduleProduce) >= scheduleProduceInterval {
			s.produceScheduleAssertions(ctx)
			lastScheduleProduce = now
		}
		s.processDueExecutions(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *AutomationService) processDueExecutions(ctx context.Context) {
	durable, ok := s.store.(AutomationExecutionStore)
	if !ok {
		return
	}
	for range 100 {
		if ctx.Err() != nil {
			return
		}
		work, err := durable.ClaimAutomationExecution(ctx)
		if err != nil {
			slog.Error("claim curtailment automation execution", "error", err)
			return
		}
		if work == nil {
			return
		}
		attemptCtx, cancel := context.WithTimeout(ctx, time.Minute)
		rule, err := s.store.GetAutomationRule(attemptCtx, work.OrgID, work.RuleID)
		if err == nil && work.MQTTSourceID == 0 {
			err = s.handleScheduleExecution(attemptCtx, rule, work)
		} else if err == nil {
			var source mqttingest.SourceConfig
			source, err = s.sourceStore.GetSourceConfigByOrg(attemptCtx, work.OrgID, work.MQTTSourceID)
			if err == nil && rule.Enabled && source.Enabled && rule.MQTTSourceID == source.ID {
				direction := mqttingest.EdgeOffToOn
				target := mqttingest.TargetOn
				if work.Target == models.AutomationSignalOff {
					direction = mqttingest.EdgeReassertOff
					if work.Direction == mqttingest.EdgeOnToOff.String() {
						direction = mqttingest.EdgeOnToOff
					}
					if work.Direction == mqttingest.EdgeWatchdogOff.String() {
						direction = mqttingest.EdgeWatchdogOff
					}
					target = mqttingest.TargetOff
				}
				err = s.handleRuleSignal(attemptCtx, rule, mqttingest.SignalEdge{Source: source, Direction: direction, Target: target, TargetAt: work.TargetAt, ReceivedAt: work.ReceivedAt, ReceivedBroker: work.ReceivedBroker, PriorEdgeAt: work.PriorEdgeAt})
			}
		}
		cancel()
		if err != nil {
			_ = s.store.RecordAutomationExecutionError(ctx, work.RuleID, err.Error(), s.clock())
			if retryErr := durable.RetryAutomationExecution(ctx, work.RuleID, work.Generation, work.LeaseExpiresAt, err.Error()); retryErr != nil {
				slog.Error("retry curtailment automation execution", "error", retryErr)
			}
		} else if err := durable.CompleteAutomationExecution(ctx, work.RuleID, work.Generation, work.LeaseExpiresAt); err != nil {
			slog.Error("complete curtailment automation execution", "error", err)
		}
	}
}
