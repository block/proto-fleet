package timescaledb_test

import (
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
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

	var eventID int64
	err := db.QueryRowContext(t.Context(), `
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
		)
		RETURNING id`, eventUUID, orgID, user.DatabaseID, time.Now().Add(-2*time.Minute)).Scan(&eventID)
	require.NoError(t, err)

	ruleSQL := loadRuleSQL(t, "Curtailment Fan Restore Failed", "FROM curtailment_event")
	require.Equal(t, map[string]float64{eventUUID.String(): 1}, runFanRestoreRule(t, db, ruleSQL, orgID))

	err = sqlstores.NewSQLCurtailmentStore(db).UpdateFanState(t.Context(), eventID, interfaces.UpdateCurtailmentFanStateParams{
		ExpectedEventState: models.EventStateCompletedWithFailures,
		LastError:          nil,
	})
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
