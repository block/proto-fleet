package command

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

func newScheduleConflictFilter(t *testing.T) (*ScheduleConflictFilter, *mocks.MockScheduleProcessorStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	procStore := mocks.NewMockScheduleProcessorStore(ctrl)
	f := NewScheduleConflictFilter(procStore)
	return f, procStore
}

// --- Command-type gating ---

func TestScheduleConflictFilter_NonPowerTargetCommandPassesThrough(t *testing.T) {
	// Reboot/StopMining/etc. shouldn't even consult the schedule store —
	// the existing inline filter only ran for SetPowerTarget and we're
	// preserving that scope.
	f, _ := newScheduleConflictFilter(t)

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
	f, _ := newScheduleConflictFilter(t)
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
	f, procStore := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetScheduleOverlaps(gomock.Any(), int64(1), []string{"miner-1", "miner-2", "miner-3"}).Return([]stores.ScheduleTargetOverlap{
		{ScheduleID: 20, SchedulePriority: 2, DeviceIdentifier: "miner-1"},
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
	assert.Equal(t, ScheduleConflictFilterName, out.Skipped[0].FilterName)
	assert.Equal(t, "schedule 20 holds higher priority for set_power_target", out.Skipped[0].Reason)
}

func TestScheduleConflictFilter_SchedulerOriginLowerPriorityIgnored(t *testing.T) {
	// Caller is schedule 10 priority 2; running schedule 20 has priority 5
	// (numerically higher → lower priority). Devices held by 20 do NOT
	// block — the caller wins, so nothing is filtered, and we don't even
	// need to inspect that running schedule's targets.
	f, procStore := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetScheduleOverlaps(gomock.Any(), int64(1), []string{"miner-1"}).Return([]stores.ScheduleTargetOverlap{
		{ScheduleID: 20, SchedulePriority: 5, DeviceIdentifier: "miner-1"},
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
	f, procStore := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetScheduleOverlaps(gomock.Any(), int64(1), []string{"miner-1"}).Return([]stores.ScheduleTargetOverlap{
		{ScheduleID: 10, SchedulePriority: 5, DeviceIdentifier: "miner-1"},
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
	f, procStore := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetScheduleOverlaps(gomock.Any(), int64(1), []string{"miner-1", "miner-2"}).Return([]stores.ScheduleTargetOverlap{
		{ScheduleID: 20, SchedulePriority: 100, DeviceIdentifier: "miner-1"},
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
	assert.Equal(t, "schedule 20 blocks set_power_target", out.Skipped[0].Reason)
}

func TestScheduleConflictFilter_ManualUnaffectedWhenNoRunningSchedules(t *testing.T) {
	f, procStore := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetScheduleOverlaps(gomock.Any(), int64(1), []string{"miner-1", "miner-2"}).Return(nil, nil)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		OrganizationID:    1,
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1", "miner-2"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
}

// --- Rack/group overlaps from store ---

func TestScheduleConflictFilter_RackOverlapFromStoreBlocks(t *testing.T) {
	f, procStore := newScheduleConflictFilter(t)
	procStore.EXPECT().GetRunningPowerTargetScheduleOverlaps(gomock.Any(), int64(1), []string{"miner-1", "miner-2", "miner-3"}).Return([]stores.ScheduleTargetOverlap{
		{ScheduleID: 20, SchedulePriority: 2, DeviceIdentifier: "miner-1"},
		{ScheduleID: 20, SchedulePriority: 2, DeviceIdentifier: "miner-3"},
	}, nil)

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
