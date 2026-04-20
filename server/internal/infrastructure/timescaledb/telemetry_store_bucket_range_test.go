package timescaledb

import (
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
