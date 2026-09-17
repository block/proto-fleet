package command

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

func TestFirmwareCompletionRetriesDatabaseWithoutRepeatingDeviceCommand(t *testing.T) {
	svc, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	ctx := t.Context()
	t.Chdir(t.TempDir())
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware image"), files.FirmwareMetadata{
		TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0",
	})
	require.NoError(t, err)
	payload, err := json.Marshal(dto.FirmwareUpdatePayload{FirmwareFileID: fileID})
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO "user" (id, user_id, username, password_hash)
		VALUES (42, 'firmware-worker', 'firmware-worker', 'unused');
		CREATE SEQUENCE firmware_completion_attempt;
		CREATE FUNCTION deadlock_first_firmware_completion() RETURNS trigger AS $$
		BEGIN
			IF nextval('firmware_completion_attempt') = 1 THEN
				RAISE EXCEPTION 'injected completion deadlock' USING ERRCODE = '40P01';
			END IF;
			RETURN NEW;
		END; $$ LANGUAGE plpgsql;
		CREATE TRIGGER deadlock_first_firmware_completion BEFORE INSERT ON device_firmware_deployment
		FOR EACH ROW EXECUTE FUNCTION deadlock_first_firmware_completion()`)
	require.NoError(t, err)
	q := sqlc.New(conn)
	const batch = "firmware-completion-retry"
	commandType := commandtype.FirmwareUpdate
	_, err = q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
		Uuid: batch, Type: commandType.String(), CreatedBy: 42, CreatedAt: time.Now(),
		Status: sqlc.BatchStatusEnumPROCESSING, DevicesCount: 1,
		OrganizationID: sql.NullInt64{Int64: 1, Valid: true},
		Payload:        pqtype.NullRawMessage{RawMessage: payload, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
		CommandBatchLogUuid: batch, CommandType: commandType.String(), DeviceID: deviceID,
		Status: sqlc.QueueStatusEnumPROCESSING, Payload: pqtype.NullRawMessage{RawMessage: payload, Valid: true},
	}))
	var messageID int64
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM queue_message WHERE command_batch_log_uuid = $1`, batch).Scan(&messageID))

	ctrl := gomock.NewController(t)
	miner := minerIfaceMocks.NewMockMiner(ctrl)
	minerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	deviceStore := storeMocks.NewMockDeviceStore(ctrl)
	minerGetter.EXPECT().GetMiner(gomock.Any(), deviceID).Return(miner, nil).Times(1)
	miner.EXPECT().GetOrgID().Return(int64(1)).Times(1)
	miner.EXPECT().GetSiteID().Return(int64(0)).Times(1)
	miner.EXPECT().FirmwareUpdate(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	miner.EXPECT().Reboot(gomock.Any()).Return(nil).Times(1)
	miner.EXPECT().GetID().Return(models.DeviceIdentifier(identifier)).Times(1)
	deviceStore.EXPECT().GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []tmodels.DeviceIdentifier{tmodels.DeviceIdentifier(identifier)}).
		Return(map[tmodels.DeviceIdentifier]models.MinerStatus{}, nil).Times(1)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), tmodels.DeviceIdentifier(identifier), models.MinerStatusRebootRequired, "").Return(nil).Times(1)
	svc.minerService = minerGetter
	svc.deviceStore = deviceStore
	svc.filesService = filesService
	svc.workerProcessCommand(ctx, queue.Message{
		ID: messageID, DeviceID: deviceID, BatchLogUUID: batch, CommandType: commandtype.FirmwareUpdate,
		Payload: payload, OrgID: 1,
	})

	var attempts int
	var queueStatus sqlc.QueueStatusEnum
	var resultStatus sqlc.DeviceCommandStatusEnum
	var checksum, witness string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT last_value FROM firmware_completion_attempt`).Scan(&attempts))
	require.Equal(t, 2, attempts, "the completion SQL must retry after the deadlock")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM queue_message WHERE id = $1`, messageID).Scan(&queueStatus))
	require.Equal(t, sqlc.QueueStatusEnumSUCCESS, queueStatus)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM command_on_device_log WHERE device_id = $1`, deviceID).Scan(&resultStatus))
	require.Equal(t, sqlc.DeviceCommandStatusEnumSUCCESS, resultStatus)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT firmware_checksum, last_command_batch_uuid FROM device_firmware_deployment WHERE device_id = $1`, deviceID).Scan(&checksum, &witness))
	require.Empty(t, checksum)
	require.Equal(t, batch, witness)
}
