package command

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	scheduletargets "github.com/block/proto-fleet/server/internal/domain/schedule/targets"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// ScheduleConflictFilter prevents a SetPowerTarget command from racing a
// running power-target schedule.
//
// Two priority semantics, distinguished by session.Source:
//
//   - Scheduler-origin (Source.ScheduleID != 0): only schedules with strictly
//     higher priority (numerically lower Priority field) than the caller block
//     overlapping devices. This preserves today's behaviour for the schedule
//     processor's normal dispatch and end-of-window revert paths.
//
//   - Manual-origin (Source.ScheduleID == 0, e.g. MinerCommandService over
//     ConnectRPC, future curtailment-or-other paths that don't claim to be
//     scheduler-origin): every running power-target schedule blocks
//     overlapping devices. Manual callers have no priority to compare
//     against; "skip the device, log it, dispatch the rest" is the same
//     outcome the schedule processor would produce against itself.
//
// The filter only applies to SetPowerTarget commands — Reboot, StopMining,
// etc. pass through unchanged, matching the existing scope of the inlined
// filterConflictedMiners that this lift-and-shift replaces.
type ScheduleConflictFilter struct {
	procStore       stores.ScheduleProcessorStore
	targetStore     stores.ScheduleTargetStore
	collectionStore stores.CollectionStore
}

// NewScheduleConflictFilter wires the stores the filter needs to enumerate
// running power-target schedules and expand their target sets to identifiers.
// The collection store is needed for rack/group expansion, mirroring the
// schedule processor's resolveTargets path.
func NewScheduleConflictFilter(
	procStore stores.ScheduleProcessorStore,
	targetStore stores.ScheduleTargetStore,
	collectionStore stores.CollectionStore,
) *ScheduleConflictFilter {
	return &ScheduleConflictFilter{
		procStore:       procStore,
		targetStore:     targetStore,
		collectionStore: collectionStore,
	}
}

func (f *ScheduleConflictFilter) Name() string {
	return "schedule_conflict"
}

func (f *ScheduleConflictFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	if in.CommandType != commandtype.SetPowerTarget {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	if len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	running, err := f.procStore.GetRunningPowerTargetSchedules(ctx, in.OrganizationID)
	if err != nil {
		return CommandFilterOutput{}, fmt.Errorf("failed to get running power target schedules: %w", err)
	}

	// device_identifier -> blocking schedule id (first one wins for diagnostic Reason)
	conflicted := make(map[string]int64)
	for _, r := range running {
		// Don't conflict with self (scheduler-origin re-entering its own dispatch).
		if r.Id == in.Source.ScheduleID {
			continue
		}
		// Scheduler-origin priority semantics: only blocks if running has
		// strictly higher priority (lower numeric value). Manual-origin
		// (Source.ScheduleID == 0): every running schedule is a blocker.
		if in.Source.ScheduleID != 0 && r.Priority >= in.Source.SchedulePriority {
			continue
		}
		rTargets, err := f.targetStore.GetScheduleTargets(ctx, in.OrganizationID, r.Id)
		if err != nil {
			return CommandFilterOutput{}, fmt.Errorf("failed to load targets for conflicting schedule %d: %w", r.Id, err)
		}
		rDevices, err := scheduletargets.Expand(ctx, f.collectionStore, rTargets, in.OrganizationID, nil)
		if err != nil {
			return CommandFilterOutput{}, fmt.Errorf("failed to expand targets for conflicting schedule %d: %w", r.Id, err)
		}
		for _, d := range rDevices {
			if _, exists := conflicted[d]; !exists {
				conflicted[d] = r.Id
			}
		}
	}

	if len(conflicted) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	var kept []string
	var skipped []SkippedDevice
	for _, id := range in.DeviceIdentifiers {
		if blockingID, blocked := conflicted[id]; blocked {
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: id,
				FilterName:       f.Name(),
				Reason:           fmt.Sprintf("schedule %d holds higher priority for set_power_target", blockingID),
			})
			continue
		}
		kept = append(kept, id)
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}
