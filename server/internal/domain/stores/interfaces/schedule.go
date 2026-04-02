package interfaces

import (
	"context"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
)

type ScheduleIDStatus struct {
	ID     int64
	Status string
}

// ScheduleStore handles schedule CRUD and status transitions.
type ScheduleStore interface {
	GetSchedule(ctx context.Context, orgID, scheduleID int64) (*pb.Schedule, error)
	ListSchedules(ctx context.Context, orgID int64, status, action string) ([]*pb.Schedule, error)
	CreateSchedule(ctx context.Context, orgID int64, schedule *pb.Schedule) (int64, error)
	UpdateSchedule(ctx context.Context, orgID int64, schedule *pb.Schedule) (int64, error)
	SoftDeleteSchedule(ctx context.Context, orgID, scheduleID int64) (int64, error)

	PauseActiveSchedule(ctx context.Context, orgID, scheduleID int64) (int64, error)
	ResumePausedSchedule(ctx context.Context, orgID, scheduleID int64, status string, nextRunAt *int64) (int64, error)
}

// ScheduleTargetStore handles schedule target CRUD.
type ScheduleTargetStore interface {
	CreateScheduleTarget(ctx context.Context, orgID, scheduleID int64, targetType, targetID string) error
	GetScheduleTargets(ctx context.Context, orgID, scheduleID int64) ([]*pb.ScheduleTarget, error)
	GetScheduleTargetsByScheduleIDs(ctx context.Context, orgID int64, scheduleIDs []int64) (map[int64][]*pb.ScheduleTarget, error)
	DeleteScheduleTargets(ctx context.Context, orgID, scheduleID int64) error
}

// SchedulePriorityStore handles priority management for schedule ordering.
type SchedulePriorityStore interface {
	GetMaxPriority(ctx context.Context, orgID int64) (int32, error)
	LockSchedulePriority(ctx context.Context, orgID int64) error
	ReorderSchedules(ctx context.Context, orgID int64, ids []int64) error
	ListScheduleIDStatuses(ctx context.Context, orgID int64) ([]ScheduleIDStatus, error)
}

// ScheduleProcessorStore defines store methods used exclusively by the schedule processor (BE-3).
// SQLScheduleStore implements ScheduleStore, ScheduleTargetStore, SchedulePriorityStore, and ScheduleProcessorStore.
type ScheduleProcessorStore interface {
	GetDueSchedules(ctx context.Context) ([]*pb.Schedule, error)
	GetActiveSchedules(ctx context.Context) ([]*pb.Schedule, error)
	GetRunningPowerTargetSchedules(ctx context.Context, orgID int64) ([]*pb.Schedule, error)
	UpdateScheduleAfterRun(ctx context.Context, scheduleID int64, lastRunAt, nextRunAt *int64, status string) error
}
