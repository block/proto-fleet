package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestWriteFenceRollbackCannotEraseOwnershipEpoch(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	service := devicewrite.NewService(database.DB)
	epoch, err := service.Reserve(t.Context(), user.OrganizationID, device.DatabaseID)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001008_internal_hosted_device_write_fences.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "rollback is unsafe")
	fence, err := sqlc.New(database.DB).LockDeviceWriteFenceRecord(t.Context(), sqlc.LockDeviceWriteFenceRecordParams{DeviceID: device.DatabaseID, OrganizationID: user.OrganizationID})
	require.NoError(t, err)
	require.Equal(t, epoch, fence.Epoch)
	require.Equal(t, "reserved", fence.State)
}

func TestWriteFenceMigrationPreservesPopulatedLegacyQueue(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	seed := database.CreateDevice(user.OrganizationID, "proto")
	for _, name := range []string{"current/001009_internal_queue_pending_order_index", "current/001008_internal_hosted_device_write_fences"} {
		script, err := migrations.Current.ReadFile(name + ".down.sql")
		require.NoError(t, err)
		_, err = database.DB.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
	}
	// Exercise a populated upgrade at the size of the initial hosting fleet.
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device(org_id,discovered_device_id,device_identifier,mac_address)
	 SELECT org_id,discovered_device_id,md5('legacy-miner-' || n::text),'00-1A-2B-3C-4D-5E'
	 FROM device CROSS JOIN generate_series(1,3999) n WHERE id=$1`, seed.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count)
	 SELECT 'legacy-batch',id,'Reboot','PENDING',0 FROM device WHERE org_id=$1`, user.OrganizationID)
	require.NoError(t, err)
	// The current runner applies private migrations and their checkpoints in
	// one transaction. Exercise the same boundary on a populated queue.
	upgrade, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer upgrade.Rollback()
	for _, name := range []string{"current/001008_internal_hosted_device_write_fences", "current/001009_internal_queue_pending_order_index"} {
		script, err := migrations.Current.ReadFile(name + ".up.sql")
		require.NoError(t, err)
		_, err = upgrade.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
	}
	require.NoError(t, upgrade.Commit())
	var messages, hostEpochs int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*),count(*) FILTER(WHERE write_epoch=0) FROM queue_message`).Scan(&messages, &hostEpochs))
	require.Equal(t, 4000, messages)
	require.Equal(t, messages, hostEpochs)
	var validIndex bool
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT indisvalid FROM pg_index WHERE indexrelid='idx_queue_message_pending_created_id'::regclass`).Scan(&validIndex))
	require.True(t, validIndex)
}
