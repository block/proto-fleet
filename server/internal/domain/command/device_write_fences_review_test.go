package command

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	minerDomain "github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type siteMoveCommandSender struct {
	commandTestFleetNodeSender
	calls int
}

func (s *siteMoveCommandSender) SendCommand(ctx context.Context, nodeID int64, version gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	s.calls++
	return s.commandTestFleetNodeSender.SendCommand(ctx, nodeID, version, cmd)
}

func TestNewQueuedCommandAfterSiteMoveResolvesCurrentBinding(t *testing.T) {
	for _, route := range []string{"direct", "remote"} {
		t.Run(route, func(t *testing.T) {
			_, conn, crypto, deviceID, identifier := setupPasswordCommandDevice(t, "antminer", true)
			q := sqlc.New(conn)
			before, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: 1, Name: "Before", Slug: "before"})
			require.NoError(t, err)
			after, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: 1, Name: "After", Slug: "after"})
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", before.ID, deviceID)
			require.NoError(t, err)
			filesService, err := files.NewService(files.Config{})
			require.NoError(t, err)
			ctrl := gomock.NewController(t)
			driver := sdkMocks.NewMockDriver(ctrl)
			device := sdkMocks.NewMockDevice(ctrl)
			physicalCalls := 0
			if route == "direct" {
				driver.EXPECT().NewDevice(gomock.Any(), identifier, gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).MinTimes(1).MaxTimes(2)
				device.EXPECT().StopMining(gomock.Any()).DoAndReturn(func(context.Context) error {
					physicalCalls++
					return nil
				}).AnyTimes()
			}
			minerService := minerDomain.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, filesService, &commandTestPluginManager{driverName: "antminer", driver: driver})
			sender := &siteMoveCommandSender{}
			if route == "remote" {
				var nodeID int64
				require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
 VALUES(1,'site-move-node',$1,$2,'CONFIRMED') RETURNING id`, []byte("site-move-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
				_, err = conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
				require.NoError(t, err)
				minerService.WithCommandSender(sender)
			}
			captured, err := minerService.GetMiner(t.Context(), deviceID)
			require.NoError(t, err)
			require.Equal(t, before.ID, captured.GetSiteID())
			_, err = conn.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", after.ID, deviceID)
			require.NoError(t, err)
			message, messages := enqueueFenceTestCommand(t, conn, deviceID, commandtype.StopMining, nil)
			executor := NewExecutionService(&Config{MaxWorkers: 1}, conn, messages, crypto, nil, minerService, sqlstores.NewSQLDeviceStore(conn), nil, filesService)
			executor.workerProcessCommand(t.Context(), message)
			rows, err := q.GetQueueMessagesByBatch(t.Context(), message.BatchLogUUID)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, sqlc.QueueStatusEnumSUCCESS, rows[0].Status, "a command submitted after reassignment must use the current binding")
			var result string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT result.status FROM command_on_device_log result
 JOIN command_batch_log batch ON batch.id=result.command_batch_log_id
 WHERE batch.uuid=$1 AND result.device_id=$2`, message.BatchLogUUID, deviceID).Scan(&result))
			require.Equal(t, "SUCCESS", result)
			require.Error(t, captured.StopMining(t.Context()), "the originally captured handle must remain stale")
			if route == "direct" {
				require.Equal(t, 1, physicalCalls)
			} else {
				require.Equal(t, 1, sender.calls)
			}
			next, err := messages.Dequeue(t.Context(), 1)
			require.NoError(t, err)
			require.Empty(t, next, "successful physical dispatch must not be replayed")
		})
	}
}

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
				getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(nil, errors.New("credentials unavailable"))
			} else {
				getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).DoAndReturn(func(context.Context, int64) (interfaces.Miner, error) {
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
	getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(firmwareStatusMiner{Miner: miner, FirmwareUpdateStatusProvider: provider}, nil)
	miner.EXPECT().GetOrgID().Return(int64(1))
	miner.EXPECT().GetSiteID().Return(int64(0))
	miner.EXPECT().FirmwareUpdate(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ sdk.FirmwareFile) error {
		return devicewrite.Dispatch(ctx, func(context.Context) error { return nil })
	})
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
