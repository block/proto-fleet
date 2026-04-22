package command_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// setupRetentionTest builds a DB service with a superadmin user for
// integration tests that need Postgres. Name retained from the retention
// test suite the helpers originated in.
func setupRetentionTest(t *testing.T) (*sql.DB, *testutil.DatabaseService, *testutil.TestUser) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()
	return dbService.DB, dbService, user
}

// seedDeviceLog inserts a command_on_device_log row and backdates updated_at
// so tests can simulate historical rows. The trigger on the table has to be
// disabled for the UPDATE because updated_at is trigger-managed.
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
