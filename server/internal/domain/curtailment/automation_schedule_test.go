package curtailment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/schedulewindow"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// The harness clock is 2026-06-11 21:00 UTC, inside the 20:00-23:00 window.
var (
	scheduleWindowStart = time.Date(2026, 6, 11, 20, 0, 0, 0, time.UTC)
	scheduleWindowEnd   = time.Date(2026, 6, 11, 23, 0, 0, 0, time.UTC)
)

func dailyEveningSchedule() *models.AutomationSchedule {
	return &models.AutomationSchedule{
		Definition: schedulewindow.Definition{
			Type:      schedulewindow.TypeRecurring,
			Timezone:  "UTC",
			StartDate: "2026-06-01",
			StartTime: "20:00",
			EndTime:   "23:00",
			Frequency: schedulewindow.FrequencyDaily,
		},
		ServiceUserID: 777,
	}
}

// scheduleDurableFake mirrors the mailbox row: a rule keeps its latest
// boundary after the work completes, and only a newer boundary replaces it.
type scheduleDurableFake struct {
	*durableAutomationFake
	schedules []models.ScheduledAutomationRule
	latest    map[int64]time.Time
}

func (f *scheduleDurableFake) ListEnabledScheduleAutomationRules(context.Context) ([]models.ScheduledAutomationRule, error) {
	return f.schedules, nil
}

func (f *scheduleDurableFake) QueueScheduleAutomationExecution(_ context.Context, ruleID int64, target models.AutomationSignal, direction string, boundaryAt time.Time) error {
	if latest, ok := f.latest[ruleID]; ok && !latest.Before(boundaryAt) {
		return nil
	}
	if f.latest == nil {
		f.latest = make(map[int64]time.Time)
	}
	f.latest[ruleID] = boundaryAt
	f.pending = append(f.pending, models.AutomationExecution{
		RuleID: ruleID, OrgID: 42, Generation: 1, Direction: direction, Target: target, TargetAt: boundaryAt, ReceivedAt: boundaryAt,
	})
	return nil
}

func newScheduleHarness(t *testing.T) (*automationHarness, *scheduleDurableFake) {
	t.Helper()
	h := newAutomationHarness(t)
	h.seedRunnableProfile()
	h.rule.RuleName = "Evening peak"
	h.rule.TriggerType = models.AutomationTriggerTypeSchedule
	h.rule.MQTTSourceID = 0
	h.rule.MQTTSourceName = ""
	h.rule.Schedule = dailyEveningSchedule()
	durable := &scheduleDurableFake{
		durableAutomationFake: &durableAutomationFake{automationFakeStore: h.rules},
		schedules:             []models.ScheduledAutomationRule{{RuleID: h.rule.ID, OrgID: h.orgID, Schedule: *h.rule.Schedule}},
	}
	h.automation.store = durable
	return h, durable
}

func (h *automationHarness) seedScheduledEvent(state models.EventState, windowStart time.Time) uuid.UUID {
	h.t.Helper()
	id := h.seedAutomationEvent(state)
	start := windowStart
	h.curtailments.eventsByUUID[id].ScheduledStartAt = &start
	return id
}

func TestScheduleAssertionAt(t *testing.T) {
	def := dailyEveningSchedule().Definition

	target, direction, boundary, ok, err := scheduleAssertionAt(def, scheduleWindowStart.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, models.AutomationSignalOff, target)
	assert.Equal(t, scheduleDirectionStart, direction)
	assert.True(t, boundary.Equal(scheduleWindowStart))

	target, direction, boundary, ok, err = scheduleAssertionAt(def, scheduleWindowEnd.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, models.AutomationSignalOn, target)
	assert.Equal(t, scheduleDirectionEnd, direction)
	assert.True(t, boundary.Equal(scheduleWindowEnd))

	_, _, _, ok, err = scheduleAssertionAt(def, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.False(t, ok, "nothing to assert before the first window")
}

func TestAutomationService_ScheduleWindowStartsScheduledEvent(t *testing.T) {
	t.Parallel()
	h, durable := newScheduleHarness(t)

	h.automation.produceScheduleAssertions(t.Context())
	require.Len(t, durable.pending, 1)
	assert.Equal(t, models.AutomationSignalOff, durable.pending[0].Target)
	assert.True(t, durable.pending[0].ReceivedAt.Equal(scheduleWindowStart))

	h.automation.processDueExecutions(t.Context())

	assert.Equal(t, []int64{h.rule.ID}, durable.completed)
	require.Equal(t, 1, h.curtailments.insertEventCalls)
	event := h.curtailments.lastInsertEvent
	assert.Equal(t, models.SourceActorScheduler, event.SourceActorType)
	assert.Equal(t, int64(0), event.AutomationMQTTSourceID)
	assert.Equal(t, h.rule.ID, event.AutomationRuleID)
	assert.Equal(t, int64(777), event.CreatedByUserID)
	require.NotNil(t, event.ScheduledStartAt)
	assert.True(t, event.ScheduledStartAt.Equal(scheduleWindowStart))
	// FULL_FLEET profiles are not forced unbounded: the window end caps them.
	assert.False(t, event.AllowUnbounded)
	require.NotNil(t, event.MaxDurationSeconds)
	assert.Equal(t, int32(2*60*60), *event.MaxDurationSeconds)
	assert.Equal(t, `Schedule "Evening peak"`, event.Reason)
	require.NotNil(t, h.rule.ActiveEventUUID)
	assert.Equal(t, h.rules.lastActiveEvent, *h.rule.ActiveEventUUID)
}

func TestAutomationService_ScheduleRepeatedAssertionDoesNotRestartWindow(t *testing.T) {
	t.Parallel()
	h, durable := newScheduleHarness(t)

	h.automation.produceScheduleAssertions(t.Context())
	h.automation.processDueExecutions(t.Context())
	require.Equal(t, 1, h.curtailments.insertEventCalls)

	// An operator stops the window's event early; the same boundary asserted
	// again on the next pass must not start it again.
	h.curtailments.eventsByUUID[*h.rule.ActiveEventUUID] = &models.Event{
		EventUUID: *h.rule.ActiveEventUUID, OrgID: h.orgID, State: models.EventStateCompleted,
	}
	h.automation.produceScheduleAssertions(t.Context())
	h.automation.processDueExecutions(t.Context())
	assert.Equal(t, 1, h.curtailments.insertEventCalls)
	assert.Empty(t, durable.pending)
}

func TestAutomationService_ScheduleWindowEndRestores(t *testing.T) {
	t.Parallel()
	h, durable := newScheduleHarness(t)
	h.automation.clock = func() time.Time { return scheduleWindowEnd.Add(30 * time.Second) }
	id := h.seedScheduledEvent(models.EventStateActive, scheduleWindowStart)

	h.automation.produceScheduleAssertions(t.Context())
	require.Len(t, durable.pending, 1)
	assert.Equal(t, models.AutomationSignalOn, durable.pending[0].Target)
	h.automation.processDueExecutions(t.Context())

	assert.Equal(t, []int64{h.rule.ID}, durable.completed)
	assert.Equal(t, 1, h.curtailments.beginRestoreCalls)
	assert.Equal(t, id, h.curtailments.beginRestoreLastEventID)
	assert.Equal(t, 1, h.rules.restoreStartedCalls)
	assert.True(t, h.rules.lastRestoreStartedAt.Equal(scheduleWindowEnd))
}

func TestAutomationService_ScheduleWindowWaitsForPreviousWindowEvent(t *testing.T) {
	t.Parallel()
	h, durable := newScheduleHarness(t)
	h.seedScheduledEvent(models.EventStateRestoring, scheduleWindowStart.AddDate(0, 0, -1))

	h.automation.produceScheduleAssertions(t.Context())
	h.automation.processDueExecutions(t.Context())

	assert.Equal(t, []int64{h.rule.ID}, durable.retried)
	assert.Equal(t, 0, h.curtailments.insertEventCalls)
	require.NotEmpty(t, h.rules.executionErrors)
	assert.Contains(t, h.rules.executionErrors[0], "previous schedule window")
}

func TestAutomationService_ScheduleWindowKeepsItsOwnLiveEvent(t *testing.T) {
	t.Parallel()
	h, durable := newScheduleHarness(t)
	h.seedScheduledEvent(models.EventStateActive, scheduleWindowStart)

	h.automation.produceScheduleAssertions(t.Context())
	h.automation.processDueExecutions(t.Context())

	assert.Equal(t, []int64{h.rule.ID}, durable.completed)
	assert.Equal(t, 0, h.curtailments.insertEventCalls)
}

func TestAutomationService_ScheduleDropsStaleOrEndedWindows(t *testing.T) {
	t.Parallel()

	t.Run("boundary no longer in the schedule", func(t *testing.T) {
		t.Parallel()
		h, durable := newScheduleHarness(t)
		durable.pending = []models.AutomationExecution{{
			RuleID: h.rule.ID, OrgID: h.orgID, Target: models.AutomationSignalOff, ReceivedAt: scheduleWindowStart.Add(-time.Hour),
		}}
		h.automation.processDueExecutions(t.Context())
		assert.Equal(t, []int64{h.rule.ID}, durable.completed)
		assert.Equal(t, 0, h.curtailments.insertEventCalls)
	})

	t.Run("window already over", func(t *testing.T) {
		t.Parallel()
		h, durable := newScheduleHarness(t)
		h.automation.clock = func() time.Time { return scheduleWindowEnd }
		durable.pending = []models.AutomationExecution{{
			RuleID: h.rule.ID, OrgID: h.orgID, Target: models.AutomationSignalOff, ReceivedAt: scheduleWindowStart,
		}}
		h.automation.processDueExecutions(t.Context())
		assert.Equal(t, []int64{h.rule.ID}, durable.completed)
		assert.Equal(t, 0, h.curtailments.insertEventCalls)
	})
}

func TestAutomationService_CreateScheduleRuleValidation(t *testing.T) {
	t.Parallel()

	pastOneTime := &models.AutomationSchedule{
		Definition: schedulewindow.Definition{
			Type: schedulewindow.TypeOneTime, Timezone: "UTC",
			StartDate: "2026-06-01", StartTime: "10:00", EndDate: "2026-06-01", EndTime: "12:00",
		},
		ServiceUserID: 777,
	}
	tests := []struct {
		name   string
		mutate func(*models.AutomationRule)
		want   string
	}{
		{"missing schedule", func(r *models.AutomationRule) { r.Schedule = nil }, "require a schedule"},
		{"mqtt source on schedule rule", func(r *models.AutomationRule) { r.MQTTSourceID = 7001 }, "cannot set mqtt_source_id"},
		{"missing saving user", func(r *models.AutomationRule) { r.Schedule.ServiceUserID = 0 }, "saving user"},
		{"invalid definition", func(r *models.AutomationRule) { r.Schedule.Definition.Timezone = "Nowhere/Else" }, "invalid schedule"},
		{"no remaining windows", func(r *models.AutomationRule) { r.Schedule = pastOneTime }, "no remaining windows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _ := newScheduleHarness(t)
			rule := *h.rule
			rule.ID = 0
			schedule := *h.rule.Schedule
			rule.Schedule = &schedule
			tt.mutate(&rule)
			_, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{
				Rule:                            rule,
				ExpectedResponseProfileRevision: h.profile.Revision,
				CanUseAdminControls:             true,
			})
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	t.Run("valid schedule", func(t *testing.T) {
		t.Parallel()
		h, _ := newScheduleHarness(t)
		rule := *h.rule
		rule.ID = 0
		created, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{
			Rule:                            rule,
			ExpectedResponseProfileRevision: h.profile.Revision,
			CanUseAdminControls:             true,
		})
		require.NoError(t, err)
		require.NotNil(t, created.Schedule)
		assert.Equal(t, models.AutomationTriggerTypeSchedule, created.TriggerType)
	})

	t.Run("mqtt rule with schedule", func(t *testing.T) {
		t.Parallel()
		h := newAutomationHarness(t)
		rule := *h.rule
		rule.ID = 0
		rule.Schedule = dailyEveningSchedule()
		_, err := h.automation.Create(t.Context(), SaveAutomationRuleRequest{Rule: rule, ExpectedResponseProfileRevision: h.profile.Revision})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot set a schedule")
	})
}

func TestAutomationService_UpdateRejectsTriggerTypeChange(t *testing.T) {
	t.Parallel()
	h := newAutomationHarness(t)
	rule := *h.rule
	rule.TriggerType = models.AutomationTriggerTypeSchedule
	rule.MQTTSourceID = 0
	rule.Schedule = dailyEveningSchedule()

	_, err := h.automation.Update(t.Context(), SaveAutomationRuleRequest{
		Rule:                            rule,
		ExpectedResponseProfileRevision: h.profile.Revision,
		CanUseAdminControls:             true,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "trigger_type cannot change")
	assert.Equal(t, 0, h.rules.updateCalls)
}

func TestValidateExecutionFenceTriggers(t *testing.T) {
	t.Parallel()
	revision := uuid.New()
	tests := []struct {
		name  string
		fence ExecutionFence
		use   ExecutionFenceUse
		ok    bool
	}{
		{"mqtt start", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2, AutomationMQTTSourceID: 3, SourceActorType: models.SourceActorAutomation}, ExecutionFenceStart, true},
		{"schedule start", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2, SourceActorType: models.SourceActorScheduler}, ExecutionFenceStart, true},
		{"schedule start with source", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2, AutomationMQTTSourceID: 3, SourceActorType: models.SourceActorScheduler}, ExecutionFenceStart, false},
		{"automation start without source", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2, SourceActorType: models.SourceActorAutomation}, ExecutionFenceStart, false},
		{"user start with rule", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2, SourceActorType: models.SourceActorUser}, ExecutionFenceStart, false},
		{"source without rule", ExecutionFence{AutomationMQTTSourceID: 3, SourceActorType: models.SourceActorUser}, ExecutionFenceStart, false},
		{"schedule store", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2, AutomationServiceUserID: 4}, ExecutionFenceStore, true},
		{"schedule recurtail", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2, AutomationServiceUserID: 4}, ExecutionFenceRecurtail, true},
		{"rule without service user", ExecutionFence{ResponseProfileID: 1, ResponseProfileRevision: revision, AutomationRuleID: 2}, ExecutionFenceStore, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateExecutionFence(tt.fence, tt.use)
			if tt.ok {
				assert.NoError(t, err)
				return
			}
			assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
		})
	}
}

func TestScheduledEventOwnership(t *testing.T) {
	t.Parallel()
	h, _ := newScheduleHarness(t)
	event := h.seedAutomationReplayEvent(models.EventStateActive)
	event.SourceActorType = models.SourceActorScheduler

	assert.NoError(t, ValidateAutomationEventOwnership(event, h.orgID, h.rule.ID))
	assert.NoError(t, validateAutomationReplayOwnership(event, h.rule, nil))

	mqttRule := *h.rule
	mqttRule.TriggerType = models.AutomationTriggerTypeMQTT
	assert.Error(t, validateAutomationReplayOwnership(event, &mqttRule, nil), "an MQTT rule does not own a scheduler event")
}
