package queue_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestEnqueueCommandBatchPersistsPerMessageAttemptCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db, service, user := setupQueueTest(t)
	device := service.CreateDevice(user.OrganizationID, "proto")
	messageQueue := queue.NewDatabaseMessageQueue(
		&queue.Config{MaxFailureRetries: 5},
		db,
	)

	err := messageQueue.EnqueueCommandBatch(t.Context(), queue.CommandBatch{
		Identifier:  "channel-queue-one-attempt",
		CommandType: commandtype.FirmwareUpdate,
		CreatedBy:   user.DatabaseID,
		OrgID:       user.OrganizationID,
		LogPayload:  []byte(`{"firmware_file_id":"firmware-1"}`),
		Messages: []queue.EnqueueMessage{{
			DeviceID: device.DatabaseID,
			Payload:  map[string]string{"firmware_file_id": "firmware-1"},
		}},
		MaxAttempts: 1,
	})
	require.NoError(t, err)

	var maxAttempts int32
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		`SELECT max_attempts
		 FROM queue_message
		 WHERE command_batch_log_uuid = $1`,
		"channel-queue-one-attempt",
	).Scan(&maxAttempts))
	assert.Equal(t, int32(1), maxAttempts)
}

func TestEnqueueCommandBatchFailureRollsBackBatchAndQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db, service, user := setupQueueTest(t)
	device := service.CreateDevice(user.OrganizationID, "proto")
	messageQueue := queue.NewDatabaseMessageQueue(
		&queue.Config{MaxFailureRetries: 5},
		db,
	)

	err := messageQueue.EnqueueCommandBatch(t.Context(), queue.CommandBatch{
		Identifier:  "channel-queue-rollback",
		CommandType: commandtype.FirmwareUpdate,
		CreatedBy:   user.DatabaseID,
		OrgID:       user.OrganizationID,
		LogPayload:  []byte(`{"firmware_file_id":"firmware-1"}`),
		Messages: []queue.EnqueueMessage{
			{
				DeviceID: device.DatabaseID,
				Payload:  map[string]string{"firmware_file_id": "firmware-1"},
			},
			{
				DeviceID: -1,
				Payload:  map[string]string{"firmware_file_id": "firmware-1"},
			},
		},
		MaxAttempts: 1,
	})
	require.Error(t, err)

	var batchCount, queueCount int
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM command_batch_log WHERE uuid = $1",
		"channel-queue-rollback",
	).Scan(&batchCount))
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM queue_message WHERE command_batch_log_uuid = $1",
		"channel-queue-rollback",
	).Scan(&queueCount))
	assert.Zero(t, batchCount)
	assert.Zero(t, queueCount)
}

func setupQueueTest(
	t *testing.T,
) (*sql.DB, *testutil.DatabaseService, *testutil.TestUser) {
	t.Helper()
	config, err := testutil.GetTestConfig()
	require.NoError(t, err)
	service := testutil.NewDatabaseService(t, config)
	user := service.CreateSuperAdminUser()
	return service.DB, service, user
}
