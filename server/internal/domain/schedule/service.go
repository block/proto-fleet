package schedule

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	statusActive    = "active"
	statusRunning   = "running"
	statusCompleted = "completed"
)

const maxScheduleNameLength = 100

type Service struct {
	store         interfaces.ScheduleStore
	targetStore   interfaces.ScheduleTargetStore
	priorityStore interfaces.SchedulePriorityStore
	transactor    interfaces.Transactor
	activitySvc   *activity.Service
	now           func() time.Time
}

func NewService(store interfaces.ScheduleStore, targetStore interfaces.ScheduleTargetStore, priorityStore interfaces.SchedulePriorityStore, transactor interfaces.Transactor, activitySvc *activity.Service) *Service {
	return &Service{
		store:         store,
		targetStore:   targetStore,
		priorityStore: priorityStore,
		transactor:    transactor,
		activitySvc:   activitySvc,
		now:           time.Now,
	}
}

func (s *Service) ListSchedules(ctx context.Context, status, action string) ([]*pb.Schedule, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	schedules, err := s.store.ListSchedules(ctx, info.OrganizationID, status, action)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, len(schedules))
	for i, sched := range schedules {
		ids[i] = sched.Id
	}
	targetsByID, err := s.targetStore.GetScheduleTargetsByScheduleIDs(ctx, info.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	for _, sched := range schedules {
		sched.Targets = targetsByID[sched.Id]
	}

	return schedules, nil
}

func (s *Service) CreateSchedule(ctx context.Context, req *pb.CreateScheduleRequest) (*pb.Schedule, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)

	if err := validateScheduleFields(name, req.Action, req.ActionConfig, req.ScheduleType, req.Recurrence); err != nil {
		return nil, err
	}
	if err := validateTargets(req.Targets); err != nil {
		return nil, err
	}

	sched := &pb.Schedule{
		Name:         name,
		Action:       req.Action,
		ActionConfig: req.ActionConfig,
		ScheduleType: req.ScheduleType,
		Recurrence:   req.Recurrence,
		StartDate:    req.StartDate,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		EndDate:      req.EndDate,
		Timezone:     req.Timezone,
		CreatedBy:    info.UserID,
	}

	nextRun, err := ComputeNextRun(sched, s.now())
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("cannot compute next run: %v", err)
	}
	if nextRun == nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("schedule has no future runs")
	}
	sched.NextRunAt = timestamppb.New(*nextRun)

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		if err := s.priorityStore.LockSchedulePriority(ctx, info.OrganizationID); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to lock priority: %v", err)
		}

		maxPriority, err := s.priorityStore.GetMaxPriority(ctx, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		sched.Priority = maxPriority + 1

		id, err := s.store.CreateSchedule(ctx, info.OrganizationID, sched)
		if err != nil {
			return nil, err
		}

		for _, target := range req.Targets {
			if err := s.targetStore.CreateScheduleTarget(ctx, info.OrganizationID, id, scheduleTargetTypeToString(target.TargetType), strings.TrimSpace(target.TargetId)); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to create target: %v", err)
			}
		}

		if err := s.normalizeSchedulePriorities(ctx, info.OrganizationID); err != nil {
			return nil, err
		}

		return s.fetchScheduleWithTargets(ctx, info.OrganizationID, id)
	})
	if err != nil {
		return nil, err
	}

	created, ok := result.(*pb.Schedule)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	s.logActivity(ctx, info, "create_schedule", fmt.Sprintf("Created schedule: %s", created.Name))
	return created, nil
}

func (s *Service) UpdateSchedule(ctx context.Context, req *pb.UpdateScheduleRequest) (*pb.Schedule, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)

	if err := validateScheduleFields(name, req.Action, req.ActionConfig, req.ScheduleType, req.Recurrence); err != nil {
		return nil, err
	}
	if err := validateTargets(req.Targets); err != nil {
		return nil, err
	}

	sched := &pb.Schedule{
		Id:           req.ScheduleId,
		Name:         name,
		Action:       req.Action,
		ActionConfig: req.ActionConfig,
		ScheduleType: req.ScheduleType,
		Recurrence:   req.Recurrence,
		StartDate:    req.StartDate,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		EndDate:      req.EndDate,
		Timezone:     req.Timezone,
	}

	nextRun, err := ComputeNextRun(sched, s.now())
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("cannot compute next run: %v", err)
	}
	if nextRun == nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("schedule has no future runs")
	}
	sched.NextRunAt = timestamppb.New(*nextRun)

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		existing, err := s.store.GetSchedule(ctx, info.OrganizationID, req.ScheduleId)
		if err != nil {
			return nil, err
		}
		if existing.Status == pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING {
			return nil, fleeterror.NewInvalidArgumentErrorf("cannot update schedule while it is running; pause it first")
		}

		sched.Status = existing.Status
		if existing.Status == pb.ScheduleStatus_SCHEDULE_STATUS_COMPLETED {
			sched.Status = pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE
		}

		if sched.Status == pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE {
			if err := s.priorityStore.LockSchedulePriority(ctx, info.OrganizationID); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to lock priority: %v", err)
			}
		}

		rows, err := s.store.UpdateSchedule(ctx, info.OrganizationID, sched)
		if err != nil {
			return nil, err
		}
		if rows == 0 {
			current, rereadErr := s.store.GetSchedule(ctx, info.OrganizationID, req.ScheduleId)
			if rereadErr != nil {
				return nil, rereadErr
			}
			if current.Status == pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING {
				return nil, fleeterror.NewInvalidArgumentErrorf("cannot update schedule while it is running; pause it first")
			}
			return nil, fleeterror.NewNotFoundErrorf("schedule not found: %d", req.ScheduleId)
		}

		if err := s.targetStore.DeleteScheduleTargets(ctx, info.OrganizationID, req.ScheduleId); err != nil {
			return nil, err
		}
		for _, target := range req.Targets {
			if err := s.targetStore.CreateScheduleTarget(ctx, info.OrganizationID, req.ScheduleId, scheduleTargetTypeToString(target.TargetType), strings.TrimSpace(target.TargetId)); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to create target: %v", err)
			}
		}

		if sched.Status == pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE {
			if err := s.normalizeSchedulePriorities(ctx, info.OrganizationID); err != nil {
				return nil, err
			}
		}

		return s.fetchScheduleWithTargets(ctx, info.OrganizationID, req.ScheduleId)
	})
	if err != nil {
		return nil, err
	}

	updated, ok := result.(*pb.Schedule)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	s.logActivity(ctx, info, "update_schedule", fmt.Sprintf("Updated schedule: %s", updated.Name))
	return updated, nil
}

func (s *Service) DeleteSchedule(ctx context.Context, scheduleID int64) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	sched, _ := s.store.GetSchedule(ctx, info.OrganizationID, scheduleID)

	rows, err := s.store.SoftDeleteSchedule(ctx, info.OrganizationID, scheduleID)
	if err != nil {
		return err
	}
	if rows == 0 {
		return fleeterror.NewNotFoundErrorf("schedule not found: %d", scheduleID)
	}

	if sched != nil {
		s.logActivity(ctx, info, "delete_schedule", fmt.Sprintf("Deleted schedule: %s", sched.Name))
	}
	return nil
}

func (s *Service) PauseSchedule(ctx context.Context, scheduleID int64) (*pb.Schedule, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.store.PauseActiveSchedule(ctx, info.OrganizationID, scheduleID)
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		existing, getErr := s.store.GetSchedule(ctx, info.OrganizationID, scheduleID)
		if getErr != nil {
			return nil, getErr
		}
		return nil, fleeterror.NewInvalidArgumentErrorf("cannot pause schedule with status %v", existing.Status)
	}

	paused, err := s.fetchScheduleWithTargets(ctx, info.OrganizationID, scheduleID)
	if err != nil {
		return nil, err
	}

	s.logActivity(ctx, info, "pause_schedule", fmt.Sprintf("Paused schedule: %s", paused.Name))
	return paused, nil
}

func (s *Service) ResumeSchedule(ctx context.Context, scheduleID int64) (*pb.Schedule, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		existing, err := s.store.GetSchedule(ctx, info.OrganizationID, scheduleID)
		if err != nil {
			return nil, err
		}

		nextRun, err := ComputeNextRun(existing, s.now())
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("cannot compute next run: %v", err)
		}

		newStatus := statusActive
		var nextRunUnix *int64
		if nextRun != nil {
			u := nextRun.Unix()
			nextRunUnix = &u
		} else {
			newStatus = statusCompleted
		}

		if newStatus == statusCompleted {
			if err := s.priorityStore.LockSchedulePriority(ctx, info.OrganizationID); err != nil {
				return nil, fleeterror.NewInternalErrorf("failed to lock priority: %v", err)
			}
		}

		rows, err := s.store.ResumePausedSchedule(ctx, info.OrganizationID, scheduleID, newStatus, nextRunUnix)
		if err != nil {
			return nil, err
		}
		if rows == 0 {
			current, rereadErr := s.store.GetSchedule(ctx, info.OrganizationID, scheduleID)
			if rereadErr != nil {
				return nil, rereadErr
			}
			return nil, fleeterror.NewInvalidArgumentErrorf("cannot resume schedule with status %v", current.Status)
		}

		if newStatus == statusCompleted {
			if err := s.normalizeSchedulePriorities(ctx, info.OrganizationID); err != nil {
				return nil, err
			}
		}

		return s.fetchScheduleWithTargets(ctx, info.OrganizationID, scheduleID)
	})
	if err != nil {
		return nil, err
	}

	resumed, ok := result.(*pb.Schedule)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	s.logActivity(ctx, info, "resume_schedule", fmt.Sprintf("Resumed schedule: %s", resumed.Name))
	return resumed, nil
}

func (s *Service) ReorderSchedules(ctx context.Context, ids []int64) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		if err := s.priorityStore.LockSchedulePriority(ctx, info.OrganizationID); err != nil {
			return fleeterror.NewInternalErrorf("failed to lock priority: %v", err)
		}

		statuses, err := s.priorityStore.ListScheduleIDStatuses(ctx, info.OrganizationID)
		if err != nil {
			return err
		}

		reorderableIDs := make(map[int64]bool)
		statusByID := make(map[int64]string, len(statuses))
		var completedIDs []int64
		for _, st := range statuses {
			statusByID[st.ID] = st.Status
			if st.Status == statusCompleted {
				completedIDs = append(completedIDs, st.ID)
			} else {
				reorderableIDs[st.ID] = true
			}
		}

		seen := make(map[int64]bool)
		reorderedIDs := make([]int64, 0, len(reorderableIDs))
		for _, id := range ids {
			if seen[id] {
				return fleeterror.NewInvalidArgumentErrorf("duplicate schedule ID: %d", id)
			}
			seen[id] = true

			status, ok := statusByID[id]
			if !ok {
				return fleeterror.NewInvalidArgumentErrorf("schedule %d is not a schedule in this organization (it may be deleted or not found)", id)
			}
			if status == statusCompleted {
				continue
			}
			reorderedIDs = append(reorderedIDs, id)
		}
		if len(reorderedIDs) != len(reorderableIDs) {
			return fleeterror.NewInvalidArgumentErrorf("submitted %d reorderable IDs but organization has %d reorderable schedules", len(reorderedIDs), len(reorderableIDs))
		}

		allIDs := slices.Concat(reorderedIDs, completedIDs)

		return s.priorityStore.ReorderSchedules(ctx, info.OrganizationID, allIDs)
	})
}

// --- Validation ---

func validateScheduleFields(name string, action pb.ScheduleAction, actionConfig *pb.PowerTargetConfig, scheduleType pb.ScheduleType, recurrence *pb.ScheduleRecurrence) error {
	if err := validateScheduleName(name); err != nil {
		return err
	}
	if action == pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED {
		return fleeterror.NewInvalidArgumentErrorf("action is required")
	}
	if scheduleType == pb.ScheduleType_SCHEDULE_TYPE_UNSPECIFIED {
		return fleeterror.NewInvalidArgumentErrorf("schedule_type is required")
	}

	if err := validateActionConfig(action, actionConfig); err != nil {
		return err
	}

	if scheduleType == pb.ScheduleType_SCHEDULE_TYPE_RECURRING {
		if recurrence == nil {
			return fleeterror.NewInvalidArgumentErrorf("recurrence is required for recurring schedules")
		}
		if err := validateRecurrence(recurrence); err != nil {
			return err
		}
	}

	return nil
}

func validateScheduleName(name string) error {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return fleeterror.NewInvalidArgumentErrorf("name is required")
	case len(trimmed) > maxScheduleNameLength:
		return fleeterror.NewInvalidArgumentErrorf("name must be at most %d characters", maxScheduleNameLength)
	default:
		return nil
	}
}

func validateActionConfig(action pb.ScheduleAction, cfg *pb.PowerTargetConfig) error {
	switch action {
	case pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED:
		return fleeterror.NewInvalidArgumentErrorf("action is required")
	case pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET:
		if cfg == nil {
			return fleeterror.NewInvalidArgumentErrorf("action_config is required for set_power_target action")
		}
		if cfg.Mode == pb.PowerTargetMode_POWER_TARGET_MODE_UNSPECIFIED {
			return fleeterror.NewInvalidArgumentErrorf("power target mode is required")
		}
		if cfg.Mode != pb.PowerTargetMode_POWER_TARGET_MODE_DEFAULT && cfg.Mode != pb.PowerTargetMode_POWER_TARGET_MODE_MAX {
			return fleeterror.NewInvalidArgumentErrorf("invalid power target mode: %v", cfg.Mode)
		}
	case pb.ScheduleAction_SCHEDULE_ACTION_REBOOT, pb.ScheduleAction_SCHEDULE_ACTION_SLEEP:
		if cfg != nil && cfg.Mode != pb.PowerTargetMode_POWER_TARGET_MODE_UNSPECIFIED {
			return fleeterror.NewInvalidArgumentErrorf("action_config is not allowed for %v action", action)
		}
	}
	return nil
}

func validateRecurrence(rec *pb.ScheduleRecurrence) error {
	if rec.Interval != 1 {
		return fleeterror.NewInvalidArgumentErrorf("recurrence interval must be 1 (got %d)", rec.Interval)
	}

	switch rec.Frequency {
	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_UNSPECIFIED:
		return fleeterror.NewInvalidArgumentErrorf("recurrence frequency is required")
	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY:
		// No additional fields required

	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY:
		if len(rec.DaysOfWeek) == 0 {
			return fleeterror.NewInvalidArgumentErrorf("at least one day of week is required for weekly recurrence")
		}
		for _, d := range rec.DaysOfWeek {
			if d == pb.DayOfWeek_DAY_OF_WEEK_UNSPECIFIED || d < pb.DayOfWeek_DAY_OF_WEEK_SUNDAY || d > pb.DayOfWeek_DAY_OF_WEEK_SATURDAY {
				return fleeterror.NewInvalidArgumentErrorf("invalid day of week: %v", d)
			}
		}

	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY:
		if rec.DayOfMonth == nil {
			return fleeterror.NewInvalidArgumentErrorf("day_of_month is required for monthly recurrence")
		}
		if *rec.DayOfMonth < 1 || *rec.DayOfMonth > 31 {
			return fleeterror.NewInvalidArgumentErrorf("day_of_month must be between 1 and 31")
		}

	default:
		return fleeterror.NewInvalidArgumentErrorf("unsupported recurrence frequency: %v", rec.Frequency)
	}

	return nil
}

func validateTargets(targets []*pb.ScheduleTarget) error {
	seen := make(map[string]bool)
	for _, t := range targets {
		if t == nil {
			return fleeterror.NewInvalidArgumentErrorf("target is required")
		}
		if !isValidScheduleTargetType(t.TargetType) {
			return fleeterror.NewInvalidArgumentErrorf("invalid target_type: %v", t.TargetType)
		}
		trimmedID := strings.TrimSpace(t.TargetId)
		if trimmedID == "" {
			return fleeterror.NewInvalidArgumentErrorf("target_id is required")
		}

		switch t.TargetType {
		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK,
			pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP:
			if _, err := strconv.ParseInt(trimmedID, 10, 64); err != nil {
				return fleeterror.NewInvalidArgumentErrorf(
					"invalid target_id for %s: %q is not a valid identifier",
					scheduleTargetTypeToString(t.TargetType), trimmedID,
				)
			}
		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED,
			pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER:
			// UNSPECIFIED already rejected by isValidScheduleTargetType above.
			// MINER IDs are opaque strings (MAC / serial); no numeric parse.
		}

		key := fmt.Sprintf("%v:%s", t.TargetType, trimmedID)
		if seen[key] {
			return fleeterror.NewInvalidArgumentErrorf("duplicate target: %s", key)
		}
		seen[key] = true
	}
	return nil
}

func isValidScheduleTargetType(targetType pb.ScheduleTargetType) bool {
	switch targetType {
	case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED:
		return false
	case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK,
		pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP,
		pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER:
		return true
	default:
		return false
	}
}

// --- Priority normalization ---

func (s *Service) normalizeSchedulePriorities(ctx context.Context, orgID int64) error {
	statuses, err := s.priorityStore.ListScheduleIDStatuses(ctx, orgID)
	if err != nil {
		return err
	}

	var activeIDs, completedIDs []int64
	for _, st := range statuses {
		if st.Status == statusCompleted {
			completedIDs = append(completedIDs, st.ID)
		} else {
			activeIDs = append(activeIDs, st.ID)
		}
	}

	ordered := slices.Concat(activeIDs, completedIDs)
	if schedulePriorityOrderMatches(statuses, ordered) {
		return nil
	}

	return s.priorityStore.ReorderSchedules(ctx, orgID, ordered)
}

func schedulePriorityOrderMatches(statuses []interfaces.ScheduleIDStatus, desired []int64) bool {
	if len(statuses) != len(desired) {
		return false
	}
	for i, st := range statuses {
		if st.ID != desired[i] {
			return false
		}
	}
	return true
}

// --- Helpers ---

func (s *Service) fetchScheduleWithTargets(ctx context.Context, orgID, scheduleID int64) (*pb.Schedule, error) {
	sched, err := s.store.GetSchedule(ctx, orgID, scheduleID)
	if err != nil {
		return nil, err
	}
	targets, err := s.targetStore.GetScheduleTargets(ctx, orgID, scheduleID)
	if err != nil {
		return nil, err
	}
	sched.Targets = targets
	return sched, nil
}

func scheduleTargetTypeToString(t pb.ScheduleTargetType) string {
	switch t {
	case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED:
		return "unknown"
	case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK:
		return "rack"
	case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP:
		return "group"
	case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER:
		return "miner"
	default:
		return "unknown"
	}
}

func (s *Service) logActivity(ctx context.Context, info *session.Info, eventType, description string) {
	if s.activitySvc == nil {
		return
	}
	s.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategorySchedule,
		Type:           eventType,
		Description:    description,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})
}
