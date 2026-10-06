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
	messageQueue := queue.NewDatabaseMessageQueue(&queue.Config{}, dbService.DB)
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
