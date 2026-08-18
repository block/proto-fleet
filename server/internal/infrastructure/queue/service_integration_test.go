package queue_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestEnqueueCommandBatchInheritsConfiguredMaxAttempts(t *testing.T) {
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
		Identifier:  "channel-queue-default-attempts",
		CommandType: commandtype.Reboot,
		CreatedBy:   user.DatabaseID,
		OrgID:       user.OrganizationID,
		LogPayload:  []byte(`{}`),
		Messages: []queue.EnqueueMessage{{
			DeviceID: device.DatabaseID,
			Payload:  map[string]string{},
		}},
	})
	require.NoError(t, err)

	var maxAttempts int32
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		`SELECT max_attempts
		 FROM queue_message
		 WHERE command_batch_log_uuid = $1`,
		"channel-queue-default-attempts",
	).Scan(&maxAttempts))
	assert.Equal(t, int32(5), maxAttempts)
}

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

func TestDatabaseMessageQueueEnqueueManyInsertsPerDevicePayloads(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()
	firstDevice := dbService.CreateDevice(user.OrganizationID, "proto")
	secondDevice := dbService.CreateDevice(user.OrganizationID, "proto")
	batchUUID := id.GenerateID()
	commandType := commandtype.UpdateMiningPools
	_, err = sqlc.New(dbService.DB).CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{
		Uuid:           batchUUID,
		Type:           commandType.String(),
		CreatedBy:      user.DatabaseID,
		CreatedAt:      time.Now(),
		Status:         sqlc.BatchStatusEnumPENDING,
		DevicesCount:   2,
		Payload:        pqtype.NullRawMessage{},
		OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true},
	})
	require.NoError(t, err)
	messageQueue := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 5}, dbService.DB)
	messages := []queue.EnqueueMessage{
		{DeviceID: firstDevice.DatabaseID, Payload: map[string]string{"worker_name": "first"}},
		{DeviceID: secondDevice.DatabaseID, Payload: map[string]string{"worker_name": "second"}},
	}

	// Act
	err = messageQueue.EnqueueMany(t.Context(), batchUUID, commandType, messages)

	// Assert
	require.NoError(t, err)
	rows, err := sqlc.New(dbService.DB).GetQueueMessagesByBatch(t.Context(), batchUUID)
	require.NoError(t, err)
	gotPayloads := make(map[int64]map[string]string)
	for _, row := range rows {
		var decoded map[string]string
		require.NoError(t, json.Unmarshal(row.Payload.RawMessage, &decoded))
		gotPayloads[row.DeviceID] = decoded
		assert.Equal(t, sqlc.QueueStatusEnumPENDING, row.Status)
	}
	assert.Equal(t, map[int64]map[string]string{
		firstDevice.DatabaseID:  {"worker_name": "first"},
		secondDevice.DatabaseID: {"worker_name": "second"},
	}, gotPayloads)
}
