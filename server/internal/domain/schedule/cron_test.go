package schedule

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"google.golang.org/protobuf/proto"
)

func TestToCronExpression(t *testing.T) {
	tests := []struct {
		name       string
		freq       pb.RecurrenceFrequency
		startTime  string
		timezone   string
		daysOfWeek []pb.DayOfWeek
		dayOfMonth *int32
		want       string
		wantErr    bool
	}{
		{
			name:      "daily",
			freq:      pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
			startTime: "14:30",
			timezone:  "America/Chicago",
			want:      "CRON_TZ=America/Chicago 30 14 * * *",
		},
		{
			name:      "daily with seconds",
			freq:      pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
			startTime: "14:30:00",
			timezone:  "America/Chicago",
			want:      "CRON_TZ=America/Chicago 30 14 * * *",
		},
		{
			name:      "weekly mon/wed/fri",
			freq:      pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY,
			startTime: "09:00",
			timezone:  "UTC",
			daysOfWeek: []pb.DayOfWeek{
				pb.DayOfWeek_DAY_OF_WEEK_MONDAY,
				pb.DayOfWeek_DAY_OF_WEEK_WEDNESDAY,
				pb.DayOfWeek_DAY_OF_WEEK_FRIDAY,
			},
			want: "CRON_TZ=UTC 0 9 * * 1,3,5",
		},
		{
			name:       "monthly day 15",
			freq:       pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
			startTime:  "00:00",
			timezone:   "Asia/Tokyo",
			dayOfMonth: proto.Int32(15),
			want:       "CRON_TZ=Asia/Tokyo 0 0 15 * *",
		},
		{
			name:      "weekly with no days",
			freq:      pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY,
			startTime: "09:00",
			timezone:  "UTC",
			wantErr:   true,
		},
		{
			name:      "monthly with no day",
			freq:      pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
			startTime: "09:00",
			timezone:  "UTC",
			wantErr:   true,
		},
		{
			name:      "invalid time format",
			freq:      pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
			startTime: "not-a-time",
			timezone:  "UTC",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToCronExpression(tt.freq, tt.startTime, tt.timezone, tt.daysOfWeek, tt.dayOfMonth)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToCronExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ToCronExpression() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComputeNextRun(t *testing.T) {
	chicago, _ := time.LoadLocation("America/Chicago")

	tests := []struct {
		name    string
		sched   *pb.Schedule
		after   time.Time
		wantNil bool
		wantErr bool
		check   func(t *testing.T, got time.Time)
	}{
		{
			name: "one-time future",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
				StartDate:    "2026-06-15",
				StartTime:    "14:30",
				Timezone:     "America/Chicago",
			},
			after: time.Date(2026, 6, 1, 0, 0, 0, 0, chicago),
			check: func(t *testing.T, got time.Time) {
				want := time.Date(2026, 6, 15, 14, 30, 0, 0, chicago)
				if !got.Equal(want) {
					t.Errorf("got %v, want %v", got, want)
				}
			},
		},
		{
			name: "one-time past",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
				StartDate:    "2026-01-01",
				StartTime:    "09:00",
				Timezone:     "America/Chicago",
			},
			after:   time.Date(2026, 6, 1, 0, 0, 0, 0, chicago),
			wantNil: true,
		},
		{
			name: "one-time past end_date",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
				StartDate:    "2026-06-15",
				StartTime:    "14:30",
				EndDate:      "2026-06-10",
				Timezone:     "America/Chicago",
			},
			after:   time.Date(2026, 6, 1, 0, 0, 0, 0, chicago),
			wantNil: true,
		},
		{
			name: "daily recurring",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
				StartDate:    "2026-04-01",
				StartTime:    "14:30",
				Timezone:     "America/Chicago",
				Recurrence: &pb.ScheduleRecurrence{
					Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
					Interval:  1,
				},
			},
			after: time.Date(2026, 4, 1, 12, 0, 0, 0, chicago),
			check: func(t *testing.T, got time.Time) {
				want := time.Date(2026, 4, 1, 14, 30, 0, 0, chicago)
				if !got.Equal(want) {
					t.Errorf("got %v, want %v", got, want)
				}
			},
		},
		{
			name: "weekly next occurrence",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
				StartDate:    "2026-04-01",
				StartTime:    "09:00",
				Timezone:     "UTC",
				Recurrence: &pb.ScheduleRecurrence{
					Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_WEEKLY,
					Interval:   1,
					DaysOfWeek: []pb.DayOfWeek{pb.DayOfWeek_DAY_OF_WEEK_MONDAY, pb.DayOfWeek_DAY_OF_WEEK_FRIDAY},
				},
			},
			after: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC), // Wednesday
			check: func(t *testing.T, got time.Time) {
				want := time.Date(2026, 4, 3, 9, 0, 0, 0, time.UTC) // Friday
				if !got.Equal(want) {
					t.Errorf("got %v, want %v", got, want)
				}
			},
		},
		{
			name: "monthly next occurrence",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
				StartDate:    "2026-01-01",
				StartTime:    "00:00",
				Timezone:     "UTC",
				Recurrence: &pb.ScheduleRecurrence{
					Frequency:  pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_MONTHLY,
					Interval:   1,
					DayOfMonth: proto.Int32(15),
				},
			},
			after: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, got time.Time) {
				want := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
				if !got.Equal(want) {
					t.Errorf("got %v, want %v", got, want)
				}
			},
		},
		{
			name: "recurring past end_date",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
				StartDate:    "2026-01-01",
				StartTime:    "14:30",
				EndDate:      "2026-03-31",
				Timezone:     "UTC",
				Recurrence: &pb.ScheduleRecurrence{
					Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
					Interval:  1,
				},
			},
			after:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantNil: true,
		},
		{
			name: "recurring respects start_date",
			sched: &pb.Schedule{
				ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
				StartDate:    "2026-06-01",
				StartTime:    "09:00",
				Timezone:     "UTC",
				Recurrence: &pb.ScheduleRecurrence{
					Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
					Interval:  1,
				},
			},
			after: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			check: func(t *testing.T, got time.Time) {
				want := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
				if !got.Equal(want) {
					t.Errorf("got %v, want %v", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ComputeNextRun(tt.sched, tt.after)
			if (err != nil) != tt.wantErr {
				t.Errorf("ComputeNextRun() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("ComputeNextRun() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ComputeNextRun() = nil, want non-nil")
			}
			if tt.check != nil {
				tt.check(t, *got)
			}
		})
	}
}

func TestParseScheduleTime_AcceptsSeconds(t *testing.T) {
	chicago, _ := time.LoadLocation("America/Chicago")

	got, err := ParseScheduleTime("2026-06-15", "14:30:00", "America/Chicago")
	if err != nil {
		t.Fatalf("ParseScheduleTime() error = %v", err)
	}

	want := time.Date(2026, 6, 15, 14, 30, 0, 0, chicago)
	if !got.Equal(want) {
		t.Fatalf("ParseScheduleTime() = %v, want %v", got, want)
	}
}

func TestParseScheduleTime(t *testing.T) {
	chicago, _ := time.LoadLocation("America/Chicago")

	got, err := ParseScheduleTime("2026-04-01", "14:30", "America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 4, 1, 14, 30, 0, 0, chicago)
	if !got.Equal(want) {
		t.Errorf("ParseScheduleTime() = %v, want %v", got, want)
	}
}

func TestEndOfDay(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")

	tests := []struct {
		name string
		day  time.Time
		want time.Time
	}{
		{
			name: "normal day",
			day:  time.Date(2026, 4, 15, 0, 0, 0, 0, ny),
			want: time.Date(2026, 4, 15, 23, 59, 59, 0, ny),
		},
		{
			// 2026-03-08 is spring-forward in America/New_York (23-hour day)
			name: "DST spring-forward day",
			day:  time.Date(2026, 3, 8, 0, 0, 0, 0, ny),
			want: time.Date(2026, 3, 8, 23, 59, 59, 0, ny),
		},
		{
			// 2026-11-01 is fall-back in America/New_York (25-hour day)
			name: "DST fall-back day",
			day:  time.Date(2026, 11, 1, 0, 0, 0, 0, ny),
			want: time.Date(2026, 11, 1, 23, 59, 59, 0, ny),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endOfDay(tt.day)
			if !got.Equal(tt.want) {
				t.Errorf("endOfDay() = %v, want %v", got, tt.want)
			}
			if got.Day() != tt.day.Day() {
				t.Errorf("endOfDay() day = %d, want %d", got.Day(), tt.day.Day())
			}
		})
	}
}

func TestComputeNextRun_EndDateDST(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")

	t.Run("one-time on spring-forward end_date", func(t *testing.T) {
		sched := &pb.Schedule{
			ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME,
			StartDate:    "2026-03-08",
			StartTime:    "23:30",
			EndDate:      "2026-03-08",
			Timezone:     "America/New_York",
		}
		got, err := ComputeNextRun(sched, time.Date(2026, 3, 1, 0, 0, 0, 0, ny))
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatal("expected non-nil for late-evening schedule on spring-forward end_date")
		}
		want := time.Date(2026, 3, 8, 23, 30, 0, 0, ny)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("recurring past fall-back end_date", func(t *testing.T) {
		sched := &pb.Schedule{
			ScheduleType: pb.ScheduleType_SCHEDULE_TYPE_RECURRING,
			StartDate:    "2026-10-01",
			StartTime:    "09:00",
			EndDate:      "2026-11-01",
			Timezone:     "America/New_York",
			Recurrence: &pb.ScheduleRecurrence{
				Frequency: pb.RecurrenceFrequency_RECURRENCE_FREQUENCY_DAILY,
				Interval:  1,
			},
		}
		got, err := ComputeNextRun(sched, time.Date(2026, 11, 1, 10, 0, 0, 0, ny))
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("expected nil for past fall-back end_date, got %v", got)
		}
	})
}
