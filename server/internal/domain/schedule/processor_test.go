package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/robfig/cron/v3"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/timestamppb"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	commanddomain "github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// newTestProcessor creates a Processor with mock dependencies and a fixed clock.
func newTestProcessor(t *testing.T, now time.Time) (*Processor, *mocks.MockScheduleProcessorStore, *mocks.MockScheduleTargetStore, *mocks.MockCollectionStore, *MockCommandDispatcher) {
	t.Helper()
	ctrl := gomock.NewController(t)
	procStore := mocks.NewMockScheduleProcessorStore(ctrl)
	targetStore := mocks.NewMockScheduleTargetStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	cmdSvc := NewMockCommandDispatcher(ctrl)

	p := NewProcessor(procStore, targetStore, collectionStore, cmdSvc, nil)
	p.now = func() time.Time { return now }
	return p, procStore, targetStore, collectionStore, cmdSvc
}

type recordingActivityStore struct {
	inserts []*activitymodels.Event
}

func (s *recordingActivityStore) Insert(_ context.Context, event *activitymodels.Event) error {
	clone := *event
	s.inserts = append(s.inserts, &clone)
	return nil
}

func (s *recordingActivityStore) List(context.Context, activitymodels.Filter) ([]activitymodels.Entry, error) {
	panic("not used in processor_test")
}
func (s *recordingActivityStore) Count(context.Context, activitymodels.Filter) (int64, error) {
	panic("not used in processor_test")
}
func (s *recordingActivityStore) GetDistinctUsers(context.Context, int64) ([]activitymodels.UserInfo, error) {
	panic("not used in processor_test")
}
func (s *recordingActivityStore) GetDistinctEventTypes(context.Context, int64) ([]activitymodels.EventTypeInfo, error) {
	panic("not used in processor_test")
}
func (s *recordingActivityStore) GetDistinctScopeTypes(context.Context, int64) ([]string, error) {
	panic("not used in processor_test")
}

func withRecordingActivity(p *Processor) *recordingActivityStore {
	store := &recordingActivityStore{}
	p.activitySvc = activity.NewService(store)
	return store
}

func countActivityType(store *recordingActivityStore, eventType string) int {
	n := 0
	for _, event := range store.inserts {
		if event.Type == eventType {
			n++
		}
	}
	return n
}

// --- recoverStaleRunning ---

func TestRecoverStaleRunning_ResetsNonWindowSchedules(t *testing.T) {
	p, procStore, _, _, _ := newTestProcessor(t, time.Now())

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: &pb.Schedule{
			Id:     1,
			Action: pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			Status: pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
		}, OrgID: 1},
	}, nil)
	procStore.EXPECT().RevertScheduleToActive(gomock.Any(), int64(1)).Return(nil)

	assert.NoError(t, p.recoverStaleRunning(context.Background()))
}

func TestRecoverStaleRunning_LeavesLegitimateWindows(t *testing.T) {
	p, procStore, _, _, _ := newTestProcessor(t, time.Now())

	lastRun := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: &pb.Schedule{
			Id:        1,
			Action:    pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
			Status:    pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
			EndTime:   "17:00",
			LastRunAt: timestamppb.New(lastRun),
		}, OrgID: 1},
	}, nil)
	// No RevertScheduleToActive call expected — legitimate window.

	assert.NoError(t, p.recoverStaleRunning(context.Background()))
}

func TestRecoverStaleRunning_ResetsWindowWithNilLastRunAt(t *testing.T) {
	p, procStore, _, _, _ := newTestProcessor(t, time.Now())

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: &pb.Schedule{
			Id:        1,
			Action:    pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
			Status:    pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
			EndTime:   "17:00",
			LastRunAt: nil,
		}, OrgID: 1},
	}, nil)
	procStore.EXPECT().RevertScheduleToActive(gomock.Any(), int64(1)).Return(nil)

	assert.NoError(t, p.recoverStaleRunning(context.Background()))
}

// --- syncSchedules ---

func TestSyncSchedules_RegistersNewJobs(t *testing.T) {
	p, procStore, _, _, _ := newTestProcessor(t, time.Now())
	p.cron = cron.New()
	p.cron.Start()
	defer p.cron.Stop()

	sched := &pb.Schedule{
		Id:           1,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-04-01",
		StartTime:    "09:00",
		Timezone:     "UTC",
		Status:       pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE,
		Recurrence:   &pb.ScheduleRecurrence{Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, Interval: 1},
	}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: sched, OrgID: 1},
	}, nil)

	assert.NoError(t, p.syncSchedules(context.Background()))

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, 1, len(p.jobs))
	_, exists := p.jobs[1]
	assert.True(t, exists)
}

func TestSyncSchedules_RemovesStaleJobs(t *testing.T) {
	p, procStore, _, _, _ := newTestProcessor(t, time.Now())
	p.cron = cron.New()
	p.cron.Start()
	defer p.cron.Stop()

	// Pre-populate a job that won't appear in the DB query.
	entryID, _ := p.cron.AddFunc("0 9 * * *", func() {})
	p.jobs[99] = jobEntry{entryID: entryID, fingerprint: "old"}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return(nil, nil)

	assert.NoError(t, p.syncSchedules(context.Background()))

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, 0, len(p.jobs))
}

func TestSyncSchedules_ReRegistersOnFingerprintChange(t *testing.T) {
	p, procStore, _, _, _ := newTestProcessor(t, time.Now())
	p.cron = cron.New()
	p.cron.Start()
	defer p.cron.Stop()

	// Pre-populate with old fingerprint.
	entryID, _ := p.cron.AddFunc("0 9 * * *", func() {})
	p.jobs[1] = jobEntry{entryID: entryID, fingerprint: "old-fingerprint"}

	sched := &pb.Schedule{
		Id:           1,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		Status:       pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE,
		Recurrence:   &pb.ScheduleRecurrence{Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, Interval: 1},
	}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: sched, OrgID: 1},
	}, nil)

	assert.NoError(t, p.syncSchedules(context.Background()))

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, 1, len(p.jobs))
	entry := p.jobs[1]
	assert.Equal(t, scheduleFingerprint(sched), entry.fingerprint)
	// Should have a new entryID (old one removed, new one registered).
	assert.NotEqual(t, entryID, entry.entryID)
}

// --- scheduleFingerprint ---

func TestScheduleFingerprint(t *testing.T) {
	base := &pb.Schedule{
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "09:00",
		Timezone:     "UTC",
	}
	fp1 := scheduleFingerprint(base)

	// Same fields → same fingerprint.
	same := &pb.Schedule{
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "09:00",
		Timezone:     "UTC",
	}
	assert.Equal(t, fp1, scheduleFingerprint(same))

	// Changed start time → different fingerprint.
	diff := &pb.Schedule{
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
	}
	assert.NotEqual(t, fp1, scheduleFingerprint(diff))

	// Recurring with recurrence fields → includes them.
	dayOfMonth := int32(15)
	rec := &pb.Schedule{
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-04-01",
		StartTime:    "09:00",
		Timezone:     "UTC",
		Recurrence: &pb.ScheduleRecurrence{
			Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
			DayOfMonth: &dayOfMonth,
		},
	}
	fpRec := scheduleFingerprint(rec)
	assert.NotEqual(t, fp1, fpRec)

	// Same recurrence → same fingerprint.
	rec2 := &pb.Schedule{
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-04-01",
		StartTime:    "09:00",
		Timezone:     "UTC",
		Recurrence: &pb.ScheduleRecurrence{
			Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
			DayOfMonth: &dayOfMonth,
		},
	}
	assert.Equal(t, fpRec, scheduleFingerprint(rec2))
}

// --- dispatch ---

func TestDispatch(t *testing.T) {
	tests := []struct {
		name   string
		action pb.ScheduleAction
		config *pb.PowerTargetConfig
		setup  func(*MockCommandDispatcher)
	}{
		{
			name:   "set_power_target_max",
			action: pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
			config: &pb.PowerTargetConfig{Mode: pb.PowerTargetMode_POWER_TARGET_MODE_MAX},
			setup: func(cmd *MockCommandDispatcher) {
				cmd.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), commandpb.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE).Return(nil, nil)
			},
		},
		{
			name:   "set_power_target_default",
			action: pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
			config: &pb.PowerTargetConfig{Mode: pb.PowerTargetMode_POWER_TARGET_MODE_DEFAULT},
			setup: func(cmd *MockCommandDispatcher) {
				cmd.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), commandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY).Return(nil, nil)
			},
		},
		{
			name:   "reboot",
			action: pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
			setup: func(cmd *MockCommandDispatcher) {
				cmd.EXPECT().Reboot(gomock.Any(), gomock.Any()).Return(nil, nil)
			},
		},
		{
			name:   "sleep",
			action: pb.ScheduleAction_SCHEDULE_ACTION_SLEEP,
			setup: func(cmd *MockCommandDispatcher) {
				cmd.EXPECT().StopMining(gomock.Any(), gomock.Any()).Return(nil, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _, _, cmdSvc := newTestProcessor(t, time.Now())
			tt.setup(cmdSvc)

			sched := &pb.Schedule{Id: 1, Action: tt.action, ActionConfig: tt.config}
			_, err := p.dispatch(context.Background(), sched, &commandpb.DeviceSelector{})
			assert.NoError(t, err)
		})
	}
}

func TestDispatchUnspecifiedAction(t *testing.T) {
	p, _, _, _, _ := newTestProcessor(t, time.Now())
	sched := &pb.Schedule{Id: 1, Action: pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED}
	_, err := p.dispatch(context.Background(), sched, &commandpb.DeviceSelector{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unspecified")
}

// --- resolveTargets ---

func TestResolveTargets_MinerAndRack(t *testing.T) {
	p, _, targetStore, collectionStore, _ := newTestProcessor(t, time.Now())

	sched := &pb.Schedule{Id: 10}
	orgID := int64(1)

	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), orgID, sched.Id).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-2"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "100"},
	}, nil)

	collectionStore.EXPECT().GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(100), orgID).
		Return([]string{"miner-2", "miner-3"}, nil)

	ids, err := p.resolveTargets(context.Background(), sched, orgID)
	assert.NoError(t, err)
	// miner-2 deduplicated
	assert.Equal(t, []string{"miner-1", "miner-2", "miner-3"}, ids)
}

func TestResolveTargets_Group(t *testing.T) {
	p, _, targetStore, collectionStore, _ := newTestProcessor(t, time.Now())

	sched := &pb.Schedule{Id: 11}
	orgID := int64(1)

	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), orgID, sched.Id).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP, TargetId: "200"},
	}, nil)

	collectionStore.EXPECT().GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(200), orgID).
		Return([]string{"miner-1", "miner-4"}, nil)

	ids, err := p.resolveTargets(context.Background(), sched, orgID)
	assert.NoError(t, err)
	// miner-1 deduplicated
	assert.Equal(t, []string{"miner-1", "miner-4"}, ids)
}

func TestResolveTargets_GroupErrorPropagates(t *testing.T) {
	p, _, targetStore, collectionStore, _ := newTestProcessor(t, time.Now())

	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP, TargetId: "200"},
	}, nil)
	collectionStore.EXPECT().GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(200), int64(1)).
		Return(nil, errors.New("db connection lost"))

	_, err := p.resolveTargets(context.Background(), &pb.Schedule{Id: 1}, int64(1))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "group 200")
}

func TestResolveTargets_RackErrorPropagates(t *testing.T) {
	p, _, targetStore, collectionStore, _ := newTestProcessor(t, time.Now())

	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "100"},
	}, nil)
	collectionStore.EXPECT().GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(100), int64(1)).
		Return(nil, errors.New("db connection lost"))

	_, err := p.resolveTargets(context.Background(), &pb.Schedule{Id: 1}, int64(1))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rack 100")
}

func TestResolveTargets_Empty(t *testing.T) {
	p, _, targetStore, _, _ := newTestProcessor(t, time.Now())

	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ids, err := p.resolveTargets(context.Background(), &pb.Schedule{Id: 1}, int64(1))
	assert.NoError(t, err)
	assert.Equal(t, 0, len(ids))
}

// Schedule-conflict filtering moved to command.ScheduleConflictFilter; the
// equivalent priority-semantics tests live in
// server/internal/domain/command/schedule_conflict_filter_test.go.

// --- executeSchedule ---

func TestExecuteSchedule_Success(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)

	sched := &pb.Schedule{
		Id:           1,
		Name:         "test",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		CreatedBy:    42,
	}

	procStore.EXPECT().SetScheduleRunning(gomock.Any(), int64(1)).Return(int64(1), nil)
	procStore.EXPECT().GetScheduleByID(gomock.Any(), int64(1)).Return(&interfaces.ScheduleWithOrg{Schedule: sched, OrgID: 1}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().Reboot(gomock.Any(), gomock.Any()).Return(nil, nil)
	// One-time past → completed.
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Nil(), statusCompleted).Return(nil)

	p.executeSchedule(context.Background(), 1)
}

func TestExecuteSchedule_SkipsWhenNotActive(t *testing.T) {
	p, procStore, _, _, _ := newTestProcessor(t, time.Now())

	procStore.EXPECT().SetScheduleRunning(gomock.Any(), int64(1)).Return(int64(0), nil)
	// No further calls expected.

	p.executeSchedule(context.Background(), 1)
}

func TestExecuteSchedule_DispatchFailureReverts(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)

	sched := &pb.Schedule{
		Id:           1,
		Name:         "test",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		CreatedBy:    42,
	}

	procStore.EXPECT().SetScheduleRunning(gomock.Any(), int64(1)).Return(int64(1), nil)
	procStore.EXPECT().GetScheduleByID(gomock.Any(), int64(1)).Return(&interfaces.ScheduleWithOrg{Schedule: sched, OrgID: 1}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().Reboot(gomock.Any(), gomock.Any()).Return(nil, errors.New("connection refused"))
	procStore.EXPECT().RevertScheduleToActive(gomock.Any(), int64(1)).Return(nil)
	// No UpdateScheduleAfterRun call expected.

	p.executeSchedule(context.Background(), 1)
}

func TestExecuteSchedule_DispatchFailureRevertFails_KeepsJob(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)
	p.cron = cron.New()
	p.cron.Start()
	defer p.cron.Stop()

	// Pre-register a job so we can verify it is NOT removed.
	entryID, _ := p.cron.AddFunc("0 10 * * *", func() {})
	p.jobs[1] = jobEntry{entryID: entryID, fingerprint: "test"}

	sched := &pb.Schedule{
		Id:           1,
		Name:         "test",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		CreatedBy:    42,
	}

	procStore.EXPECT().SetScheduleRunning(gomock.Any(), int64(1)).Return(int64(1), nil)
	procStore.EXPECT().GetScheduleByID(gomock.Any(), int64(1)).Return(&interfaces.ScheduleWithOrg{Schedule: sched, OrgID: 1}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().Reboot(gomock.Any(), gomock.Any()).Return(nil, errors.New("connection refused"))
	procStore.EXPECT().RevertScheduleToActive(gomock.Any(), int64(1)).Return(errors.New("db down"))
	// No removeJob expected because revert failed.

	p.executeSchedule(context.Background(), 1)

	p.mu.Lock()
	defer p.mu.Unlock()
	_, exists := p.jobs[1]
	assert.True(t, exists, "job should still exist when revert fails")
}

func TestExecuteSchedule_FullyConflictFilteredSkipsExecutionActivity(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)
	activityStore := withRecordingActivity(p)

	sched := &pb.Schedule{
		Id:           1,
		Name:         "power",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		CreatedBy:    42,
	}

	procStore.EXPECT().SetScheduleRunning(gomock.Any(), int64(1)).Return(int64(1), nil)
	procStore.EXPECT().GetScheduleByID(gomock.Any(), int64(1)).Return(&interfaces.ScheduleWithOrg{Schedule: sched, OrgID: 1}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), revertPerformanceMode).Return(&commanddomain.CommandResult{
		Skipped: []commanddomain.SkippedDevice{{DeviceIdentifier: "miner-1", FilterName: commanddomain.ScheduleConflictFilterName}},
	}, nil)
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Nil(), statusCompleted).Return(nil)

	p.executeSchedule(context.Background(), 1)

	assert.Equal(t, 1, countActivityType(activityStore, "schedule_conflict_skip"))
	assert.Equal(t, 0, countActivityType(activityStore, "schedule_executed"))
}

func TestExecuteSchedule_NonConflictZeroDispatchLogsExecution(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)
	activityStore := withRecordingActivity(p)

	sched := &pb.Schedule{
		Id:           1,
		Name:         "power",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		CreatedBy:    42,
	}

	procStore.EXPECT().SetScheduleRunning(gomock.Any(), int64(1)).Return(int64(1), nil)
	procStore.EXPECT().GetScheduleByID(gomock.Any(), int64(1)).Return(&interfaces.ScheduleWithOrg{Schedule: sched, OrgID: 1}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "stale-miner"},
	}, nil)
	cmdSvc.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), revertPerformanceMode).Return(&commanddomain.CommandResult{}, nil)
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Nil(), statusCompleted).Return(nil)

	p.executeSchedule(context.Background(), 1)

	assert.Equal(t, 0, countActivityType(activityStore, "schedule_conflict_skip"))
	assert.Equal(t, 1, countActivityType(activityStore, "schedule_executed"))
}

// --- updateAfterRun ---

func TestUpdateAfterRun_RecurringStaysActive(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, _, _, _ := newTestProcessor(t, now)

	sched := &pb.Schedule{
		Id:           1,
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		Recurrence: &pb.ScheduleRecurrence{
			Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
			Interval:  1,
		},
	}

	// Recurring with no end_date → next run tomorrow, status active.
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Not(gomock.Nil()), statusActive).Return(nil)

	p.updateAfterRun(context.Background(), sched, int64(1), now)
}

func TestUpdateAfterRun_OneTimeCompletes(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, _, _, _ := newTestProcessor(t, now)

	sched := &pb.Schedule{
		Id:           1,
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
	}

	// One-time past start → nil next run → completed.
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Nil(), statusCompleted).Return(nil)

	p.updateAfterRun(context.Background(), sched, int64(1), now)
}

func TestUpdateAfterRun_PowerTargetWindowStaysRunning(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, _, _, _ := newTestProcessor(t, now)

	sched := &pb.Schedule{
		Id:           1,
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		EndTime:      "18:00",
		Timezone:     "UTC",
		Recurrence: &pb.ScheduleRecurrence{
			Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
			Interval:  1,
		},
	}

	// Power target with end_time → stays running.
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Any(), statusRunning).Return(nil)

	p.updateAfterRun(context.Background(), sched, int64(1), now)
}

func TestUpdateAfterRun_UpdateFailsRevertFails_KeepsJob(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, _, _, _ := newTestProcessor(t, now)
	p.cron = cron.New()
	p.cron.Start()
	defer p.cron.Stop()

	entryID, _ := p.cron.AddFunc("0 10 * * *", func() {})
	p.jobs[1] = jobEntry{entryID: entryID, fingerprint: "test"}

	sched := &pb.Schedule{
		Id:           1,
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-04-01",
		StartTime:    "10:00",
		Timezone:     "UTC",
		Recurrence: &pb.ScheduleRecurrence{
			Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
			Interval:  1,
		},
	}

	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Any(), statusActive).Return(errors.New("db down"))
	procStore.EXPECT().RevertScheduleToActive(gomock.Any(), int64(1)).Return(errors.New("db down"))

	p.updateAfterRun(context.Background(), sched, int64(1), now)

	p.mu.Lock()
	defer p.mu.Unlock()
	_, exists := p.jobs[1]
	assert.True(t, exists, "job should still exist when revert fails")
}

// --- transitionToCompleted ---

func TestTransitionToCompleted_UpdateFailsRevertFails_KeepsJob(t *testing.T) {
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	p, procStore, _, _, _ := newTestProcessor(t, now)
	p.cron = cron.New()
	p.cron.Start()
	defer p.cron.Stop()

	entryID, _ := p.cron.AddFunc("0 10 * * *", func() {})
	p.jobs[1] = jobEntry{entryID: entryID, fingerprint: "test"}

	sched := &pb.Schedule{
		Id:           1,
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_REBOOT,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		StartDate:    "2026-03-01",
		StartTime:    "10:00",
		EndDate:      "2026-03-31",
		Timezone:     "UTC",
		Recurrence: &pb.ScheduleRecurrence{
			Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
			Interval:  1,
		},
	}

	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Any(), gomock.Nil(), statusCompleted).Return(errors.New("db down"))
	procStore.EXPECT().RevertScheduleToActive(gomock.Any(), int64(1)).Return(errors.New("db down"))

	p.transitionToCompleted(context.Background(), sched, int64(1), now)

	p.mu.Lock()
	defer p.mu.Unlock()
	_, exists := p.jobs[1]
	assert.True(t, exists, "job should still exist when revert fails")
}

// --- checkEndOfWindow ---

func TestCheckEndOfWindow_RevertsExpiredWindow(t *testing.T) {
	// Schedule ran at 09:00, end_time is 17:00, now is 17:30.
	now := time.Date(2026, 4, 1, 17, 30, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)

	lastRun := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	sched := &pb.Schedule{
		Id:           1,
		Name:         "power-sched",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		Status:       pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
		StartTime:    "09:00",
		EndTime:      "17:00",
		StartDate:    "2026-04-01",
		Timezone:     "UTC",
		LastRunAt:    timestamppb.New(lastRun),
		CreatedBy:    42,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		Recurrence:   &pb.ScheduleRecurrence{Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, Interval: 1},
	}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: sched, OrgID: 1},
	}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), revertPerformanceMode).Return(nil, nil)
	// Recurring → reverts to active with next run.
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Nil(), gomock.Not(gomock.Nil()), statusActive).Return(nil)

	p.checkEndOfWindow(context.Background())
}

func TestCheckEndOfWindow_OvernightWindowNotExpired(t *testing.T) {
	// Schedule started at 22:00, end_time is 06:00 (overnight), now is 23:00 same night.
	now := time.Date(2026, 4, 1, 23, 0, 0, 0, time.UTC)
	p, procStore, _, _, _ := newTestProcessor(t, now)

	lastRun := time.Date(2026, 4, 1, 22, 0, 0, 0, time.UTC)
	sched := &pb.Schedule{
		Id:           1,
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		Status:       pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
		StartTime:    "22:00",
		EndTime:      "06:00",
		StartDate:    "2026-04-01",
		Timezone:     "UTC",
		LastRunAt:    timestamppb.New(lastRun),
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		Recurrence:   &pb.ScheduleRecurrence{Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, Interval: 1},
	}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: sched, OrgID: 1},
	}, nil)
	// No revert calls expected — window hasn't expired yet.

	p.checkEndOfWindow(context.Background())
}

func TestCheckEndOfWindow_OvernightWindowExpired(t *testing.T) {
	// Schedule started at 22:00, end_time is 06:00, now is 06:30 next morning.
	now := time.Date(2026, 4, 2, 6, 30, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)

	lastRun := time.Date(2026, 4, 1, 22, 0, 0, 0, time.UTC)
	sched := &pb.Schedule{
		Id:           1,
		Name:         "overnight",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		Status:       pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
		StartTime:    "22:00",
		EndTime:      "06:00",
		StartDate:    "2026-04-01",
		Timezone:     "UTC",
		LastRunAt:    timestamppb.New(lastRun),
		CreatedBy:    42,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		Recurrence:   &pb.ScheduleRecurrence{Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, Interval: 1},
	}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: sched, OrgID: 1},
	}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), revertPerformanceMode).Return(nil, nil)
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Nil(), gomock.Any(), statusActive).Return(nil)

	p.checkEndOfWindow(context.Background())
}

func TestCheckEndOfWindow_FailedRevertRetries(t *testing.T) {
	// Revert dispatch fails → schedule stays running (no status update).
	now := time.Date(2026, 4, 1, 17, 30, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)

	lastRun := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	sched := &pb.Schedule{
		Id:           1,
		Name:         "power-sched",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		Status:       pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
		StartTime:    "09:00",
		EndTime:      "17:00",
		StartDate:    "2026-04-01",
		Timezone:     "UTC",
		LastRunAt:    timestamppb.New(lastRun),
		CreatedBy:    42,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		Recurrence:   &pb.ScheduleRecurrence{Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, Interval: 1},
	}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: sched, OrgID: 1},
	}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), revertPerformanceMode).Return(nil, errors.New("timeout"))
	// No UpdateScheduleAfterRun — stays running for retry.

	p.checkEndOfWindow(context.Background())
}

func TestCheckEndOfWindow_RevertStateFailSkipsLog(t *testing.T) {
	now := time.Date(2026, 4, 1, 17, 30, 0, 0, time.UTC)
	p, procStore, targetStore, _, cmdSvc := newTestProcessor(t, now)

	lastRun := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	sched := &pb.Schedule{
		Id:           1,
		Name:         "power-sched",
		Action:       pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET,
		Status:       pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING,
		StartTime:    "09:00",
		EndTime:      "17:00",
		StartDate:    "2026-04-01",
		Timezone:     "UTC",
		LastRunAt:    timestamppb.New(lastRun),
		CreatedBy:    42,
		ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
		Recurrence:   &pb.ScheduleRecurrence{Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY, Interval: 1},
	}

	procStore.EXPECT().GetActiveSchedules(gomock.Any()).Return([]interfaces.ScheduleWithOrg{
		{Schedule: sched, OrgID: 1},
	}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(1)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)
	cmdSvc.EXPECT().SetPowerTarget(gomock.Any(), gomock.Any(), revertPerformanceMode).Return(nil, nil)
	procStore.EXPECT().UpdateScheduleAfterRun(gomock.Any(), int64(1), gomock.Nil(), gomock.Any(), statusActive).Return(errors.New("db down"))
	// No LogActivity call expected — revert state failed so log is skipped.

	p.checkEndOfWindow(context.Background())
}
