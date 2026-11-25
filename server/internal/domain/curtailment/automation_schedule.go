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

// scheduleBoundaryAt is the window boundary a schedule asserts at now: curtail
// from the start of the window in progress, or else restore from the end of the
// window that most recently ended, or nil when there is neither. Its ReceivedAt
// is the boundary time, so repeating it is harmless and a window an operator
// stopped early is not restarted.
func scheduleBoundaryAt(def schedulewindow.Definition, now time.Time) (*models.AutomationExecution, error) {
	current, err := def.Current(now)
	if err != nil {
		return nil, err
	}
	if current != nil {
		return &models.AutomationExecution{Target: models.AutomationSignalOff, Direction: scheduleDirectionStart, ReceivedAt: current.Start}, nil
	}
	ended, err := def.LastEnded(now)
	if err != nil || ended == nil {
		return nil, err
	}
	return &models.AutomationExecution{Target: models.AutomationSignalOn, Direction: scheduleDirectionEnd, ReceivedAt: ended.End}, nil
}

func (s *AutomationService) produceScheduleAssertions(ctx context.Context) {
	rules, err := s.store.ListEnabledScheduleAutomationRules(ctx)
	if err != nil {
		slog.Error("list curtailment schedule rules", "error", err)
		return
	}
	now := s.clock()
	for _, rule := range rules {
		boundary, err := scheduleBoundaryAt(rule.Schedule.Definition, now)
		if err != nil {
			slog.Error("evaluate curtailment schedule", "rule_id", rule.RuleID, "error", err)
			continue
		}
		if boundary == nil {
			continue
		}
		boundary.RuleID = rule.RuleID
		boundary.ScheduleRevision = rule.Schedule.Revision
		if err := s.store.QueueScheduleAutomationExecution(ctx, *boundary); err != nil {
			slog.Error("queue curtailment schedule window", "rule_id", rule.RuleID, "error", err)
		}
	}
}

// handleScheduleExecution applies one queued window boundary. Boundaries from
// an earlier schedule revision or that no longer match the saved schedule, and
// windows already over, are dropped.
func (s *AutomationService) handleScheduleExecution(ctx context.Context, rule *models.AutomationRule, work *models.AutomationExecution) error {
	if rule == nil || !rule.Enabled || rule.TriggerType != models.AutomationTriggerTypeSchedule || rule.Schedule == nil {
		return nil
	}
	if work.ScheduleRevision != rule.Schedule.Revision {
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
		window, err := rule.Schedule.Definition.LastEnded(at)
		if err != nil {
			return fleeterror.NewInvalidArgumentErrorf("invalid schedule: %v", err)
		}
		if window == nil || !window.End.Equal(at) {
			return nil
		}
		if err := s.store.RecordAutomationSignal(ctx, rule.ID, models.AutomationSignalOn, at); err != nil {
			return err
		}
		// Only the event this window started under this schedule revision is
		// restored; an edited schedule's event can share the window's start.
		return s.restoreRuleEvent(ctx, rule, at, func(event *models.Event) bool {
			return !eventStartedForWindow(event, window.Start) || event.ScheduleRevision != work.ScheduleRevision
		})
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
			if eventStartedForWindow(event, window.Start) {
				return nil
			}
			return errWaitingForPreviousScheduleWindow()
		}
	}
	// A window runs once per schedule revision: a retried start does not run it
	// again after its event was stopped, even when the rule's active-event
	// pointer was never recorded.
	state, windowRan, err := s.store.ScheduleWindowEventState(ctx, rule.OrgID, rule.ID, window.Start, rule.Schedule.Revision)
	if err != nil {
		return err
	}
	if windowRan && state.IsTerminal() {
		return nil
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
		if !eventStartedForWindow(replayEvent, window.Start) {
			return errWaitingForPreviousScheduleWindow()
		}
		return s.store.SetAutomationActiveEvent(ctx, rule.ID, 0, replayEvent.EventUUID, at)
	}
	if windowRan {
		// The window's event ended after the lookup above.
		return nil
	}
	return s.startBoundAutomationEvent(ctx, rule, startReq, at)
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
	// One-time windows last at most the duration cap and recurring windows
	// under a day, so the time left always fits it.
	capSeconds := int32(math.Ceil(remaining.Seconds()))
	startReq.MaxDurationSeconds = &capSeconds
	startReq.AllowUnbounded = false
	startReq.ScheduledStartAt = &window.Start
	startReq.AutomationScheduleRevision = rule.Schedule.Revision
	startReq.AutomationWindowEndAt = window.End
	return startReq, true, nil
}

func eventStartedForWindow(event *models.Event, windowStart time.Time) bool {
	return event != nil && event.ScheduledStartAt != nil && event.ScheduledStartAt.Equal(windowStart)
}

func errWaitingForPreviousScheduleWindow() error {
	return fleeterror.NewFailedPreconditionError("waiting for the previous schedule window's curtailment event to finish")
}
