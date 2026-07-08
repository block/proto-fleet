package sqlstores_test

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func seedMQTTSourceConfig(t *testing.T, db *sql.DB, orgID, serviceUserID int64, name string, enabled bool) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_mqtt_source_config
			(organization_id, service_user_id, source_name, topic,
			 broker_primary_host, broker_secondary_host, mqtt_username, mqtt_password_enc, enabled)
		VALUES ($1, $2, $3, 'signals/topic', 'broker-a', 'broker-b', 'user', 'enc', $4)
		RETURNING id`,
		orgID, serviceUserID, name, enabled).Scan(&id))
	return id
}

func seedResponseProfile(t *testing.T, db *sql.DB, orgID int64, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_response_profile (org_id, profile_name, mode)
		VALUES ($1, $2, 'FULL_FLEET')
		RETURNING id`,
		orgID, name).Scan(&id))
	return id
}

func seedAutomationRule(t *testing.T, db *sql.DB, orgID, sourceID, profileID int64, name string, enabled bool) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_automation_rule
			(org_id, rule_name, mqtt_source_id, response_profile_id, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		orgID, name, sourceID, profileID, enabled).Scan(&id))
	return id
}

func seedAutomationEvent(
	t *testing.T,
	ctx context.Context,
	store *sqlstores.SQLCurtailmentStore,
	orgID, userID, ruleID int64,
	state models.EventState,
) {
	t.Helper()
	actor := "alert-metrics-" + strconv.FormatInt(ruleID, 10)
	params := curtailmentStoreTestEvent(orgID, userID, uuid.New(), state, actor)
	externalSource := "curtailment_automation"
	externalReference := strconv.FormatInt(ruleID, 10)
	params.ExternalSource = &externalSource
	params.ExternalReference = &externalReference
	var targets []models.InsertTargetParams
	if !state.IsTerminal() {
		targets = []models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-"+actor, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		}
	}
	_, err := store.InsertEventWithTargets(ctx, params, targets)
	require.NoError(t, err)
}

// Pins the semantics the curtailment alert-metrics loop depends on: a
// non-terminal automation event marks its source as actively curtailing even
// when the rule or the source has been disabled, matched via the event's
// external reference (no rule-state pointer is ever written here); terminal
// events and non-automation events do not.
func TestSQLCurtailmentStore_ListMQTTSourcesWithActiveCurtailment(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	db := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(db)
	orgID := user.OrganizationID
	profileID := seedResponseProfile(t, db, orgID, "alert-metrics-profile")

	// Enabled source, enabled rule, active event: must be reported.
	curtailingSource := seedMQTTSourceConfig(t, db, orgID, user.DatabaseID, "curtailing", true)
	curtailingRule := seedAutomationRule(t, db, orgID, curtailingSource, profileID, "rule-curtailing", true)
	seedAutomationEvent(t, ctx, store, orgID, user.DatabaseID, curtailingRule, models.EventStateActive)

	// Disabled rule with a still-live event: must be reported.
	disabledRuleSource := seedMQTTSourceConfig(t, db, orgID, user.DatabaseID, "rule-off", true)
	disabledRule := seedAutomationRule(t, db, orgID, disabledRuleSource, profileID, "rule-disabled", false)
	seedAutomationEvent(t, ctx, store, orgID, user.DatabaseID, disabledRule, models.EventStateRestoring)

	// Disabled source with a still-live event: must be reported.
	disabledSource := seedMQTTSourceConfig(t, db, orgID, user.DatabaseID, "source-off", false)
	disabledSourceRule := seedAutomationRule(t, db, orgID, disabledSource, profileID, "rule-source-off", true)
	seedAutomationEvent(t, ctx, store, orgID, user.DatabaseID, disabledSourceRule, models.EventStatePending)

	// Terminal event only: must not be reported.
	restoredSource := seedMQTTSourceConfig(t, db, orgID, user.DatabaseID, "restored", true)
	restoredRule := seedAutomationRule(t, db, orgID, restoredSource, profileID, "rule-restored", true)
	seedAutomationEvent(t, ctx, store, orgID, user.DatabaseID, restoredRule, models.EventStateCompleted)

	// Non-automation event whose external reference happens to match a rule id:
	// must not be reported.
	manualSource := seedMQTTSourceConfig(t, db, orgID, user.DatabaseID, "manual", true)
	manualRule := seedAutomationRule(t, db, orgID, manualSource, profileID, "rule-manual", true)
	manualParams := curtailmentStoreTestEvent(orgID, user.DatabaseID, uuid.New(), models.EventStateActive, "alert-metrics-manual")
	manualSourceName := "operator_api"
	manualReference := strconv.FormatInt(manualRule, 10)
	manualParams.ExternalSource = &manualSourceName
	manualParams.ExternalReference = &manualReference
	_, err := store.InsertEventWithTargets(ctx, manualParams, []models.InsertTargetParams{
		curtailmentStoreTestTarget("miner-alert-metrics-manual", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)

	rows, err := store.ListMQTTSourcesWithActiveCurtailment(ctx)
	require.NoError(t, err)

	bySourceID := make(map[int64]*models.MQTTSourceActiveCurtailment, len(rows))
	for _, row := range rows {
		bySourceID[row.SourceID] = row
	}

	require.Contains(t, bySourceID, curtailingSource, "active event on an enabled rule must be reported")
	require.Equal(t, "curtailing", bySourceID[curtailingSource].SourceName)
	require.Equal(t, orgID, bySourceID[curtailingSource].OrganizationID)
	require.Contains(t, bySourceID, disabledRuleSource, "a live event must survive its rule being disabled")
	require.Contains(t, bySourceID, disabledSource, "a live event must survive its source being disabled")
	require.NotContains(t, bySourceID, restoredSource, "a terminal event must not be reported")
	require.NotContains(t, bySourceID, manualSource, "a non-automation event must not be reported")
}
