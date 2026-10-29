package hostedaccounts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDashboardHistoryWorkClipsAndWeightsAuthorizedIntervals(t *testing.T) {
	start := time.Now().UTC()
	end := start.Add(24 * time.Hour)
	before := start.Add(-time.Hour)
	middle := start.Add(12 * time.Hour)
	after := end.Add(time.Hour)
	for _, test := range []struct {
		name      string
		intervals []dashboardInterval
		want      time.Duration
	}{
		{"empty", nil, 0},
		{"miner clipped", []dashboardInterval{{Kind: 5, Start: before, End: &after}}, 24 * time.Hour},
		{"recalled miner", []dashboardInterval{{Kind: 5, Start: before, End: &middle}}, 12 * time.Hour},
		{"later allocation", []dashboardInterval{{Kind: 5, Start: middle}}, 12 * time.Hour},
		{"outside range", []dashboardInterval{{Kind: 6, Start: after}}, 0},
		{"module contributor cost", []dashboardInterval{{Kind: 6, Start: start}}, (24*time.Hour + 125*time.Second) * 8},
		{"disjoint ownership", []dashboardInterval{{Kind: 5, Start: start, End: &middle}, {Kind: 5, Start: middle}}, 24 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) { require.Equal(t, test.want, dashboardHistoryWork(test.intervals, start, end)) })
	}
	intervals := make([]dashboardInterval, 301)
	for i := range intervals {
		intervals[i] = dashboardInterval{Kind: 5, Start: start}
	}
	require.Greater(t, dashboardHistoryWork(intervals, start, end), dashboardHistoryBudget)
	require.LessOrEqual(t, dashboardHistoryWork(intervals, start, start.Add(time.Hour)), dashboardHistoryBudget)
}

func TestDashboardSummaryWorkCountsIntersectingSourceBuckets(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	middle := start.Add(90 * time.Minute)
	for _, test := range []struct {
		name       string
		intervals  []dashboardInterval
		resolution time.Duration
		want       int64
	}{
		{"empty", nil, time.Hour, 0},
		{"open hourly", []dashboardInterval{{Start: start}}, time.Hour, 25},
		{"recalled hourly", []dashboardInterval{{Start: start, End: &middle}}, time.Hour, 3},
		{"open daily", []dashboardInterval{{Start: start}}, 24 * time.Hour, 2},
		{"outside range", []dashboardInterval{{Start: end}}, time.Hour, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, dashboardSummaryWork(test.intervals, start, end, test.resolution))
		})
	}
	year := start.Add(366 * 24 * time.Hour)
	intervals := make([]dashboardInterval, dashboardSummaryBudget/367+1)
	for i := range intervals {
		intervals[i] = dashboardInterval{Start: start}
	}
	require.Greater(t, dashboardSummaryWork(intervals, start, year, 24*time.Hour), int64(dashboardSummaryBudget))
	require.LessOrEqual(t, dashboardSummaryWork(intervals[:len(intervals)-1], start, year, 24*time.Hour), int64(dashboardSummaryBudget))
}

func TestDashboardSummaryTailWorkClipsToRealtimeWindow(t *testing.T) {
	end := time.Now().UTC()
	tailWindow := 3 * time.Hour
	tail := end.Add(-tailWindow)
	recalled := end.Add(-time.Hour)
	for _, test := range []struct {
		name      string
		intervals []dashboardInterval
		want      time.Duration
	}{
		{"empty", nil, 0},
		{"older allocation", []dashboardInterval{{Start: end.Add(-365 * 24 * time.Hour)}}, tailWindow},
		{"recalled in tail", []dashboardInterval{{Start: tail.Add(-time.Hour), End: &recalled}}, tailWindow - time.Hour},
		{"recalled before tail", []dashboardInterval{{Start: tail.Add(-2 * time.Hour), End: &tail}}, 0},
	} {
		t.Run(test.name, func(t *testing.T) { require.Equal(t, test.want, dashboardSummaryTailWork(test.intervals, tail, end)) })
	}
	assets := int(dashboardSummaryTailBudget / tailWindow)
	intervals := make([]dashboardInterval, assets+1)
	for i := range intervals {
		intervals[i] = dashboardInterval{Start: tail}
	}
	require.Greater(t, dashboardSummaryTailWork(intervals, tail, end), dashboardSummaryTailBudget)
	require.LessOrEqual(t, dashboardSummaryTailWork(intervals[:assets], tail, end), dashboardSummaryTailBudget)
}

func TestDashboardRetainedWorkUsesBothWatermarks(t *testing.T) {
	end := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	start := end.Add(-10 * 24 * time.Hour)
	for _, tc := range []struct {
		name                     string
		resolution               time.Duration
		hourly, daily, allocated time.Time
		summary                  int64
		raw                      time.Duration
	}{
		{"healthy hourly", time.Hour, end.Add(-2 * time.Hour), start, start, 241, 2 * time.Hour},
		{"stalled hourly", time.Hour, end.Add(-4 * 24 * time.Hour), start, start, 241, 4 * 24 * time.Hour},
		{"uninitialized", time.Hour, start, start, start, 241, 10 * 24 * time.Hour},
		{"historical before hourly watermark", time.Hour, end.Add(time.Hour), start, start, 241, 0},
		{"recent allocation", time.Hour, start, start, end.Add(-time.Hour), 2, time.Hour},
		{"daily hourly rollup", 24 * time.Hour, end.Add(-time.Hour), end.Add(-2 * 24 * time.Hour), start, 58, time.Hour},
		{"stalled daily only", 24 * time.Hour, end, start, start, 241, 0},
		{"daily ahead of hourly", 24 * time.Hour, start, end.Add(-24 * time.Hour), start, 35, 24 * time.Hour},
		{"fully saved daily", 24 * time.Hour, end, end, start, 11, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summaries, raw := dashboardRetainedWork([]dashboardInterval{{Start: tc.allocated}}, start, end, tc.resolution, tc.hourly, tc.daily)
			require.Equal(t, tc.summary, summaries)
			require.Equal(t, tc.raw, raw)
		})
	}
}

func TestDashboardRetainedYearFitsHealthyRefresh(t *testing.T) {
	for _, clock := range [][3]int{{0, 0, 0}, {0, 29, 59}, {5, 29, 59}, {5, 59, 0}, {10, 59, 0}, {23, 59, 0}} {
		end := time.Date(2026, time.October, 29, clock[0], clock[1], clock[2], 0, time.UTC)
		start := end.Add(-366 * 24 * time.Hour).Truncate(24 * time.Hour)
		intervals := make([]dashboardInterval, 2500)
		for i := range intervals {
			intervals[i].Start = start
		}
		// Last completed refreshes at the oldest times allowed by each policy.
		hourly := end.Add(-90 * time.Minute).Truncate(time.Hour)
		daily := end.Add(-30 * time.Hour).Truncate(24 * time.Hour)
		summary, tail := dashboardRetainedWork(intervals, start, end, dashboardDailyResolution, hourly, daily)
		require.LessOrEqual(t, summary, int64(dashboardSummaryBudget), "healthy year at %s", end)
		require.LessOrEqual(t, tail, dashboardSummaryTailBudget, "healthy raw tail at %s", end)
		steadySummary, steadyTail := dashboardHealthyRetainedWork(intervals, start, end, dashboardDailyResolution, end)
		require.LessOrEqual(t, summary, steadySummary)
		require.LessOrEqual(t, tail, steadyTail)
		require.LessOrEqual(t, steadySummary, int64(dashboardSummaryBudget))
		require.Equal(t, 2500*(2*time.Hour+30*time.Minute), steadyTail, "normal tail allowance must not vary with clock phase")
	}
}

func TestDashboardHealthyRetainedWorkClipsToAvailableHistory(t *testing.T) {
	now := time.Date(2026, time.October, 29, 5, 29, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	summary, tail := dashboardHealthyRetainedWork([]dashboardInterval{{Start: start}}, start, now, dashboardDailyResolution, now)
	require.EqualValues(t, 2, summary)
	require.Equal(t, time.Hour, tail)

	// The simple source-bucket precheck fits, but normal raw-tail work does not.
	start = now.Add(-7 * 24 * time.Hour)
	intervals := make([]dashboardInterval, 3000)
	for i := range intervals {
		intervals[i].Start = start
	}
	require.LessOrEqual(t, dashboardSummaryWork(intervals, start, now, dashboardHourlyResolution), int64(dashboardSummaryBudget))
	_, tail = dashboardHealthyRetainedWork(intervals, start, now, dashboardHourlyResolution, now)
	require.Greater(t, tail, dashboardSummaryTailBudget)
}
