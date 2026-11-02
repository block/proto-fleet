package curtailment

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/schedulewindow"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	// scheduleProduceInterval is how often enabled schedules assert their
	// current window state; a window starts within this much of its start.
	scheduleProduceInterval = 5 * time.Second

	scheduleDirectionStart = "schedule_start"
	scheduleDirectionEnd   = "schedule_end"
)

// ScheduleAutomationStore reads enabled schedule rules and queues their window
// boundaries into the per-rule execution mailbox.
type ScheduleAutomationStore interface {
	ListEnabledScheduleAutomationRules(ctx context.Context) ([]models.ScheduledAutomationRule, error)
	QueueScheduleAutomationExecution(ctx context.Context, ruleID int64, target models.AutomationSignal, direction string, boundaryAt time.Time) error
}

func (s *AutomationService) validateAutomationSchedule(rule models.AutomationRule) error {
	if rule.MQTTSourceID != 0 {
		return fleeterror.NewInvalidArgumentError("schedule automation rules cannot set mqtt_source_id")
	}
	if rule.Schedule == nil {
		return fleeterror.NewInvalidArgumentError("schedule automation rules require a schedule")
	}
	if rule.Schedule.ServiceUserID <= 0 {
		return fleeterror.NewInvalidArgumentError("schedule automation rules require the saving user")
	}
	window, err := rule.Schedule.Definition.CurrentOrNext(s.clock())
	if err != nil {
		return fleeterror.NewInvalidArgumentErrorf("invalid schedule: %v", err)
	}
	if window == nil {
		return fleeterror.NewInvalidArgumentError("schedule has no remaining windows")
	}
	return nil
}

// scheduleAssertionAt is the state a schedule asserts at now: curtail from the
// start of the window in progress, or else restore from the end of the window
// that most recently ended. Assertions carry their boundary time, so repeating
// one is harmless and a window an operator stopped early is not restarted.
func scheduleAssertionAt(def schedulewindow.Definition, now time.Time) (models.AutomationSignal, string, time.Time, bool, error) {
	current, err := def.Current(now)
	if err != nil {
		return "", "", time.Time{}, false, err
	}
	if current != nil {
		return models.AutomationSignalOff, scheduleDirectionStart, current.Start, true, nil
	}
	ended, err := def.LastEnded(now)
	if err != nil || ended == nil {
		return "", "", time.Time{}, false, err
	}
	return models.AutomationSignalOn, scheduleDirectionEnd, ended.End, true, nil
}

func (s *AutomationService) produceScheduleAssertions(ctx context.Context) {
	store, ok := s.store.(ScheduleAutomationStore)
	if !ok {
		return
	}
	rules, err := store.ListEnabledScheduleAutomationRules(ctx)
	if err != nil {
		slog.Error("list curtailment schedule rules", "error", err)
		return
	}
	now := s.clock()
	for _, rule := range rules {
		target, direction, boundary, ok, err := scheduleAssertionAt(rule.Schedule.Definition, now)
		if err != nil {
			slog.Error("evaluate curtailment schedule", "rule_id", rule.RuleID, "error", err)
			continue
		}
		if !ok {
			continue
		}
		if err := store.QueueScheduleAutomationExecution(ctx, rule.RuleID, target, direction, boundary); err != nil {
			slog.Error("queue curtailment schedule window", "rule_id", rule.RuleID, "error", err)
		}
	}
}

// handleScheduleExecution applies one queued window boundary. Boundaries that
// no longer match the saved schedule, or windows already over, are dropped.
func (s *AutomationService) handleScheduleExecution(ctx context.Context, rule *models.AutomationRule, work *models.AutomationExecution) error {
	if rule == nil || !rule.Enabled || rule.TriggerType != models.AutomationTriggerTypeSchedule || rule.Schedule == nil {
		return nil
	}
	at := work.ReceivedAt
	switch work.Target {
	case models.AutomationSignalOff:
		window, err := rule.Schedule.Definition.Current(at)
		if err != nil {
			return fleeterror.NewInvalidArgumentErrorf("invalid schedule: %v", err)
		}
		if window == nil || !window.Start.Equal(at) || !s.clock().Before(window.End) {
			return nil
		}
		if err := s.store.RecordAutomationSignal(ctx, rule.ID, models.AutomationSignalOff, at); err != nil {
			return err
		}
		return s.startScheduleWindow(ctx, rule, *window, at)
	case models.AutomationSignalOn:
		if err := s.store.RecordAutomationSignal(ctx, rule.ID, models.AutomationSignalOn, at); err != nil {
			return err
		}
		return s.handleRuleOn(ctx, rule, at)
	default:
		return fleeterror.NewInvalidArgumentErrorf("unsupported automation signal %q", work.Target)
	}
}

// startScheduleWindow starts one event per window. An earlier window's event
// that is still live is left to finish first, so windows never share an event.
func (s *AutomationService) startScheduleWindow(ctx context.Context, rule *models.AutomationRule, window schedulewindow.Window, at time.Time) error {
	if rule.ActiveEventUUID != nil {
		event, err := s.curtailment.GetEvent(ctx, rule.OrgID, *rule.ActiveEventUUID)
		if err != nil && !fleeterror.IsNotFoundError(err) {
			return err
		}
		if event != nil && !event.State.IsTerminal() {
			if eventStartedForWindow(event, window) {
				return nil
			}
			return errWaitingForPreviousScheduleWindow()
		}
	}

	profile, err := s.profiles.Get(ctx, rule.OrgID, rule.ResponseProfileID)
	if err != nil {
		return err
	}
	if profile == nil {
		return fleeterror.NewNotFoundError("curtailment response profile not found")
	}
	startReq, ok, err := scheduleStartRequest(rule, profile, window, s.clock())
	if err != nil || !ok {
		return err
	}
	if startReq.Scope.Type == models.ScopeTypeDeviceList {
		startReq.AuthorizedDeviceSites, err = s.profiles.ListDeviceSites(ctx, rule.OrgID, startReq.Scope.DeviceIdentifiers)
		if err != nil {
			return err
		}
	}
	replayEvent, err := s.curtailment.LookupStartReplay(ctx, startReq)
	if err != nil {
		return err
	}
	if replayEvent != nil {
		if err := validateAutomationReplayEvent(replayEvent, rule); err != nil {
			return err
		}
		if !eventStartedForWindow(replayEvent, window) {
			return errWaitingForPreviousScheduleWindow()
		}
		return s.store.SetAutomationActiveEvent(ctx, rule.ID, 0, replayEvent.EventUUID, at)
	}
	return s.startBoundAutomationEvent(ctx, rule, startReq, 0, at)
}

// scheduleStartRequest starts the window from the bound profile and caps it at
// the window's end, so the reconciler restores it even if the end boundary is
// never processed. ok is false when the window has already ended.
func scheduleStartRequest(rule *models.AutomationRule, profile *models.ResponseProfile, window schedulewindow.Window, now time.Time) (StartRequest, bool, error) {
	remaining := window.End.Sub(now)
	if remaining <= 0 {
		return StartRequest{}, false, nil
	}
	startReq, err := automationProfileStartRequest(rule, profile, automationPrincipal{
		serviceUserID: rule.Schedule.ServiceUserID,
		actor:         models.SourceActorScheduler,
		reason:        fmt.Sprintf("Schedule %q", rule.RuleName),
	})
	if err != nil {
		return StartRequest{}, false, err
	}
	capSeconds := int32(min(math.Ceil(remaining.Seconds()), float64(maxFiniteDurationSeconds)))
	startReq.MaxDurationSeconds = &capSeconds
	startReq.AllowUnbounded = false
	windowStart := window.Start
	startReq.ScheduledStartAt = &windowStart
	return startReq, true, nil
}

func eventStartedForWindow(event *models.Event, window schedulewindow.Window) bool {
	return event != nil && event.ScheduledStartAt != nil && event.ScheduledStartAt.Equal(window.Start)
}

func errWaitingForPreviousScheduleWindow() error {
	return fleeterror.NewFailedPreconditionError("waiting for the previous schedule window's curtailment event to finish")
}
