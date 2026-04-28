package command

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const ScheduleConflictFilterName = "schedule_conflict"

// ScheduleConflictFilter gates SetPowerTarget against running power-target
// schedules. Scheduler-origin calls use schedule priority; manual calls treat
// any overlapping running schedule as a blocker.
type ScheduleConflictFilter struct {
	procStore stores.ScheduleProcessorStore
}

func NewScheduleConflictFilter(procStore stores.ScheduleProcessorStore) *ScheduleConflictFilter {
	return &ScheduleConflictFilter{
		procStore: procStore,
	}
}

func (f *ScheduleConflictFilter) Name() string {
	return ScheduleConflictFilterName
}

func (f *ScheduleConflictFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	if in.CommandType != commandtype.SetPowerTarget {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	if len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	overlaps, err := f.procStore.GetRunningPowerTargetScheduleOverlaps(ctx, in.OrganizationID, in.DeviceIdentifiers)
	if err != nil {
		return CommandFilterOutput{}, fmt.Errorf("failed to get running power target schedule overlaps: %w", err)
	}

	// device_identifier -> blocking schedule id (first one wins for diagnostic Reason)
	conflicted := make(map[string]int64)
	for _, r := range overlaps {
		// Scheduler-origin dispatch should not conflict with itself.
		if r.ScheduleID == in.Source.ScheduleID {
			continue
		}
		if in.Source.ScheduleID != 0 && r.SchedulePriority >= in.Source.SchedulePriority {
			continue
		}
		if _, exists := conflicted[r.DeviceIdentifier]; !exists {
			conflicted[r.DeviceIdentifier] = r.ScheduleID
		}
	}

	if len(conflicted) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	var kept []string
	var skipped []SkippedDevice
	for _, id := range in.DeviceIdentifiers {
		if blockingID, blocked := conflicted[id]; blocked {
			reason := fmt.Sprintf("schedule %d blocks set_power_target", blockingID)
			if in.Source.ScheduleID != 0 {
				reason = fmt.Sprintf("schedule %d holds higher priority for set_power_target", blockingID)
			}
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: id,
				FilterName:       f.Name(),
				Reason:           reason,
			})
			continue
		}
		kept = append(kept, id)
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}
