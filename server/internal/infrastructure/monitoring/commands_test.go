package monitoring_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newCommandBacklogTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := dbtest.GetTestDB(t)
	_, err := db.ExecContext(t.Context(), `INSERT INTO organization(id,org_id,name) VALUES(1,'command-test','Test');
 INSERT INTO discovered_device(id,org_id,device_identifier,ip_address,port,url_scheme,driver_name,is_active)
 VALUES(1,1,'miner-1','127.0.0.1','80','http','proto',true);
 INSERT INTO device(id,org_id,device_identifier,mac_address,discovered_device_id) VALUES(1,1,'miner-1','00:00:00:00:00:01',1);`)
	require.NoError(t, err)
	return db
}

func TestCommandBacklogAgesSurviveRetryAndRecoverToZero(t *testing.T) {
	db := newCommandBacklogTestDB(t)
	_, err := db.ExecContext(t.Context(), `INSERT INTO queue_message(id,command_batch_log_uuid,device_id,command_type,status,retry_count,created_at,updated_at) VALUES
 (1,'test',1,'StopMining','PENDING',2,now()-interval '10 minutes',now()),
 (2,'test',1,'StopMining','PROCESSING',0,now()-interval '5 minutes',now()),
 (3,'test',1,'FirmwareUpdate','PENDING',0,now()-interval '20 minutes',now()),
 (4,'test',1,'StopMining','SUCCESS',0,now()-interval '1 day',now());
 UPDATE queue_message SET processing_started_at = now() - interval '3 minutes' WHERE id = 2;`)
	require.NoError(t, err)
	var queued, running, outstanding int64
	var queuedAge, runningAge, totalAge float64
	read := func() {
		t.Helper()
		require.NoError(t, db.QueryRow(`SELECT queued_count,running_count,outstanding_count,oldest_queued_age_seconds,oldest_running_age_seconds,oldest_outstanding_age_seconds FROM command_backlog_summary WHERE org_id=1 AND command_class='control'`).Scan(&queued, &running, &outstanding, &queuedAge, &runningAge, &totalAge))
	}
	read()
	require.EqualValues(t, 1, queued)
	require.EqualValues(t, 1, running)
	require.EqualValues(t, 2, outstanding)
	require.InDelta(t, 600, queuedAge, 10)
	require.InDelta(t, 180, runningAge, 10)
	require.InDelta(t, 600, totalAge, 10)
	_, err = db.Exec(`UPDATE queue_message SET retry_count=3,updated_at=now() WHERE id=1`)
	require.NoError(t, err)
	read()
	require.InDelta(t, 600, queuedAge, 10)
	_, err = db.Exec(`UPDATE queue_message SET status='SUCCESS' WHERE command_type='StopMining'`)
	require.NoError(t, err)
	read()
	require.Zero(t, queued)
	require.Zero(t, running)
	require.Zero(t, totalAge)
	require.NoError(t, db.QueryRow(`SELECT outstanding_count FROM command_backlog_summary WHERE org_id=1 AND command_class='firmware'`).Scan(&outstanding))
	require.EqualValues(t, 1, outstanding, "firmware has an independent threshold population")
}

func TestCommandBacklogRunningAgeTracksFirmwareAttempts(t *testing.T) {
	db := newCommandBacklogTestDB(t)
	ctx := t.Context()
	// Keep now() fixed while testing exact age values.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	q := sqlc.New(tx)
	const messageID int64 = 1

	_, err = tx.ExecContext(ctx, `
INSERT INTO queue_message(id, command_batch_log_uuid, device_id, command_type, status, retry_count, created_at)
VALUES ($1, 'firmware-attempt', 1, 'FirmwareUpdate', 'PENDING', 0, now() - interval '20 minutes')`, messageID)
	require.NoError(t, err)
	claim := func() {
		t.Helper()
		result, err := q.ClaimMessageForProcessing(ctx, messageID)
		require.NoError(t, err)
		claimed, err := result.RowsAffected()
		require.NoError(t, err)
		require.EqualValues(t, 1, claimed)

		var startedNow bool
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT processing_started_at = now() FROM queue_message WHERE id = $1`, messageID).Scan(&startedNow))
		require.True(t, startedNow)
	}
	readAges := func(wantRunningAge float64) {
		t.Helper()
		var runningAge, outstandingAge float64
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT oldest_running_age_seconds, oldest_outstanding_age_seconds
FROM command_backlog_summary WHERE org_id = 1 AND command_class = 'firmware'`).Scan(&runningAge, &outstandingAge))
		require.Equal(t, wantRunningAge, runningAge)
		require.Equal(t, 1200.0, outstandingAge)
	}

	claim()
	_, err = tx.ExecContext(ctx, `
UPDATE queue_message SET processing_started_at = now() - interval '10 minutes' WHERE id = $1`, messageID)
	require.NoError(t, err)
	require.NoError(t, q.TouchProcessingMessage(ctx, messageID))
	readAges(600)

	_, err = q.UpdateMessageAfterFailure(ctx, sqlc.UpdateMessageAfterFailureParams{ID: messageID, RetryCount: 3})
	require.NoError(t, err)
	var hasPreviousStart bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT processing_started_at = now() - interval '10 minutes' FROM queue_message WHERE id = $1`, messageID).Scan(&hasPreviousStart))
	require.True(t, hasPreviousStart)
	readAges(0)

	claim()
	readAges(0)
}

func TestCommandBacklogMigrationGrantsMonitoringRoleReadAccess(t *testing.T) {
	db := newCommandBacklogTestDB(t)
	ctx := t.Context()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	const migration = "current/001113_internal_command_lifecycle_monitoring"
	down, err := migrations.Current.ReadFile(migration + ".down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile(migration + ".up.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(down))
	require.NoError(t, err)

	// Simulate HA bootstrap with a role scoped to this test transaction.
	role := "command_backlog_read_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = tx.ExecContext(ctx, "CREATE ROLE "+role+" NOINHERIT")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "GRANT USAGE ON SCHEMA public TO "+role)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, strings.ReplaceAll(string(up), "grafana_ha_ro", role))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SET LOCAL ROLE "+role)
	require.NoError(t, err)

	var scopes int64
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT count(*) FROM command_backlog_summary WHERE org_id = 1`).Scan(&scopes))
	require.EqualValues(t, 2, scopes)

	var canReadSourceTables bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT has_table_privilege(current_user, 'queue_message', 'SELECT')
    OR has_table_privilege(current_user, 'device', 'SELECT')
    OR has_table_privilege(current_user, 'organization', 'SELECT')`).Scan(&canReadSourceTables))
	require.False(t, canReadSourceTables)
}
