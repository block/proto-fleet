package timescaledb_test

import (
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCurtailmentFanRestoreRulePersistsTerminalFailureUntilClear(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	orgID := user.OrganizationID
	eventUUID := uuid.New()

	_, err := db.ExecContext(t.Context(), `
		INSERT INTO curtailment_event (
			event_uuid, org_id, state, mode, strategy, level, priority,
			loop_type, scope_type, scope_jsonb, restore_batch_size,
			restore_batch_interval_sec, source_actor_type, reason,
			created_by_user_id, fan_restore_delay_sec, fan_on_sent_at,
			fan_last_error
		) VALUES (
			$1, $2, 'completed_with_failures', 'FIXED_KW',
			'LEAST_EFFICIENT_FIRST', 'FULL', 'NORMAL', 'open', 'whole_org',
			'{}'::jsonb, 1, 0, 'user', 'fan alert integration test',
			$3, 60, $4, 'device 501: command failed'
		)`, eventUUID, orgID, user.DatabaseID, time.Now().Add(-2*time.Minute))
	require.NoError(t, err)

	ruleSQL := loadRuleSQL(t, "Curtailment Fan Restore Failed", "FROM curtailment_event")
	require.Equal(t, map[string]float64{eventUUID.String(): 1}, runFanRestoreRule(t, db, ruleSQL, orgID))

	_, err = db.ExecContext(t.Context(), `
		UPDATE curtailment_event
		SET fan_last_error = NULL
		WHERE event_uuid = $1`, eventUUID)
	require.NoError(t, err)
	require.Empty(t, runFanRestoreRule(t, db, ruleSQL, orgID))
}

func runFanRestoreRule(t *testing.T, db *sql.DB, ruleSQL string, orgID int64) map[string]float64 {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), ruleSQL)
	require.NoError(t, err)
	defer rows.Close()

	out := map[string]float64{}
	expectedOrgID := strconv.FormatInt(orgID, 10)
	for rows.Next() {
		var gotOrgID, eventUUID string
		var value float64
		require.NoError(t, rows.Scan(&gotOrgID, &eventUUID, &value))
		if gotOrgID == expectedOrgID {
			out[eventUUID] = value
		}
	}
	require.NoError(t, rows.Err())
	return out
}
