package hostedaccounts

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/stretchr/testify/require"
)

func TestDashboardHistoryTierAndResolution(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name             string
		age, span        time.Duration
		requested        int32
		resolution, step time.Duration
	}{
		{"recent day", 0, 24 * time.Hour, 90, 0, 90 * time.Second},
		{"week", 0, 7 * 24 * time.Hour, 900, time.Hour, time.Hour},
		{"month", 0, 30 * 24 * time.Hour, 2700, 24 * time.Hour, 24 * time.Hour},
		{"old short window", 20 * 24 * time.Hour, time.Hour, 90, time.Hour, time.Hour},
		{"expired hourly", 100 * 24 * time.Hour, time.Hour, 90, 24 * time.Hour, 24 * time.Hour},
		{"round source multiple", 0, 7 * 24 * time.Hour, 4500, time.Hour, 2 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			end := now.Add(-tc.age)
			resolution, step := dashboardHistoryResolution(now, end.Add(-tc.span), end, tc.requested)
			require.Equal(t, tc.resolution, resolution)
			require.Equal(t, tc.step, step)
		})
	}
}

func TestDashboardDirectSummaryBucketsEligibility(t *testing.T) {
	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	miner := dashboardInterval{IntervalID: 1, Kind: int32(pb.MiningAssetKind_MINING_ASSET_KIND_MINER), AssetID: 42, Start: start}
	recalled := miner
	recalled.IntervalID = 2
	module := miner
	module.Kind = int32(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE)
	for _, tc := range []struct {
		name             string
		intervals        []dashboardInterval
		start            time.Time
		resolution, step time.Duration
		direct           bool
	}{
		{"hourly", []dashboardInterval{miner}, start, time.Hour, time.Hour, true},
		{"daily", []dashboardInterval{miner}, start, 24 * time.Hour, 24 * time.Hour, true},
		{"coarser output", []dashboardInterval{miner}, start, time.Hour, 2 * time.Hour, false},
		{"raw", []dashboardInterval{miner}, start, dashboardRawResolution, time.Hour, false},
		{"unaligned output", []dashboardInterval{miner}, start.Add(30 * time.Minute), time.Hour, time.Hour, false},
		{"repeated asset", []dashboardInterval{miner, recalled}, start, time.Hour, time.Hour, false},
		{"typed ID collision", []dashboardInterval{miner, module}, start, time.Hour, time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.direct, dashboardDirectSummaryBuckets(tc.intervals, tc.start, tc.resolution, tc.step))
		})
	}
}
