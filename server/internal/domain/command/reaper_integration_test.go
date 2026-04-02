package command_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupReaperTest(t *testing.T) (*sql.DB, *testutil.DatabaseService, *testutil.TestUser) {
	t.Helper()
	testConfig, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, testConfig)
	user := dbService.CreateSuperAdminUser()
	return dbService.DB, dbService, user
}

func createBatchLog(t *testing.T, conn *sql.DB, batchUUID string, userID int64, deviceCount int32) {
	t.Helper()
	err := db2.WithTransactionNoResult(context.Background(), conn, func(q *sqlc.Queries) error {
		_, err := q.CreateCommandBatchLog(context.Background(), sqlc.CreateCommandBatchLogParams{
			Uuid:         batchUUID,
			Type:         "REBOOT",
			CreatedBy:    userID,
			CreatedAt:    time.Now(),
			Status:       sqlc.BatchStatusEnumPROCESSING,
			DevicesCount: deviceCount,
			Payload:      pqtype.NullRawMessage{Valid: false},
		})
		return err
	})
	require.NoError(t, err)
}

func createStuckMessage(t *testing.T, conn *sql.DB, batchUUID string, deviceID int64, age time.Duration) {
	t.Helper()
	ctx := context.Background()
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		return q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
			CommandBatchLogUuid: batchUUID,
			CommandType:         "REBOOT",
			DeviceID:            deviceID,
			Status:              sqlc.QueueStatusEnumPROCESSING,
			RetryCount:          0,
			Payload:             pqtype.NullRawMessage{Valid: false},
		})
	})
	require.NoError(t, err)

	// Disable trigger temporarily to backdate updated_at (trigger auto-sets it to NOW())
	_, err = conn.ExecContext(ctx, "ALTER TABLE queue_message DISABLE TRIGGER update_queue_message_updated_at")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx,
		"UPDATE queue_message SET updated_at = $1 WHERE command_batch_log_uuid = $2 AND device_id = $3",
		time.Now().Add(-age), batchUUID, deviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "ALTER TABLE queue_message ENABLE TRIGGER update_queue_message_updated_at")
	require.NoError(t, err)
}

func getQueueMessageStatus(t *testing.T, conn *sql.DB, batchUUID string, deviceID int64) sqlc.QueueStatusEnum {
	t.Helper()
	var status sqlc.QueueStatusEnum
	err := conn.QueryRowContext(context.Background(),
		"SELECT status FROM queue_message WHERE command_batch_log_uuid = $1 AND device_id = $2",
		batchUUID, deviceID).Scan(&status)
	require.NoError(t, err)
	return status
}

func getAuditLogStatus(t *testing.T, conn *sql.DB, batchUUID string, deviceID int64) (sqlc.DeviceCommandStatusEnum, bool) {
	t.Helper()
	var status sqlc.DeviceCommandStatusEnum
	err := conn.QueryRowContext(context.Background(),
		`SELECT cdl.status FROM command_on_device_log cdl
		 JOIN command_batch_log cbl ON cdl.command_batch_log_id = cbl.id
		 WHERE cbl.uuid = $1 AND cdl.device_id = $2`,
		batchUUID, deviceID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", false
	}
	require.NoError(t, err)
	return status, true
}

// noopMessageQueue is a minimal MessageQueue that blocks on Dequeue forever.
type noopMessageQueue struct{}

func (n *noopMessageQueue) Enqueue(_ context.Context, _ string, _ commandtype.Type, _ []int64, _ interface{}) error {
	return nil
}
func (n *noopMessageQueue) Dequeue(ctx context.Context) ([]queue.Message, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("dequeue cancelled: %w", ctx.Err())
}
func (n *noopMessageQueue) MarkSuccess(_ context.Context, _ int64) error          { return nil }
func (n *noopMessageQueue) MarkFailed(_ context.Context, _ int64, _ string) error { return nil }
func (n *noopMessageQueue) MarkPermanentlyFailed(_ context.Context, _ int64, _ string) error {
	return nil
}
func (n *noopMessageQueue) IsBatchFinished(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (n *noopMessageQueue) IsBatchProcessing(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (n *noopMessageQueue) MaxFailureRetries() int32 { return 5 }

func TestReaperIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("reaps stuck PROCESSING messages and writes audit log", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupReaperTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")

		batchUUID := "reap-test-batch-1"
		createBatchLog(t, conn, batchUUID, user.DatabaseID, 1)
		createStuckMessage(t, conn, batchUUID, device.DatabaseID, 10*time.Minute)

		svc := command.NewExecutionService(t.Context(), &command.Config{
			MaxWorkers:            5,
			MasterPollingInterval: 100 * time.Millisecond,
			StuckMessageTimeout:   5 * time.Minute,
			ReaperInterval:        50 * time.Millisecond,
		}, conn, &noopMessageQueue{}, nil, nil, nil, nil, nil, nil)

		// Act — start the service, the reaper should fire within 50ms
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Assert — reaper should mark the stuck message as FAILED
		assert.Eventually(t, func() bool {
			return getQueueMessageStatus(t, conn, batchUUID, device.DatabaseID) == sqlc.QueueStatusEnumFAILED
		}, 500*time.Millisecond, 25*time.Millisecond, "reaper should mark stuck message as FAILED")

		// Audit log should also be written
		auditStatus, found := getAuditLogStatus(t, conn, batchUUID, device.DatabaseID)
		assert.True(t, found, "audit log should exist for reaped message")
		assert.Equal(t, sqlc.DeviceCommandStatusEnumFAILED, auditStatus)
	})

	t.Run("skips messages not yet past timeout", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupReaperTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")

		batchUUID := "reap-test-batch-2"
		createBatchLog(t, conn, batchUUID, user.DatabaseID, 1)
		createStuckMessage(t, conn, batchUUID, device.DatabaseID, 1*time.Minute)

		svc := command.NewExecutionService(t.Context(), &command.Config{
			MaxWorkers:            5,
			MasterPollingInterval: 100 * time.Millisecond,
			StuckMessageTimeout:   5 * time.Minute,
			ReaperInterval:        50 * time.Millisecond,
		}, conn, &noopMessageQueue{}, nil, nil, nil, nil, nil, nil)

		// Act
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Wait for a few reaper ticks
		time.Sleep(200 * time.Millisecond)

		// Assert — message should still be PROCESSING
		status := getQueueMessageStatus(t, conn, batchUUID, device.DatabaseID)
		assert.Equal(t, sqlc.QueueStatusEnumPROCESSING, status)
	})

	t.Run("does not overwrite terminal SUCCESS status", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupReaperTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")

		batchUUID := "reap-test-batch-3"
		createBatchLog(t, conn, batchUUID, user.DatabaseID, 1)

		// Create a message in SUCCESS state with an old timestamp
		ctx := context.Background()
		err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
			return q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
				CommandBatchLogUuid: batchUUID,
				CommandType:         "REBOOT",
				DeviceID:            device.DatabaseID,
				Status:              sqlc.QueueStatusEnumSUCCESS,
				RetryCount:          0,
				Payload:             pqtype.NullRawMessage{Valid: false},
			})
		})
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, "ALTER TABLE queue_message DISABLE TRIGGER update_queue_message_updated_at")
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx,
			"UPDATE queue_message SET updated_at = $1 WHERE command_batch_log_uuid = $2 AND device_id = $3",
			time.Now().Add(-10*time.Minute), batchUUID, device.DatabaseID)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, "ALTER TABLE queue_message ENABLE TRIGGER update_queue_message_updated_at")
		require.NoError(t, err)

		svc := command.NewExecutionService(t.Context(), &command.Config{
			MaxWorkers:            5,
			MasterPollingInterval: 100 * time.Millisecond,
			StuckMessageTimeout:   5 * time.Minute,
			ReaperInterval:        50 * time.Millisecond,
		}, conn, &noopMessageQueue{}, nil, nil, nil, nil, nil, nil)

		// Act
		err = svc.Start(t.Context())
		require.NoError(t, err)
		time.Sleep(200 * time.Millisecond)

		// Assert — SUCCESS should not be overwritten
		status := getQueueMessageStatus(t, conn, batchUUID, device.DatabaseID)
		assert.Equal(t, sqlc.QueueStatusEnumSUCCESS, status)
	})

	t.Run("reaps multiple stuck messages atomically with audit logs", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupReaperTest(t)
		device1 := dbService.CreateDevice(user.OrganizationID, "proto")
		device2 := dbService.CreateDevice(user.OrganizationID, "proto")

		batchUUID := "reap-test-batch-4"
		createBatchLog(t, conn, batchUUID, user.DatabaseID, 2)
		createStuckMessage(t, conn, batchUUID, device1.DatabaseID, 10*time.Minute)
		createStuckMessage(t, conn, batchUUID, device2.DatabaseID, 10*time.Minute)

		svc := command.NewExecutionService(t.Context(), &command.Config{
			MaxWorkers:            5,
			MasterPollingInterval: 100 * time.Millisecond,
			StuckMessageTimeout:   5 * time.Minute,
			ReaperInterval:        50 * time.Millisecond,
		}, conn, &noopMessageQueue{}, nil, nil, nil, nil, nil, nil)

		// Act
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Assert — both should be reaped
		assert.Eventually(t, func() bool {
			s1 := getQueueMessageStatus(t, conn, batchUUID, device1.DatabaseID)
			s2 := getQueueMessageStatus(t, conn, batchUUID, device2.DatabaseID)
			return s1 == sqlc.QueueStatusEnumFAILED && s2 == sqlc.QueueStatusEnumFAILED
		}, 500*time.Millisecond, 25*time.Millisecond)

		// Both should have audit log entries
		status1, found1 := getAuditLogStatus(t, conn, batchUUID, device1.DatabaseID)
		status2, found2 := getAuditLogStatus(t, conn, batchUUID, device2.DatabaseID)
		assert.True(t, found1)
		assert.True(t, found2)
		assert.Equal(t, sqlc.DeviceCommandStatusEnumFAILED, status1)
		assert.Equal(t, sqlc.DeviceCommandStatusEnumFAILED, status2)
	})
}
