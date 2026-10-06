package command

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

// Exercise the production command/queue boundary without importing rollout,
// which depends on command. The callback performs the same atomic enqueue and
// prepared attempt/reservation write as an enforcement dispatch.
func TestRolloutDispatchCommitsQueueAndBookkeepingTogether(t *testing.T) {
	for _, failure := range []struct {
		name     string
		queue    bool
		deferred bool
	}{
		{name: "attempt write fails"},
		{name: "commit fails", deferred: true},
		{name: "queue insert fails", queue: true},
	} {
		t.Run(failure.name, func(t *testing.T) {
			_, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
			ctx := manualSessionCtx(1)
			_, err := conn.ExecContext(ctx, `INSERT INTO "user" (id, user_id, username, password_hash)
				VALUES (42, 'user-1', 'test-user', 'unused')`)
			require.NoError(t, err)

			t.Chdir(t.TempDir())
			filesService, err := files.NewService(files.Config{})
			require.NoError(t, err)
			fileID, err := filesService.SaveFirmwareFile("assigned.swu", strings.NewReader("firmware payload"), files.FirmwareMetadata{
				TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0",
			})
			require.NoError(t, err)
			artifact, err := filesService.ResolveFirmwareArtifact(fileID)
			require.NoError(t, err)
			var channelID, rolloutID int64
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO release_channel (org_id, name, created_by)
				VALUES (1, 'Transactional dispatch', 42) RETURNING id`).Scan(&channelID))
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO firmware_rollout
				(org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation)
				VALUES (1, $1, 'TestCorp', 'TestMiner', $2, '2.0.0', 1) RETURNING id`, channelID, artifact.Checksum).Scan(&rolloutID))
			_, err = conn.ExecContext(ctx, `INSERT INTO firmware_rollout_device (rollout_id, device_id) VALUES ($1, $2)`, rolloutID, deviceID)
			require.NoError(t, err)

			run := newExecutionRun(t.Context())
			t.Cleanup(run.cancelAdmission)
			t.Cleanup(run.cancelWork)
			finalizers := 0
			svc := &Service{
				config: &Config{}, conn: conn, executionService: &ExecutionService{run: run},
				messageQueue: queue.NewDatabaseMessageQueue(&queue.Config{}, conn),
				filesService: filesService, deviceStore: sqlstores.NewSQLDeviceStore(conn),
				startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {
					finalizers++
					assertRolloutDispatchRows(t, conn, 1)
				},
			}
			_, err = conn.ExecContext(ctx, `CREATE FUNCTION reject_rollout_attempt() RETURNS trigger AS $$
				BEGIN
					RAISE EXCEPTION 'injected rollout dispatch failure' USING ERRCODE = '40001';
					RETURN NEW;
				END;
				$$ LANGUAGE plpgsql`)
			require.NoError(t, err)
			trigger := `CREATE TRIGGER reject_rollout_attempt BEFORE UPDATE ON firmware_rollout_device
				FOR EACH ROW EXECUTE FUNCTION reject_rollout_attempt()`
			if failure.deferred {
				trigger = `CREATE CONSTRAINT TRIGGER reject_rollout_attempt AFTER UPDATE ON firmware_rollout_device
					DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_rollout_attempt()`
			}
			if failure.queue {
				trigger = `CREATE TRIGGER reject_rollout_attempt BEFORE INSERT ON queue_message
					FOR EACH ROW EXECUTE FUNCTION reject_rollout_attempt()`
			}
			_, err = conn.ExecContext(ctx, trigger)
			require.NoError(t, err)

			transactor := sqlstores.NewSQLTransactor(conn)
			attempts := 0
			var batchUUID string
			dispatch := func(txCtx context.Context) error {
				attempts++
				result, err := svc.FirmwareUpdateArtifact(txCtx, includeSelector(identifier), artifact.Checksum, artifact.Metadata)
				if err != nil {
					return err
				}
				require.Equal(t, 1, result.DispatchedCount)
				batchUUID = result.BatchIdentifier
				assertRolloutDispatchRows(t, conn, 0)
				assert.Zero(t, finalizers, "tracking must not start before the owning transaction commits")
				err = db.GetTxQueries(txCtx).MarkFirmwareRolloutDevicesSent(txCtx, sqlc.MarkFirmwareRolloutDevicesSentParams{
					RolloutID: rolloutID, DeviceIds: []int64{deviceID},
					DispatchedDeviceIds: []int64{deviceID}, BatchUuid: batchUUID,
				})
				if err == nil {
					assertRolloutDispatchRows(t, conn, 0)
				}
				return err
			}
			err = transactor.RunInTxNoRetry(ctx, dispatch)
			require.ErrorContains(t, err, "injected rollout dispatch failure")
			assert.Equal(t, 1, attempts, "serialization failure must not replay the dispatch callback")
			assert.Zero(t, finalizers, "rollback must discard pending finalizers")
			assertRolloutDispatchRows(t, conn, 0)
			var sentAt sql.NullTime
			var recordedBatch sql.NullString
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT last_sent_at, last_dispatched_batch_uuid
				FROM firmware_rollout_device WHERE rollout_id = $1 AND device_id = $2`, rolloutID, deviceID).Scan(&sentAt, &recordedBatch))
			assert.False(t, sentAt.Valid)
			assert.False(t, recordedBatch.Valid)

			dropTrigger := `DROP TRIGGER reject_rollout_attempt ON firmware_rollout_device`
			if failure.queue {
				dropTrigger = `DROP TRIGGER reject_rollout_attempt ON queue_message`
			}
			_, err = conn.ExecContext(ctx, dropTrigger)
			require.NoError(t, err)
			require.NoError(t, transactor.RunInTxNoRetry(ctx, dispatch))
			assert.Equal(t, 2, attempts)
			assert.Equal(t, 1, finalizers)
			assertRolloutDispatchRows(t, conn, 1)
			var linked int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*)
				FROM firmware_rollout_device rd
				JOIN queue_message qm ON qm.command_batch_log_uuid = rd.last_dispatched_batch_uuid AND qm.device_id = rd.device_id
				JOIN firmware_rollout_reservation reservation ON reservation.batch_uuid = qm.command_batch_log_uuid AND reservation.device_id = rd.device_id
				WHERE rd.rollout_id = $1 AND rd.device_id = $2 AND rd.last_dispatched_batch_uuid = $3
				AND reservation.channel_id = $4 AND qm.payload->>'firmware_checksum' = $5`,
				rolloutID, deviceID, batchUUID, channelID, artifact.Checksum).Scan(&linked))
			assert.Equal(t, 1, linked, "the committed queue item, rollout attempt and reservation must identify the same dispatch")
		})
	}
}

func assertRolloutDispatchRows(t *testing.T, conn *sql.DB, want int) {
	t.Helper()
	var batches, messages, attempts, reservations int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT
		(SELECT count(*) FROM command_batch_log),
		(SELECT count(*) FROM queue_message),
		(SELECT sum(attempts) FROM firmware_rollout_device),
		(SELECT count(*) FROM firmware_rollout_reservation)`).Scan(&batches, &messages, &attempts, &reservations))
	assert.Equal(t, []int{want, want, want, want}, []int{batches, messages, attempts, reservations}, "committed batches/messages/attempts/reservations")
}

func TestCommandRejectsTransactionWithoutCommitHooksBeforeWriting(t *testing.T) {
	for _, operation := range []string{"reboot", "download logs", "worker names"} {
		t.Run(operation, func(t *testing.T) {
			svc, conn, identifier, finalizers := newTransactionalCommandFixture(t)
			ctx := manualSessionCtx(1)
			err := db.WithTransactionNoRetryNoResult(ctx, conn, func(q sqlc.Querier) error {
				batchUUID, dispatchErr := dispatchTransactionalCommand(db.WithTxQueries(ctx, q), svc, identifier, operation)
				assert.Empty(t, batchUUID)
				assert.ErrorContains(t, dispatchErr, "transactional command dispatch requires post-commit callbacks")
				// Deliberately commit: rejection must precede any write rather
				// than rely on the caller rolling back.
				return nil
			})
			require.NoError(t, err)
			assertCommandDispatchRows(t, conn, 0)
			assert.Zero(t, *finalizers)
		})
	}
}

func TestSpecializedCommandDispatchWaitsForCommit(t *testing.T) {
	for _, failure := range []struct {
		operation  string
		queueError bool
	}{
		{operation: "download logs"},
		{operation: "worker names"},
		{operation: "worker names", queueError: true},
	} {
		name := failure.operation
		if failure.queueError {
			name += " enqueue failure"
		}
		t.Run(name, func(t *testing.T) {
			svc, conn, identifier, finalizers := newTransactionalCommandFixture(t)
			ctx, cancel := context.WithTimeout(manualSessionCtx(1), 10*time.Second)
			defer cancel()
			if failure.queueError {
				_, err := conn.ExecContext(ctx, `CREATE FUNCTION reject_specialized_queue() RETURNS trigger AS $$
					BEGIN RAISE EXCEPTION 'injected queue failure' USING ERRCODE = '40001'; END;
					$$ LANGUAGE plpgsql;
					CREATE TRIGGER reject_specialized_queue BEFORE INSERT ON queue_message
					FOR EACH ROW EXECUTE FUNCTION reject_specialized_queue()`)
				require.NoError(t, err)
			}
			transactor := sqlstores.NewSQLTransactor(conn)
			dispatch := func(txCtx context.Context) error {
				batchUUID, err := dispatchTransactionalCommand(txCtx, svc, identifier, failure.operation)
				if err != nil {
					return err
				}
				assert.NotEmpty(t, batchUUID)
				assertCommandDispatchRows(t, conn, 0)
				assert.Zero(t, *finalizers)
				return nil
			}
			err := transactor.RunInTxNoRetry(ctx, func(txCtx context.Context) error {
				if err := dispatch(txCtx); err != nil {
					return err
				}
				return errors.New("roll back after dispatch")
			})
			if failure.queueError {
				require.ErrorContains(t, err, "injected queue failure", "enqueue failure must not wait on a separate batch reconciliation transaction")
				_, err = conn.ExecContext(ctx, `DROP TRIGGER reject_specialized_queue ON queue_message`)
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "roll back after dispatch")
			}
			assertCommandDispatchRows(t, conn, 0)
			assert.Zero(t, *finalizers)

			require.NoError(t, transactor.RunInTxNoRetry(ctx, dispatch))
			assertCommandDispatchRows(t, conn, 1)
			assert.Equal(t, 1, *finalizers)
		})
	}
}

func newTransactionalCommandFixture(t *testing.T) (*Service, *sql.DB, string, *int) {
	t.Helper()
	_, conn, _, _, identifier := setupPasswordCommandDevice(t, "proto", false)
	ctx := manualSessionCtx(1)
	_, err := conn.ExecContext(ctx, `INSERT INTO "user" (id, user_id, username, password_hash)
		VALUES (42, 'user-1', 'test-user', 'unused')`)
	require.NoError(t, err)
	t.Chdir(t.TempDir())
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	run := newExecutionRun(t.Context())
	t.Cleanup(run.cancelAdmission)
	t.Cleanup(run.cancelWork)
	finalizers := 0
	svc := &Service{
		config: &Config{}, conn: conn, executionService: &ExecutionService{run: run},
		messageQueue: queue.NewDatabaseMessageQueue(&queue.Config{}, conn),
		filesService: filesService, deviceStore: sqlstores.NewSQLDeviceStore(conn),
		startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {
			finalizers++
			assertCommandDispatchRows(t, conn, 1)
		},
	}
	return svc, conn, identifier, &finalizers
}

func dispatchTransactionalCommand(ctx context.Context, svc *Service, identifier, operation string) (string, error) {
	if operation == "worker names" {
		return svc.ReapplyCurrentPoolsWithWorkerNames(ctx, map[string]string{identifier: "new-worker"})
	}
	dispatch := svc.Reboot
	if operation == "download logs" {
		dispatch = svc.DownloadLogs
	}
	result, err := dispatch(ctx, includeSelector(identifier))
	if err != nil {
		return "", err
	}
	return result.BatchIdentifier, nil
}

func assertCommandDispatchRows(t *testing.T, conn *sql.DB, want int) {
	t.Helper()
	var batches, messages int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT
		(SELECT count(*) FROM command_batch_log),
		(SELECT count(*) FROM queue_message)`).Scan(&batches, &messages))
	assert.Equal(t, []int{want, want}, []int{batches, messages}, "committed batches/messages")
}
