package migrations_test

import (
	"context"
	"testing"
	"time"

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
	down, err := migrations.Migrations.ReadFile("000165_hosted_device_write_fences.down.sql")
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
	for _, name := range []string{"000166_queue_pending_order_index", "000165_hosted_device_write_fences"} {
		script, err := migrations.Migrations.ReadFile(name + ".down.sql")
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
	for _, name := range []string{"000165_hosted_device_write_fences", "000166_queue_pending_order_index"} {
		script, err := migrations.Migrations.ReadFile(name + ".up.sql")
		require.NoError(t, err)
		if name == "000165_hosted_device_write_fences" {
			_, err = database.DB.ExecContext(t.Context(), string(script))
			require.NoError(t, err)
			continue
		}
		// Keep an earlier writer open so the index build visibly waits for it.
		// A second writer must still proceed while that build is waiting.
		indexConn, err := database.DB.Conn(t.Context())
		require.NoError(t, err)
		defer indexConn.Close()
		var indexPID int
		require.NoError(t, indexConn.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&indexPID))
		blocker, err := database.DB.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		defer blocker.Rollback()
		holdSQL := "UPDATE queue_message SET retry_count=retry_count WHERE device_id=$1"
		writeSQL := "UPDATE queue_message SET retry_count=retry_count WHERE device_id<>$1"
		_, err = blocker.ExecContext(t.Context(), holdSQL, seed.DatabaseID)
		require.NoError(t, err)
		indexDone := make(chan error, 1)
		go func() { _, err := indexConn.ExecContext(t.Context(), string(script)); indexDone <- err }()
		waiting := false
		deadline := time.Now().Add(5 * time.Second)
		for !waiting && time.Now().Before(deadline) {
			err = database.DB.QueryRowContext(t.Context(), "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", indexPID).Scan(&waiting)
			if err != nil {
				break
			}
			if !waiting {
				time.Sleep(10 * time.Millisecond)
			}
		}
		writeCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		_, writeErr := database.DB.ExecContext(writeCtx, writeSQL, seed.DatabaseID)
		cancel()
		require.NoError(t, blocker.Rollback())
		select {
		case indexErr := <-indexDone:
			require.NoError(t, indexErr)
		case <-time.After(5 * time.Second):
			t.Fatal("index build did not finish after the original writer released")
		}
		require.NoError(t, indexConn.Close())
		require.NoError(t, err)
		require.True(t, waiting, "index build must be observed waiting on the held writer")
		require.NoError(t, writeErr, "a waiting concurrent index build must not block subsequent writes")
	}
	var messages, hostEpochs int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*),count(*) FILTER(WHERE write_epoch=0) FROM queue_message`).Scan(&messages, &hostEpochs))
	require.Equal(t, 4000, messages)
	require.Equal(t, messages, hostEpochs)
	var validIndex bool
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT indisvalid FROM pg_index WHERE indexrelid='idx_queue_message_pending_created_id'::regclass`).Scan(&validIndex))
	require.True(t, validIndex)
}
