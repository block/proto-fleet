package timescaledb

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func TestRawMetricBucketDuration_PreservesFractionalSeconds(t *testing.T) {
	slideInterval := 1500 * time.Millisecond

	got := rawMetricBucketDuration(&slideInterval, false)

	assert.Equal(t, slideInterval, got)
}

func TestRawMetricWorkCount(t *testing.T) {
	assert.Equal(t, int64(0), rawMetricWorkCount(0, 10))
	assert.Equal(t, int64(0), rawMetricWorkCount(10, 0))
	assert.Equal(t, int64(1200), rawMetricWorkCount(120, 10))
	assert.Equal(t, int64(math.MaxInt64), rawMetricWorkCount(math.MaxInt64, 2))
}

func TestRawMetricBucketDurationForWork_CoarsensLargeFleet(t *testing.T) {
	endTime := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-24 * time.Hour)
	requestedBucketDuration := 10 * time.Second

	got := rawMetricBucketDurationForWork(startTime, endTime, requestedBucketDuration, 5000)
	gotBucketCount := rawMetricBucketCount(startTime, endTime, got)

	assert.Greater(t, got, requestedBucketDuration)
	assert.LessOrEqual(t, rawMetricWorkCount(gotBucketCount, 5000), int64(maxRawMetricWork))
}

func TestShouldUseHourlyForRawMetricSampleCost(t *testing.T) {
	endTime := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-24 * time.Hour)

	assert.False(t, shouldUseHourlyForRawMetricSampleCost(startTime, endTime, 100))
	assert.True(t, shouldUseHourlyForRawMetricSampleCost(startTime, endTime, 5000))
}

func TestShouldUseHourlyForRawMetricSampleCost_KeepsShortRangesRaw(t *testing.T) {
	endTime := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-15 * time.Minute)

	assert.False(t, shouldUseHourlyForRawMetricSampleCost(startTime, endTime, 5000))
}

func TestShouldUseHourlyForRawMetricSampleCost_BucketAlignment(t *testing.T) {
	tests := []struct {
		name      string
		startTime time.Time
		endTime   time.Time
		expected  bool
	}{
		{
			name:      "unaligned one-hour window stays raw",
			startTime: time.Date(2026, time.January, 10, 10, 15, 0, 0, time.UTC),
			endTime:   time.Date(2026, time.January, 10, 11, 15, 0, 0, time.UTC),
			expected:  false,
		},
		{
			name:      "aligned one-hour window routes to hourly",
			startTime: time.Date(2026, time.January, 10, 10, 0, 0, 0, time.UTC),
			endTime:   time.Date(2026, time.January, 10, 11, 0, 0, 0, time.UTC),
			expected:  true,
		},
		{
			name:      "unaligned window ending on boundary routes to hourly",
			startTime: time.Date(2026, time.January, 10, 10, 15, 0, 0, time.UTC),
			endTime:   time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC),
			expected:  true,
		},
		{
			name:      "unaligned multi-hour window routes to hourly",
			startTime: time.Date(2026, time.January, 10, 10, 15, 0, 0, time.UTC),
			endTime:   time.Date(2026, time.January, 10, 13, 15, 0, 0, time.UTC),
			expected:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := shouldUseHourlyForRawMetricSampleCost(tc.startTime, tc.endTime, 100000)

			// Assert
			assert.Equal(t, tc.expected, got)
		})
	}
}
