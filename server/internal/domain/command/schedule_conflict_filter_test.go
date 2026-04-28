package command

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// newScheduleConflictFilter wires the filter against fresh gomock-backed
// stores. The collection store is included because expandTargets needs it
// for rack/group target types — leaving it nil would panic on the first
// non-miner target.
func newScheduleConflictFilter(t *testing.T) (*ScheduleConflictFilter, *mocks.MockScheduleProcessorStore, *mocks.MockScheduleTargetStore, *mocks.MockCollectionStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	procStore := mocks.NewMockScheduleProcessorStore(ctrl)
	targetStore := mocks.NewMockScheduleTargetStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	f := NewScheduleConflictFilter(procStore, targetStore, collectionStore)
	return f, procStore, targetStore, collectionStore
}

// --- Command-type gating ---

func TestScheduleConflictFilter_NonPowerTargetCommandPassesThrough(t *testing.T) {
	// Reboot/StopMining/etc. shouldn't even consult the schedule store —
	// the existing inline filter only ran for SetPowerTarget and we're
	// preserving that scope.
	f, _, _, _ := newScheduleConflictFilter(t)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Reboot,
		OrganizationID:    1,
		DeviceIdentifiers: []string{"miner-1"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
}

func TestScheduleConflictFilter_EmptyInputPassesThrough(t *testing.T) {
	f, _, _, _ := newScheduleConflictFilter(t)
	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:    commandtype.SetPowerTarget,
		OrganizationID: 1,
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(out.Kept))
	assert.Equal(t, 0, len(out.Skipped))
}

// --- Scheduler-origin priority semantics ---

func TestScheduleConflictFilter_SchedulerOriginHigherPriorityBlocks(t *testing.T) {
	// Caller is schedule 10 priority 5; running schedule 20 has priority 2
	// (numerically lower → higher priority). Devices held by 20 must drop.
	f, procStore, targetStore, _ := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetSchedules(gomock.Any(), int64(1)).Return([]*pb.Schedule{
		{Id: 20, Priority: 2, Action: pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET},
	}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(20)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		OrganizationID:    1,
		Actor:             session.ActorScheduler,
		Source:            session.Source{ScheduleID: 10, SchedulePriority: 5},
		DeviceIdentifiers: []string{"miner-1", "miner-2", "miner-3"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-2", "miner-3"}, out.Kept)
	assert.Equal(t, 1, len(out.Skipped))
	assert.Equal(t, "miner-1", out.Skipped[0].DeviceIdentifier)
	assert.Equal(t, "schedule_conflict", out.Skipped[0].FilterName)
}

func TestScheduleConflictFilter_SchedulerOriginLowerPriorityIgnored(t *testing.T) {
	// Caller is schedule 10 priority 2; running schedule 20 has priority 5
	// (numerically higher → lower priority). Devices held by 20 do NOT
	// block — the caller wins, so nothing is filtered, and we don't even
	// need to inspect that running schedule's targets.
	f, procStore, _, _ := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetSchedules(gomock.Any(), int64(1)).Return([]*pb.Schedule{
		{Id: 20, Priority: 5},
	}, nil)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		OrganizationID:    1,
		Actor:             session.ActorScheduler,
		Source:            session.Source{ScheduleID: 10, SchedulePriority: 2},
		DeviceIdentifiers: []string{"miner-1"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
}

func TestScheduleConflictFilter_SchedulerOriginIgnoresSelf(t *testing.T) {
	// A schedule must not conflict with itself even when its targets and
	// the caller's selector overlap (which they always do by definition).
	f, procStore, _, _ := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetSchedules(gomock.Any(), int64(1)).Return([]*pb.Schedule{
		{Id: 10, Priority: 5},
	}, nil)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		OrganizationID:    1,
		Actor:             session.ActorScheduler,
		Source:            session.Source{ScheduleID: 10, SchedulePriority: 5},
		DeviceIdentifiers: []string{"miner-1"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
}

// --- Manual-origin (Source.ScheduleID == 0) semantics ---

func TestScheduleConflictFilter_ManualBlockedByAnyRunningSchedule(t *testing.T) {
	// This is the headline pre-work behaviour change. With Source unset
	// (manual API call, no priority), every running power-target schedule
	// is a blocker for overlapping devices.
	f, procStore, targetStore, _ := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetSchedules(gomock.Any(), int64(1)).Return([]*pb.Schedule{
		{Id: 20, Priority: 100, Action: pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET},
	}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(20)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: "miner-1"},
	}, nil)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:    commandtype.SetPowerTarget,
		OrganizationID: 1,
		// Actor empty; Source zero — both indicate user/API origin.
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-2"}, out.Kept)
	assert.Equal(t, 1, len(out.Skipped))
	assert.Equal(t, "miner-1", out.Skipped[0].DeviceIdentifier)
}

func TestScheduleConflictFilter_ManualUnaffectedWhenNoRunningSchedules(t *testing.T) {
	f, procStore, _, _ := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetSchedules(gomock.Any(), int64(1)).Return(nil, nil)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		OrganizationID:    1,
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1", "miner-2"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
}

// --- Rack/group expansion ---

func TestScheduleConflictFilter_ExpandsRackTargetsBeforeMatching(t *testing.T) {
	// The running schedule targets a rack rather than individual miners;
	// the filter must expand it via the collection store before deciding
	// which of the caller's identifiers to skip.
	f, procStore, targetStore, collectionStore := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetSchedules(gomock.Any(), int64(1)).Return([]*pb.Schedule{
		{Id: 20, Priority: 2, Action: pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET},
	}, nil)
	targetStore.EXPECT().GetScheduleTargets(gomock.Any(), int64(1), int64(20)).Return([]*pb.ScheduleTarget{
		{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK, TargetId: "100"},
	}, nil)
	collectionStore.EXPECT().GetDeviceIdentifiersByDeviceSetID(gomock.Any(), int64(100), int64(1)).
		Return([]string{"miner-1", "miner-3"}, nil)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		OrganizationID:    1,
		Actor:             session.ActorScheduler,
		Source:            session.Source{ScheduleID: 10, SchedulePriority: 5},
		DeviceIdentifiers: []string{"miner-1", "miner-2", "miner-3"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-2"}, out.Kept)
	assert.Equal(t, 2, len(out.Skipped))
}
