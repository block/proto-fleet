package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	curtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/schedulewindow"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const scheduleDateLayout = "2006-01-02"

var (
	_ curtailment.AutomationExecutionStore = (*SQLCurtailmentStore)(nil)
	_ curtailment.ScheduleAutomationStore  = (*SQLCurtailmentStore)(nil)
)

func (s *SQLCurtailmentStore) ListEnabledScheduleAutomationRules(ctx context.Context) ([]models.ScheduledAutomationRule, error) {
	rows, err := s.GetQueries(ctx).ListEnabledCurtailmentScheduleRules(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list enabled curtailment schedule rules: %v", err)
	}
	out := make([]models.ScheduledAutomationRule, 0, len(rows))
	for _, row := range rows {
		schedule := automationScheduleFromRow(sqlc.CurtailmentAutomationSchedule{
			RuleID:        row.RuleID,
			ScheduleType:  row.ScheduleType,
			Timezone:      row.Timezone,
			StartDate:     row.StartDate,
			StartTime:     row.StartTime,
			EndTime:       row.EndTime,
			EndDate:       row.EndDate,
			Frequency:     row.Frequency,
			DaysOfWeek:    row.DaysOfWeek,
			DayOfMonth:    row.DayOfMonth,
			UntilDate:     row.UntilDate,
			ServiceUserID: row.ServiceUserID,
			Revision:      row.Revision,
		})
		out = append(out, models.ScheduledAutomationRule{RuleID: row.RuleID, OrgID: row.OrgID, Schedule: *schedule})
	}
	return out, nil
}

// attachAutomationSchedules loads the schedule of every SCHEDULE rule in rules.
func (s *SQLCurtailmentStore) attachAutomationSchedules(ctx context.Context, orgID int64, rules []*models.AutomationRule) error {
	hasSchedule := false
	for _, rule := range rules {
		if rule != nil && rule.TriggerType == models.AutomationTriggerTypeSchedule {
			hasSchedule = true
			break
		}
	}
	if !hasSchedule {
		return nil
	}
	rows, err := s.GetQueries(ctx).ListCurtailmentAutomationSchedulesByOrg(ctx, orgID)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to list curtailment automation schedules: %v", err)
	}
	byRule := make(map[int64]*models.AutomationSchedule, len(rows))
	for _, row := range rows {
		byRule[row.RuleID] = automationScheduleFromRow(row)
	}
	for _, rule := range rules {
		if rule != nil && rule.TriggerType == models.AutomationTriggerTypeSchedule {
			rule.Schedule = byRule[rule.ID]
		}
	}
	return nil
}

func (s *SQLCurtailmentStore) attachAutomationSchedule(ctx context.Context, rule *models.AutomationRule) error {
	if rule == nil || rule.TriggerType != models.AutomationTriggerTypeSchedule {
		return nil
	}
	row, err := s.GetQueries(ctx).GetCurtailmentAutomationScheduleByRule(ctx, sqlc.GetCurtailmentAutomationScheduleByRuleParams{
		RuleID: rule.ID,
		OrgID:  rule.OrgID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to get curtailment automation schedule: %v", err)
	}
	rule.Schedule = automationScheduleFromRow(row)
	return nil
}

// capScheduledEventAtWindowEnd rejects a scheduled start whose window ended
// before insertion and caps the event at the window's end. The event's
// duration counts from its created_at, the transaction start, so the cap is
// measured from txStart rather than from after the transaction's lock waits.
func capScheduledEventAtWindowEnd(event *models.InsertEventParams, txStart, now time.Time) error {
	if event.AutomationWindowEndAt.IsZero() {
		return nil
	}
	if !now.Before(event.AutomationWindowEndAt) {
		return fleeterror.NewFailedPreconditionError("curtailment schedule window ended before its event could start")
	}
	capSeconds := int32(min(math.Ceil(event.AutomationWindowEndAt.Sub(txStart).Seconds()), math.MaxInt32))
	if event.MaxDurationSeconds == nil || capSeconds < *event.MaxDurationSeconds {
		event.MaxDurationSeconds = &capSeconds
	}
	return nil
}

func upsertAutomationSchedule(ctx context.Context, q sqlc.Querier, ruleID int64, schedule *models.AutomationSchedule) error {
	if schedule == nil {
		return fleeterror.NewInvalidArgumentError("schedule rules require a schedule")
	}
	params, err := automationScheduleParams(ruleID, *schedule)
	if err != nil {
		return err
	}
	if err := q.UpsertCurtailmentAutomationSchedule(ctx, params); err != nil {
		return mapAutomationRuleWriteError("save schedule for", err)
	}
	return nil
}

func automationScheduleParams(ruleID int64, schedule models.AutomationSchedule) (sqlc.UpsertCurtailmentAutomationScheduleParams, error) {
	def := schedule.Definition
	startDate, err := parseScheduleDate(def.StartDate)
	if err != nil {
		return sqlc.UpsertCurtailmentAutomationScheduleParams{}, fleeterror.NewInvalidArgumentError(err.Error())
	}
	endDate, err := parseNullDate(def.EndDate)
	if err != nil {
		return sqlc.UpsertCurtailmentAutomationScheduleParams{}, fleeterror.NewInvalidArgumentError(err.Error())
	}
	untilDate, err := parseNullDate(def.UntilDate)
	if err != nil {
		return sqlc.UpsertCurtailmentAutomationScheduleParams{}, fleeterror.NewInvalidArgumentError(err.Error())
	}
	days := make([]int32, 0, len(def.DaysOfWeek))
	for _, day := range def.DaysOfWeek {
		days = append(days, int32(day)) //nolint:gosec // weekdays are 0-6
	}
	params := sqlc.UpsertCurtailmentAutomationScheduleParams{
		RuleID:        ruleID,
		ScheduleType:  string(def.Type),
		Timezone:      def.Timezone,
		StartDate:     startDate,
		StartTime:     def.StartTime,
		EndTime:       def.EndTime,
		EndDate:       endDate,
		Frequency:     sql.NullString{String: string(def.Frequency), Valid: def.Frequency != ""},
		DaysOfWeek:    days,
		UntilDate:     untilDate,
		ServiceUserID: schedule.ServiceUserID,
	}
	if def.DayOfMonth != 0 {
		params.DayOfMonth = sql.NullInt16{Int16: int16(def.DayOfMonth), Valid: true} //nolint:gosec // validated to 1-31
	}
	return params, nil
}

func automationScheduleFromRow(row sqlc.CurtailmentAutomationSchedule) *models.AutomationSchedule {
	def := schedulewindow.Definition{
		Type:      schedulewindow.Type(row.ScheduleType),
		Timezone:  row.Timezone,
		StartDate: row.StartDate.Format(scheduleDateLayout),
		StartTime: normalizeScheduleTimeString(row.StartTime),
		EndTime:   normalizeScheduleTimeString(row.EndTime),
	}
	if row.EndDate.Valid {
		def.EndDate = row.EndDate.Time.Format(scheduleDateLayout)
	}
	if row.Frequency.Valid {
		def.Frequency = schedulewindow.Frequency(row.Frequency.String)
	}
	for _, day := range row.DaysOfWeek {
		def.DaysOfWeek = append(def.DaysOfWeek, time.Weekday(day))
	}
	if row.DayOfMonth.Valid {
		def.DayOfMonth = int(row.DayOfMonth.Int16)
	}
	if row.UntilDate.Valid {
		def.UntilDate = row.UntilDate.Time.Format(scheduleDateLayout)
	}
	return &models.AutomationSchedule{Definition: def, ServiceUserID: row.ServiceUserID, Revision: row.Revision}
}
