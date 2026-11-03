package sqlstores_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/schedulewindow"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_ScheduleAutomationRuleLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("database integration")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := tc.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	profileID := seedResponseProfile(t, database, user.OrganizationID, "schedule-profile")
	profile, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)

	schedule := &models.AutomationSchedule{
		Definition: schedulewindow.Definition{
			Type:       schedulewindow.TypeRecurring,
			Timezone:   "America/Chicago",
			StartDate:  "2026-06-01",
			StartTime:  "22:00",
			EndTime:    "06:00",
			Frequency:  schedulewindow.FrequencyWeekly,
			DaysOfWeek: []time.Weekday{time.Monday, time.Friday},
			UntilDate:  "2026-12-31",
		},
		ServiceUserID: user.DatabaseID,
	}
	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{
		OrgID: user.OrganizationID, RuleName: "weeknight-peak", TriggerType: models.AutomationTriggerTypeSchedule,
		Schedule: schedule, ResponseProfileID: profileID, ResponseProfileRevision: profile.Revision,
		Enabled: true, AdminControlsGranted: true,
	}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	assert.Equal(t, models.AutomationTriggerTypeSchedule, rule.TriggerType)
	assert.Equal(t, int64(0), rule.MQTTSourceID)
	assert.Empty(t, rule.MQTTSourceName)
	assert.Equal(t, "bound", rule.BindingStatus)
	assert.Equal(t, "idle", rule.ExecutionStatus, "saving a schedule clears its mailbox for a fresh assertion")
	require.NotNil(t, rule.Schedule)
	assert.Equal(t, schedule.Definition, rule.Schedule.Definition)
	assert.Equal(t, schedule.ServiceUserID, rule.Schedule.ServiceUserID)
	assert.NotEqual(t, uuid.Nil, rule.Schedule.Revision)
	firstRevision := rule.Schedule.Revision

	listed, err := store.ListAutomationRules(ctx, user.OrganizationID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.NotNil(t, listed[0].Schedule)
	assert.Equal(t, schedule.Definition, listed[0].Schedule.Definition)

	enabled, err := store.ListEnabledScheduleAutomationRules(ctx)
	require.NoError(t, err)
	var found *models.ScheduledAutomationRule
	for i := range enabled {
		if enabled[i].RuleID == rule.ID {
			found = &enabled[i]
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, user.OrganizationID, found.OrgID)
	assert.Equal(t, schedule.Definition, found.Schedule.Definition)
	assert.Equal(t, firstRevision, found.Schedule.Revision)

	// Updating the schedule replaces its definition and revision.
	updated := *rule
	daily := *schedule
	daily.Definition.Frequency = schedulewindow.FrequencyDaily
	daily.Definition.DaysOfWeek = nil
	updated.Schedule = &daily
	rule, err = store.UpdateAutomationRule(ctx, updated, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	assert.Equal(t, daily.Definition, rule.Schedule.Definition)
	assert.NotEqual(t, firstRevision, rule.Schedule.Revision)
	revision := rule.Schedule.Revision

	// A producer that read the schedule before the edit cannot queue its boundary.
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, store.QueueScheduleAutomationExecution(ctx, rule.ID, firstRevision, models.AutomationSignalOff, "schedule_start", start))
	stale, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	assert.Nil(t, stale, "a boundary from an earlier schedule revision must not be queued")

	// A window boundary is applied once; only a newer boundary replaces it.
	require.NoError(t, store.QueueScheduleAutomationExecution(ctx, rule.ID, revision, models.AutomationSignalOff, "schedule_start", start))
	claimed, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, int64(0), claimed.MQTTSourceID)
	assert.Equal(t, models.AutomationSignalOff, claimed.Target)
	assert.True(t, claimed.ReceivedAt.Equal(start))
	assert.Equal(t, revision, claimed.ScheduleRevision)
	require.NoError(t, store.CompleteAutomationExecution(ctx, rule.ID, claimed.Generation, claimed.LeaseExpiresAt))
	require.NoError(t, store.QueueScheduleAutomationExecution(ctx, rule.ID, revision, models.AutomationSignalOff, "schedule_start", start))
	again, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	assert.Nil(t, again, "a completed window boundary must not run again")
	require.NoError(t, store.QueueScheduleAutomationExecution(ctx, rule.ID, revision, models.AutomationSignalOn, "schedule_end", start.Add(time.Hour)))
	end, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, end)
	assert.Equal(t, models.AutomationSignalOn, end.Target)
	require.NoError(t, store.CompleteAutomationExecution(ctx, rule.ID, end.Generation, end.LeaseExpiresAt))

	// A row left by an earlier revision does not hold back the saved schedule's
	// earlier boundary.
	_, err = database.ExecContext(ctx, `UPDATE curtailment_automation_execution SET schedule_revision = $1 WHERE rule_id = $2`, firstRevision, rule.ID)
	require.NoError(t, err)
	require.NoError(t, store.QueueScheduleAutomationExecution(ctx, rule.ID, revision, models.AutomationSignalOff, "schedule_start", start))
	replaced, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, replaced, "the saved revision's boundary replaces an earlier revision's row")
	assert.True(t, replaced.ReceivedAt.Equal(start))
	assert.Equal(t, revision, replaced.ScheduleRevision)
	require.NoError(t, store.CompleteAutomationExecution(ctx, rule.ID, replaced.Generation, replaced.LeaseExpiresAt))

	// A scheduled event starts under the schedule's grant and service user.
	ref := fmt.Sprint(rule.ID)
	external := "curtailment_automation"
	key := "curtailment_automation_rule:" + ref
	capSeconds := int32(3600)
	event := curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, ref)
	event.SourceActorType = models.SourceActorScheduler
	event.ExternalSource = &external
	event.ExternalReference = &ref
	event.IdempotencyKey = &key
	event.ScheduledStartAt = &start
	event.MaxDurationSeconds = &capSeconds
	event.ResponseProfileID = profileID
	event.ResponseProfileRevision = profile.Revision
	event.AutomationRuleID = rule.ID
	event.AutomationScheduleRevision = revision
	event.AutomationWindowEndAt = time.Now().Add(30 * time.Minute)
	event.DecisionSnapshotJSON = []byte(fmt.Sprintf(`{"response_profile_id":%d,"response_profile_revision":%q}`, profileID, profile.Revision.String()))

	wrongUser := event
	wrongUser.CreatedByUserID = tc.DatabaseService.CreateSuperAdminUser2().DatabaseID
	_, err = store.InsertEventWithTargets(ctx, wrongUser, nil)
	require.Error(t, err, "a scheduled event must run as the schedule's service user")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "automation rule changed before execution")

	// A worker read the rule, then the schedule was postponed before the worker
	// inserted the event; the obsolete window must not start.
	postponed := *rule
	later := daily
	later.Definition.StartTime = "23:00"
	postponed.Schedule = &later
	rule, err = store.UpdateAutomationRule(ctx, postponed, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	require.NotEqual(t, revision, rule.Schedule.Revision)
	_, err = store.InsertEventWithTargets(ctx, event, nil)
	require.Error(t, err, "an event read from an earlier schedule revision must not start")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "automation rule changed before execution")

	event.AutomationScheduleRevision = rule.Schedule.Revision

	// A start delayed past its window's end must not curtail.
	ended := event
	ended.AutomationWindowEndAt = time.Now().Add(-time.Second)
	_, err = store.InsertEventWithTargets(ctx, ended, nil)
	require.Error(t, err, "a scheduled start whose window ended must not insert")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), "window ended before its event could start")

	// The insert waits on the rule's lock inside its transaction; the wait must
	// not shorten the cap, which counts from the transaction's created_at.
	holder, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = holder.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('curtailment_automation_rule:' || $1::bigint::text || ':' || $2::bigint::text, 0))`, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	inserted := make(chan error, 1)
	go func() {
		_, insertErr := store.InsertEventWithTargets(ctx, event, nil)
		inserted <- insertErr
	}()
	time.Sleep(2 * time.Second)
	require.NoError(t, holder.Commit())
	require.NoError(t, <-inserted)
	require.NoError(t, store.SetAutomationActiveEvent(ctx, rule.ID, 0, event.EventUUID, start))
	persisted, err := store.GetEventByUUID(ctx, user.OrganizationID, event.EventUUID)
	require.NoError(t, err)
	assert.Equal(t, models.SourceActorScheduler, persisted.SourceActorType)
	require.NotNil(t, persisted.ScheduledStartAt)
	assert.True(t, persisted.ScheduledStartAt.Equal(start))
	require.NotNil(t, persisted.MaxDurationSeconds)
	deadline := persisted.CreatedAt.Add(time.Duration(*persisted.MaxDurationSeconds) * time.Second)
	assert.WithinDuration(t, event.AutomationWindowEndAt, deadline, time.Second, "the cap ends at the window's end")
	allowed, err := store.AuthorizeAutomationCurtailDispatch(ctx, persisted, interfaces.CurtailmentTopologyScopeCoverage{RequireOrgWide: true})
	require.NoError(t, err)
	assert.True(t, allowed)

	rule, err = store.GetAutomationRule(ctx, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	require.NotNil(t, rule.ActiveEventUUID)
	assert.Equal(t, event.EventUUID, *rule.ActiveEventUUID)

	// Cancel or Abort disables the owning schedule so later windows don't run.
	released, err := store.ForceReleaseEvent(ctx, user.OrganizationID, event.EventUUID, "operator needs manual control")
	require.NoError(t, err)
	assert.True(t, released.AutomationDisabled)
	rule, err = store.GetAutomationRule(ctx, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	assert.False(t, rule.Enabled)
	enabled, err = store.ListEnabledScheduleAutomationRules(ctx)
	require.NoError(t, err)
	for _, scheduled := range enabled {
		assert.NotEqual(t, rule.ID, scheduled.RuleID, "a force-released schedule must not produce more windows")
	}

	// Re-enabling starts a new schedule revision so the current window runs
	// again; enabling an enabled schedule keeps its revision.
	stoppedRevision := rule.Schedule.Revision
	rule, err = store.SetAutomationRuleEnabled(ctx, user.OrganizationID, rule.ID, true, profile.Revision, models.ResponseProfileFanSettings{}, true)
	require.NoError(t, err)
	assert.True(t, rule.Enabled)
	assert.NotEqual(t, stoppedRevision, rule.Schedule.Revision)
	enabledRevision := rule.Schedule.Revision
	rule, err = store.SetAutomationRuleEnabled(ctx, user.OrganizationID, rule.ID, true, profile.Revision, models.ResponseProfileFanSettings{}, true)
	require.NoError(t, err)
	assert.Equal(t, enabledRevision, rule.Schedule.Revision)
}

func TestSQLCurtailmentStore_AutomationRuleTriggerConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("database integration")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := tc.DatabaseService.DB
	sourceID := seedMQTTSourceConfig(t, database, user.OrganizationID, user.DatabaseID, "constraint-source", true)
	profileID := seedResponseProfile(t, database, user.OrganizationID, "constraint-profile")

	insert := `INSERT INTO curtailment_automation_rule (org_id, rule_name, trigger_type, mqtt_source_id, response_profile_id) VALUES ($1, $2, $3, $4, $5)`
	_, err := database.ExecContext(ctx, insert, user.OrganizationID, "schedule-with-source", "SCHEDULE", sourceID, profileID)
	require.Error(t, err, "schedule rules cannot reference an MQTT source")
	_, err = database.ExecContext(ctx, insert, user.OrganizationID, "mqtt-without-source", "MQTT", nil, profileID)
	require.Error(t, err, "MQTT rules require an MQTT source")
	_, err = database.ExecContext(ctx, insert, user.OrganizationID, "unknown-trigger", "WEBHOOK", nil, profileID)
	require.Error(t, err)
}
