package command

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
)

func TestQueuedAdapterPanicRetainsPhysicalWriteAttempt(t *testing.T) {
	service, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
	message, _ := enqueueFenceTestCommand(t, conn, deviceID, commandtype.StopMining, nil)
	ctrl := gomock.NewController(t)
	miner := minerIfaceMocks.NewMockMiner(ctrl)
	getter := minerMocks.NewMockCachedMinerGetter(ctrl)
	getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(miner, nil)
	miner.EXPECT().GetOrgID().Return(int64(1))
	miner.EXPECT().GetSiteID().Return(int64(0))
	miner.EXPECT().StopMining(gomock.Any()).DoAndReturn(func(context.Context) error {
		panic("adapter panicked after transport dispatch")
	})
	service.minerService = getter
	require.PanicsWithValue(t, "adapter panicked after transport dispatch", func() {
		_, _, _ = service.executeQueuedCommand(t.Context(), message)
	})
	var status string
	var finishedAt sql.NullTime
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT status, finished_at FROM device_write_attempt WHERE queue_message_id = $1`, message.ID).Scan(&status, &finishedAt))
	require.Equal(t, "running", status)
	require.False(t, finishedAt.Valid)
	writes := devicewrite.NewService(conn)
	epoch, err := writes.Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	drained, err := writes.Drained(t.Context(), 1, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained, "stack unwinding is not evidence that the physical operation completed")
	require.Error(t, writes.ReleaseHost(t.Context(), 1, deviceID, epoch))
}
