package interfaces

import (
	"context"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
)

type ScheduleIDStatus struct {
	ID     int64
	Status string
}

type ScheduleTargetOverlap struct {
	ScheduleID       int64
	SchedulePriority int32
	DeviceIdentifier string
}

// ScheduleWithOrg pairs a proto Schedule with its org_id for cross-org processor queries.
// The proto Schedule message is org-agnostic (all CRUD is org-scoped via session),
// but the processor operates across orgs and needs the org_id for command dispatch.
type ScheduleWithOrg struct {
	Schedule *pb.Schedule
	OrgID    int64
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

//go:generate go run go.uber.org/mock/mockgen -source=schedule.go -destination=mocks/mock_schedule_store.go -package=mocks ScheduleProcessorStore ScheduleTargetStore

// ScheduleProcessorStore defines store methods used exclusively by the schedule processor (BE-3).
// Cross-org queries return ScheduleWithOrg to provide the org_id that the proto
// Schedule message omits. Org-scoped queries return plain *pb.Schedule.
type ScheduleProcessorStore interface {
	GetActiveSchedules(ctx context.Context) ([]ScheduleWithOrg, error)
	GetRunningPowerTargetScheduleOverlaps(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]ScheduleTargetOverlap, error)
	UpdateScheduleAfterRun(ctx context.Context, scheduleID int64, lastRunAt, nextRunAt *int64, status string) error
	SetScheduleRunning(ctx context.Context, scheduleID int64) (int64, error)
	GetScheduleByID(ctx context.Context, scheduleID int64) (*ScheduleWithOrg, error)
	RevertScheduleToActive(ctx context.Context, scheduleID int64) error
}
