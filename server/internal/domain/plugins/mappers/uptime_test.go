package mappers

import (
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestSDKDeviceMetricsToV2PreservesUptimeEvidence(t *testing.T) {
	observedAt := time.Date(2026, 10, 15, 10, 0, 0, 123456789, time.UTC)
	for _, uptime := range []*uint64{nil, new(uint64(0)), new(uint64(3600))} {
		metrics := SDKDeviceMetricsToV2(sdk.DeviceMetrics{
			Timestamp:        observedAt.Add(time.Minute),
			UptimeSeconds:    uptime,
			UptimeObservedAt: observedAt,
		})
		require.Equal(t, uptime, metrics.UptimeSeconds)
		require.Equal(t, observedAt, metrics.UptimeObservedAt)
	}
	metrics := SDKDeviceMetricsToV2(sdk.DeviceMetrics{Timestamp: observedAt, UptimeSeconds: new(uint64(0))})
	require.True(t, metrics.UptimeObservedAt.IsZero())
}
