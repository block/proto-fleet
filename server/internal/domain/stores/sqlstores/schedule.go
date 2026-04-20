package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/sqlc-dev/pqtype"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.ScheduleStore = &SQLScheduleStore{}
var _ interfaces.ScheduleTargetStore = &SQLScheduleStore{}
var _ interfaces.SchedulePriorityStore = &SQLScheduleStore{}
var _ interfaces.ScheduleProcessorStore = &SQLScheduleStore{}

type SQLScheduleStore struct {
	SQLConnectionManager
}

func NewSQLScheduleStore(conn *sql.DB) *SQLScheduleStore {
	return &SQLScheduleStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLScheduleStore) GetSchedule(ctx context.Context, orgID, scheduleID int64) (*pb.Schedule, error) {
	row, err := s.GetQueries(ctx).GetSchedule(ctx, sqlc.GetScheduleParams{OrgID: orgID, ID: scheduleID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("schedule not found: %d", scheduleID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get schedule: %v", err)
	}
	return convertGetScheduleRowToProtoSchedule(row)
}

func (s *SQLScheduleStore) ListSchedules(ctx context.Context, orgID int64, status, action string) ([]*pb.Schedule, error) {
	rows, err := s.GetQueries(ctx).ListSchedules(ctx, sqlc.ListSchedulesParams{
		OrgID:  orgID,
		Status: toNullString(status),
		Action: toNullString(action),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list schedules: %v", err)
	}

	result := make([]*pb.Schedule, 0, len(rows))
	for _, row := range rows {
		sched, err := convertListSchedulesRowToProtoSchedule(row)
		if err != nil {
			return nil, err
		}
		result = append(result, sched)
	}
	return result, nil
}

func (s *SQLScheduleStore) CreateSchedule(ctx context.Context, orgID int64, sched *pb.Schedule) (int64, error) {
	actionConfig, err := marshalActionConfig(sched.ActionConfig)
	if err != nil {
		return 0, err
	}
	recurrence, err := marshalRecurrence(sched.Recurrence)
	if err != nil {
		return 0, err
	}

	startDate, err := parseScheduleDate(sched.StartDate)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid start_date: %v", err)
	}
	startTime, err := parseScheduleTime(sched.StartTime)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid start_time: %v", err)
	}

	endTime, err := parseNullTime(sched.EndTime)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid end_time: %v", err)
	}
	endDate, err := parseNullDate(sched.EndDate)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid end_date: %v", err)
	}

	id, err := s.GetQueries(ctx).CreateSchedule(ctx, sqlc.CreateScheduleParams{
		OrgID:        orgID,
		Name:         sched.Name,
		Action:       scheduleActionToString(sched.Action),
		ActionConfig: actionConfig,
		ScheduleType: scheduleTypeToString(sched.ScheduleType),
		Recurrence:   recurrence,
		StartDate:    startDate,
		StartTime:    startTime,
		EndTime:      endTime,
		EndDate:      endDate,
		Timezone:     sched.Timezone,
		Status:       "active",
		Priority:     sched.Priority,
		CreatedBy:    sched.CreatedBy,
		NextRunAt:    timestampToNullTime(sched.NextRunAt),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to create schedule: %v", err)
	}
	return id, nil
}

func (s *SQLScheduleStore) UpdateSchedule(ctx context.Context, orgID int64, sched *pb.Schedule) (int64, error) {
	actionConfig, err := marshalActionConfig(sched.ActionConfig)
	if err != nil {
		return 0, err
	}
	recurrence, err := marshalRecurrence(sched.Recurrence)
	if err != nil {
		return 0, err
	}

	startDate, err := parseScheduleDate(sched.StartDate)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid start_date: %v", err)
	}
	startTime, err := parseScheduleTime(sched.StartTime)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid start_time: %v", err)
	}

	endTime, err := parseNullTime(sched.EndTime)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid end_time: %v", err)
	}
	endDate, err := parseNullDate(sched.EndDate)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid end_date: %v", err)
	}

	rows, err := s.GetQueries(ctx).UpdateSchedule(ctx, sqlc.UpdateScheduleParams{
		Name:         sched.Name,
		Action:       scheduleActionToString(sched.Action),
		ActionConfig: actionConfig,
		ScheduleType: scheduleTypeToString(sched.ScheduleType),
		Recurrence:   recurrence,
		StartDate:    startDate,
		StartTime:    startTime,
		EndTime:      endTime,
		EndDate:      endDate,
		Timezone:     sched.Timezone,
		NextRunAt:    timestampToNullTime(sched.NextRunAt),
		Status:       scheduleStatusToString(sched.Status),
		OrgID:        orgID,
		ID:           sched.Id,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to update schedule: %v", err)
	}
	return rows, nil
}

func (s *SQLScheduleStore) SoftDeleteSchedule(ctx context.Context, orgID, scheduleID int64) (int64, error) {
	rows, err := s.GetQueries(ctx).SoftDeleteSchedule(ctx, sqlc.SoftDeleteScheduleParams{OrgID: orgID, ID: scheduleID})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to delete schedule: %v", err)
	}
	return rows, nil
}

func (s *SQLScheduleStore) GetMaxPriority(ctx context.Context, orgID int64) (int32, error) {
	p, err := s.GetQueries(ctx).GetMaxPriority(ctx, orgID)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to get max priority: %v", err)
	}
	return p, nil
}

func (s *SQLScheduleStore) LockSchedulePriority(ctx context.Context, orgID int64) error {
	if err := s.GetQueries(ctx).LockSchedulePriority(ctx, strconv.FormatInt(orgID, 10)); err != nil {
		return fleeterror.NewInternalErrorf("failed to acquire schedule priority lock: %v", err)
	}
	return nil
}

func (s *SQLScheduleStore) ReorderSchedules(ctx context.Context, orgID int64, ids []int64) error {
	q := s.GetQueries(ctx)
	if err := q.NegateSchedulePriorities(ctx, sqlc.NegateSchedulePrioritiesParams{OrgID: orgID, Ids: ids}); err != nil {
		return fleeterror.NewInternalErrorf("failed to negate priorities: %v", err)
	}
	if err := q.SetSchedulePriorities(ctx, sqlc.SetSchedulePrioritiesParams{OrgID: orgID, Ids: ids}); err != nil {
		return fleeterror.NewInternalErrorf("failed to set priorities: %v", err)
	}
	return nil
}

func (s *SQLScheduleStore) ListScheduleIDStatuses(ctx context.Context, orgID int64) ([]interfaces.ScheduleIDStatus, error) {
	rows, err := s.GetQueries(ctx).ListScheduleIDStatuses(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list schedule statuses: %v", err)
	}
	result := make([]interfaces.ScheduleIDStatus, len(rows))
	for i, row := range rows {
		result[i] = interfaces.ScheduleIDStatus{ID: row.ID, Status: row.Status}
	}
	return result, nil
}

func (s *SQLScheduleStore) CreateScheduleTarget(ctx context.Context, orgID, scheduleID int64, targetType, targetID string) error {
	if err := s.GetQueries(ctx).CreateScheduleTarget(ctx, sqlc.CreateScheduleTargetParams{
		ScheduleID: scheduleID,
		TargetType: targetType,
		TargetID:   targetID,
		OrgID:      orgID,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to create schedule target: %v", err)
	}
	return nil
}

func (s *SQLScheduleStore) GetScheduleTargets(ctx context.Context, orgID, scheduleID int64) ([]*pb.ScheduleTarget, error) {
	rows, err := s.GetQueries(ctx).GetScheduleTargets(ctx, sqlc.GetScheduleTargetsParams{OrgID: orgID, ScheduleID: scheduleID})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get schedule targets: %v", err)
	}
	return convertScheduleTargets(rows), nil
}

func (s *SQLScheduleStore) GetScheduleTargetsByScheduleIDs(ctx context.Context, orgID int64, scheduleIDs []int64) (map[int64][]*pb.ScheduleTarget, error) {
	rows, err := s.GetQueries(ctx).GetScheduleTargetsByScheduleIDs(ctx, sqlc.GetScheduleTargetsByScheduleIDsParams{
		OrgID:       orgID,
		ScheduleIds: scheduleIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get schedule targets by IDs: %v", err)
	}

	result := make(map[int64][]*pb.ScheduleTarget, len(scheduleIDs))
	for _, row := range rows {
		result[row.ScheduleID] = append(result[row.ScheduleID], &pb.ScheduleTarget{
			TargetType: stringToScheduleTargetType(row.TargetType),
			TargetId:   row.TargetID,
		})
	}
	return result, nil
}

func (s *SQLScheduleStore) DeleteScheduleTargets(ctx context.Context, orgID, scheduleID int64) error {
	if err := s.GetQueries(ctx).DeleteScheduleTargets(ctx, sqlc.DeleteScheduleTargetsParams{OrgID: orgID, ScheduleID: scheduleID}); err != nil {
		return fleeterror.NewInternalErrorf("failed to delete schedule targets: %v", err)
	}
	return nil
}

func (s *SQLScheduleStore) PauseActiveSchedule(ctx context.Context, orgID, scheduleID int64) (int64, error) {
	rows, err := s.GetQueries(ctx).PauseActiveSchedule(ctx, sqlc.PauseActiveScheduleParams{OrgID: orgID, ID: scheduleID})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to pause schedule: %v", err)
	}
	return rows, nil
}

func (s *SQLScheduleStore) ResumePausedSchedule(ctx context.Context, orgID, scheduleID int64, status string, nextRunAt *int64) (int64, error) {
	var nra sql.NullTime
	if nextRunAt != nil {
		nra = sql.NullTime{Time: time.Unix(*nextRunAt, 0), Valid: true}
	}
	rows, err := s.GetQueries(ctx).ResumePausedSchedule(ctx, sqlc.ResumePausedScheduleParams{
		Status:    status,
		NextRunAt: nra,
		OrgID:     orgID,
		ID:        scheduleID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to resume schedule: %v", err)
	}
	return rows, nil
}

func (s *SQLScheduleStore) GetActiveSchedules(ctx context.Context) ([]interfaces.ScheduleWithOrg, error) {
	rows, err := s.GetQueries(ctx).GetActiveSchedules(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get active schedules: %v", err)
	}
	result := make([]interfaces.ScheduleWithOrg, 0, len(rows))
	for _, row := range rows {
		sched, err := convertToProtoSchedule(row)
		if err != nil {
			return nil, err
		}
		result = append(result, interfaces.ScheduleWithOrg{Schedule: sched, OrgID: row.OrgID})
	}
	return result, nil
}

func (s *SQLScheduleStore) GetRunningPowerTargetSchedules(ctx context.Context, orgID int64) ([]*pb.Schedule, error) {
	rows, err := s.GetQueries(ctx).GetRunningPowerTargetSchedules(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get running power target schedules: %v", err)
	}
	result := make([]*pb.Schedule, 0, len(rows))
	for _, row := range rows {
		sched, err := convertToProtoSchedule(row)
		if err != nil {
			return nil, err
		}
		result = append(result, sched)
	}
	return result, nil
}

func (s *SQLScheduleStore) UpdateScheduleAfterRun(ctx context.Context, scheduleID int64, lastRunAt, nextRunAt *int64, status string) error {
	var lra, nra sql.NullTime
	if lastRunAt != nil {
		lra = sql.NullTime{Time: time.Unix(*lastRunAt, 0), Valid: true}
	}
	if nextRunAt != nil {
		nra = sql.NullTime{Time: time.Unix(*nextRunAt, 0), Valid: true}
	}
	return s.GetQueries(ctx).UpdateScheduleAfterRun(ctx, sqlc.UpdateScheduleAfterRunParams{
		LastRunAt: lra,
		NextRunAt: nra,
		Status:    status,
		ID:        scheduleID,
	})
}

func (s *SQLScheduleStore) SetScheduleRunning(ctx context.Context, scheduleID int64) (int64, error) {
	rows, err := s.GetQueries(ctx).SetScheduleRunning(ctx, scheduleID)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to set schedule running: %v", err)
	}
	return rows, nil
}

func (s *SQLScheduleStore) GetScheduleByID(ctx context.Context, scheduleID int64) (*interfaces.ScheduleWithOrg, error) {
	row, err := s.GetQueries(ctx).GetScheduleByIDForProcessor(ctx, scheduleID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get schedule by ID: %v", err)
	}
	sched, err := convertToProtoSchedule(row)
	if err != nil {
		return nil, err
	}
	return &interfaces.ScheduleWithOrg{Schedule: sched, OrgID: row.OrgID}, nil
}

func (s *SQLScheduleStore) RevertScheduleToActive(ctx context.Context, scheduleID int64) error {
	if err := s.GetQueries(ctx).RevertScheduleToActive(ctx, scheduleID); err != nil {
		return fleeterror.NewInternalErrorf("failed to revert schedule %d to active: %v", scheduleID, err)
	}
	return nil
}

// --- Conversion helpers ---

func convertToProtoSchedule(row sqlc.Schedule) (*pb.Schedule, error) {
	sched := &pb.Schedule{
		Id:           row.ID,
		Name:         row.Name,
		Action:       stringToScheduleAction(row.Action),
		ScheduleType: stringToScheduleType(row.ScheduleType),
		StartDate:    row.StartDate.Format("2006-01-02"),
		StartTime:    normalizeScheduleTimeString(row.StartTime),
		Timezone:     row.Timezone,
		Status:       stringToScheduleStatus(row.Status),
		Priority:     row.Priority,
		CreatedBy:    row.CreatedBy,
		CreatedAt:    timestamppb.New(row.CreatedAt),
		UpdatedAt:    timestamppb.New(row.UpdatedAt),
	}

	if row.EndTime.Valid {
		sched.EndTime = normalizeScheduleTimeString(row.EndTime.String)
	}
	if row.EndDate.Valid {
		sched.EndDate = row.EndDate.Time.Format("2006-01-02")
	}
	if row.LastRunAt.Valid {
		sched.LastRunAt = timestamppb.New(row.LastRunAt.Time)
	}
	if row.NextRunAt.Valid {
		sched.NextRunAt = timestamppb.New(row.NextRunAt.Time)
	}

	if len(row.ActionConfig) > 0 && string(row.ActionConfig) != "{}" {
		var cfg pb.PowerTargetConfig
		if err := json.Unmarshal(row.ActionConfig, &cfg); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to unmarshal action_config: %v", err)
		}
		sched.ActionConfig = &cfg
	}

	if row.Recurrence.Valid && len(row.Recurrence.RawMessage) > 0 {
		var rec pb.ScheduleRecurrence
		if err := json.Unmarshal(row.Recurrence.RawMessage, &rec); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to unmarshal recurrence: %v", err)
		}
		sched.Recurrence = &rec
	}

	return sched, nil
}

func convertGetScheduleRowToProtoSchedule(row sqlc.GetScheduleRow) (*pb.Schedule, error) {
	sched, err := convertToProtoSchedule(sqlc.Schedule{
		ID:           row.ID,
		OrgID:        row.OrgID,
		Name:         row.Name,
		Action:       row.Action,
		ActionConfig: row.ActionConfig,
		ScheduleType: row.ScheduleType,
		Recurrence:   row.Recurrence,
		StartDate:    row.StartDate,
		StartTime:    row.StartTime,
		EndTime:      row.EndTime,
		EndDate:      row.EndDate,
		Timezone:     row.Timezone,
		Status:       row.Status,
		Priority:     row.Priority,
		CreatedBy:    row.CreatedBy,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		DeletedAt:    row.DeletedAt,
		LastRunAt:    row.LastRunAt,
		NextRunAt:    row.NextRunAt,
	})
	if err != nil {
		return nil, err
	}
	sched.CreatedByUsername = row.CreatedByUsername.String
	return sched, nil
}

func convertListSchedulesRowToProtoSchedule(row sqlc.ListSchedulesRow) (*pb.Schedule, error) {
	sched, err := convertToProtoSchedule(sqlc.Schedule{
		ID:           row.ID,
		OrgID:        row.OrgID,
		Name:         row.Name,
		Action:       row.Action,
		ActionConfig: row.ActionConfig,
		ScheduleType: row.ScheduleType,
		Recurrence:   row.Recurrence,
		StartDate:    row.StartDate,
		StartTime:    row.StartTime,
		EndTime:      row.EndTime,
		EndDate:      row.EndDate,
		Timezone:     row.Timezone,
		Status:       row.Status,
		Priority:     row.Priority,
		CreatedBy:    row.CreatedBy,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		DeletedAt:    row.DeletedAt,
		LastRunAt:    row.LastRunAt,
		NextRunAt:    row.NextRunAt,
	})
	if err != nil {
		return nil, err
	}
	sched.CreatedByUsername = row.CreatedByUsername.String
	return sched, nil
}

func convertScheduleTargets(rows []sqlc.ScheduleTarget) []*pb.ScheduleTarget {
	targets := make([]*pb.ScheduleTarget, len(rows))
	for i, row := range rows {
		targets[i] = &pb.ScheduleTarget{
			TargetType: stringToScheduleTargetType(row.TargetType),
			TargetId:   row.TargetID,
		}
	}
	return targets
}

// --- Enum converters ---

func scheduleActionToString(a pb.ScheduleAction) string {
	switch a {
	case pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED:
		return "unknown"
	case pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET:
		return "set_power_target"
	case pb.ScheduleAction_SCHEDULE_ACTION_REBOOT:
		return "reboot"
	case pb.ScheduleAction_SCHEDULE_ACTION_SLEEP:
		return "sleep"
	default:
		return "unknown"
	}
}

func stringToScheduleAction(s string) pb.ScheduleAction {
	switch s {
	case "set_power_target":
		return pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET
	case "reboot":
		return pb.ScheduleAction_SCHEDULE_ACTION_REBOOT
	case "sleep":
		return pb.ScheduleAction_SCHEDULE_ACTION_SLEEP
	default:
		return pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED
	}
}

func scheduleTypeToString(t pb.ScheduleType) string {
	switch t {
	case pb.ScheduleType_SCHEDULE_TYPE_UNSPECIFIED:
		return "unknown"
	case pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME:
		return "one_time"
	case pb.ScheduleType_SCHEDULE_TYPE_RECURRING:
		return "recurring"
	default:
		return "unknown"
	}
}

func stringToScheduleType(s string) pb.ScheduleType {
	switch s {
	case "one_time":
		return pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME
	case "recurring":
		return pb.ScheduleType_SCHEDULE_TYPE_RECURRING
	default:
		return pb.ScheduleType_SCHEDULE_TYPE_UNSPECIFIED
	}
}

func scheduleStatusToString(st pb.ScheduleStatus) string {
	switch st {
	case pb.ScheduleStatus_SCHEDULE_STATUS_UNSPECIFIED:
		return "active"
	case pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE:
		return "active"
	case pb.ScheduleStatus_SCHEDULE_STATUS_PAUSED:
		return "paused"
	case pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING:
		return "running"
	case pb.ScheduleStatus_SCHEDULE_STATUS_COMPLETED:
		return "completed"
	default:
		return "active"
	}
}

func stringToScheduleStatus(s string) pb.ScheduleStatus {
	switch s {
	case "active":
		return pb.ScheduleStatus_SCHEDULE_STATUS_ACTIVE
	case "paused":
		return pb.ScheduleStatus_SCHEDULE_STATUS_PAUSED
	case "running":
		return pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING
	case "completed":
		return pb.ScheduleStatus_SCHEDULE_STATUS_COMPLETED
	default:
		return pb.ScheduleStatus_SCHEDULE_STATUS_UNSPECIFIED
	}
}

func stringToScheduleTargetType(s string) pb.ScheduleTargetType {
	switch s {
	case "rack":
		return pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK
	case "group":
		return pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP
	case "miner":
		return pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER
	default:
		return pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED
	}
}

// --- Marshal/unmarshal helpers ---

func marshalActionConfig(cfg *pb.PowerTargetConfig) (json.RawMessage, error) {
	if cfg == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to marshal action_config: %v", err)
	}
	return b, nil
}

func marshalRecurrence(rec *pb.ScheduleRecurrence) (pqtype.NullRawMessage, error) {
	if rec == nil {
		return pqtype.NullRawMessage{}, nil
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return pqtype.NullRawMessage{}, fleeterror.NewInternalErrorf("failed to marshal recurrence: %v", err)
	}
	return pqtype.NullRawMessage{RawMessage: b, Valid: true}, nil
}

func parseScheduleDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: %w", s, err)
	}
	return t, nil
}

func parseScheduleTime(s string) (string, error) {
	for _, layout := range []string{"15:04", "15:04:05"} {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t.Format("15:04"), nil
		}
	}
	return "", fmt.Errorf("invalid time %q", s)
}

func parseNullTime(s string) (sql.NullString, error) {
	if s == "" {
		return sql.NullString{}, nil
	}
	parsed, err := parseScheduleTime(s)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: parsed, Valid: true}, nil
}

func parseNullDate(s string) (sql.NullTime, error) {
	if s == "" {
		return sql.NullTime{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return sql.NullTime{}, fmt.Errorf("invalid date %q: %w", s, err)
	}
	return sql.NullTime{Time: t, Valid: true}, nil
}

func timestampToNullTime(ts *timestamppb.Timestamp) sql.NullTime {
	if ts == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: ts.AsTime(), Valid: true}
}

func normalizeScheduleTimeString(value string) string {
	parsed, err := parseScheduleTime(value)
	if err != nil {
		return value
	}
	return parsed
}
