package timescaledb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSelectUptimeDataSource(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		startTime time.Time
		want      uptimeDataSource
	}{
		{"last 24h uses raw", now.Add(-24 * time.Hour), uptimeDataSourceRaw},
		{"29 day window still uses raw", now.Add(-29 * 24 * time.Hour), uptimeDataSourceRaw},
		{"31 day window uses hourly rollup", now.Add(-31 * 24 * time.Hour), uptimeDataSourceHourly},
		{"1 year window uses hourly rollup", now.Add(-365 * 24 * time.Hour), uptimeDataSourceHourly},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := selectUptimeDataSource(tc.startTime)

			// Assert
			assert.Equal(t, tc.want, got)
		})
	}
}
