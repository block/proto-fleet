package telemetry

import (
	"math"
	"testing"
	"time"

	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/stretchr/testify/require"
)

func TestRebootLowerBound(t *testing.T) {
	now := time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		uptime     *uint64
		observedAt time.Time
		want       time.Time
	}{
		{name: "missing uptime", observedAt: now},
		{name: "missing observation", uptime: new(uint64(5))},
		{name: "future observation", uptime: new(uint64(5)), observedAt: now.Add(time.Second)},
		{name: "overflow", uptime: new(uint64(math.MaxUint64)), observedAt: now},
		{name: "duration overflow", uptime: new(uint64(math.MaxInt64 / int64(time.Second))), observedAt: now},
		{name: "largest safe duration", uptime: new(uint64(math.MaxInt64/int64(time.Second) - 1)), observedAt: now, want: now.Add(-time.Duration(math.MaxInt64/int64(time.Second)) * time.Second)},
		{name: "zero is observed uptime", uptime: new(uint64(0)), observedAt: now, want: now.Add(-time.Second)},
		{name: "whole second precision", uptime: new(uint64(5)), observedAt: now, want: now.Add(-6 * time.Second)},
		{name: "cached sample does not advance boot time", uptime: new(uint64(5)), observedAt: now.Add(-5 * time.Minute), want: now.Add(-5*time.Minute - 6*time.Second)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := (&modelsV2.DeviceMetrics{UptimeSeconds: test.uptime, UptimeObservedAt: test.observedAt}).BootTimeLowerBound(now)
			require.Equal(t, !test.want.IsZero(), ok)
			require.Equal(t, test.want, got)
		})
	}
}
