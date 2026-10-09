package command

import (
	"testing"

	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
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
	getter.EXPECT().GetMiner(gomock.Any(), deviceID).Return(miner, nil)
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
