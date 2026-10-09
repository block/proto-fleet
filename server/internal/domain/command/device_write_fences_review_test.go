package command

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func enqueueFenceTestCommand(t *testing.T, conn *sql.DB, deviceID int64, kind commandtype.Type, payload any) (queue.Message, *queue.DatabaseMessageQueue) {
	t.Helper()
	q := sqlc.New(conn)
	userID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: id.GenerateID(), Username: "write-fence-test", PasswordHash: "unused", CreatedAt: time.Now()})
	require.NoError(t, err)
	batch := id.GenerateID()
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: userID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: 1, Valid: true}})
	require.NoError(t, err)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, conn)
	require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{deviceID}, payload))
	rows, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0], messages
}

func TestQueuedSetupFailureDoesNotPoisonDeviceDrain(t *testing.T) {
	for _, failure := range []string{"miner resolution", "invalid payload", "reservation before dispatch"} {
		t.Run(failure, func(t *testing.T) {
			_, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
			kind := commandtype.StopMining
			var payload any
			if failure == "invalid payload" {
				kind = commandtype.SetCoolingMode
				payload = "not an object"
			}
			message, messages := enqueueFenceTestCommand(t, conn, deviceID, kind, payload)
			fences := devicewrite.NewService(conn)
			var epoch int64
			ctrl := gomock.NewController(t)
			getter := minerMocks.NewMockCachedMinerGetter(ctrl)
			miner := minerIfaceMocks.NewMockMiner(ctrl)
			if failure == "miner resolution" {
				getter.EXPECT().GetMiner(gomock.Any(), deviceID).Return(nil, errors.New("credentials unavailable"))
			} else {
				getter.EXPECT().GetMiner(gomock.Any(), deviceID).DoAndReturn(func(context.Context, int64) (interfaces.Miner, error) {
					if failure == "reservation before dispatch" {
						var err error
						epoch, err = fences.Reserve(t.Context(), 1, deviceID)
						require.NoError(t, err)
					}
					return miner, nil
				})
				miner.EXPECT().GetOrgID().Return(int64(1))
				miner.EXPECT().GetSiteID().Return(int64(0))
			}
			service := NewExecutionService(&Config{MaxWorkers: 1}, conn, messages, nil, nil, getter, nil, nil, nil)
			_, _, err := service.executeQueuedCommand(t.Context(), message)
			require.Error(t, err)
			if epoch == 0 {
				epoch, err = fences.Reserve(t.Context(), 1, deviceID)
				require.NoError(t, err)
			}
			drained, err := fences.Drained(t.Context(), 1, deviceID, epoch)
			require.NoError(t, err)
			require.True(t, drained, "no physical call was invoked")
			require.NoError(t, fences.ReleaseHost(t.Context(), 1, deviceID, epoch))
		})
	}
}

func TestQueuedFirmwareFollowOnCannotCrossOwnershipReservation(t *testing.T) {
	_, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	t.Chdir(t.TempDir())
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware image"), files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "1.2.3"})
	require.NoError(t, err)
	message, messages := enqueueFenceTestCommand(t, conn, deviceID, commandtype.FirmwareUpdate, dto.FirmwareUpdatePayload{FirmwareFileID: fileID})
	fences := devicewrite.NewService(conn)
	q := sqlc.New(conn)
	ctrl := gomock.NewController(t)
	getter := minerMocks.NewMockCachedMinerGetter(ctrl)
	miner := minerIfaceMocks.NewMockMiner(ctrl)
	provider := minerIfaceMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
	deviceStore := storeMocks.NewMockDeviceStore(ctrl)
	getter.EXPECT().GetMiner(gomock.Any(), deviceID).Return(firmwareStatusMiner{Miner: miner, FirmwareUpdateStatusProvider: provider}, nil)
	miner.EXPECT().GetOrgID().Return(int64(1))
	miner.EXPECT().GetSiteID().Return(int64(0))
	miner.EXPECT().FirmwareUpdate(gomock.Any(), gomock.Any()).Return(nil)
	var epoch int64
	provider.EXPECT().GetFirmwareUpdateStatus(gomock.Any()).DoAndReturn(func(context.Context) (*sdk.FirmwareUpdateStatus, error) {
		epoch, err = fences.Reserve(t.Context(), 1, deviceID)
		require.NoError(t, err)
		drained, err := fences.Drained(t.Context(), 1, deviceID, epoch)
		require.NoError(t, err)
		require.False(t, drained, "the queued attempt spans install polling")
		_, err = q.UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: message.ID, Status: sqlc.QueueStatusEnumFAILED})
		require.NoError(t, err)
		drained, err = fences.Drained(t.Context(), 1, deviceID, epoch)
		require.NoError(t, err)
		require.False(t, drained, "reaping is not physical drain")
		return &sdk.FirmwareUpdateStatus{State: "installed"}, nil
	})
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), tmodels.DeviceIdentifier(identifier), models.MinerStatusUpdating, "").Return(nil)
	deviceStore.EXPECT().UpsertDeviceStatus(gomock.Any(), tmodels.DeviceIdentifier(identifier), models.MinerStatusRebootRequired, "").Return(nil)
	service := NewExecutionService(&Config{MaxWorkers: 1}, conn, messages, nil, nil, getter, deviceStore, nil, filesService)
	_, _, err = service.executeQueuedCommand(t.Context(), message)
	require.Error(t, err)
	drained, err := fences.Drained(t.Context(), 1, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained, "a dispatched upload followed by a fenced reboot retains uncertain outcome")
	require.Error(t, fences.ReleaseHost(t.Context(), 1, deviceID, epoch))
}
