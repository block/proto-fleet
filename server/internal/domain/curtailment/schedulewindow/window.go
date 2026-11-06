// Package schedulewindow computes curtailment schedule occurrences from local
// wall-clock definitions. Each occurrence is a window with a start and an end;
// recurring windows are defined by clock times and may cross midnight.
package schedulewindow

import (
	"errors"
	"fmt"
	"time"
)

// Type distinguishes one-time windows from recurring ones.
type Type string

const (
	TypeOneTime   Type = "ONE_TIME"
	TypeRecurring Type = "RECURRING"
)

// Frequency is the recurrence cadence of a recurring definition.
type Frequency string

const (
	FrequencyDaily   Frequency = "DAILY"
	FrequencyWeekly  Frequency = "WEEKLY"
	FrequencyMonthly Frequency = "MONTHLY"
)

// MaxOneTimeDuration bounds a one-time window by the curtailment duration cap.
const MaxOneTimeDuration = 7 * 24 * time.Hour

const (
	clockLayout = "15:04"
	// A recurring window is shorter than 24 hours of wall-clock time, plus at
	// most one hour of DST shift, so the window containing any instant starts
	// on that local date or one of the two before it.
	recurringLookbackDays = 2
	// Monthly occurrences on day 31 can be up to 62 days apart; one year
	// covers every reachable occurrence of a valid definition.
	nextSearchDays = 400
)

// Definition is a schedule in local wall-clock terms. Dates are YYYY-MM-DD and
// clock times are HH:MM in Timezone.
type Definition struct {
	Type      Type
	Timezone  string
	StartDate string
	StartTime string
	EndTime   string
	// EndDate is the local date the one-time window ends on.
	EndDate string
	// Recurring fields.
	Frequency  Frequency
	DaysOfWeek []time.Weekday
	DayOfMonth int
	// UntilDate is the last local date a recurring occurrence may start on.
	UntilDate string
}

// Window is one occurrence, as instants.
type Window struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether at falls inside the window. The end is exclusive.
func (w Window) Contains(at time.Time) bool {
	return !at.Before(w.Start) && at.Before(w.End)
}

// parsed holds local dates as UTC midnights and clock times as offsets from
// midnight; at converts a date and clock time into an instant in loc.
type parsed struct {
	def       Definition
	loc       *time.Location
	startDate time.Time
	startTime time.Duration
	endTime   time.Duration
	endDate   time.Time
	// untilDate is zero when the definition has no until date.
	untilDate time.Time
	days      map[time.Weekday]bool
}

// Current returns the window that contains at, or nil.
func (d Definition) Current(at time.Time) (*Window, error) {
	p, err := d.parse()
	if err != nil {
		return nil, err
	}
	return p.current(at), nil
}

// LastEnded returns the most recent window that ended at or before at, or nil.
// Recurring definitions only look back as far as a window can reach, so a
// window that ended days ago is not reported.
func (d Definition) LastEnded(at time.Time) (*Window, error) {
	p, err := d.parse()
	if err != nil {
		return nil, err
	}
	return p.lastEnded(at), nil
}

// CurrentOrNext returns the window containing at, or else the next window.
func (d Definition) CurrentOrNext(at time.Time) (*Window, error) {
	p, err := d.parse()
	if err != nil {
		return nil, err
	}
	if w := p.current(at); w != nil {
		return w, nil
	}
	return p.next(at), nil
}

func (d Definition) parse() (*parsed, error) {
	if d.Timezone == "" || d.Timezone == "Local" {
		return nil, errors.New("timezone must be an IANA time zone name")
	}
	loc, err := time.LoadLocation(d.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q", d.Timezone)
	}
	p := &parsed{def: d, loc: loc}
	if p.startDate, err = parseDate("start_date", d.StartDate); err != nil {
		return nil, err
	}
	if p.startTime, err = parseClock("start_time", d.StartTime); err != nil {
		return nil, err
	}
	if p.endTime, err = parseClock("end_time", d.EndTime); err != nil {
		return nil, err
	}
	switch d.Type {
	case TypeOneTime:
		if err := p.parseOneTime(); err != nil {
			return nil, err
		}
	case TypeRecurring:
		if err := p.parseRecurring(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported schedule type %q", d.Type)
	}
	return p, nil
}

func (p *parsed) parseOneTime() error {
	d := p.def
	if d.Frequency != "" || len(d.DaysOfWeek) > 0 || d.DayOfMonth != 0 || d.UntilDate != "" {
		return errors.New("one-time schedules cannot set recurrence fields")
	}
	var err error
	if p.endDate, err = parseDate("end_date", d.EndDate); err != nil {
		return err
	}
	w := p.oneTimeWindow()
	if !w.End.After(w.Start) {
		return errors.New("one-time schedule must end after it starts")
	}
	if w.End.Sub(w.Start) > MaxOneTimeDuration {
		return fmt.Errorf("one-time schedule must last at most %s", MaxOneTimeDuration)
	}
	return nil
}

func (p *parsed) parseRecurring() error {
	d := p.def
	if d.EndDate != "" {
		return errors.New("recurring schedules use until_date, not end_date")
	}
	if p.startTime == p.endTime {
		return errors.New("recurring schedule start_time and end_time must differ")
	}
	switch d.Frequency {
	case FrequencyDaily:
		if len(d.DaysOfWeek) > 0 || d.DayOfMonth != 0 {
			return errors.New("daily schedules cannot set days_of_week or day_of_month")
		}
	case FrequencyWeekly:
		if len(d.DaysOfWeek) == 0 {
			return errors.New("weekly schedules require at least one day of the week")
		}
		if d.DayOfMonth != 0 {
			return errors.New("weekly schedules cannot set day_of_month")
		}
		p.days = make(map[time.Weekday]bool, len(d.DaysOfWeek))
		for _, day := range d.DaysOfWeek {
			if day < time.Sunday || day > time.Saturday {
				return fmt.Errorf("invalid day of week %d", day)
			}
			if p.days[day] {
				return errors.New("days_of_week must be unique")
			}
			p.days[day] = true
		}
	case FrequencyMonthly:
		if len(d.DaysOfWeek) > 0 {
			return errors.New("monthly schedules cannot set days_of_week")
		}
		if d.DayOfMonth < 1 || d.DayOfMonth > 31 {
			return errors.New("monthly schedules require day_of_month between 1 and 31")
		}
	default:
		return fmt.Errorf("unsupported recurrence frequency %q", d.Frequency)
	}
	if d.UntilDate != "" {
		until, err := parseDate("until_date", d.UntilDate)
		if err != nil {
			return err
		}
		if until.Before(p.startDate) {
			return errors.New("until_date must not be before start_date")
		}
		p.untilDate = until
	}
	return nil
}

func (p *parsed) oneTimeWindow() Window {
	return Window{
		Start: p.at(p.startDate, p.startTime),
		End:   p.at(p.endDate, p.endTime),
	}
}

func (p *parsed) afterUntil(day time.Time) bool {
	return !p.untilDate.IsZero() && p.untilDate.Before(day)
}

// occurrence returns the recurring window starting on local date day, if any.
func (p *parsed) occurrence(day time.Time) (Window, bool) {
	if day.Before(p.startDate) || p.afterUntil(day) {
		return Window{}, false
	}
	switch p.def.Frequency {
	case FrequencyWeekly:
		if !p.days[day.Weekday()] {
			return Window{}, false
		}
	case FrequencyMonthly:
		if day.Day() != p.def.DayOfMonth {
			return Window{}, false
		}
	case FrequencyDaily:
	}
	endDay := day
	if p.startTime >= p.endTime {
		endDay = day.AddDate(0, 0, 1)
	}
	w := Window{Start: p.at(day, p.startTime), End: p.at(endDay, p.endTime)}
	if !w.End.After(w.Start) {
		return Window{}, false
	}
	return w, true
}

// recent returns the one-time window, or the recurring windows that start on
// at's local date or the days before it. It looks one day further back than a
// window containing at can start, so lastEnded also sees the window before.
func (p *parsed) recent(at time.Time) []Window {
	if p.def.Type == TypeOneTime {
		return []Window{p.oneTimeWindow()}
	}
	today := localDate(at.In(p.loc))
	windows := make([]Window, 0, recurringLookbackDays+2)
	for offset := 0; offset <= recurringLookbackDays+1; offset++ {
		if w, ok := p.occurrence(today.AddDate(0, 0, -offset)); ok {
			windows = append(windows, w)
		}
	}
	return windows
}

func (p *parsed) current(at time.Time) *Window {
	for _, w := range p.recent(at) {
		if w.Contains(at) {
			return &w
		}
	}
	return nil
}

func (p *parsed) lastEnded(at time.Time) *Window {
	var latest *Window
	for _, w := range p.recent(at) {
		if !w.End.After(at) && (latest == nil || w.End.After(latest.End)) {
			latest = &w
		}
	}
	return latest
}

func (p *parsed) next(at time.Time) *Window {
	if p.def.Type == TypeOneTime {
		w := p.oneTimeWindow()
		if w.Start.After(at) {
			return &w
		}
		return nil
	}
	day := localDate(at.In(p.loc)).AddDate(0, 0, -recurringLookbackDays)
	if day.Before(p.startDate) {
		day = p.startDate
	}
	for range nextSearchDays {
		if p.afterUntil(day) {
			return nil
		}
		if w, ok := p.occurrence(day); ok && w.Start.After(at) {
			return &w
		}
		day = day.AddDate(0, 0, 1)
	}
	return nil
}

// at resolves a local date and clock time. A time skipped by a spring-forward
// gap runs at the same instant as under the offset before the gap, so 02:30
// in a 02:00-03:00 gap runs at 03:30; a time repeated by a fall-back
// transition resolves to its first occurrence.
func (p *parsed) at(day time.Time, clock time.Duration) time.Time {
	wall := day.Add(clock)
	t := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), 0, 0, p.loc)
	if wallClock(t).Equal(wall) {
		// time.Date may return either occurrence of a repeated time; an earlier
		// one lies in the zone period before t's.
		if start, _ := t.ZoneBounds(); !start.IsZero() {
			_, offsetBefore := start.Add(-time.Second).Zone()
			earlier := wall.Add(-time.Duration(offsetBefore) * time.Second).In(p.loc)
			if earlier.Before(start) && wallClock(earlier).Equal(wall) {
				return earlier
			}
		}
		return t
	}
	_, offsetBeforeGap := t.Add(-12 * time.Hour).Zone()
	return wall.Add(-time.Duration(offsetBeforeGap) * time.Second).In(p.loc)
}

func parseDate(field, value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a YYYY-MM-DD date", field)
	}
	return date, nil
}

func parseClock(field, value string) (time.Duration, error) {
	t, err := time.Parse(clockLayout, value)
	if err != nil || len(value) != len(clockLayout) {
		return 0, fmt.Errorf("%s must be an HH:MM time", field)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

// wallClock is t's local date and clock time, expressed in UTC.
func wallClock(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
}

// localDate is t's local date as a UTC midnight.
func localDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
