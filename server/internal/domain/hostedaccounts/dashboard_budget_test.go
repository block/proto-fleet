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
