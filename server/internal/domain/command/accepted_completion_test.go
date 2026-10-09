package command

import (
	"context"
	"testing"

	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerDomain "github.com/block/proto-fleet/server/internal/domain/miner"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAcceptedRebootSucceedsWithoutReleasingPhysicalFence(t *testing.T) {
	service, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
	service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	message, messages := enqueueFenceTestCommand(t, conn, deviceID, commandtype.Reboot, nil)
	ctrl := gomock.NewController(t)
	getter := minerMocks.NewMockCachedMinerGetter(ctrl)
	miner := minerIfaceMocks.NewMockMiner(ctrl)
	getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(miner, nil)
	miner.EXPECT().GetOrgID().Return(int64(1))
	miner.EXPECT().GetSiteID().Return(int64(0))
	// This represents the existing error-only SDK's successful HTTP 202 response.
	miner.EXPECT().Reboot(gomock.Any()).Return(nil).Times(1)
	service.minerService = getter
	service.messageQueue = messages
	service.workerProcessCommand(t.Context(), message)
	var queueStatus, attemptStatus, fenceState string
	var retries int
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status,retry_count FROM queue_message WHERE id=$1", message.ID).Scan(&queueStatus, &retries))
	require.Equal(t, "SUCCESS", queueStatus)
	require.Zero(t, retries, "acceptance must not cause another reboot")
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE queue_message_id=$1", message.ID).Scan(&attemptStatus))
	require.Equal(t, "unknown", attemptStatus)
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT state FROM device_write_fence WHERE device_id=$1", deviceID).Scan(&fenceState))
	require.Equal(t, "host", fenceState)
	next, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, next)
	_, err = devicewrite.NewService(conn).Reserve(t.Context(), 1, deviceID)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestAcceptedFirmwareFollowOnRebootCannotProveDrain(t *testing.T) {
	service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	message, _ := enqueueFenceTestCommand(t, conn, deviceID, commandtype.FirmwareUpdate, nil)
	fences := devicewrite.NewService(conn)
	attempt, err := fences.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	miner := minerIfaceMocks.NewMockMiner(ctrl)
	miner.EXPECT().Reboot(gomock.Any()).Return(nil).Times(1)
	miner.EXPECT().GetID().Return(models.DeviceIdentifier(identifier))
	require.NoError(t, service.rebootAfterFirmwareInstall(attempt.Context(t.Context()), miner, deviceID))
	require.NoError(t, attempt.Finish(t.Context(), nil))
	inspections, err := fences.InspectRecovery(t.Context(), 1, deviceID)
	require.NoError(t, err)
	require.Len(t, inspections, 1)
	require.Equal(t, "unknown", inspections[0].Attempt.Status)
	_, err = fences.Reserve(t.Context(), 1, deviceID)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestAcceptedQueuedRebootSurvivesBindingCommitFailure(t *testing.T) {
	service, conn, crypto, deviceID, identifier := setupPasswordCommandDevice(t, "antminer", true)
	message, messages := enqueueFenceTestCommand(t, conn, deviceID, commandtype.Reboot, nil)
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	driver := sdkMocks.NewMockDriver(ctrl)
	device := sdkMocks.NewMockDevice(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), identifier, gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).Times(1)
	device.EXPECT().Reboot(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		// Cut the real lock transaction connection after the SDK accepted its
		// reboot, so the enclosing Commit fails after external dispatch.
		var terminated bool
		err := conn.QueryRowContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
			WHERE datname=current_database() AND state='idle in transaction'
			AND query LIKE '%CheckDeviceWriteAttempt%'`).Scan(&terminated)
		require.NoError(t, err)
		require.True(t, terminated)
		return nil
	}).Times(1)
	service.minerService = minerDomain.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, filesService,
		&commandTestPluginManager{driverName: "antminer", driver: driver})
	service.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	service.messageQueue = messages
	service.workerProcessCommand(t.Context(), message)
	var queueStatus, attemptStatus string
	var retries int
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status,retry_count FROM queue_message WHERE id=$1", message.ID).Scan(&queueStatus, &retries))
	require.Equal(t, "SUCCESS", queueStatus)
	require.Zero(t, retries)
	next, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, next, "failure to release binding locks must not enqueue a second reboot")
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE queue_message_id=$1", message.ID).Scan(&attemptStatus))
	require.Equal(t, "unknown", attemptStatus, "transaction cleanup cannot prove physical completion")
	_, err = devicewrite.NewService(conn).Reserve(t.Context(), 1, deviceID)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "accepted reboot must still block ownership transfer")
}
