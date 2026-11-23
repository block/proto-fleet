package command

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

func TestWorkerCompletionPersistenceFailurePreservesDeviceOutcome(t *testing.T) {
	for _, tc := range []struct {
		name        string
		deviceError error
		queueStatus sqlc.QueueStatusEnum
		logStatus   sqlc.DeviceCommandStatusEnum
	}{
		{
			name:        "successful device command",
			queueStatus: sqlc.QueueStatusEnumSUCCESS,
			logStatus:   sqlc.DeviceCommandStatusEnumSUCCESS,
		},
		{
			name:        "failed device command",
			deviceError: fleeterror.NewFailedPreconditionError("device refused stop mining"),
			queueStatus: sqlc.QueueStatusEnumFAILED,
			logStatus:   sqlc.DeviceCommandStatusEnumFAILED,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
			ctx := t.Context()
			// The sequence survives transaction rollback. Only completion fails;
			// the subsequent queue/result transaction can use the database normally.
			_, err := conn.ExecContext(ctx, `
				INSERT INTO "user" (id, user_id, username, password_hash)
				VALUES (42, 'completion-worker', 'completion-worker', 'unused');
				CREATE SEQUENCE write_completion_attempt;
				CREATE FUNCTION fail_first_write_completion() RETURNS trigger AS $$
				BEGIN
					IF nextval('write_completion_attempt') = 1 THEN
						RAISE EXCEPTION 'injected attempt completion persistence failure';
					END IF;
					RETURN NEW;
				END; $$ LANGUAGE plpgsql;
				CREATE TRIGGER fail_first_write_completion BEFORE UPDATE ON device_write_attempt
				FOR EACH ROW EXECUTE FUNCTION fail_first_write_completion()`)
			require.NoError(t, err)
			q := sqlc.New(conn)
			const batch = "write-completion-failure"
			commandType := commandtype.StopMining
			_, err = q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
				Uuid: batch, Type: commandType.String(), CreatedBy: 42, CreatedAt: time.Now(),
				Status: sqlc.BatchStatusEnumPROCESSING, DevicesCount: 1,
				OrganizationID: sql.NullInt64{Int64: 1, Valid: true},
			})
			require.NoError(t, err)
			require.NoError(t, q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
				CommandBatchLogUuid: batch, CommandType: commandType.String(), DeviceID: deviceID,
				Status: sqlc.QueueStatusEnumPROCESSING,
			}))
			var messageID int64
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM queue_message WHERE command_batch_log_uuid = $1`, batch).Scan(&messageID))

			ctrl := gomock.NewController(t)
			miner := minerIfaceMocks.NewMockMiner(ctrl)
			minerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
			minerGetter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(miner, nil).Times(1)
			miner.EXPECT().GetOrgID().Return(int64(1)).Times(1)
			miner.EXPECT().GetSiteID().Return(int64(0)).Times(1)
			miner.EXPECT().StopMining(gomock.Any()).Return(tc.deviceError).Times(1)
			svc.minerService = minerGetter
			svc.messageQueue = queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, conn)
			operational := &recordingOperationalMetrics{}
			svc.WithOperationalMetrics(operational)
			svc.workerProcessCommand(ctx, queue.Message{
				ID: messageID, DeviceID: deviceID, BatchLogUUID: batch, CommandType: commandType, OrgID: 1,
			})

			require.Equal(t, 1, operational.started)
			require.Equal(t, 1, operational.finished)
			require.Equal(t, 1, operational.committed)
			require.Zero(t, operational.persistenceFailed)

			var completionAttempts, retryCount int
			var queueStatus sqlc.QueueStatusEnum
			var queueError, logError sql.NullString
			var logStatus sqlc.DeviceCommandStatusEnum
			var attemptStatus string
			var finishedAt sql.NullTime
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT last_value FROM write_completion_attempt`).Scan(&completionAttempts))
			require.Equal(t, 1, completionAttempts, "completion persistence must actually fail")
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status, retry_count, error_info FROM queue_message WHERE id = $1`, messageID).
				Scan(&queueStatus, &retryCount, &queueError))
			require.Equal(t, tc.queueStatus, queueStatus, "completion failure must not make the device outcome retryable")
			require.Zero(t, retryCount)
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status, error_info FROM command_on_device_log
				WHERE command_batch_log_id = (SELECT id FROM command_batch_log WHERE uuid = $1) AND device_id = $2`, batch, deviceID).
				Scan(&logStatus, &logError))
			require.Equal(t, tc.logStatus, logStatus)
			if tc.deviceError == nil {
				require.False(t, queueError.Valid)
				require.False(t, logError.Valid)
			} else {
				require.Equal(t, tc.deviceError.Error(), queueError.String)
				require.Equal(t, sanitizedErrorInfo(tc.deviceError), logError)
			}
			messages, err := svc.messageQueue.Dequeue(ctx, 1)
			require.NoError(t, err)
			require.Empty(t, messages, "terminal commands must not execute a second time")
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status, finished_at FROM device_write_attempt WHERE queue_message_id = $1`, messageID).
				Scan(&attemptStatus, &finishedAt))
			require.Equal(t, "running", attemptStatus, "failed completion must retain unresolved attempt evidence")
			require.False(t, finishedAt.Valid)
			writes := devicewrite.NewService(conn)
			epoch, err := writes.Reserve(ctx, 1, deviceID)
			require.NoError(t, err)
			drained, err := writes.Drained(ctx, 1, deviceID, epoch)
			require.NoError(t, err)
			require.False(t, drained, "terminal queue status must not falsely clear physical drain evidence")
		})
	}
}
