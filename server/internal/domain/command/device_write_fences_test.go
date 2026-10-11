package command

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

func TestQueuedDispatchCannotCrossOwnershipEpoch(t *testing.T) {
	_, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
	q := sqlc.New(conn)
	userID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: id.GenerateID(), Username: "write-fence-actor", PasswordHash: "unused", CreatedAt: time.Now()})
	require.NoError(t, err)
	const organizationID int64 = 1
	batch := id.GenerateID()
	kind := commandtype.StopMining
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: userID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: organizationID, Valid: true}})
	require.NoError(t, err)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, conn)
	require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{deviceID}, nil))
	rows, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	ctrl := gomock.NewController(t)
	getter := minerMocks.NewMockCachedMinerGetter(ctrl)
	miner := minerIfaceMocks.NewMockMiner(ctrl)
	started := make(chan struct{})
	complete := make(chan struct{})
	finish := sync.OnceFunc(func() { close(complete) })
	t.Cleanup(finish)
	getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(miner, nil).Times(1)
	miner.EXPECT().GetOrgID().Return(organizationID)
	miner.EXPECT().GetSiteID().Return(int64(0))
	miner.EXPECT().StopMining(gomock.Any()).DoAndReturn(func(context.Context) error { close(started); <-complete; return nil }).Times(1)
	service := NewExecutionService(&Config{MaxWorkers: 1}, conn, messages, nil, nil, getter, nil, nil, nil)
	done := make(chan error, 1)
	go func() { _, _, err := service.executeQueuedCommand(t.Context(), rows[0]); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not start")
	}
	fences := devicewrite.NewService(conn)
	epoch, err := fences.Reserve(t.Context(), organizationID, deviceID)
	require.NoError(t, err)
	drained, err := fences.Drained(t.Context(), organizationID, deviceID, epoch)
	require.NoError(t, err)
	require.False(t, drained)
	finish()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not complete")
	}
	drained, err = fences.Drained(t.Context(), organizationID, deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained, "the ordered StopMining call has returned")
	require.NoError(t, fences.ReleaseHost(t.Context(), organizationID, deviceID, epoch))
	// No second miner resolution or firmware call is permitted for this old row.
	_, _, err = service.executeQueuedCommand(t.Context(), rows[0])
	require.Error(t, err)
}

func TestPostInstallFenceDatabaseErrorPermanentlyFailsFirmwareCommand(t *testing.T) {
	_, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
	q := sqlc.New(conn)
	userID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: id.GenerateID(), Username: "firmware-fence-actor", PasswordHash: "unused", CreatedAt: time.Now()})
	require.NoError(t, err)
	batch := id.GenerateID()
	kind := commandtype.FirmwareUpdate
	_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: userID, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: 1, Valid: true}})
	require.NoError(t, err)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, conn)
	require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{deviceID}, nil))
	rows, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	attempt, err := devicewrite.NewService(conn).BeginQueuedHost(t.Context(), rows[0])
	require.NoError(t, err)
	ctx := attempt.Context(t.Context())
	uploads := 0
	// The upload/install mutation has succeeded before the automatic reboot.
	require.NoError(t, devicewrite.Dispatch(ctx, func(context.Context) error { uploads++; return nil }))
	// Fault only this test's isolated real database, then restore it before queue completion.
	_, err = conn.ExecContext(t.Context(), "ALTER TABLE device_write_attempt RENAME TO unavailable_device_write_attempt")
	require.NoError(t, err)
	restore := sync.OnceFunc(func() {
		_, restoreErr := conn.ExecContext(context.Background(), "ALTER TABLE unavailable_device_write_attempt RENAME TO device_write_attempt")
		require.NoError(t, restoreErr)
	})
	t.Cleanup(restore)
	ctrl := gomock.NewController(t)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().Reboot(gomock.Any()).Times(0)
	service := &ExecutionService{conn: conn, messageQueue: messages}
	rebootErr := service.rebootAfterFirmwareInstall(ctx, mockMiner, deviceID)
	restore()
	require.Error(t, rebootErr)
	require.Contains(t, rebootErr.Error(), "check device write attempt")
	require.True(t, fleeterror.IsFailedPreconditionError(rebootErr), "post-install database error must not schedule another upload: %v", rebootErr)
	updated, terminal, err := service.markQueueMessageStatus(t.Context(), q, rows[0], rebootErr)
	require.NoError(t, err)
	require.True(t, updated)
	require.True(t, terminal)
	var status string
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT status FROM queue_message WHERE id=$1", rows[0].ID).Scan(&status))
	require.Equal(t, "FAILED", status)
	next, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, next, "completed installation must not be retried")
	require.Equal(t, 1, uploads)
}
