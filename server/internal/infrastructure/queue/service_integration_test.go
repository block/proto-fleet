package queue_test

import (
	"context"
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

func TestEnqueueCannotPassConcurrentMiningGroupDeactivation(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
	batch := id.GenerateID()
	kind := commandtype.UpdateMiningPools
	_, err := q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tx, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = sqlc.New(tx).LockDeviceWriteFences(ctx, []int64{device.DatabaseID})
	require.NoError(t, err)
	var pid int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	done := make(chan error, 1)
	go func() {
		done <- queue.NewDatabaseMessageQueue(&queue.Config{}, database.DB).Enqueue(ctx, batch, commandtype.UpdateMiningPools, []int64{device.DatabaseID}, map[string]string{"worker": "next"})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	_, err = tx.ExecContext(ctx, `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate','pending')`, device.DatabaseID, user.OrganizationID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.ErrorContains(t, <-done, "being disabled")
	rows, err := q.GetQueueMessagesByBatch(ctx, batch)
	require.NoError(t, err)
	require.Empty(t, rows, "the entire queued mutation is rejected")
}

func TestEnqueueChecksDeactivationAcrossSelectedBatch(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	var ids []int64
	for range 3 {
		ids = append(ids, database.CreateDevice(user.OrganizationID, "proto").DatabaseID)
	}
	q := sqlc.New(database.DB)
	require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), ids))
	_, err := database.DB.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate','blocked')`, ids[1], user.OrganizationID)
	require.NoError(t, err)
	batch := id.GenerateID()
	kind := commandtype.UpdateMiningPools
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 2, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
	require.NoError(t, err)
	service := queue.NewDatabaseMessageQueue(&queue.Config{}, database.DB)
	require.ErrorContains(t, service.Enqueue(t.Context(), batch, commandtype.UpdateMiningPools, ids[:2], map[string]string{"worker": "next"}), "being disabled")
	rows, err := q.GetQueueMessagesByBatch(t.Context(), batch)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = database.DB.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, ids[1])
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate','blocked')`, ids[2], user.OrganizationID)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), batch, commandtype.UpdateMiningPools, ids[:2], map[string]string{"worker": "next"}), "an unselected controller must not reject the batch")
	rows, err = q.GetQueueMessagesByBatch(t.Context(), batch)
	require.NoError(t, err)
	var enqueued []int64
	for _, row := range rows {
		enqueued = append(enqueued, row.DeviceID)
	}
	require.ElementsMatch(t, ids[:2], enqueued)
}

func TestEnqueueDeactivationOnlyBlocksPoolAffectingCommands(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	first := database.CreateDevice(user.OrganizationID, "proto")
	second := database.CreateDevice(user.OrganizationID, "proto")
	ids := []int64{first.DatabaseID, second.DatabaseID}
	q := sqlc.New(database.DB)
	service := queue.NewDatabaseMessageQueue(&queue.Config{}, database.DB)
	for _, status := range []string{"pending", "applying", "blocked"} {
		_, err := database.DB.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate',$3) ON CONFLICT (device_id) DO UPDATE SET status=EXCLUDED.status`, second.DatabaseID, user.OrganizationID, status)
		require.NoError(t, err)
		for _, tc := range []struct {
			kind    commandtype.Type
			blocked bool
		}{
			{commandtype.BlinkLED, false}, {commandtype.DownloadLogs, false},
			{commandtype.SetCoolingMode, false}, {commandtype.SetPowerTarget, false},
			{commandtype.SetBoardIdentify, false},
			{commandtype.Curtail, false}, {commandtype.Uncurtail, false},
			{commandtype.ApplyCurtailmentConfig, false},
			{commandtype.UpdateMiningPools, true}, {commandtype.Reboot, true},
		} {
			t.Run(status+"/"+tc.kind.String(), func(t *testing.T) {
				batch := id.GenerateID()
				_, err := q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: tc.kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 2, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
				require.NoError(t, err)
				err = service.Enqueue(t.Context(), batch, tc.kind, ids, nil)
				if tc.blocked {
					require.ErrorContains(t, err, "being disabled")
				} else {
					require.NoError(t, err)
				}
				rows, err := q.GetQueueMessagesByBatch(t.Context(), batch)
				require.NoError(t, err)
				if tc.blocked {
					require.Empty(t, rows, "no commands in the selected batch may pass")
				} else {
					require.Len(t, rows, len(ids))
				}
			})
		}
	}
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

func TestSameTimestampDeviceCommandsDispatchInStableOrder(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	batch := id.GenerateID()
	kind := commandtype.Reboot
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 2, OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true}})
	require.NoError(t, err)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, database.DB)
	// Both inserts share statement/transaction time. The ID breaks that tie.
	require.NoError(t, messages.EnqueueMany(t.Context(), batch, kind, []queue.EnqueueMessage{{DeviceID: device.DatabaseID, Payload: map[string]int{"order": 1}}, {DeviceID: device.DatabaseID, Payload: map[string]int{"order": 2}}}))
	first, err := messages.Dequeue(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, first, 1)
	blocked, err := messages.Dequeue(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, blocked)
	_, err = q.UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: first[0].ID, Status: sqlc.QueueStatusEnumSUCCESS})
	require.NoError(t, err)
	second, err := messages.Dequeue(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Greater(t, second[0].ID, first[0].ID)
}
