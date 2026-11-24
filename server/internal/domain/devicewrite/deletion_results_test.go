package devicewrite_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/stretchr/testify/require"
)

func TestDeletionRecordsCanceledCommandResults(t *testing.T) {
	database, orgID, deviceID, processing, messages := queuedFenceFixture(t)
	q := sqlc.New(database.DB)
	otherDevice := database.CreateDevice(orgID, "proto")
	var actorID int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT created_by FROM command_batch_log WHERE uuid=$1", processing.BatchLogUUID).Scan(&actorID))
	enqueue := func(kind commandtype.Type, deviceIDs []int64, deviceCount int32) string {
		require.Equal(t, len(deviceIDs), int(deviceCount))
		batch := id.GenerateID()
		_, err := q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: actorID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: deviceCount, OrganizationID: sql.NullInt64{Int64: orgID, Valid: true}})
		require.NoError(t, err)
		require.NoError(t, messages.Enqueue(t.Context(), batch, kind, deviceIDs, nil))
		return batch
	}
	pendingBatches := []string{
		enqueue(commandtype.Reboot, []int64{deviceID, otherDevice.DatabaseID}, 2),
		enqueue(commandtype.StopMining, []int64{deviceID}, 1),
		enqueue(commandtype.FirmwareUpdate, []int64{deviceID}, 1),
	}
	// Retries preserve the first terminal snapshot while replacing the failure reason.
	require.NoError(t, q.UpsertCommandOnDeviceLog(t.Context(), sqlc.UpsertCommandOnDeviceLogParams{Uuid: pendingBatches[0], DeviceID: deviceID, Status: sqlc.DeviceCommandStatusEnumFAILED, UpdatedAt: time.Now(), ErrorInfo: sql.NullString{String: "previous failure", Valid: true}}))
	priorResults, err := q.ListBatchDeviceResults(t.Context(), sqlc.ListBatchDeviceResultsParams{Uuid: pendingBatches[0], MaxRows: 10})
	require.NoError(t, err)
	require.Len(t, priorResults, 1)
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET custom_name='name after first result' WHERE id=$1", deviceID)
	require.NoError(t, err)
	// Duplicate pending messages still produce one result for a device in a batch.
	kind := commandtype.StopMining
	require.NoError(t, q.CreateQueueMessage(t.Context(), sqlc.CreateQueueMessageParams{CommandBatchLogUuid: pendingBatches[1], CommandType: kind.String(), DeviceID: deviceID, Status: sqlc.QueueStatusEnumPENDING}))
	var installedAt time.Time
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `INSERT INTO device_firmware_deployment
		(device_id, firmware_checksum, firmware_version, deployed_at, last_command_batch_uuid)
		VALUES ($1, 'installed-checksum', '1.2.3', now(), 'installed-batch') RETURNING deployed_at`, deviceID).Scan(&installedAt))
	// An already terminal message keeps its result when another command is canceled.
	terminalBatch := id.GenerateID()
	kind = commandtype.Reboot
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: terminalBatch, Type: kind.String(), CreatedBy: actorID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumFINISHED, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: orgID, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, q.CreateQueueMessage(t.Context(), sqlc.CreateQueueMessageParams{CommandBatchLogUuid: terminalBatch, CommandType: kind.String(), DeviceID: deviceID, Status: sqlc.QueueStatusEnumSUCCESS}))
	require.NoError(t, q.UpsertCommandOnDeviceLog(t.Context(), sqlc.UpsertCommandOnDeviceLogParams{Uuid: terminalBatch, DeviceID: deviceID, Status: sqlc.DeviceCommandStatusEnumSUCCESS, UpdatedAt: time.Now()}))

	identifier, err := q.GetDeviceIdentifierByID(t.Context(), deviceID)
	require.NoError(t, err)
	deleted, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{identifier}, orgID)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	const reason = "device command canceled by configuration handover"
	for _, batch := range []string{pendingBatches[1], pendingBatches[2], pendingBatches[0]} {
		rows, err := q.GetQueueMessagesByBatch(t.Context(), batch)
		require.NoError(t, err)
		for _, row := range rows {
			if row.DeviceID == deviceID {
				require.Equal(t, sqlc.QueueStatusEnumFAILED, row.Status)
				require.Equal(t, sql.NullString{String: reason, Valid: true}, row.ErrorInfo)
			} else {
				require.Equal(t, sqlc.QueueStatusEnumPENDING, row.Status)
			}
		}
		counts, err := q.GetBatchStatusAndDeviceCounts(t.Context(), batch)
		require.NoError(t, err)
		require.Equal(t, int64(1), counts.FailedDevices)
		require.Zero(t, counts.SuccessfulDevices)
		results, err := q.ListBatchDeviceResults(t.Context(), sqlc.ListBatchDeviceResultsParams{Uuid: batch, MaxRows: 10})
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, sqlc.DeviceCommandStatusEnumFAILED, results[0].Status)
		require.Equal(t, sql.NullString{String: reason, Valid: true}, results[0].ErrorInfo)
		require.True(t, results[0].DeviceIdentifier.Valid)
		require.True(t, results[0].Manufacturer.Valid, "cancellation records the device snapshot")
		if batch == pendingBatches[0] {
			require.Equal(t, priorResults[0].CustomName, results[0].CustomName, "a retry keeps its first terminal snapshot")
		} else {
			require.Equal(t, sql.NullString{String: "name after first result", Valid: true}, results[0].CustomName)
		}
	}
	processingRows, err := q.GetQueueMessagesByBatch(t.Context(), processing.BatchLogUUID)
	require.NoError(t, err)
	require.Len(t, processingRows, 1)
	require.Equal(t, sqlc.QueueStatusEnumPROCESSING, processingRows[0].Status)
	results, err := q.ListBatchDeviceResults(t.Context(), sqlc.ListBatchDeviceResultsParams{Uuid: processing.BatchLogUUID, MaxRows: 10})
	require.NoError(t, err)
	require.Empty(t, results, "deletion cannot publish a result for a processing command")
	counts, err := q.GetBatchStatusAndDeviceCounts(t.Context(), terminalBatch)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.SuccessfulDevices)
	require.Zero(t, counts.FailedDevices)
	var checksum, version, witness string
	var deployedAt time.Time
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT firmware_checksum, firmware_version, deployed_at, last_command_batch_uuid
		FROM device_firmware_deployment WHERE device_id=$1`, deviceID).Scan(&checksum, &version, &deployedAt, &witness))
	require.Equal(t, "installed-checksum", checksum, "a canceled update never changed the installed firmware")
	require.Equal(t, "1.2.3", version)
	require.Equal(t, "installed-batch", witness)
	require.True(t, installedAt.Equal(deployedAt), "cancellation must not advance the firmware completion witness")
}

func TestDeletionRollsBackWhenCanceledResultFails(t *testing.T) {
	for _, throughStore := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone statement", true: "whole deletion transaction"}[throughStore], func(t *testing.T) {
			database, orgID, deviceID, message, _ := queuedFenceFixture(t)
			q := sqlc.New(database.DB)
			_, err := q.UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: message.ID, Status: sqlc.QueueStatusEnumPENDING})
			require.NoError(t, err)
			identifier, err := q.GetDeviceIdentifierByID(t.Context(), deviceID)
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), `
    CREATE FUNCTION reject_deletion_command_result() RETURNS trigger AS $$
    BEGIN RAISE EXCEPTION 'injected deletion result failure'; END;
    $$ LANGUAGE plpgsql;
    CREATE TRIGGER reject_deletion_command_result BEFORE INSERT ON command_on_device_log
    FOR EACH ROW EXECUTE FUNCTION reject_deletion_command_result();`)
			require.NoError(t, err)
			if throughStore {
				_, err = sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{identifier}, orgID)
			} else {
				err = q.CancelPendingDeviceWritesForDeletion(t.Context(), []int64{deviceID})
			}
			require.ErrorContains(t, err, "injected deletion result failure")
			fence, err := q.LockDeviceWriteFenceRecord(t.Context(), sqlc.LockDeviceWriteFenceRecordParams{DeviceID: deviceID, OrganizationID: orgID})
			require.NoError(t, err)
			require.Zero(t, fence.Epoch)
			require.Equal(t, "host", fence.State)
			rows, err := q.GetQueueMessagesByBatch(t.Context(), message.BatchLogUUID)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, sqlc.QueueStatusEnumPENDING, rows[0].Status)
			require.False(t, rows[0].ErrorInfo.Valid)
			results, err := q.ListBatchDeviceResults(t.Context(), sqlc.ListBatchDeviceResultsParams{Uuid: message.BatchLogUUID, MaxRows: 10})
			require.NoError(t, err)
			require.Empty(t, results)
			var unchanged bool
			require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT d.deleted_at IS NULL AND dd.deleted_at IS NULL
    FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id WHERE d.id=$1`, deviceID).Scan(&unchanged))
			require.True(t, unchanged, "failed result persistence must roll back both device and discovery retirement")
		})
	}
}
