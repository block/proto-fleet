// Package schedulewindow computes curtailment schedule occurrences from local
// wall-clock definitions. Each occurrence is a window with a start and an end;
// recurring windows are defined by clock times and may cross midnight.
package schedulewindow

import (
	"errors"
	"fmt"
	"slices"
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
	dateLayout  = "2006-01-02"
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

type civilDate struct {
	year  int
	month time.Month
	day   int
}

type clock struct {
	hour   int
	minute int
}

type parsed struct {
	def       Definition
	loc       *time.Location
	startDate civilDate
	startTime clock
	endTime   clock
	endDate   civilDate
	untilDate *civilDate
	days      map[time.Weekday]bool
}

// Validate checks the definition's shape without regard to the current time.
func (d Definition) Validate() error {
	_, err := d.parse()
	return err
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

// Next returns the first window that starts after at, or nil when none remain.
func (d Definition) Next(at time.Time) (*Window, error) {
	p, err := d.parse()
	if err != nil {
		return nil, err
	}
	return p.next(at), nil
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
		if until.before(p.startDate) {
			return errors.New("until_date must not be before start_date")
		}
		p.untilDate = &until
	}
	return nil
}

func (p *parsed) oneTimeWindow() Window {
	return Window{
		Start: p.at(p.startDate, p.startTime),
		End:   p.at(p.endDate, p.endTime),
	}
}

// occurrence returns the recurring window starting on local date day, if any.
func (p *parsed) occurrence(day civilDate) (Window, bool) {
	if day.before(p.startDate) || (p.untilDate != nil && p.untilDate.before(day)) {
		return Window{}, false
	}
	switch p.def.Frequency {
	case FrequencyWeekly:
		if !p.days[day.weekday()] {
			return Window{}, false
		}
	case FrequencyMonthly:
		if day.day != p.def.DayOfMonth {
			return Window{}, false
		}
	case FrequencyDaily:
	}
	endDay := day
	if !p.startTime.before(p.endTime) {
		endDay = day.addDays(1)
	}
	w := Window{Start: p.at(day, p.startTime), End: p.at(endDay, p.endTime)}
	if !w.End.After(w.Start) {
		return Window{}, false
	}
	return w, true
}

func (p *parsed) current(at time.Time) *Window {
	if p.def.Type == TypeOneTime {
		w := p.oneTimeWindow()
		if w.Contains(at) {
			return &w
		}
		return nil
	}
	today := civilDateOf(at.In(p.loc))
	for offset := 0; offset <= recurringLookbackDays; offset++ {
		if w, ok := p.occurrence(today.addDays(-offset)); ok && w.Contains(at) {
			return &w
		}
	}
	return nil
}

func (p *parsed) lastEnded(at time.Time) *Window {
	if p.def.Type == TypeOneTime {
		w := p.oneTimeWindow()
		if !w.End.After(at) {
			return &w
		}
		return nil
	}
	today := civilDateOf(at.In(p.loc))
	var latest *Window
	for offset := 0; offset <= recurringLookbackDays+1; offset++ {
		w, ok := p.occurrence(today.addDays(-offset))
		if !ok || w.End.After(at) {
			continue
		}
		if latest == nil || w.End.After(latest.End) {
			candidate := w
			latest = &candidate
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
	day := civilDateOf(at.In(p.loc)).addDays(-recurringLookbackDays)
	if day.before(p.startDate) {
		day = p.startDate
	}
	for range nextSearchDays {
		if p.untilDate != nil && p.untilDate.before(day) {
			return nil
		}
		if w, ok := p.occurrence(day); ok && w.Start.After(at) {
			return &w
		}
		day = day.addDays(1)
	}
	return nil
}

func (p *parsed) at(day civilDate, c clock) time.Time {
	return time.Date(day.year, day.month, day.day, c.hour, c.minute, 0, 0, p.loc)
}

func parseDate(field, value string) (civilDate, error) {
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		return civilDate{}, fmt.Errorf("%s must be a YYYY-MM-DD date", field)
	}
	return civilDateOf(t), nil
}

func parseClock(field, value string) (clock, error) {
	t, err := time.Parse(clockLayout, value)
	if err != nil || len(value) != len(clockLayout) {
		return clock{}, fmt.Errorf("%s must be an HH:MM time", field)
	}
	return clock{hour: t.Hour(), minute: t.Minute()}, nil
}

func civilDateOf(t time.Time) civilDate {
	y, m, d := t.Date()
	return civilDate{year: y, month: m, day: d}
}

func (c civilDate) addDays(n int) civilDate {
	return civilDateOf(time.Date(c.year, c.month, c.day+n, 12, 0, 0, 0, time.UTC))
}

func (c civilDate) weekday() time.Weekday {
	return time.Date(c.year, c.month, c.day, 12, 0, 0, 0, time.UTC).Weekday()
}

func (c civilDate) before(other civilDate) bool {
	return slices.Compare([]int{c.year, int(c.month), c.day}, []int{other.year, int(other.month), other.day}) < 0
}

func (c clock) before(other clock) bool {
	return c.hour < other.hour || (c.hour == other.hour && c.minute < other.minute)
}
