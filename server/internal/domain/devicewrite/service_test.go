package devicewrite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestReservationFencesQueuedWritesAndWaitsForActualCompletion(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, database.DB)
	enqueue := func() string {
		batch := id.GenerateID()
		kind := commandtype.Reboot
		_, err := q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, Payload: pqtype.NullRawMessage{}, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
		require.NoError(t, err)
		require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{device.DatabaseID}, nil))
		return batch
	}
	enqueue()
	dequeued, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, dequeued, 1)
	require.Zero(t, dequeued[0].WriteEpoch)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), dequeued[0])
	require.NoError(t, err)
	pendingBatch := enqueue()
	epoch, err := service.Reserve(t.Context(), user.OrganizationID, device.DatabaseID)
	require.NoError(t, err)
	require.Equal(t, int64(1), epoch)
	require.Error(t, attempt.Recheck(t.Context()))
	drained, err := service.Drained(t.Context(), user.OrganizationID, device.DatabaseID, epoch)
	require.NoError(t, err)
	require.False(t, drained)
	require.Error(t, service.ReleaseHost(t.Context(), user.OrganizationID, device.DatabaseID, epoch))
	rows, err := q.GetQueueMessagesByBatch(t.Context(), pendingBatch)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sqlc.QueueStatusEnumFAILED, rows[0].Status)
	// A reaped queue row is not evidence that the actual call has returned.
	_, err = q.UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: dequeued[0].ID, Status: sqlc.QueueStatusEnumFAILED})
	require.NoError(t, err)
	drained, err = service.Drained(t.Context(), user.OrganizationID, device.DatabaseID, epoch)
	require.NoError(t, err)
	require.False(t, drained)
	require.NoError(t, attempt.Finish(t.Context(), nil))
	drained, err = service.Drained(t.Context(), user.OrganizationID, device.DatabaseID, epoch)
	require.NoError(t, err)
	require.True(t, drained)
	require.NoError(t, service.ReleaseHost(t.Context(), user.OrganizationID, device.DatabaseID, epoch))
	// Returning the miner to host custody must not revive its old command.
	_, err = service.BeginQueuedHost(t.Context(), dequeued[0])
	require.Error(t, err)
	enqueue()
	next, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.Equal(t, epoch, next[0].WriteEpoch)
	resumed, err := service.BeginQueuedHost(t.Context(), next[0])
	require.NoError(t, err)
	require.NoError(t, resumed.Finish(t.Context(), nil))
}

func TestUnknownWriteOutcomeCannotBecomeDrainEvidence(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	batch := id.GenerateID()
	kind := commandtype.Reboot
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
	require.NoError(t, err)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, database.DB)
	require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{device.DatabaseID}, nil))
	rows, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), rows[0])
	require.NoError(t, err)
	physicalError := errors.New("firmware echoed test-only-secret")
	require.ErrorIs(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return physicalError }), physicalError)
	epoch, err := service.Reserve(t.Context(), user.OrganizationID, device.DatabaseID)
	require.NoError(t, err)
	require.NoError(t, attempt.Finish(t.Context(), physicalError))
	require.Error(t, attempt.Finish(t.Context(), nil), "a duplicate completion must not erase an unknown outcome")
	drained, err := service.Drained(t.Context(), user.OrganizationID, device.DatabaseID, epoch)
	require.NoError(t, err)
	require.False(t, drained)
	require.Error(t, service.ReleaseHost(t.Context(), user.OrganizationID, device.DatabaseID, epoch))
	// Cancellation has no bearing on the durable outstanding attempt.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, attempt.Recheck(cancelled))
}

func TestReservationJoinsAllocationTransaction(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
	abort := errors.New("allocation bookkeeping failed")
	err = db.WithTransactionNoResult(t.Context(), database.DB, func(tx sqlc.Querier) error {
		_, err := devicewrite.NewService(database.DB).Reserve(db.WithTxQueries(t.Context(), tx), user.OrganizationID, device.DatabaseID)
		if err != nil {
			return err
		}
		return abort
	})
	require.ErrorIs(t, err, abort)
	fence, err := q.LockDeviceWriteFenceRecord(t.Context(), sqlc.LockDeviceWriteFenceRecordParams{DeviceID: device.DatabaseID, OrganizationID: user.OrganizationID})
	require.NoError(t, err)
	require.Zero(t, fence.Epoch)
	require.Equal(t, "host", fence.State, "a failed booking must not leave an orphaned reservation")
}

func queuedFenceFixture(t *testing.T) (*testutil.DatabaseService, int64, int64, queue.Message, *queue.DatabaseMessageQueue) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	batch := id.GenerateID()
	kind := commandtype.Reboot
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
	require.NoError(t, err)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, database.DB)
	require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{device.DatabaseID}, nil))
	rows, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return database, user.OrganizationID, device.DatabaseID, rows[0], messages
}

func TestQueuedAttemptRejectsChangedPersistedMessageIdentity(t *testing.T) {
	for _, field := range []string{"device", "organization", "command", "batch", "epoch"} {
		t.Run(field, func(t *testing.T) {
			database, _, _, message, _ := queuedFenceFixture(t)
			switch field {
			case "device":
				message.DeviceID++
			case "organization":
				message.OrgID++
			case "command":
				message.CommandType = commandtype.StopMining
			case "batch":
				message.BatchLogUUID = id.GenerateID()
			case "epoch":
				message.WriteEpoch++
			}
			_, err := devicewrite.NewService(database.DB).BeginQueuedHost(t.Context(), message)
			require.Error(t, err)
			var attempts int
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt").Scan(&attempts))
			require.Zero(t, attempts, "a changed message cannot mint an attempt")
		})
	}
}

func TestReservedDeviceRejectsNewEnqueue(t *testing.T) {
	database, orgID, deviceID, message, messages := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	epoch, err := service.Reserve(t.Context(), orgID, deviceID)
	require.NoError(t, err)
	q := sqlc.New(database.DB)
	var actorID int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT created_by FROM command_batch_log WHERE uuid=$1", message.BatchLogUUID).Scan(&actorID))
	batch := id.GenerateID()
	kind := commandtype.Reboot
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: actorID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: orgID, Valid: true}})
	require.NoError(t, err)
	require.Error(t, messages.Enqueue(t.Context(), batch, kind, []int64{deviceID}, nil))
	rows, err := q.GetQueueMessagesByBatch(t.Context(), batch)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, service.ReleaseHost(t.Context(), orgID, deviceID, epoch))
	require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{deviceID}, nil))
	rows, err = q.GetQueueMessagesByBatch(t.Context(), batch)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	var queuedEpoch int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT write_epoch FROM queue_message WHERE command_batch_log_uuid=$1", batch).Scan(&queuedEpoch))
	require.Equal(t, epoch, queuedEpoch)
}

func TestReapedAttemptRejectsFurtherDispatch(t *testing.T) {
	database, orgID, deviceID, message, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	_, err = sqlc.New(database.DB).UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: message.ID, Status: sqlc.QueueStatusEnumFAILED})
	require.NoError(t, err)
	require.Error(t, attempt.Recheck(t.Context()), "a lost queue claim cannot start another physical call")
	require.NoError(t, attempt.Finish(t.Context(), errors.New("queue claim lost before dispatch")))
	epoch, err := service.Reserve(t.Context(), orgID, deviceID)
	require.NoError(t, err)
	drained, err := service.Drained(t.Context(), orgID, deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained)
}
