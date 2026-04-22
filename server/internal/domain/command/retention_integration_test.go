package command_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Integration coverage for command.RetentionCleaner. Exercises the full
// paginated drain with a real Postgres connection, so the SQL statements are
// validated against the live schema (sqlc does not type-check these at build
// time).

func setupRetentionTest(t *testing.T) (*sql.DB, *testutil.DatabaseService, *testutil.TestUser) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()
	return dbService.DB, dbService, user
}

func seedFinishedBatch(t *testing.T, conn *sql.DB, batchUUID string, userID int64, deviceCount int32, finishedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		if _, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:         batchUUID,
			Type:         "REBOOT",
			CreatedBy:    userID,
			CreatedAt:    finishedAt,
			Status:       sqlc.BatchStatusEnumFINISHED,
			DevicesCount: deviceCount,
			Payload:      pqtype.NullRawMessage{Valid: false},
		}); err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)

	// Backdate created_at, finished_at, and status via direct UPDATE because the
	// sqlc insert only accepts created_at and never finished_at.
	_, err = conn.ExecContext(context.Background(),
		`UPDATE command_batch_log
		 SET finished_at = $1, created_at = $2
		 WHERE uuid = $3`,
		finishedAt, finishedAt, batchUUID)
	require.NoError(t, err)
}

func seedTerminalQueueMessage(t *testing.T, conn *sql.DB, batchUUID string, deviceID int64, status sqlc.QueueStatusEnum, updatedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		return q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
			CommandBatchLogUuid: batchUUID,
			CommandType:         "REBOOT",
			DeviceID:            deviceID,
			Status:              status,
			RetryCount:          0,
			Payload:             pqtype.NullRawMessage{Valid: false},
		})
	})
	require.NoError(t, err)

	// queue_message has an updated_at trigger so we disable it just for the
	// backdate write and restore afterwards.
	_, err = conn.ExecContext(ctx, "ALTER TABLE queue_message DISABLE TRIGGER update_queue_message_updated_at")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx,
		`UPDATE queue_message SET updated_at = $1
		 WHERE command_batch_log_uuid = $2 AND device_id = $3`,
		updatedAt, batchUUID, deviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "ALTER TABLE queue_message ENABLE TRIGGER update_queue_message_updated_at")
	require.NoError(t, err)
}

func seedDeviceLog(t *testing.T, conn *sql.DB, batchUUID string, deviceID int64, status sqlc.DeviceCommandStatusEnum, updatedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		return q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
			Uuid:      batchUUID,
			DeviceID:  deviceID,
			Status:    status,
			UpdatedAt: updatedAt,
		})
	})
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, "ALTER TABLE command_on_device_log DISABLE TRIGGER update_command_on_device_log_updated_at")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx,
		`UPDATE command_on_device_log codl
		 SET updated_at = $1
		 FROM command_batch_log cbl
		 WHERE codl.command_batch_log_id = cbl.id
		   AND cbl.uuid = $2
		   AND codl.device_id = $3`,
		updatedAt, batchUUID, deviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "ALTER TABLE command_on_device_log ENABLE TRIGGER update_command_on_device_log_updated_at")
	require.NoError(t, err)
}

func countWhere(t *testing.T, conn *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	err := conn.QueryRowContext(context.Background(), query, args...).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestRetentionCleaner_PrunesOldRows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	conn, dbService, user := setupRetentionTest(t)
	dev := dbService.CreateDevice(user.OrganizationID, "proto")

	oldUUID := "retention-old-1"
	recentUUID := "retention-recent-1"

	oldTime := time.Now().Add(-400 * 24 * time.Hour)
	recentTime := time.Now()

	seedFinishedBatch(t, conn, oldUUID, user.DatabaseID, 1, oldTime)
	seedFinishedBatch(t, conn, recentUUID, user.DatabaseID, 1, recentTime)

	seedTerminalQueueMessage(t, conn, oldUUID, dev.DatabaseID, sqlc.QueueStatusEnumSUCCESS, oldTime)
	seedTerminalQueueMessage(t, conn, recentUUID, dev.DatabaseID, sqlc.QueueStatusEnumSUCCESS, recentTime)

	seedDeviceLog(t, conn, oldUUID, dev.DatabaseID, sqlc.DeviceCommandStatusEnumSUCCESS, oldTime)
	seedDeviceLog(t, conn, recentUUID, dev.DatabaseID, sqlc.DeviceCommandStatusEnumSUCCESS, recentTime)

	// Tight retentions so both "old" rows fall outside the window and "recent"
	// rows stay safely inside.
	cfg := &command.RetentionConfig{
		QueueMessageRetention: 24 * time.Hour,
		DeviceLogRetention:    24 * time.Hour,
		BatchLogRetention:     24 * time.Hour,
		CleanupInterval:       time.Hour,
		DeleteBatchLimit:      50,
	}
	cleaner := command.NewRetentionCleaner(conn, cfg)
	err := cleaner.RunOnceForTest(context.Background())
	require.NoError(t, err)

	assert.Zero(t, countWhere(t, conn,
		`SELECT COUNT(*) FROM queue_message WHERE command_batch_log_uuid = $1`, oldUUID),
		"old queue_message rows should be pruned")
	assert.Equal(t, 1, countWhere(t, conn,
		`SELECT COUNT(*) FROM queue_message WHERE command_batch_log_uuid = $1`, recentUUID),
		"recent queue_message rows should be kept")

	assert.Zero(t, countWhere(t, conn,
		`SELECT COUNT(*) FROM command_on_device_log codl
		 JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
		 WHERE cbl.uuid = $1`, oldUUID),
		"old command_on_device_log rows should be pruned")
	assert.Equal(t, 1, countWhere(t, conn,
		`SELECT COUNT(*) FROM command_on_device_log codl
		 JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
		 WHERE cbl.uuid = $1`, recentUUID),
		"recent command_on_device_log rows should be kept")

	assert.Zero(t, countWhere(t, conn,
		`SELECT COUNT(*) FROM command_batch_log WHERE uuid = $1`, oldUUID),
		"old command_batch_log header should be pruned once children are gone")
	assert.Equal(t, 1, countWhere(t, conn,
		`SELECT COUNT(*) FROM command_batch_log WHERE uuid = $1`, recentUUID),
		"recent command_batch_log header should be kept")
}
