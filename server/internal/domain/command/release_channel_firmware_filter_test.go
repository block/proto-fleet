package command

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

func TestReleaseChannelFirmwareFilterReusesAdmissionTransaction(t *testing.T) {
	_, conn, _, _, identifier := setupPasswordCommandDevice(t, "proto", false)
	_, err := conn.ExecContext(t.Context(), `INSERT INTO "user" (id, user_id, username, password_hash) VALUES (42, 'user-1', 'test-user', 'unused')`)
	require.NoError(t, err)
	var channelID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO release_channel (org_id, name, created_by) VALUES (1, 'Stable', 42) RETURNING id`).Scan(&channelID))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO release_channel_target (channel_id, target_type, device_identifier) VALUES ($1, 'miner', $2)`, channelID, identifier)
	require.NoError(t, err)

	// The migration harness retains a reserved connection. Leave only one
	// additional connection for admission; a nested transaction would wait on
	// itself and could not see its uncommitted assignment.
	conn.SetMaxOpenConns(conn.Stats().InUse + 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = sqlstores.NewSQLTransactor(conn).RunInTxNoRetry(ctx, func(ctx context.Context) error {
		q := db.GetTxQueries(ctx)
		_, err := q.UpsertReleaseChannelFirmware(ctx, sqlc.UpsertReleaseChannelFirmwareParams{
			ChannelID: channelID, Manufacturer: "TestCorp", Model: "TestMiner",
			FirmwareChecksum: "assigned", AssignedBy: 42,
		})
		if err != nil {
			return err
		}
		out, err := NewReleaseChannelFirmwareFilter(conn).Apply(ctx, CommandFilterInput{
			CommandType: commandtype.FirmwareUpdate, OrganizationID: 1, DeviceIdentifiers: []string{identifier},
		})
		if err != nil {
			return err
		}
		require.Empty(t, out.Kept)
		require.Len(t, out.Skipped, 1)
		require.Contains(t, out.Skipped[0].Reason, "Stable")
		return nil
	})
	require.NoError(t, err)
}

func TestReleaseChannelFirmwareFilterPreservesNormalMinerActions(t *testing.T) {
	filter := NewReleaseChannelFirmwareFilter(nil)
	for _, input := range []CommandFilterInput{
		{CommandType: commandtype.Reboot, DeviceIdentifiers: []string{"miner"}},
		{CommandType: commandtype.FirmwareUpdate, Actor: session.ActorRolloutEnforcement, DeviceIdentifiers: []string{"miner"}},
		{CommandType: commandtype.FirmwareUpdate},
	} {
		out, err := filter.Apply(t.Context(), input)
		require.NoError(t, err)
		require.Equal(t, input.DeviceIdentifiers, out.Kept)
		require.Empty(t, out.Skipped)
	}
}

func TestManualFirmwareUpdateRequiresChangingManagedAssignment(t *testing.T) {
	_, conn, _, _, identifier := setupPasswordCommandDevice(t, "proto", false)
	ctx := manualSessionCtx(1)
	_, err := conn.ExecContext(ctx, `INSERT INTO "user" (id, user_id, username, password_hash) VALUES (42, 'user-1', 'test-user', 'unused')`)
	require.NoError(t, err)
	var channelID int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO release_channel (org_id, name, created_by) VALUES (1, 'Stable', 42) RETURNING id`).Scan(&channelID))
	_, err = conn.ExecContext(ctx, `INSERT INTO release_channel_target (channel_id, target_type, device_identifier) VALUES ($1, 'miner', $2)`, channelID, identifier)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO release_channel_firmware (channel_id, manufacturer, model, firmware_checksum, assigned_by) VALUES ($1, ' testcorp ', 'TESTMINER', 'assigned', 42)`, channelID)
	require.NoError(t, err)

	filter := NewReleaseChannelFirmwareFilter(conn)
	out, err := filter.Apply(ctx, CommandFilterInput{CommandType: commandtype.FirmwareUpdate, OrganizationID: 1, DeviceIdentifiers: []string{identifier, "unmanaged"}})
	require.NoError(t, err)
	require.Equal(t, []string{"unmanaged"}, out.Kept)
	require.Len(t, out.Skipped, 1)
	require.Contains(t, out.Skipped[0].Reason, "Stable")
	foreign, err := filter.Apply(ctx, CommandFilterInput{CommandType: commandtype.FirmwareUpdate, OrganizationID: 2, DeviceIdentifiers: []string{identifier}})
	require.NoError(t, err)
	require.Empty(t, foreign.Skipped, "ownership must be scoped to the caller's org")

	t.Chdir(t.TempDir())
	fs, err := files.NewService(files.Config{})
	require.NoError(t, err)
	fileID, err := fs.SaveFirmwareFile("update.swu", strings.NewReader("manual update"), files.FirmwareMetadata{
		TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0",
	})
	require.NoError(t, err)
	run := newExecutionRun(t.Context())
	t.Cleanup(run.cancelAdmission)
	t.Cleanup(run.cancelWork)
	svc := &Service{
		config: &Config{}, conn: conn, executionService: &ExecutionService{run: run},
		filesService: fs, deviceStore: sqlstores.NewSQLDeviceStore(conn),
		messageQueue:                     queue.NewDatabaseMessageQueue(&queue.Config{}, conn),
		startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {},
	}
	svc.RegisterFilter(filter)
	_, err = svc.FirmwareUpdate(ctx, includeSelector(identifier), fileID)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "managed firmware must fail before enqueue: %v", err)
	require.Contains(t, err.Error(), "Release channels")
	var commands int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&commands))
	require.Zero(t, commands)

	_, err = conn.ExecContext(ctx, `UPDATE release_channel_firmware SET firmware_checksum = '' WHERE channel_id = $1`, channelID)
	require.NoError(t, err)
	observedEnqueue := false
	svc.messageQueue = &firmwareAdmissionObservingQueue{MessageQueue: svc.messageQueue, afterEnqueue: func() {
		observedEnqueue = true
		var available bool
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('release_channel_scope:1', 0))`).Scan(&available))
		require.False(t, available, "assignment writers must remain blocked until admission commits")
		var queued int
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&queued))
		require.Zero(t, queued, "the queue insert must share the admission transaction")
	}}
	result, err := svc.FirmwareUpdate(ctx, includeSelector(identifier), fileID)
	require.NoError(t, err, "normal manual updates work once the assignment is cleared")
	require.Equal(t, 1, result.DispatchedCount)
	require.True(t, observedEnqueue)
	var available bool
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('release_channel_scope:1', 0))`).Scan(&available))
	require.True(t, available, "admission must release the scope lock after commit")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&commands))
	require.Equal(t, 1, commands)
}

func TestFirmwareUpdateSelectorsWithOneConnection(t *testing.T) {
	for _, selectorKind := range []string{"explicit", "all devices", "matching filter"} {
		t.Run(selectorKind, func(t *testing.T) {
			_, conn, _, _, identifier := setupPasswordCommandDevice(t, "proto", false)
			_, err := conn.ExecContext(t.Context(), `INSERT INTO "user" (id, user_id, username, password_hash) VALUES (42, 'user-1', 'test-user', 'unused')`)
			require.NoError(t, err)
			var channelID int64
			require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO release_channel (org_id, name, created_by) VALUES (1, 'Stable', 42) RETURNING id`).Scan(&channelID))
			_, err = conn.ExecContext(t.Context(), `INSERT INTO release_channel_target (channel_id, target_type, device_identifier) VALUES ($1, 'miner', $2)`, channelID, identifier)
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), `INSERT INTO release_channel_firmware (channel_id, manufacturer, model, firmware_checksum, assigned_by) VALUES ($1, 'TestCorp', 'TestMiner', 'assigned', 42)`, channelID)
			require.NoError(t, err)

			t.Chdir(t.TempDir())
			fs, err := files.NewService(files.Config{})
			require.NoError(t, err)
			fileID, err := fs.SaveFirmwareFile("update.swu", strings.NewReader("manual update"), files.FirmwareMetadata{
				TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0",
			})
			require.NoError(t, err)
			deviceStore := sqlstores.NewSQLDeviceStore(conn)
			run := newExecutionRun(t.Context())
			t.Cleanup(run.cancelAdmission)
			t.Cleanup(run.cancelWork)
			svc := &Service{
				config: &Config{}, conn: conn, executionService: &ExecutionService{run: run},
				filesService: fs, deviceStore: deviceStore,
				deviceResolver:                   fleetmanagement.NewService(deviceStore, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
				activitySvc:                      activity.NewService(sqlstores.NewSQLActivityStore(conn)),
				messageQueue:                     queue.NewDatabaseMessageQueue(&queue.Config{}, conn),
				startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {},
			}
			svc.RegisterFilter(NewScheduleConflictFilter(nil))
			svc.RegisterFilter(NewCurtailmentActiveFilter(sqlstores.NewSQLCurtailmentStore(conn)))
			svc.RegisterFilter(NewReleaseChannelFirmwareFilter(conn))
			selector := includeSelector(identifier)
			switch selectorKind {
			case "all devices":
				selector = &pb.DeviceSelector{SelectionType: &pb.DeviceSelector_AllDevices{AllDevices: &pb.DeviceFilter{Models: []string{"TestMiner"}}}}
			case "matching filter":
				selector = &pb.DeviceSelector{SelectionType: &pb.DeviceSelector_AllMatchingFilter{AllMatchingFilter: &fleetpb.MinerListFilter{Models: []string{"TestMiner"}}}}
			}

			// Keep the harness's migration reservation plus one usable connection.
			conn.SetMaxOpenConns(conn.Stats().InUse + 1)
			ctx, cancel := context.WithTimeout(manualSessionCtx(1), 5*time.Second)
			defer cancel()
			_, err = svc.FirmwareUpdate(ctx, selector, fileID)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "managed firmware should be rejected without waiting for another connection: %v", err)
			var audits, commands int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM activity_log WHERE event_type = 'command_preflight_blocked'`).Scan(&audits))
			require.Equal(t, 1, audits, "rejection must leave a durable audit after admission rolls back")
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&commands))
			require.Zero(t, commands)

			_, err = conn.ExecContext(ctx, `UPDATE release_channel_firmware SET firmware_checksum = '' WHERE channel_id = $1`, channelID)
			require.NoError(t, err)
			result, err := svc.FirmwareUpdate(ctx, selector, fileID)
			require.NoError(t, err, "unassigned miners should update with any supported selector")
			require.Equal(t, 1, result.DispatchedCount)
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&commands))
			require.Equal(t, 1, commands)
		})
	}
}

type firmwareAdmissionObservingQueue struct {
	queue.MessageQueue
	afterEnqueue func()
}

func (q *firmwareAdmissionObservingQueue) Enqueue(ctx context.Context, batchID string, commandType commandtype.Type, deviceIDs []int64, payload interface{}) error {
	if err := q.MessageQueue.Enqueue(ctx, batchID, commandType, deviceIDs, payload); err != nil {
		return err
	}
	q.afterEnqueue()
	return nil
}
