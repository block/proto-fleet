package migrations_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestDirectWriteRollbackPreservesCompletedDirectEvidence(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	attempt, err := devicewrite.NewService(database.DB).BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
	require.NoError(t, err)
	require.NoError(t, attempt.Finish(t.Context(), nil))
	// Explicitly seed qualified completion history for the rollback fixture.
	// A nil adapter result above now leaves the real attempt unresolved.
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device_write_attempt SET status='complete' WHERE device_id=$1", device.DatabaseID)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001011_internal_direct_device_write_attempts.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "rollback is unsafe")
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND queue_message_id IS NULL AND status='complete'", device.DatabaseID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestRetiredWriteRollbackPreservesSafetyEvidence(t *testing.T) {
	for _, evidence := range []string{"running attempt", "deletion permit"} {
		t.Run(evidence, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			service := devicewrite.NewService(database.DB)
			q := sqlc.New(database.DB)
			keys, err := q.GetDeviceWriteIdentityKeys(t.Context(), device.DatabaseID)
			require.NoError(t, err)
			if evidence == "running attempt" {
				_, err = service.BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
				require.NoError(t, err)
			} else {
				epoch, err := service.Reserve(t.Context(), user.OrganizationID, device.DatabaseID)
				require.NoError(t, err)
				require.NoError(t, q.CreateDeletionUnpairPermit(t.Context(), sqlc.CreateDeletionUnpairPermitParams{DeviceID: device.DatabaseID, OrganizationID: user.OrganizationID, Epoch: epoch, BindingHash: "fixture-binding-digest", IdentityKeys: keys}))
			}
			down, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.down.sql")
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), string(down))
			require.ErrorContains(t, err, "rollback is unsafe")
			retained, err := q.GetDeviceWriteIdentityKeys(t.Context(), device.DatabaseID)
			require.NoError(t, err)
			require.Equal(t, keys, retained)
			var count int
			if evidence == "running attempt" {
				require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND identity_keys=$2 AND status='running'", device.DatabaseID, keys).Scan(&count))
			} else {
				require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_deletion_unpair_permit WHERE device_id=$1", device.DatabaseID).Scan(&count))
			}
			require.Equal(t, 1, count)
		})
	}
}

func TestRetiredIdentityUpgradePreservesOutstandingAttempts(t *testing.T) {
	for _, status := range []string{"running", "unknown"} {
		t.Run(status, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			retired := database.CreateDevice(user.OrganizationID, "proto")
			down, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.down.sql")
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), string(down))
			require.NoError(t, err)
			q := sqlc.New(database.DB)
			require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{retired.DatabaseID}))
			_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET serial_number='  RETIRED-Serial  ',mac_address='AA-BB-CC-DD-EE-FF' WHERE id=$1", retired.DatabaseID)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET ip_address='192.0.2.60',port='8080' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", retired.DatabaseID)
			require.NoError(t, err)
			var nodeID, messageID int64
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,'retired-upgrade',$2,$3,'CONFIRMED') RETURNING id", user.OrganizationID, []byte("fixture-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
			_, err = database.DB.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)", nodeID, retired.DatabaseID, user.OrganizationID)
			require.NoError(t, err)
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES('retired-upgrade',$1,'Reboot','PROCESSING',0) RETURNING id", retired.DatabaseID).Scan(&messageID))
			attemptID := uuid.New()
			// The predecessor has owner and snapshot evidence but no identity_keys column.
			_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,queue_message_id,status,owner_instance_id,identity_snapshot,finished_at) VALUES($1,$2,$3,0,$4,$5,$6,'{"serial_number":"  RETIRED-Serial  ","mac_address":"AA-BB-CC-DD-EE-FF","ip_address":"192.0.2.60","port":"8080"}'::jsonb,CASE WHEN $5='running' THEN NULL ELSE statement_timestamp() END)`, attemptID, retired.DatabaseID, user.OrganizationID, messageID, status, uuid.New())
			require.NoError(t, err)
			// Today's row now describes B. The attempt still describes A.
			_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET serial_number='replacement-B',mac_address='11:22:33:44:55:66' WHERE id=$1", retired.DatabaseID)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET ip_address='192.0.2.61' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", retired.DatabaseID)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", retired.DatabaseID)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET deleted_at=statement_timestamp(),is_active=false WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", retired.DatabaseID)
			require.NoError(t, err)
			up, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.up.sql")
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), string(up))
			require.NoError(t, err)
			keys, err := q.GetDeviceWriteIdentityKeys(t.Context(), retired.DatabaseID)
			require.NoError(t, err)
			require.Equal(t, []string{fmt.Sprintf("org:%d:serial:replacement-b", user.OrganizationID), fmt.Sprintf("org:%d:mac:112233445566", user.OrganizationID), fmt.Sprintf("org:%d:endpoint:%d:192.0.2.61:8080", user.OrganizationID, nodeID)}, keys)
			retainedKeys := []string{fmt.Sprintf("org:%d:serial:retired-serial", user.OrganizationID), fmt.Sprintf("org:%d:mac:aabbccddeeff", user.OrganizationID)}
			var retainedStatus string
			var retainedMessageID sql.NullInt64
			var retainedKeysEqual bool
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,queue_message_id,identity_keys=$2 FROM device_write_attempt WHERE attempt_id=$1", attemptID, retainedKeys).Scan(&retainedStatus, &retainedMessageID, &retainedKeysEqual))
			require.Equal(t, status, retainedStatus)
			require.Equal(t, sql.NullInt64{Int64: messageID, Valid: true}, retainedMessageID)
			require.True(t, retainedKeysEqual)
			// Retained evidence survives retiring the old node association after upgrade.
			_, err = database.DB.ExecContext(t.Context(), "DELETE FROM fleet_node_device WHERE device_id=$1", retired.DatabaseID)
			require.NoError(t, err)
			successor := database.CreateDevice(user.OrganizationID, "proto")
			_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET serial_number='retired-SERIAL',mac_address='aa:bb:cc:dd:ee:ff' WHERE id=$1", successor.DatabaseID)
			require.NoError(t, err)
			service := devicewrite.NewService(database.DB)
			_, err = service.BeginHost(t.Context(), user.OrganizationID, successor.DatabaseID, 0)
			require.Error(t, err, "a new numeric device ID must not erase pre-upgrade evidence")
			_, err = service.Reserve(t.Context(), user.OrganizationID, successor.DatabaseID)
			require.Error(t, err)
			var successorMessageID int64
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES('successor-upgrade',$1,'Reboot','PROCESSING',0) RETURNING id", successor.DatabaseID).Scan(&successorMessageID))
			_, err = service.BeginQueuedHost(t.Context(), queue.Message{ID: successorMessageID, DeviceID: successor.DatabaseID, OrgID: user.OrganizationID, BatchLogUUID: "successor-upgrade", CommandType: commandtype.Reboot, WriteEpoch: 0})
			require.Error(t, err)
			var count int
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt").Scan(&count))
			require.Equal(t, 1, count, "denied successor operations must not mint new evidence")
		})
	}
}

func TestRetiredIdentityUpgradeRefusesUntrustworthyOutstandingSnapshot(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	down, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	require.NoError(t, sqlc.New(database.DB).EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
	id := uuid.New()
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,owner_instance_id,identity_snapshot) VALUES($1,$2,$3,0,$4,'{}')`, id, device.DatabaseID, user.OrganizationID, uuid.New())
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.up.sql")
	require.NoError(t, err)
	tx, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), string(up))
	require.ErrorContains(t, err, "resolve with audited operator recovery")
	require.NoError(t, tx.Rollback())
	var snapshot string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT identity_snapshot::text FROM device_write_attempt WHERE attempt_id=$1", id).Scan(&snapshot))
	require.JSONEq(t, "{}", snapshot, "failed migration must preserve the evidence")
	var columnCount int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_write_attempt' AND column_name='identity_keys'").Scan(&columnCount))
	require.Zero(t, columnCount, "failed migration must retain the predecessor schema")
}

func TestRetiredIdentityLookupUsesOutstandingIndex(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,status,owner_instance_id,identity_snapshot,identity_keys,finished_at)
 SELECT md5('attempt-'||n)::uuid,$1,$2,0,CASE WHEN n%2=0 THEN 'unknown' ELSE 'complete' END,md5('owner-'||n)::uuid,'{}',ARRAY['org:'||$2::bigint::text||':serial:'||n::text],statement_timestamp() FROM generate_series(1,40000) n`, device.DatabaseID, user.OrganizationID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", device.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "VACUUM (ANALYZE) device_write_attempt")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "ANALYZE device")
	require.NoError(t, err)
	var plan []byte
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT EXISTS(SELECT 1 WHERE NOT EXISTS(
 SELECT 1 FROM device_write_attempt a JOIN device d ON d.id=a.device_id WHERE d.deleted_at IS NOT NULL AND a.status NOT IN ('complete','resolved') AND a.identity_keys && $1::text[]))`, []string{fmt.Sprintf("org:%d:serial:40000", user.OrganizationID)}).Scan(&plan))
	require.True(t, json.Valid(plan))
	require.Contains(t, string(plan), "idx_device_write_attempt_outstanding_identity_keys")
	t.Logf("retired identity lookup over 40000 attempts: %s", plan)
	drained, err := q.IsRetiredDeviceWriteDrained(t.Context(), []string{fmt.Sprintf("org:%d:serial:40000", user.OrganizationID)})
	require.NoError(t, err)
	require.False(t, drained)
	drained, err = q.IsRetiredDeviceWriteDrained(t.Context(), []string{fmt.Sprintf("org:%d:serial:39999", user.OrganizationID)})
	require.NoError(t, err)
	require.True(t, drained, "completed retained history must not block admission")
}
