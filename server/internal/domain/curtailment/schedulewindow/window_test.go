package schedulewindow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

func at(loc *time.Location, year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

func TestOneTimeWindow(t *testing.T) {
	chicago := mustLoad(t, "America/Chicago")
	def := Definition{
		Type:      TypeOneTime,
		Timezone:  "America/Chicago",
		StartDate: "2026-07-01",
		StartTime: "14:00",
		EndDate:   "2026-07-02",
		EndTime:   "09:30",
	}
	require.NoError(t, def.Validate())
	want := Window{Start: at(chicago, 2026, 7, 1, 14, 0), End: at(chicago, 2026, 7, 2, 9, 30)}

	before := at(chicago, 2026, 7, 1, 13, 59)
	current, err := def.Current(before)
	require.NoError(t, err)
	assert.Nil(t, current)
	next, err := def.Next(before)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.True(t, next.Start.Equal(want.Start))
	assert.True(t, next.End.Equal(want.End))

	inside := at(chicago, 2026, 7, 2, 2, 0)
	current, err = def.Current(inside)
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.True(t, current.End.Equal(want.End))
	ended, err := def.LastEnded(inside)
	require.NoError(t, err)
	assert.Nil(t, ended)

	after := want.End
	current, err = def.Current(after)
	require.NoError(t, err)
	assert.Nil(t, current, "end is exclusive")
	ended, err = def.LastEnded(after)
	require.NoError(t, err)
	require.NotNil(t, ended)
	assert.True(t, ended.End.Equal(want.End))
	next, err = def.Next(after)
	require.NoError(t, err)
	assert.Nil(t, next)
}

func TestOneTimeWindowBounds(t *testing.T) {
	base := Definition{Type: TypeOneTime, Timezone: "UTC", StartDate: "2026-07-01", StartTime: "00:00"}

	endsAtStart := base
	endsAtStart.EndDate, endsAtStart.EndTime = "2026-07-01", "00:00"
	assert.ErrorContains(t, endsAtStart.Validate(), "must end after it starts")

	sevenDays := base
	sevenDays.EndDate, sevenDays.EndTime = "2026-07-08", "00:00"
	assert.NoError(t, sevenDays.Validate())

	tooLong := base
	tooLong.EndDate, tooLong.EndTime = "2026-07-08", "00:01"
	assert.ErrorContains(t, tooLong.Validate(), "at most")
}

func TestDailyWindowCrossingMidnight(t *testing.T) {
	def := Definition{
		Type:      TypeRecurring,
		Timezone:  "UTC",
		StartDate: "2026-07-01",
		StartTime: "22:00",
		EndTime:   "06:00",
		Frequency: FrequencyDaily,
	}
	require.NoError(t, def.Validate())

	current, err := def.Current(at(time.UTC, 2026, 7, 3, 3, 0))
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.True(t, current.Start.Equal(at(time.UTC, 2026, 7, 2, 22, 0)))
	assert.True(t, current.End.Equal(at(time.UTC, 2026, 7, 3, 6, 0)))

	gap := at(time.UTC, 2026, 7, 3, 12, 0)
	current, err = def.Current(gap)
	require.NoError(t, err)
	assert.Nil(t, current)
	ended, err := def.LastEnded(gap)
	require.NoError(t, err)
	require.NotNil(t, ended)
	assert.True(t, ended.End.Equal(at(time.UTC, 2026, 7, 3, 6, 0)))
	next, err := def.Next(gap)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.True(t, next.Start.Equal(at(time.UTC, 2026, 7, 3, 22, 0)))

	// Nothing occurs before the start date, including the window that would
	// otherwise have started the evening before.
	early := at(time.UTC, 2026, 7, 1, 3, 0)
	current, err = def.Current(early)
	require.NoError(t, err)
	assert.Nil(t, current)
	ended, err = def.LastEnded(early)
	require.NoError(t, err)
	assert.Nil(t, ended)
}

func TestWeeklyWindow(t *testing.T) {
	def := Definition{
		Type:       TypeRecurring,
		Timezone:   "UTC",
		StartDate:  "2026-07-01",
		StartTime:  "16:00",
		EndTime:    "20:00",
		Frequency:  FrequencyWeekly,
		DaysOfWeek: []time.Weekday{time.Monday, time.Thursday},
	}
	require.NoError(t, def.Validate())

	// 2026-07-01 is a Wednesday, so the first occurrence is Thursday the 2nd.
	next, err := def.Next(at(time.UTC, 2026, 7, 1, 0, 0))
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.True(t, next.Start.Equal(at(time.UTC, 2026, 7, 2, 16, 0)))

	next, err = def.Next(at(time.UTC, 2026, 7, 2, 16, 0))
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.True(t, next.Start.Equal(at(time.UTC, 2026, 7, 6, 16, 0)), "the following Monday")

	current, err := def.Current(at(time.UTC, 2026, 7, 7, 17, 0))
	require.NoError(t, err)
	assert.Nil(t, current, "Tuesday is not selected")
}

func TestMonthlyWindowSkipsShortMonths(t *testing.T) {
	def := Definition{
		Type:       TypeRecurring,
		Timezone:   "UTC",
		StartDate:  "2026-01-01",
		StartTime:  "10:00",
		EndTime:    "11:00",
		Frequency:  FrequencyMonthly,
		DayOfMonth: 31,
	}
	require.NoError(t, def.Validate())

	next, err := def.Next(at(time.UTC, 2026, 1, 31, 11, 0))
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.True(t, next.Start.Equal(at(time.UTC, 2026, 3, 31, 10, 0)), "February has no 31st")
}

func TestUntilDateLimitsOccurrences(t *testing.T) {
	def := Definition{
		Type:      TypeRecurring,
		Timezone:  "UTC",
		StartDate: "2026-07-01",
		StartTime: "23:00",
		EndTime:   "01:00",
		Frequency: FrequencyDaily,
		UntilDate: "2026-07-02",
	}
	require.NoError(t, def.Validate())

	// The last occurrence starts on the until date and may end after it.
	current, err := def.Current(at(time.UTC, 2026, 7, 3, 0, 30))
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.True(t, current.Start.Equal(at(time.UTC, 2026, 7, 2, 23, 0)))

	next, err := def.Next(at(time.UTC, 2026, 7, 2, 23, 0))
	require.NoError(t, err)
	assert.Nil(t, next)
}

func TestDaylightSavingTransitions(t *testing.T) {
	newYork := mustLoad(t, "America/New_York")
	def := Definition{
		Type:      TypeRecurring,
		Timezone:  "America/New_York",
		StartDate: "2026-03-01",
		StartTime: "22:00",
		EndTime:   "06:00",
		Frequency: FrequencyDaily,
	}
	require.NoError(t, def.Validate())

	// Spring forward on 2026-03-08: the overnight window loses an hour.
	current, err := def.Current(at(newYork, 2026, 3, 8, 5, 0))
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.Equal(t, 7*time.Hour, current.End.Sub(current.Start))

	// Fall back on 2026-11-01: the overnight window gains an hour.
	current, err = def.Current(at(newYork, 2026, 11, 1, 5, 0))
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.Equal(t, 9*time.Hour, current.End.Sub(current.Start))
}

func TestDaylightSavingNonexistentAndRepeatedTimes(t *testing.T) {
	newYork := mustLoad(t, "America/New_York")
	utc := func(month time.Month, day, hour, minute int) time.Time {
		return time.Date(2027, month, day, hour, minute, 0, 0, time.UTC)
	}
	tests := []struct {
		name               string
		startTime, endTime string
		day                int
		month              time.Month
		wantStart, wantEnd time.Time
	}{
		// Spring forward on 2027-03-14: 02:00-03:00 EST does not exist.
		{"start inside the gap runs after it", "02:30", "04:00", 14, time.March, utc(time.March, 14, 7, 30), utc(time.March, 14, 8, 0)},
		{"end inside the gap runs after it", "01:00", "02:30", 14, time.March, utc(time.March, 14, 6, 0), utc(time.March, 14, 7, 30)},
		{"window inside the gap still runs", "02:00", "02:30", 14, time.March, utc(time.March, 14, 7, 0), utc(time.March, 14, 7, 30)},
		// Fall back on 2027-11-07: 01:00-02:00 repeats; the first occurrence is EDT.
		{"repeated start uses its first occurrence", "01:30", "03:00", 7, time.November, utc(time.November, 7, 5, 30), utc(time.November, 7, 8, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := Definition{
				Type:      TypeRecurring,
				Timezone:  "America/New_York",
				StartDate: "2027-01-01",
				StartTime: tt.startTime,
				EndTime:   tt.endTime,
				Frequency: FrequencyDaily,
			}
			require.NoError(t, def.Validate())
			next, err := def.Next(at(newYork, 2027, tt.month, tt.day, 0, 0))
			require.NoError(t, err)
			require.NotNil(t, next)
			assert.True(t, next.Start.Equal(tt.wantStart), "start %s, want %s", next.Start.UTC(), tt.wantStart)
			assert.True(t, next.End.Equal(tt.wantEnd), "end %s, want %s", next.End.UTC(), tt.wantEnd)
		})
	}
}

func TestCurrentOrNext(t *testing.T) {
	def := Definition{Type: TypeRecurring, Timezone: "UTC", StartDate: "2026-07-01", StartTime: "08:00", EndTime: "09:00", Frequency: FrequencyDaily}

	w, err := def.CurrentOrNext(at(time.UTC, 2026, 7, 1, 8, 30))
	require.NoError(t, err)
	require.NotNil(t, w)
	assert.True(t, w.Start.Equal(at(time.UTC, 2026, 7, 1, 8, 0)))

	w, err = def.CurrentOrNext(at(time.UTC, 2026, 7, 1, 9, 0))
	require.NoError(t, err)
	require.NotNil(t, w)
	assert.True(t, w.Start.Equal(at(time.UTC, 2026, 7, 2, 8, 0)))
}

func TestValidateRejectsMalformedDefinitions(t *testing.T) {
	valid := Definition{Type: TypeRecurring, Timezone: "UTC", StartDate: "2026-07-01", StartTime: "08:00", EndTime: "09:00", Frequency: FrequencyDaily}
	tests := []struct {
		name   string
		mutate func(*Definition)
		want   string
	}{
		{"unknown type", func(d *Definition) { d.Type = "SOMETIMES" }, "unsupported schedule type"},
		{"missing timezone", func(d *Definition) { d.Timezone = "" }, "IANA"},
		{"local timezone", func(d *Definition) { d.Timezone = "Local" }, "IANA"},
		{"unknown timezone", func(d *Definition) { d.Timezone = "Mars/Olympus" }, "invalid timezone"},
		{"bad start date", func(d *Definition) { d.StartDate = "2026-7-1" }, "start_date"},
		{"bad start time", func(d *Definition) { d.StartTime = "8:00" }, "start_time"},
		{"bad end time", func(d *Definition) { d.EndTime = "24:00" }, "end_time"},
		{"equal clock times", func(d *Definition) { d.EndTime = "08:00" }, "must differ"},
		{"unknown frequency", func(d *Definition) { d.Frequency = "HOURLY" }, "frequency"},
		{"daily with days", func(d *Definition) { d.DaysOfWeek = []time.Weekday{time.Monday} }, "daily"},
		{"weekly without days", func(d *Definition) { d.Frequency = FrequencyWeekly }, "at least one day"},
		{"weekly duplicate days", func(d *Definition) {
			d.Frequency = FrequencyWeekly
			d.DaysOfWeek = []time.Weekday{time.Monday, time.Monday}
		}, "unique"},
		{"monthly without day", func(d *Definition) { d.Frequency = FrequencyMonthly }, "day_of_month"},
		{"until before start", func(d *Definition) { d.UntilDate = "2026-06-30" }, "until_date"},
		{"recurring with end date", func(d *Definition) { d.EndDate = "2026-07-02" }, "until_date"},
		{"one-time with recurrence", func(d *Definition) {
			d.Type = TypeOneTime
			d.EndDate = "2026-07-01"
		}, "recurrence"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := valid
			tt.mutate(&def)
			assert.ErrorContains(t, def.Validate(), tt.want)
		})
	}
}
