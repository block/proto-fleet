package timescaledb

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCompleteBucketRange_Hourly(t *testing.T) {
	startTime := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)

	t.Run("excludes in-progress trailing bucket", func(t *testing.T) {
		endTime := time.Date(2026, time.January, 10, 10, 35, 0, 0, time.UTC)

		gotStart, gotEnd, ok := normalizeCompleteBucketRange(startTime, endTime, hourlyBucketDuration)
		assert.True(t, ok)
		assert.Equal(t, startTime, gotStart)
		assert.Equal(t, time.Date(2026, time.January, 10, 9, 35, 0, 0, time.UTC), gotEnd)
	})

	t.Run("includes most recent complete bucket when end is at boundary", func(t *testing.T) {
		endTime := time.Date(2026, time.January, 10, 11, 0, 0, 0, time.UTC)

		_, gotEnd, ok := normalizeCompleteBucketRange(startTime, endTime, hourlyBucketDuration)
		assert.True(t, ok)
		assert.Equal(t, time.Date(2026, time.January, 10, 10, 0, 0, 0, time.UTC), gotEnd)
	})
}

func TestNormalizeCompleteBucketRange_Daily(t *testing.T) {
	startTime := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)

	gotStart, gotEnd, ok := normalizeCompleteBucketRange(startTime, endTime, dailyBucketDuration)
	assert.True(t, ok)
	assert.Equal(t, startTime, gotStart)
	assert.Equal(t, time.Date(2026, time.January, 14, 12, 0, 0, 0, time.UTC), gotEnd)
}

func TestNormalizeCompleteBucketRange_NoCompleteBuckets(t *testing.T) {
	startTime := time.Date(2026, time.January, 10, 10, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, time.January, 10, 10, 30, 0, 0, time.UTC)

	_, _, ok := normalizeCompleteBucketRange(startTime, endTime, hourlyBucketDuration)
	assert.False(t, ok)
}

func TestUptimeRollupCoverage(t *testing.T) {
	start := time.Date(2026, time.January, 10, 10, 0, 0, 0, time.UTC)

	t.Run("complete contiguous rollup", func(t *testing.T) {
		counts := []models.UptimeStatusCount{
			{Timestamp: start},
			{Timestamp: start.Add(time.Minute)},
		}

		complete, rawTailStart, canMergeTail := uptimeRollupCoverage(counts, start, start.Add(time.Minute), time.Minute)

		assert.True(t, complete)
		assert.False(t, canMergeTail)
		assert.True(t, rawTailStart.IsZero())
	})

	t.Run("fresh tail can be merged from raw snapshots", func(t *testing.T) {
		counts := []models.UptimeStatusCount{{Timestamp: start}}

		complete, rawTailStart, canMergeTail := uptimeRollupCoverage(counts, start, start.Add(time.Minute), time.Minute)

		assert.False(t, complete)
		assert.True(t, canMergeTail)
		// The tail starts at the last rollup bucket so a partially
		// materialized bucket is recomputed from raw
		assert.Equal(t, start, rawTailStart)
	})

	t.Run("head or middle gaps need full raw fallback", func(t *testing.T) {
		headComplete, _, headTail := uptimeRollupCoverage(
			[]models.UptimeStatusCount{{Timestamp: start.Add(time.Minute)}},
			start,
			start.Add(time.Minute),
			time.Minute,
		)
		assert.False(t, headComplete)
		assert.False(t, headTail)

		middleComplete, _, middleTail := uptimeRollupCoverage(
			[]models.UptimeStatusCount{{Timestamp: start}, {Timestamp: start.Add(2 * time.Minute)}},
			start,
			start.Add(2*time.Minute),
			time.Minute,
		)
		assert.False(t, middleComplete)
		assert.False(t, middleTail)
	})
}

func TestSelectDataSource(t *testing.T) {
	// Arrange
	end := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		duration    time.Duration
		deviceCount int
		expected    dataSource
	}{
		{"all devices short range stays raw", time.Hour, 0, dataSourceRaw},
		{"all devices at the wide-selector cap stays raw", rawAllDevicesMaxDuration, 0, dataSourceRaw},
		{"all devices past the wide-selector cap routes hourly", rawAllDevicesMaxDuration + time.Second, 0, dataSourceHourly},
		{"all devices full day routes hourly", 24 * time.Hour, 0, dataSourceHourly},
		{"all devices beyond hourly window routes daily", 11 * 24 * time.Hour, 0, dataSourceDaily},
		{"single device full day stays raw", 24 * time.Hour, 1, dataSourceRaw},
		{"list at the size cap stays raw", 24 * time.Hour, maxRawDeviceList, dataSourceRaw},
		{"list past the size cap routes hourly", 24 * time.Hour, maxRawDeviceList + 1, dataSourceHourly},
		{"huge list short range stays raw", 4 * time.Hour, maxRawDeviceList + 1, dataSourceRaw},
		{"narrow list past a day routes hourly", 25 * time.Hour, 1, dataSourceHourly},
		{"narrow list past hourly window routes daily", 11 * 24 * time.Hour, 1, dataSourceDaily},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := selectDataSource(end.Add(-tt.duration), end, tt.deviceCount)

			// Assert
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestRawMetricBucketDuration_PreservesFractionalSeconds(t *testing.T) {
	slideInterval := 1500 * time.Millisecond

	got := rawMetricBucketDuration(&slideInterval, false)

	assert.Equal(t, slideInterval, got)
}

func TestFleetMetricRollupEligible(t *testing.T) {
	start := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	tests := []struct {
		name           string
		query          models.CombinedMetricsQuery
		bucketDuration time.Duration
		end            time.Time
		want           bool
	}{
		{
			name:           "org all-devices dashboard query uses rollup",
			query:          models.CombinedMetricsQuery{OrganizationID: 42},
			bucketDuration: models.FleetMetricRollupBucketDuration,
			end:            end,
			want:           true,
		},
		{
			name: "site scope resolved by service stays raw",
			query: models.CombinedMetricsQuery{
				OrganizationID:          42,
				DeviceIDs:               []models.DeviceIdentifier{"device-a", "device-b"},
				SiteIDs:                 []int64{7},
				DeviceListFromSiteScope: true,
			},
			bucketDuration: models.FleetMetricRollupBucketDuration,
			end:            end,
			want:           false,
		},
		{
			name: "explicit device list stays raw",
			query: models.CombinedMetricsQuery{
				OrganizationID: 42,
				DeviceIDs:      []models.DeviceIdentifier{"device-a"},
			},
			bucketDuration: models.FleetMetricRollupBucketDuration,
			end:            end,
			want:           false,
		},
		{
			name:           "legacy no-org query stays raw",
			query:          models.CombinedMetricsQuery{},
			bucketDuration: models.FleetMetricRollupBucketDuration,
			end:            end,
			want:           false,
		},
		{
			name:           "non-dashboard bucket stays raw",
			query:          models.CombinedMetricsQuery{OrganizationID: 42},
			bucketDuration: time.Minute,
			end:            end,
			want:           false,
		},
		{
			name:           "longer than raw window stays on hourly or daily path",
			query:          models.CombinedMetricsQuery{OrganizationID: 42},
			bucketDuration: models.FleetMetricRollupBucketDuration,
			end:            start.Add(fleetMetricRollupReadMaxDuration + time.Second),
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fleetMetricRollupEligible(tt.query, start, tt.end, tt.bucketDuration)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFleetMetricRollupWindows(t *testing.T) {
	aligned := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	end := aligned.Add(4*models.FleetMetricRollupBucketDuration - time.Nanosecond)

	bodyStart, bodyEndExclusive, ok := fleetMetricRollupWindows(aligned, end)

	require.True(t, ok)
	assert.Equal(t, aligned, bodyStart)
	assert.Equal(t, aligned.Add(2*models.FleetMetricRollupBucketDuration), bodyEndExclusive)
	assert.Equal(t, int64(2), fleetRollupBucketCountExclusive(bodyStart, bodyEndExclusive))

	_, _, ok = fleetMetricRollupWindows(aligned, aligned.Add(2*models.FleetMetricRollupBucketDuration-time.Nanosecond))
	assert.False(t, ok, "two-bucket windows are served entirely from raw tail")
}

func TestCompleteRawBucketWindow(t *testing.T) {
	// Arrange: grid-aligned reference on the 90s time_bucket grid
	aligned := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)

	t.Run("unaligned edges shrink to whole buckets", func(t *testing.T) {
		// Act: window starts 10s into a bucket and ends 30s into another
		start, end, ok := completeRawBucketWindow(aligned.Add(10*time.Second), aligned.Add(4*90*time.Second+30*time.Second), 90*time.Second)

		// Assert: first partial and in-progress final buckets are excluded
		require.True(t, ok)
		assert.True(t, start.Equal(aligned.Add(90*time.Second)))
		assert.True(t, end.Equal(aligned.Add(4*90*time.Second-time.Nanosecond)))
	})

	t.Run("aligned end keeps its final complete bucket", func(t *testing.T) {
		start, end, ok := completeRawBucketWindow(aligned, aligned.Add(2*90*time.Second), 90*time.Second)

		require.True(t, ok)
		assert.True(t, start.Equal(aligned))
		assert.True(t, end.Equal(aligned.Add(2*90*time.Second-time.Nanosecond)))
	})

	t.Run("window smaller than one bucket has no complete bucket", func(t *testing.T) {
		_, _, ok := completeRawBucketWindow(aligned.Add(10*time.Second), aligned.Add(80*time.Second), 90*time.Second)

		assert.False(t, ok)
	})
}

func TestRawMetricBucketCount(t *testing.T) {
	// Arrange
	startTime := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)

	// Assert
	assert.Equal(t, int64(0), rawMetricBucketCount(startTime, startTime.Add(-time.Second), time.Minute))
	assert.Equal(t, int64(1), rawMetricBucketCount(startTime, startTime, time.Minute))
	assert.Equal(t, int64(1441), rawMetricBucketCount(startTime, startTime.Add(24*time.Hour), time.Minute))

	// An unaligned window straddles one more time_bucket boundary than its
	// duration implies: 00:00:59..00:01:00 at 10s spans buckets :50 and :00
	unaligned := time.Date(2026, time.January, 10, 0, 0, 59, 0, time.UTC)
	assert.Equal(t, int64(2), rawMetricBucketCount(unaligned, unaligned.Add(time.Second), 10*time.Second))
	assert.Equal(t, int64(1), rawMetricBucketCount(unaligned, unaligned.Add(time.Second-time.Nanosecond), 10*time.Second))

	// Pre-origin timestamps still floor to the correct bucket index
	preOrigin := time.Date(1999, time.December, 31, 23, 59, 59, 0, time.UTC)
	assert.Equal(t, int64(2), rawMetricBucketCount(preOrigin, preOrigin.Add(time.Second), 10*time.Second))
}
