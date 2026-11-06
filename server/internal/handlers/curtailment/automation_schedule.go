package curtailment

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/schedulewindow"
)

// automationScheduleFromProto maps a request schedule; the saving user becomes
// the schedule's service user.
func automationScheduleFromProto(msg *pb.CurtailmentSchedule, userID int64) *models.AutomationSchedule {
	if msg == nil {
		return nil
	}
	def := schedulewindow.Definition{
		Timezone:   msg.GetTimezone(),
		StartDate:  msg.GetStartDate(),
		StartTime:  msg.GetStartTime(),
		EndTime:    msg.GetEndTime(),
		EndDate:    msg.GetEndDate(),
		DayOfMonth: int(msg.GetDayOfMonth()),
		UntilDate:  msg.GetUntilDate(),
	}
	switch msg.GetScheduleType() {
	case pb.CurtailmentScheduleType_CURTAILMENT_SCHEDULE_TYPE_ONE_TIME:
		def.Type = schedulewindow.TypeOneTime
	case pb.CurtailmentScheduleType_CURTAILMENT_SCHEDULE_TYPE_RECURRING:
		def.Type = schedulewindow.TypeRecurring
	case pb.CurtailmentScheduleType_CURTAILMENT_SCHEDULE_TYPE_UNSPECIFIED:
	}
	switch msg.GetFrequency() {
	case pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_DAILY:
		def.Frequency = schedulewindow.FrequencyDaily
	case pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_WEEKLY:
		def.Frequency = schedulewindow.FrequencyWeekly
	case pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_MONTHLY:
		def.Frequency = schedulewindow.FrequencyMonthly
	case pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_UNSPECIFIED:
	}
	for _, day := range msg.GetDaysOfWeek() {
		// Proto days start at SUNDAY = 1; time.Sunday is 0.
		def.DaysOfWeek = append(def.DaysOfWeek, time.Weekday(day-1))
	}
	return &models.AutomationSchedule{Definition: def, ServiceUserID: userID}
}

func automationScheduleProto(schedule *models.AutomationSchedule) *pb.CurtailmentSchedule {
	if schedule == nil {
		return nil
	}
	def := schedule.Definition
	out := &pb.CurtailmentSchedule{
		Timezone:   def.Timezone,
		StartDate:  def.StartDate,
		StartTime:  def.StartTime,
		EndTime:    def.EndTime,
		EndDate:    def.EndDate,
		DayOfMonth: uint32(max(def.DayOfMonth, 0)), //nolint:gosec // bounded to 0-31
		UntilDate:  def.UntilDate,
	}
	switch def.Type {
	case schedulewindow.TypeOneTime:
		out.ScheduleType = pb.CurtailmentScheduleType_CURTAILMENT_SCHEDULE_TYPE_ONE_TIME
	case schedulewindow.TypeRecurring:
		out.ScheduleType = pb.CurtailmentScheduleType_CURTAILMENT_SCHEDULE_TYPE_RECURRING
	}
	switch def.Frequency {
	case schedulewindow.FrequencyDaily:
		out.Frequency = pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_DAILY
	case schedulewindow.FrequencyWeekly:
		out.Frequency = pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_WEEKLY
	case schedulewindow.FrequencyMonthly:
		out.Frequency = pb.CurtailmentScheduleFrequency_CURTAILMENT_SCHEDULE_FREQUENCY_MONTHLY
	}
	for _, day := range def.DaysOfWeek {
		out.DaysOfWeek = append(out.DaysOfWeek, pb.CurtailmentScheduleDayOfWeek(day+1)) //nolint:gosec // weekdays are 0-6
	}
	return out
}

// setAutomationScheduleWindow reports the window in progress, or else the next
// one, so clients can show when the schedule curtails.
func setAutomationScheduleWindow(out *pb.CurtailmentAutomationRule, schedule *models.AutomationSchedule, now time.Time) {
	if out == nil || schedule == nil {
		return
	}
	window, err := schedule.Definition.CurrentOrNext(now)
	if err != nil || window == nil {
		return
	}
	out.WindowStartAt = timestamppb.New(window.Start)
	out.WindowEndAt = timestamppb.New(window.End)
}
