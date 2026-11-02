package sqlstores_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_AutomationMailboxAppliesCurrentStateAndSerializesSupersession(t *testing.T) {
	if testing.Short() {
		t.Skip("database integration")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := tc.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	sourceID := seedMQTTSourceConfig(t, database, user.OrganizationID, user.DatabaseID, "mailbox-source", true)
	profileID := seedResponseProfile(t, database, user.OrganizationID, "mailbox-profile")
	profile, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	received := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, mqttingest.NewSQLCStore(sqlc.New(database)).UpsertSourceState(ctx, mqttingest.StateUpdate{SourceConfigID: sourceID, LastTarget: mqttingest.TargetOff, LastTargetAt: received, LastReceivedAt: received}))
	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{OrgID: user.OrganizationID, RuleName: "mailbox-rule", MQTTSourceID: sourceID, ResponseProfileID: profileID, ResponseProfileRevision: profile.Revision, TriggerType: models.AutomationTriggerTypeMQTT, Enabled: true, AdminControlsGranted: true}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	assert.Equal(t, "bound", rule.BindingStatus)
	assert.Equal(t, "pending", rule.ExecutionStatus)
	first, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, models.AutomationSignalOff, first.Target)
	require.NoError(t, store.QueueAutomationExecution(ctx, models.AutomationExecution{RuleID: rule.ID, MQTTSourceID: sourceID, Target: models.AutomationSignalOn, Direction: "off_to_on", ReceivedAt: time.Now().UTC().Add(time.Second)}))
	concurrent, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	assert.Nil(t, concurrent, "new assertions must wait for the current leased attempt")
	require.NoError(t, store.CompleteAutomationExecution(ctx, rule.ID, first.Generation, first.LeaseExpiresAt))
	next, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, models.AutomationSignalOn, next.Target)
	assert.Greater(t, next.Generation, first.Generation)
	require.NoError(t, store.RetryAutomationExecution(ctx, rule.ID, next.Generation, next.LeaseExpiresAt, "load unavailable"))
	rule, err = store.GetAutomationRule(ctx, user.OrganizationID, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, "retry", rule.ExecutionStatus)

	_, err = database.ExecContext(ctx, "UPDATE curtailment_automation_execution SET retry_at=now() WHERE rule_id=$1", rule.ID)
	require.NoError(t, err)
	current, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, current)
	require.NoError(t, store.CompleteAutomationExecution(ctx, rule.ID, first.Generation, first.LeaseExpiresAt))
	other, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.Nil(t, other, "a stale completion must not release a newer worker's lease")
	require.NoError(t, store.CompleteAutomationExecution(ctx, rule.ID, current.Generation, current.LeaseExpiresAt))
	changed := *profile
	changed.ProfileName = "blocked-profile-edit"
	_, err = store.UpdateResponseProfile(ctx, changed, nil, nil, profile.SiteID, profile.ScopeJSON, models.ResponseProfileFanSettings{})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "enabled automation rules")
	_, err = store.SetAutomationRuleEnabled(ctx, user.OrganizationID, rule.ID, false, uuid.Nil, models.ResponseProfileFanSettings{}, true)
	require.NoError(t, err)
	_, err = store.UpdateResponseProfile(ctx, changed, nil, nil, profile.SiteID, profile.ScopeJSON, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
}

func TestSQLCurtailmentStore_AutomationGrantRevocationAndSourceDisableGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("database integration")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := tc.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	sourceID := seedMQTTSourceConfig(t, database, user.OrganizationID, user.DatabaseID, "grant-source", true)
	profileID := seedResponseProfile(t, database, user.OrganizationID, "grant-profile")
	profile, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{OrgID: user.OrganizationID, RuleName: "grant-rule", MQTTSourceID: sourceID, ResponseProfileID: profileID, ResponseProfileRevision: profile.Revision, TriggerType: models.AutomationTriggerTypeMQTT, Enabled: true, AdminControlsGranted: true}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	ref := fmt.Sprint(rule.ID)
	external := "curtailment_automation"
	key := "curtailment_automation_rule:" + ref
	event := curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, ref)
	event.SourceActorType = models.SourceActorAutomation
	event.ExternalSource = &external
	event.ExternalReference = &ref
	event.IdempotencyKey = &key
	event.ResponseProfileID = profileID
	event.ResponseProfileRevision = profile.Revision
	event.AutomationRuleID = rule.ID
	event.AutomationMQTTSourceID = sourceID
	event.DecisionSnapshotJSON = []byte(fmt.Sprintf(`{"response_profile_id":%d,"response_profile_revision":%q}`, profileID, profile.Revision.String()))
	_, err = store.InsertEventWithTargets(ctx, event, nil)
	require.NoError(t, err)
	persisted, err := store.GetEventByUUID(ctx, user.OrganizationID, event.EventUUID)
	require.NoError(t, err)
	allowed, err := store.AuthorizeAutomationCurtailDispatch(ctx, persisted, interfaces.CurtailmentTopologyScopeCoverage{RequireOrgWide: true})
	require.NoError(t, err)
	assert.True(t, allowed)
	settings := mqttingest.NewSQLCSettingsStore(sqlc.New(database))
	_, err = settings.SetSourceConfigEnabled(ctx, user.OrganizationID, sourceID, false)
	require.ErrorIs(t, err, mqttingest.ErrSourceConfigActiveCurtailment)
	_, err = database.ExecContext(ctx, "UPDATE curtailment_mqtt_source_config SET enabled=FALSE WHERE id=$1", sourceID)
	require.Error(t, err, "raw source mutations must obey the same active-event guard")
	_, err = database.ExecContext(ctx, "UPDATE curtailment_automation_execution_grant SET admin_controls_granted=FALSE WHERE rule_id=$1", rule.ID)
	require.NoError(t, err)
	elevated := *persisted
	elevated.RestoreBatchIntervalSec = 301 // above the 300-second operator limit
	allowed, err = store.AuthorizeAutomationCurtailDispatch(ctx, &elevated, interfaces.CurtailmentTopologyScopeCoverage{RequireOrgWide: true})
	require.NoError(t, err)
	assert.False(t, allowed, "admin-only controls must be evaluated against the grant's admin authority")
	_, err = database.ExecContext(ctx, "DELETE FROM curtailment_automation_execution_grant WHERE rule_id = $1", rule.ID)
	require.NoError(t, err)
	allowed, err = store.AuthorizeAutomationCurtailDispatch(ctx, persisted, interfaces.CurtailmentTopologyScopeCoverage{RequireOrgWide: true})
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = database.ExecContext(ctx, "UPDATE curtailment_event SET state='completed' WHERE event_uuid=$1", event.EventUUID)
	require.NoError(t, err)
	_, err = settings.SetSourceConfigEnabled(ctx, user.OrganizationID, sourceID, false)
	require.NoError(t, err)
}

func TestSQLCurtailmentStore_AutomationReassertPreservesPendingCycleAndRetryBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("database integration")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := tc.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	sourceID := seedMQTTSourceConfig(t, database, user.OrganizationID, user.DatabaseID, "backoff-source", true)
	profileID := seedResponseProfile(t, database, user.OrganizationID, "backoff-profile")
	profile, err := store.GetResponseProfile(ctx, user.OrganizationID, profileID)
	require.NoError(t, err)
	rule, err := store.CreateAutomationRule(ctx, models.AutomationRule{OrgID: user.OrganizationID, RuleName: "backoff-rule", MQTTSourceID: sourceID, ResponseProfileID: profileID, ResponseProfileRevision: profile.Revision, TriggerType: models.AutomationTriggerTypeMQTT, Enabled: true, AdminControlsGranted: true}, models.ResponseProfileFanSettings{})
	require.NoError(t, err)
	edgeAt := time.Now().UTC().Truncate(time.Microsecond)
	work := models.AutomationExecution{RuleID: rule.ID, MQTTSourceID: sourceID, Direction: "on_to_off", Target: models.AutomationSignalOff, ReceivedAt: edgeAt}
	require.NoError(t, store.QueueAutomationExecution(ctx, work))
	claimed, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, store.RetryAutomationExecution(ctx, rule.ID, claimed.Generation, claimed.LeaseExpiresAt, "previous capped event still restoring"))
	var retryAt time.Time
	require.NoError(t, database.QueryRowContext(ctx, "SELECT retry_at FROM curtailment_automation_execution WHERE rule_id=$1", rule.ID).Scan(&retryAt))
	work.Direction = "reassert_off"
	work.ReceivedAt = edgeAt.Add(time.Minute)
	require.NoError(t, store.QueueAutomationExecution(ctx, work))
	var direction, status string
	var attempts int32
	var generation int64
	var received, afterRetry time.Time
	require.NoError(t, database.QueryRowContext(ctx, "SELECT direction,status,attempts,generation,received_at,retry_at FROM curtailment_automation_execution WHERE rule_id=$1", rule.ID).Scan(&direction, &status, &attempts, &generation, &received, &afterRetry))
	assert.Equal(t, "on_to_off", direction)
	assert.Equal(t, "retry", status)
	assert.Equal(t, int32(1), attempts)
	assert.Equal(t, claimed.Generation, generation)
	assert.True(t, edgeAt.Equal(received))
	assert.True(t, retryAt.Equal(afterRetry))
	next, err := store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	assert.Nil(t, next, "same-target heartbeats cannot defeat retry backoff")
	work.Direction = "off_to_on"
	work.Target = models.AutomationSignalOn
	work.ReceivedAt = edgeAt.Add(2 * time.Minute)
	require.NoError(t, store.QueueAutomationExecution(ctx, work))
	next, err = store.ClaimAutomationExecution(ctx)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, "off_to_on", next.Direction)
	assert.Greater(t, next.Generation, generation)
}
