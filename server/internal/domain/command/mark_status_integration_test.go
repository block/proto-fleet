package command_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMarkStatusTest(t *testing.T) (*sql.DB, *testutil.DatabaseService, *testutil.TestUser) {
	t.Helper()
	testConfig, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, testConfig)
	user := dbService.CreateSuperAdminUser()
	return dbService.DB, dbService, user
}

func seedBatchLog(t *testing.T, conn *sql.DB, batchUUID string, userID int64, deviceCount int32) {
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

func seedProcessingMessage(t *testing.T, conn *sql.DB, batchUUID string, deviceID int64) int64 {
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

	var msgID int64
	err = conn.QueryRowContext(ctx,
		"SELECT id FROM queue_message WHERE command_batch_log_uuid = $1 AND device_id = $2",
		batchUUID, deviceID).Scan(&msgID)
	require.NoError(t, err)
	return msgID
}

func queryQueueStatus(t *testing.T, conn *sql.DB, msgID int64) sqlc.QueueStatusEnum {
	t.Helper()
	var status sqlc.QueueStatusEnum
	err := conn.QueryRowContext(context.Background(),
		"SELECT status FROM queue_message WHERE id = $1", msgID).Scan(&status)
	require.NoError(t, err)
	return status
}

func queryDeviceLogExists(t *testing.T, conn *sql.DB, batchUUID string, deviceID int64) bool {
	t.Helper()
	var count int
	err := conn.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM command_on_device_log cdl
		 JOIN command_batch_log cbl ON cdl.command_batch_log_id = cbl.id
		 WHERE cbl.uuid = $1 AND cdl.device_id = $2`,
		batchUUID, deviceID).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

// TestMarkQueueMessageStatusTransitions tests the queue_message state transitions
// that markQueueMessageStatus performs, by exercising the same sqlc queries directly.
func TestMarkQueueMessageStatusTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("SUCCESS transition on PROCESSING message", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupMarkStatusTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")
		batchUUID := "mark-status-success-1"
		seedBatchLog(t, conn, batchUUID, user.DatabaseID, 1)
		msgID := seedProcessingMessage(t, conn, batchUUID, device.DatabaseID)

		// Act — same query as markQueueMessageStatus with nil workerError
		result, err := db2.WithTransaction(context.Background(), conn, func(q *sqlc.Queries) (sql.Result, error) {
			return q.UpdateMessageStatus(context.Background(), sqlc.UpdateMessageStatusParams{
				ID:     msgID,
				Status: sqlc.QueueStatusEnumSUCCESS,
			})
		})

		// Assert
		require.NoError(t, err)
		rowsAffected, _ := result.RowsAffected()
		assert.Equal(t, int64(1), rowsAffected)
		assert.Equal(t, sqlc.QueueStatusEnumSUCCESS, queryQueueStatus(t, conn, msgID))
	})

	t.Run("retryable failure sets PENDING when under max retries", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupMarkStatusTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")
		batchUUID := "mark-status-retry-1"
		seedBatchLog(t, conn, batchUUID, user.DatabaseID, 1)
		msgID := seedProcessingMessage(t, conn, batchUUID, device.DatabaseID)

		// Act — same query as markQueueMessageStatus with retryable error
		result, err := db2.WithTransaction(context.Background(), conn, func(q *sqlc.Queries) (sql.Result, error) {
			return q.UpdateMessageAfterFailure(context.Background(), sqlc.UpdateMessageAfterFailureParams{
				ID:         msgID,
				RetryCount: 5, // MaxFailureRetries = 5, retry_count starts at 0
				ErrorInfo:  sql.NullString{String: "temporary failure", Valid: true},
			})
		})

		// Assert
		require.NoError(t, err)
		rowsAffected, _ := result.RowsAffected()
		assert.Equal(t, int64(1), rowsAffected)
		assert.Equal(t, sqlc.QueueStatusEnumPENDING, queryQueueStatus(t, conn, msgID))
	})

	t.Run("permanent failure sets FAILED", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupMarkStatusTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")
		batchUUID := "mark-status-perm-1"
		seedBatchLog(t, conn, batchUUID, user.DatabaseID, 1)
		msgID := seedProcessingMessage(t, conn, batchUUID, device.DatabaseID)

		// Act — same query as markQueueMessageStatus with unimplemented error
		result, err := db2.WithTransaction(context.Background(), conn, func(q *sqlc.Queries) (sql.Result, error) {
			return q.UpdateMessagePermanentlyFailed(context.Background(), sqlc.UpdateMessagePermanentlyFailedParams{
				ID:        msgID,
				ErrorInfo: sql.NullString{String: "not supported", Valid: true},
			})
		})

		// Assert
		require.NoError(t, err)
		rowsAffected, _ := result.RowsAffected()
		assert.Equal(t, int64(1), rowsAffected)
		assert.Equal(t, sqlc.QueueStatusEnumFAILED, queryQueueStatus(t, conn, msgID))
	})

	t.Run("stale message returns zero rows affected", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupMarkStatusTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")
		batchUUID := "mark-status-stale-1"
		seedBatchLog(t, conn, batchUUID, user.DatabaseID, 1)

		// Create message directly as FAILED (simulating reaper)
		ctx := context.Background()
		err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
			return q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
				CommandBatchLogUuid: batchUUID,
				CommandType:         "REBOOT",
				DeviceID:            device.DatabaseID,
				Status:              sqlc.QueueStatusEnumFAILED,
				RetryCount:          0,
				Payload:             pqtype.NullRawMessage{Valid: false},
			})
		})
		require.NoError(t, err)

		var msgID int64
		err = conn.QueryRowContext(ctx,
			"SELECT id FROM queue_message WHERE command_batch_log_uuid = $1 AND device_id = $2",
			batchUUID, device.DatabaseID).Scan(&msgID)
		require.NoError(t, err)

		// Act — try SUCCESS on already-FAILED message (WHERE status = 'PROCESSING' won't match)
		result, err := db2.WithTransaction(ctx, conn, func(q *sqlc.Queries) (sql.Result, error) {
			return q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{
				ID:     msgID,
				Status: sqlc.QueueStatusEnumSUCCESS,
			})
		})

		// Assert
		require.NoError(t, err)
		rowsAffected, _ := result.RowsAffected()
		assert.Equal(t, int64(0), rowsAffected, "stale message should not be updated")
		assert.Equal(t, sqlc.QueueStatusEnumFAILED, queryQueueStatus(t, conn, msgID),
			"FAILED status should not be overwritten")
	})
}

// TestAtomicQueueStatusAndDeviceLog verifies that queue status and device log
// are committed together in a single transaction, and that stale messages
// produce neither a status change nor a device log entry.
func TestAtomicQueueStatusAndDeviceLog(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	t.Run("both queue status and device log committed together", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupMarkStatusTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")
		batchUUID := "atomic-both-1"
		seedBatchLog(t, conn, batchUUID, user.DatabaseID, 1)
		msgID := seedProcessingMessage(t, conn, batchUUID, device.DatabaseID)

		// Act — single transaction: mark SUCCESS + write device log
		err := db2.WithTransactionNoResult(context.Background(), conn, func(q *sqlc.Queries) error {
			result, err := q.UpdateMessageStatus(context.Background(), sqlc.UpdateMessageStatusParams{
				ID:     msgID,
				Status: sqlc.QueueStatusEnumSUCCESS,
			})
			if err != nil {
				return err
			}
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				return nil // stale — skip device log
			}
			return q.UpsertCommandOnDeviceLog(context.Background(), sqlc.UpsertCommandOnDeviceLogParams{
				Uuid:      batchUUID,
				DeviceID:  device.DatabaseID,
				Status:    sqlc.DeviceCommandStatusEnumSUCCESS,
				UpdatedAt: time.Now(),
			})
		})

		// Assert
		require.NoError(t, err)
		assert.Equal(t, sqlc.QueueStatusEnumSUCCESS, queryQueueStatus(t, conn, msgID))
		assert.True(t, queryDeviceLogExists(t, conn, batchUUID, device.DatabaseID))
	})

	t.Run("stale message skips both queue status and device log", func(t *testing.T) {
		// Arrange
		conn, dbService, user := setupMarkStatusTest(t)
		device := dbService.CreateDevice(user.OrganizationID, "proto")
		batchUUID := "atomic-stale-1"
		seedBatchLog(t, conn, batchUUID, user.DatabaseID, 1)

		// Seed as FAILED (simulating reaper)
		ctx := context.Background()
		err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
			return q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
				CommandBatchLogUuid: batchUUID,
				CommandType:         "REBOOT",
				DeviceID:            device.DatabaseID,
				Status:              sqlc.QueueStatusEnumFAILED,
				RetryCount:          0,
				Payload:             pqtype.NullRawMessage{Valid: false},
			})
		})
		require.NoError(t, err)

		var msgID int64
		err = conn.QueryRowContext(ctx,
			"SELECT id FROM queue_message WHERE command_batch_log_uuid = $1 AND device_id = $2",
			batchUUID, device.DatabaseID).Scan(&msgID)
		require.NoError(t, err)

		// Act — same atomic transaction pattern; stale detection skips device log
		err = db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
			result, err := q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{
				ID:     msgID,
				Status: sqlc.QueueStatusEnumSUCCESS,
			})
			if err != nil {
				return err
			}
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				return nil // stale — skip device log
			}
			return q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
				Uuid:      batchUUID,
				DeviceID:  device.DatabaseID,
				Status:    sqlc.DeviceCommandStatusEnumSUCCESS,
				UpdatedAt: time.Now(),
			})
		})

		// Assert
		require.NoError(t, err)
		assert.Equal(t, sqlc.QueueStatusEnumFAILED, queryQueueStatus(t, conn, msgID))
		assert.False(t, queryDeviceLogExists(t, conn, batchUUID, device.DatabaseID),
			"no device log should be written for stale message")
	})
}
