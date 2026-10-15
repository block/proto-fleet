package schedule

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/timestamppb"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	sitemodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type scheduleTestCapabilities struct{}

func (scheduleTestCapabilities) GetMinerCapabilitiesForDevice(context.Context, *pairingpb.Device) *capabilitiespb.MinerCapabilities {
	// Model capability discovery is unavailable. This must not exclude an
	// ordinary miner or turn a window restore into a successful empty batch.
	return nil
}

func TestProcessorDispatchMixedSiteSkipsUnsupportedContainers(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	ctx := t.Context()
	user := database.CreateSuperAdminUser()
	controller := database.CreateDevice(user.OrganizationID, "proto")
	miner := database.CreateDevice(user.OrganizationID, "antminer")
	_, err = database.DB.ExecContext(ctx, `UPDATE discovered_device SET device_class=2, model='CT1' WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED'),($2,'PAIRED')`, controller.DatabaseID, miner.DatabaseID)
	require.NoError(t, err)
	siteStore := sqlstores.NewSQLSiteStore(database.DB)
	site, err := siteStore.CreateSite(ctx, sitemodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "Mixed site"})
	require.NoError(t, err)
	_, err = siteStore.AssignDevicesToSite(ctx, user.OrganizationID, &site.ID, []string{miner.ID})
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE id=$2`, site.ID, controller.DatabaseID)
	require.NoError(t, err)

	deviceStore := sqlstores.NewSQLDeviceStore(database.DB)
	messageQueue := queue.NewDatabaseMessageQueue(&queue.Config{}, database.DB)
	// No execution slots: inspect durable admission without contacting miners.
	commandConfig := &command.Config{BatchStatusUpdatePollingInterval: 10 * time.Millisecond}
	execution := command.NewExecutionService(commandConfig, database.DB, messageQueue, nil, nil, nil, deviceStore, nil, nil)
	require.NoError(t, execution.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, execution.Stop(stopCtx))
	})
	commands := command.NewService(commandConfig, database.DB, execution, messageQueue,
		command.NewStatusService(database.DB, messageQueue), nil, nil, deviceStore, nil, nil, nil, scheduleTestCapabilities{}, nil)
	commands.RegisterFilter(command.NewScheduleContainerFilter(database.DB))
	processor := NewProcessor(nil, nil, nil, deviceStore, commands, nil)
	identifiers, err := processor.expandTargets(ctx, []*pb.ScheduleTarget{{
		TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE,
		TargetId:   strconv.FormatInt(site.ID, 10),
	}}, user.OrganizationID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{controller.ID, miner.ID}, identifiers)
	selector := &commandpb.DeviceSelector{SelectionType: &commandpb.DeviceSelector_IncludeDevices{
		IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: identifiers},
	}}

	for _, tc := range []struct {
		action pb.ScheduleAction
		kind   commandtype.Type
	}{
		{pb.ScheduleAction_SCHEDULE_ACTION_SLEEP, commandtype.StopMining},
		{pb.ScheduleAction_SCHEDULE_ACTION_REBOOT, commandtype.Reboot},
		{pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET, commandtype.SetPowerTarget},
	} {
		t.Run(tc.action.String(), func(t *testing.T) {
			sched := &pb.Schedule{Action: tc.action, CreatedBy: user.DatabaseID}
			result, err := processor.dispatch(schedulerContext(ctx, sched, user.OrganizationID), sched, selector)
			require.NoError(t, err)
			require.Equal(t, 1, result.DispatchedCount)
			require.Equal(t, []string{miner.ID}, result.DispatchedDeviceIdentifiers)
			require.Equal(t, []command.SkippedDevice{{
				DeviceIdentifier: controller.ID,
				FilterName:       command.ScheduleContainerFilterName,
				Reason:           "containers do not support generic " + tc.kind.String() + " commands",
			}}, result.Skipped)
			var count, deviceID int64
			var kind string
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*), min(device_id), min(command_type) FROM queue_message WHERE command_batch_log_uuid=$1`, result.BatchIdentifier).Scan(&count, &deviceID, &kind))
			require.EqualValues(t, 1, count)
			require.Equal(t, miner.DatabaseID, deviceID)
			require.Equal(t, tc.kind.String(), kind)
			// Finish the admitted row so its normal status tracker can exit.
			_, err = database.DB.ExecContext(ctx, `UPDATE queue_message SET status='FAILED' WHERE command_batch_log_uuid=$1`, result.BatchIdentifier)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				var status string
				err := database.DB.QueryRowContext(ctx, `SELECT status FROM command_batch_log WHERE uuid=$1`, result.BatchIdentifier).Scan(&status)
				return err == nil && status == "FINISHED"
			}, time.Second, 10*time.Millisecond)
		})
	}

	t.Run("window restore proceeds without capability discovery", func(t *testing.T) {
		mockController := gomock.NewController(t)
		procStore := mocks.NewMockScheduleProcessorStore(mockController)
		targetStore := mocks.NewMockScheduleTargetStore(mockController)
		windowProcessor := NewProcessor(procStore, targetStore, nil, deviceStore, commands, nil)
		now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
		windowProcessor.now = func() time.Time { return now }
		sched := &pb.Schedule{
			Id: 1, CreatedBy: user.DatabaseID, Action: pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
			ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME, Status: pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
			StartDate: "2026-10-15", StartTime: "09:00", EndTime: "10:00", Timezone: "UTC",
			LastRunAt: timestamppb.New(now.Add(-3 * time.Hour)),
		}
		procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{{Schedule: sched, OrgID: user.OrganizationID}}, nil)
		targetStore.EXPECT().GetScheduleTargets(gomock.Any(), user.OrganizationID, sched.Id).Return([]*pb.ScheduleTarget{
			{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: miner.ID},
		}, nil)
		var before int
		require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&before))
		var batchID string
		procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), sched.Id, gomock.Nil(), gomock.Nil(), statusCompleted).
			DoAndReturn(func(context.Context, int64, *int64, *int64, string) error {
				var after int
				require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM queue_message`).Scan(&after))
				require.Equal(t, before+1, after, "window must not complete without enqueueing the miner's restore")
				var deviceID int64
				require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT command_batch_log_uuid, device_id FROM queue_message ORDER BY id DESC LIMIT 1`).Scan(&batchID, &deviceID))
				require.Equal(t, miner.DatabaseID, deviceID)
				return nil
			})
		windowProcessor.checkEndOfWindow(ctx)
		_, err := database.DB.ExecContext(ctx, `UPDATE queue_message SET status='FAILED' WHERE command_batch_log_uuid=$1`, batchID)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			var status string
			err := database.DB.QueryRowContext(ctx, `SELECT status FROM command_batch_log WHERE uuid=$1`, batchID).Scan(&status)
			return err == nil && status == "FINISHED"
		}, time.Second, 10*time.Millisecond)
	})

	controllerOnly := &commandpb.DeviceSelector{SelectionType: &commandpb.DeviceSelector_IncludeDevices{
		IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{controller.ID}},
	}}
	sched := &pb.Schedule{Action: pb.ScheduleAction_SCHEDULE_ACTION_SLEEP, CreatedBy: user.DatabaseID}
	result, err := processor.dispatch(schedulerContext(ctx, sched, user.OrganizationID), sched, controllerOnly)
	require.NoError(t, err)
	require.Zero(t, result.DispatchedCount)
	require.Empty(t, result.BatchIdentifier)
	require.Len(t, result.Skipped, 1)
}
