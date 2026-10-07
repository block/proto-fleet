package migrations_test

import (
	"database/sql"
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
	down, err := migrations.Current.ReadFile("current/001010_internal_direct_device_write_attempts.down.sql")
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
			down, err := migrations.Current.ReadFile("current/001011_internal_retired_device_write_permits.down.sql")
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
			down, err := migrations.Current.ReadFile("current/001011_internal_retired_device_write_permits.down.sql")
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
			// Version 167 has no identity_keys column; populate the actual old shape.
			_, err = database.DB.ExecContext(t.Context(), "INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,queue_message_id,status,finished_at) VALUES($1,$2,$3,0,$4,$5,CASE WHEN $5='running' THEN NULL ELSE statement_timestamp() END)", attemptID, retired.DatabaseID, user.OrganizationID, messageID, status)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", retired.DatabaseID)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET deleted_at=statement_timestamp(),is_active=false WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", retired.DatabaseID)
			require.NoError(t, err)
			up, err := migrations.Current.ReadFile("current/001011_internal_retired_device_write_permits.up.sql")
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), string(up))
			require.NoError(t, err)
			keys, err := q.GetDeviceWriteIdentityKeys(t.Context(), retired.DatabaseID)
			require.NoError(t, err)
			require.Equal(t, []string{"serial:retired-serial", "mac:aabbccddeeff", fmt.Sprintf("endpoint:%d:192.0.2.60:8080", nodeID)}, keys)
			var retainedStatus string
			var retainedMessageID sql.NullInt64
			var retainedKeysEqual bool
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,queue_message_id,identity_keys=$2 FROM device_write_attempt WHERE attempt_id=$1", attemptID, keys).Scan(&retainedStatus, &retainedMessageID, &retainedKeysEqual))
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
