package migrations_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestWriteFenceMigrationPreservesPopulatedLegacyQueue(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	seed := database.CreateDevice(user.OrganizationID, "proto")
	rollbackEmptyMiningGroups(t, database.DB)
	for _, name := range []string{"current/001018_internal_hosted_curtailment_control", "current/001012_internal_retired_device_write_permits", "current/001011_internal_direct_device_write_attempts", "current/001009_internal_queue_pending_order_index", "current/001008_internal_hosted_device_write_fences"} {
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
	for _, name := range []string{"current/001008_internal_hosted_device_write_fences", "current/001009_internal_queue_pending_order_index", "current/001011_internal_direct_device_write_attempts", "current/001012_internal_retired_device_write_permits", "current/001018_internal_hosted_curtailment_control"} {
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

func writeFenceRollbackFixture(t *testing.T) (*testutil.DatabaseService, int64, int64, queue.Message) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	batch := id.GenerateID()
	kind := commandtype.StopMining
	_, err = sqlc.New(database.DB).CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{
		Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(),
		Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1,
		OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true},
	})
	require.NoError(t, err)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, database.DB)
	require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{device.DatabaseID}, nil))
	rows, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return database, user.OrganizationID, device.DatabaseID, rows[0]
}

func assertWriteFenceRollbackRefusesWithoutErasingState(t *testing.T, database *testutil.DatabaseService, diagnostic string) {
	t.Helper()
	snapshot := func() string {
		var state string
		require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT jsonb_build_object(
   'attempts', (SELECT jsonb_agg(to_jsonb(a) ORDER BY a.attempt_id) FROM device_write_attempt a),
   'fences', (SELECT jsonb_agg(to_jsonb(f) ORDER BY f.device_id) FROM device_write_fence f),
   'queue', (SELECT jsonb_agg(to_jsonb(m) ORDER BY m.id) FROM queue_message m))::text`).Scan(&state))
		return state
	}
	before := snapshot()
	down, err := migrations.Current.ReadFile("current/001008_internal_hosted_device_write_fences.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, diagnostic)
	require.ErrorContains(t, err, "rollback is unsafe")
	require.Equal(t, before, snapshot(), "refused rollback must preserve all ownership, attempt, and queued-command evidence")
}

func TestWriteFenceRollbackCannotEraseHostOnlyAttempt(t *testing.T) {
	database, orgID, deviceID, message := writeFenceRollbackFixture(t)
	service := devicewrite.NewService(database.DB)
	_, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	fence, err := sqlc.New(database.DB).LockDeviceWriteFenceRecord(t.Context(), sqlc.LockDeviceWriteFenceRecordParams{DeviceID: deviceID, OrganizationID: orgID})
	require.NoError(t, err)
	require.Zero(t, fence.Epoch)
	require.Equal(t, "host", fence.State, "the ownership guard alone cannot refuse this rollback")
	require.Zero(t, message.WriteEpoch, "the queued-epoch guard alone cannot refuse this rollback")
	var attempts int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE queue_message_id=$1 AND status='running'`, message.ID).Scan(&attempts))
	require.Equal(t, 1, attempts)
	assertWriteFenceRollbackRefusesWithoutErasingState(t, database, "device write attempt history exists")
}

func TestWriteFenceRollbackReportsDurableStateCause(t *testing.T) {
	for _, cause := range []string{"advanced epoch", "non-host ownership", "queued epoch"} {
		t.Run(cause, func(t *testing.T) {
			database, orgID, deviceID, message := writeFenceRollbackFixture(t)
			diagnostic := "advanced epochs or non-host ownership"
			switch cause {
			case "advanced epoch":
				service := devicewrite.NewService(database.DB)
				epoch, err := service.Reserve(t.Context(), orgID, deviceID)
				require.NoError(t, err)
				require.NoError(t, service.ReleaseHost(t.Context(), orgID, deviceID, epoch))
			case "non-host ownership":
				_, err := database.DB.ExecContext(t.Context(), `UPDATE device_write_fence SET state='reserved' WHERE device_id=$1`, deviceID)
				require.NoError(t, err)
			case "queued epoch":
				_, err := database.DB.ExecContext(t.Context(), `UPDATE queue_message SET write_epoch=1 WHERE id=$1`, message.ID)
				require.NoError(t, err)
				diagnostic = "queued device writes retain nonzero epochs"
			}
			assertWriteFenceRollbackRefusesWithoutErasingState(t, database, diagnostic)
		})
	}
}
