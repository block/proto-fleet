package schedule

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

var protoDayToCron = map[pb.DayOfWeek]string{
	pb.DayOfWeek_DAY_OF_WEEK_SUNDAY:    "0",
	pb.DayOfWeek_DAY_OF_WEEK_MONDAY:    "1",
	pb.DayOfWeek_DAY_OF_WEEK_TUESDAY:   "2",
	pb.DayOfWeek_DAY_OF_WEEK_WEDNESDAY: "3",
	pb.DayOfWeek_DAY_OF_WEEK_THURSDAY:  "4",
	pb.DayOfWeek_DAY_OF_WEEK_FRIDAY:    "5",
	pb.DayOfWeek_DAY_OF_WEEK_SATURDAY:  "6",
}

// ToCronExpression converts schedule recurrence fields into a cron expression
// with a CRON_TZ= prefix for timezone-aware scheduling.
func ToCronExpression(freq pb.RecurrenceFrequency, startTime, timezone string, daysOfWeek []pb.DayOfWeek, dayOfMonth *int32) (string, error) {
	t, err := parseClockValue(startTime)
	if err != nil {
		return "", fmt.Errorf("invalid start_time %q: %w", startTime, err)
	}

	var fields string
	switch freq {
	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_UNSPECIFIED:
		return "", fmt.Errorf("unsupported frequency: %v", freq)
	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY:
		fields = fmt.Sprintf("%d %d * * *", t.Minute(), t.Hour())

	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY:
		days := make([]string, 0, len(daysOfWeek))
		for _, d := range daysOfWeek {
			if s, ok := protoDayToCron[d]; ok {
				days = append(days, s)
			}
		}
		if len(days) == 0 {
			return "", fmt.Errorf("weekly recurrence requires at least one day")
		}
		fields = fmt.Sprintf("%d %d * * %s", t.Minute(), t.Hour(), strings.Join(days, ","))

	case pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY:
		if dayOfMonth == nil {
			return "", fmt.Errorf("monthly recurrence requires day_of_month")
		}
		// Standard cron behavior: months without this day are skipped (e.g., day 31
		// skips Feb, Apr, Jun, Sep, Nov). Day-of-month clamping is deferred to v1.1.
		fields = fmt.Sprintf("%d %d %d * *", t.Minute(), t.Hour(), *dayOfMonth)

	default:
		return "", fmt.Errorf("unsupported frequency: %v", freq)
	}

	return fmt.Sprintf("CRON_TZ=%s %s", timezone, fields), nil
}

// ComputeNextRun returns the next run time for a schedule after the given time.
// Returns nil if no future runs remain (one-time past, or recurring past end_date).
func ComputeNextRun(sched *pb.Schedule, after time.Time) (*time.Time, error) {
	if sched.ScheduleType == pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME {
		return computeNextRunOneTime(sched, after)
	}
	return computeNextRunRecurring(sched, after)
}

func computeNextRunOneTime(sched *pb.Schedule, after time.Time) (*time.Time, error) {
	t, err := ParseScheduleTime(sched.StartDate, sched.StartTime, sched.Timezone)
	if err != nil {
		return nil, err
	}

	if sched.EndDate != "" {
		deadline, err := parseDateInLocation(sched.EndDate, sched.Timezone)
		if err != nil {
			return nil, err
		}
		if t.After(endOfDay(deadline)) {
			return nil, nil
		}
	}

	if !t.After(after) {
		return nil, nil
	}
	return &t, nil
}

func computeNextRunRecurring(sched *pb.Schedule, after time.Time) (*time.Time, error) {
	rec := sched.Recurrence
	if rec == nil {
		return nil, fmt.Errorf("recurring schedule missing recurrence")
	}

	cronExpr, err := ToCronExpression(rec.Frequency, sched.StartTime, sched.Timezone, rec.DaysOfWeek, rec.DayOfMonth)
	if err != nil {
		return nil, err
	}

	parsed, err := cronParser.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	startDate, err := parseDateInLocation(sched.StartDate, sched.Timezone)
	if err != nil {
		return nil, err
	}
	earliest := after
	if startDate.After(after) {
		earliest = startDate.Add(-time.Second)
	}

	next := parsed.Next(earliest)

	if sched.EndDate != "" {
		deadline, err := parseDateInLocation(sched.EndDate, sched.Timezone)
		if err != nil {
			return nil, err
		}
		if next.After(endOfDay(deadline)) {
			return nil, nil
		}
	}

	return &next, nil
}

// ParseScheduleTime parses date (YYYY-MM-DD) + time (HH:MM) + timezone into a time.Time.
func ParseScheduleTime(date, timeStr, timezone string) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	normalizedTime, err := normalizeClockValue(timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date/time %q %q: %w", date, timeStr, err)
	}

	dt, err := time.ParseInLocation("2006-01-02 15:04", date+" "+normalizedTime, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date/time %q %q: %w", date, timeStr, err)
	}
	return dt, nil
}

func parseDateInLocation(date, timezone string) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	dt, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: %w", date, err)
	}
	return dt, nil
}

// endOfDay returns the last second of the calendar day in the local timezone.
// Uses time.Date normalization to handle DST transitions correctly without
// spilling into the next local day across spring-forward or fall-back changes.
func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location()).Add(-time.Second)
}

func parseClockValue(value string) (time.Time, error) {
	for _, layout := range []string{"15:04", "15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid time %q", value)
}

func normalizeClockValue(value string) (string, error) {
	parsed, err := parseClockValue(value)
	if err != nil {
		return "", err
	}

	return parsed.Format("15:04"), nil
}
