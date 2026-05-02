package timescaledb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSelectUptimeDataSource(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		duration time.Duration
		want     uptimeDataSource
	}{
		{"last 1h uses raw", 1 * time.Hour, uptimeDataSourceRaw},
		{"last 24h uses raw", 24 * time.Hour, uptimeDataSourceRaw},
		{"5 day window uses hourly", 5 * 24 * time.Hour, uptimeDataSourceHourly},
		{"exactly 10 days uses hourly", 10 * 24 * time.Hour, uptimeDataSourceHourly},
		{"11 day window uses daily", 11 * 24 * time.Hour, uptimeDataSourceDaily},
		{"1 year window uses daily", 365 * 24 * time.Hour, uptimeDataSourceDaily},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			start := now.Add(-tc.duration)

			// Act
			got := selectUptimeDataSource(&start, &now)

			// Assert
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("nil range defaults to raw", func(t *testing.T) {
		// Act
		got := selectUptimeDataSource(nil, nil)

		// Assert
		assert.Equal(t, uptimeDataSourceRaw, got)
	})
}
