package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/generated/sqlc"
	commandMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	queueMocks "github.com/block/proto-fleet/server/internal/infrastructure/queue/mocks"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestFirmwareProgressWindowRestartsOnlyForHigherProgress(t *testing.T) {
	advanced := 0
	window := newFirmwareProgressWindow(t.Context(), time.Hour, func() { advanced++ })
	defer window.stop()
	for _, tc := range []struct {
		progress *int
		want     int
	}{
		{nil, 0},
		{new(-1), 0},
		{new(0), 1},
		{new(0), 1},
		{new(40), 2},
		{new(39), 2},
		{new(40), 2},
		{new(101), 2},
		{new(100), 3},
	} {
		window.advance(tc.progress)
		require.Equal(t, tc.want, advanced)
	}
	var none *firmwareProgressWindow
	none.advance(new(50))
	require.NoError(t, window.ctx.Err())
}

func TestFirmwareProgressWindowEndsWithoutProgress(t *testing.T) {
	const timeout = 150 * time.Millisecond
	advanced := 0
	window := newFirmwareProgressWindow(t.Context(), timeout, func() { advanced++ })
	defer window.stop()

	time.Sleep(timeout * 2 / 3)
	window.advance(new(10))
	time.Sleep(timeout * 2 / 3)
	require.NoError(t, window.ctx.Err(), "progress must restart the window")

	select {
	case <-window.ctx.Done():
	case <-time.After(10 * timeout):
		t.Fatal("a window without further progress must end")
	}
	require.ErrorIs(t, context.Cause(window.ctx), context.DeadlineExceeded)
	window.advance(new(20))
	require.Equal(t, 1, advanced, "progress cannot reopen a window that ended")
}

func TestFirmwareProgressWindowContextOutlastsTheWorkerTimeout(t *testing.T) {
	type key struct{}
	window := newFirmwareProgressWindow(t.Context(), time.Hour, nil)
	worker, cancelWorker := context.WithCancel(context.WithValue(withFirmwareProgressWindow(t.Context(), window), key{}, "attempt"))
	install, stop := firmwareProgressWindowFrom(worker).context(worker)
	defer stop()

	cancelWorker()
	require.NoError(t, install.Err(), "the worker's own timeout must not end install polling")
	require.Equal(t, "attempt", install.Value(key{}), "the device write attempt travels with the context")
	require.Same(t, window, firmwareProgressWindowFrom(install))

	window.cancel(context.DeadlineExceeded)
	<-install.Done()
	require.ErrorIs(t, context.Cause(install), context.DeadlineExceeded)

	// Without a window the worker's context bounds the command, as before.
	plain, stopPlain := firmwareProgressWindowFrom(t.Context()).context(t.Context())
	defer stopPlain()
	require.Equal(t, t.Context(), plain)
}

// firmwareWorkerTest queues one firmware update for a device whose driver
// reports install status.
type firmwareWorkerTest struct {
	svc       *ExecutionService
	conn      *sql.DB
	message   queue.Message
	lastTouch func() time.Duration
}

func newFirmwareWorkerTest(t *testing.T) *firmwareWorkerTest {
	t.Helper()
	svc, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
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
	_, err = conn.ExecContext(ctx, `INSERT INTO "user" (id, user_id, username, password_hash) VALUES (42, 'firmware-worker', 'firmware-worker', 'unused')`)
	require.NoError(t, err)
	q := sqlc.New(conn)
	const batch = "firmware-progress-window"
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
	f := &firmwareWorkerTest{
		svc: svc, conn: conn,
		message: queue.Message{ID: messageID, DeviceID: deviceID, BatchLogUUID: batch, CommandType: commandtype.FirmwareUpdate, Payload: payload, OrgID: 1},
		lastTouch: func() time.Duration {
			var seconds float64
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT extract(epoch FROM now() - updated_at) FROM queue_message WHERE id = $1`, messageID).Scan(&seconds))
			return time.Duration(seconds * float64(time.Second))
		},
	}
	// The message was claimed long ago, so only a progress report keeps the reaper away.
	f.backdated(t, `UPDATE queue_message SET updated_at = now() - interval '30 minutes' WHERE id = $1`)
	svc.deviceStore = sqlstores.NewSQLDeviceStore(conn)
	svc.filesService = filesService
	return f
}

// backdated runs an update without the trigger that stamps updated_at.
func (f *firmwareWorkerTest) backdated(t *testing.T, update string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `ALTER TABLE queue_message DISABLE TRIGGER update_queue_message_updated_at`)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(t.Context(), update, f.message.ID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(t.Context(), `ALTER TABLE queue_message ENABLE TRIGGER update_queue_message_updated_at`)
	require.NoError(t, err)
}

// expectUpload has the device accept the firmware once and then answer
// install status polls from status.
func (f *firmwareWorkerTest) expectUpload(t *testing.T, status func() *sdk.FirmwareUpdateStatus) {
	t.Helper()
	ctrl := gomock.NewController(t)
	miner := minerMocks.NewMockMiner(ctrl)
	provider := minerMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
	minerGetter := commandMocks.NewMockCachedMinerGetter(ctrl)
	minerGetter.EXPECT().GetMinerForCommand(gomock.Any(), f.message.DeviceID).Return(firmwareStatusReportingMiner{Miner: miner, FirmwareUpdateStatusProvider: provider}, nil)
	miner.EXPECT().GetOrgID().Return(int64(1))
	miner.EXPECT().GetSiteID().Return(int64(0))
	miner.EXPECT().FirmwareUpdate(gomock.Any(), gomock.Any()).Return(nil)
	provider.EXPECT().GetFirmwareUpdateStatus(gomock.Any()).DoAndReturn(func(context.Context) (*sdk.FirmwareUpdateStatus, error) {
		return status(), nil
	}).AnyTimes()
	messages := queueMocks.NewMockMessageQueue(ctrl)
	messages.EXPECT().MaxFailureRetries().Return(int32(5)).AnyTimes()
	f.svc.minerService = minerGetter
	f.svc.messageQueue = messages
	interval := firmwareInstallPollInterval
	firmwareInstallPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { firmwareInstallPollInterval = interval })
}

func (f *firmwareWorkerTest) status(t *testing.T) (sqlc.QueueStatusEnum, int32) {
	t.Helper()
	var status sqlc.QueueStatusEnum
	var retries int32
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT status, retry_count FROM queue_message WHERE id = $1`, f.message.ID).Scan(&status, &retries))
	return status, retries
}

func TestFirmwareCommandStaysOpenWhileTheDeviceReportsProgress(t *testing.T) {
	f := newFirmwareWorkerTest(t)
	f.svc.config.FirmwareUpdateTimeout = 150 * time.Millisecond
	f.svc.config.FirmwareUpdateStuckTimeout = 20 * time.Minute
	const reports = 100
	var polls atomic.Int32
	f.expectUpload(t, func() *sdk.FirmwareUpdateStatus {
		poll := int(polls.Add(1))
		if poll > reports {
			return &sdk.FirmwareUpdateStatus{State: "completed"}
		}
		if poll == reports/2 {
			// Midway, well past the fixed timeout, the reaper finds a message
			// that reported progress moments ago and leaves it alone.
			require.Less(t, f.lastTouch(), time.Minute)
			reaped, err := f.svc.reapMessages(t.Context(), reapModeStuck)
			require.NoError(t, err)
			require.Empty(t, reaped)
		}
		return &sdk.FirmwareUpdateStatus{State: "installing", Progress: new(poll)}
	})

	started := time.Now()
	f.svc.processWithinTimeout(t.Context(), f.message)

	require.Greater(t, time.Since(started), 2*f.svc.config.FirmwareUpdateTimeout, "the update ran well past the fixed timeout")
	require.Equal(t, int32(reports+1), polls.Load())
	status, retries := f.status(t)
	require.Equal(t, sqlc.QueueStatusEnumSUCCESS, status)
	require.Zero(t, retries)
}

func TestFirmwareCommandTimesOutWhenProgressStops(t *testing.T) {
	f := newFirmwareWorkerTest(t)
	f.svc.config.FirmwareUpdateTimeout = 150 * time.Millisecond
	var polls atomic.Int32
	f.expectUpload(t, func() *sdk.FirmwareUpdateStatus {
		// Progress advances for a while, then the device keeps reporting the
		// same percentage.
		return &sdk.FirmwareUpdateStatus{State: "installing", Progress: new(min(int(polls.Add(1)), 40))}
	})

	started := time.Now()
	f.svc.processWithinTimeout(t.Context(), f.message)

	elapsed := time.Since(started)
	require.Greater(t, elapsed, f.svc.config.FirmwareUpdateTimeout+40*firmwareInstallPollInterval, "the window restarted while progress advanced")
	require.Less(t, elapsed, 10*f.svc.config.FirmwareUpdateTimeout, "the command must end once progress stops")
	status, retries := f.status(t)
	require.Equal(t, sqlc.QueueStatusEnumPENDING, status, "a polling timeout is retried, as before")
	require.Equal(t, int32(1), retries)
	var reason string
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT error_info FROM queue_message WHERE id = $1`, f.message.ID).Scan(&reason))
	require.Contains(t, reason, "firmware install polling timed out")
}

func TestTouchProcessingMessageLeavesSettledMessagesAlone(t *testing.T) {
	f := newFirmwareWorkerTest(t)
	f.svc.touchProcessingMessage(t.Context(), f.message.ID)
	require.Less(t, f.lastTouch(), time.Minute, "a message still being processed records the progress report")

	f.backdated(t, `UPDATE queue_message SET status = 'FAILED', updated_at = now() - interval '30 minutes' WHERE id = $1`)
	f.svc.touchProcessingMessage(t.Context(), f.message.ID)

	require.Greater(t, f.lastTouch(), 29*time.Minute, "a reaped or finished message keeps the time it settled")
}
