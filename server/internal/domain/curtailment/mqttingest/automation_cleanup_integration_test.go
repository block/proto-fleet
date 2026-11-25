package mqttingest

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationCleanupPreservesLastRestoreInDatabase(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	var sourceID, profileID, ruleID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_mqtt_source_config
			(organization_id, service_user_id, source_name, topic,
			 broker_primary_host, broker_secondary_host, mqtt_username, mqtt_password_enc)
		VALUES ($1, $2, 'cleanup-source', 'signals', 'broker-a', 'broker-b', 'user', 'enc')
		RETURNING id`, f.orgID, f.userID).Scan(&sourceID))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_response_profile (org_id, profile_name, mode, authorization_envelope_jsonb)
		VALUES ($1, 'cleanup-profile', 'FULL_FLEET',
			'{"schema_version":1,"selected_resource_site_ids":[],"current_member_site_ids":[],"miner_scope_unbounded":true,"facility_fan_site_ids":[],"facility_fan_scope_unbounded":false}'::jsonb)
		RETURNING id`, f.orgID).Scan(&profileID))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
		INSERT INTO curtailment_automation_rule (org_id, rule_name, mqtt_source_id, response_profile_id, enabled)
		VALUES ($1, 'cleanup-rule', $2, $3, true)
		RETURNING id`, f.orgID, sourceID, profileID).Scan(&ruleID))
	// Clearing a pointer before any actual restore must leave history absent.
	clearParams := sqlc.ClearCurtailmentAutomationActiveEventParams{RuleID: ruleID}
	getParams := sqlc.GetCurtailmentAutomationRuleByOrgParams{OrgID: f.orgID, ID: ruleID}
	require.NoError(t, f.queries.ClearCurtailmentAutomationActiveEvent(t.Context(), clearParams))
	rule, err := f.queries.GetCurtailmentAutomationRuleByOrg(t.Context(), getParams)
	require.NoError(t, err)
	assert.False(t, rule.LastRestoredAt.Valid)

	restoredAt := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	require.NoError(t, f.queries.SetCurtailmentAutomationRestoreStarted(t.Context(), sqlc.SetCurtailmentAutomationRestoreStartedParams{
		RuleID: ruleID, LastRestoredAt: sql.NullTime{Time: restoredAt, Valid: true},
	}))
	require.NoError(t, f.queries.ClearCurtailmentAutomationActiveEvent(t.Context(), clearParams))
	rule, err = f.queries.GetCurtailmentAutomationRuleByOrg(t.Context(), getParams)
	require.NoError(t, err)
	require.True(t, rule.LastRestoredAt.Valid)
	assert.True(t, restoredAt.Equal(rule.LastRestoredAt.Time))

	// ON signals still record their explicit restore time when clearing a pointer.
	later := restoredAt.Add(time.Hour)
	clearParams.LastRestoredAt = sql.NullTime{Time: later, Valid: true}
	require.NoError(t, f.queries.ClearCurtailmentAutomationActiveEvent(t.Context(), clearParams))
	rule, err = f.queries.GetCurtailmentAutomationRuleByOrg(t.Context(), getParams)
	require.NoError(t, err)
	require.True(t, rule.LastRestoredAt.Valid)
	assert.True(t, later.Equal(rule.LastRestoredAt.Time))
}
