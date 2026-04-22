package timescaledb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUseHourlyRollup(t *testing.T) {
	now := time.Now()

	t.Run("recent start uses raw", func(t *testing.T) {
		// Act
		got := useHourlyRollup(now.Add(-24 * time.Hour))

		// Assert
		assert.False(t, got)
	})

	t.Run("29 day window still uses raw", func(t *testing.T) {
		// Act
		got := useHourlyRollup(now.Add(-29 * 24 * time.Hour))

		// Assert
		assert.False(t, got)
	})

	t.Run("31 day window uses hourly rollup", func(t *testing.T) {
		// Act
		got := useHourlyRollup(now.Add(-31 * 24 * time.Hour))

		// Assert
		assert.True(t, got)
	})

	t.Run("1 year window uses hourly rollup", func(t *testing.T) {
		// Act
		got := useHourlyRollup(now.Add(-365 * 24 * time.Hour))

		// Assert
		assert.True(t, got)
	})
}
