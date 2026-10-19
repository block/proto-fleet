package main

import (
	"encoding/json"
	"testing"
	"time"

	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTelemetryResultPreservesUptimeEvidence(t *testing.T) {
	observedAt := time.Now().UTC().Add(-time.Minute)
	for _, tc := range []struct {
		name       string
		uptime     *uint64
		observedAt time.Time
	}{
		{name: "unknown"},
		{name: "timestamp without uptime", observedAt: observedAt},
		{name: "fresh boot", uptime: new(uint64(0)), observedAt: observedAt},
		{name: "running", uptime: new(uint64(3600)), observedAt: observedAt},
		{name: "missing observation time", uptime: new(uint64(1))},
		{name: "future observation time", uptime: new(uint64(1)), observedAt: time.Now().UTC().Add(time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := time.Now()
			result, err := telemetryResultFromV2("miner", modelsV2.DeviceMetrics{
				// The result timestamp must not replace the original observation time.
				Timestamp:        observedAt.Add(time.Minute),
				UptimeSeconds:    tc.uptime,
				UptimeObservedAt: tc.observedAt,
			}, telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE)
			after := time.Now()
			require.NoError(t, err)
			encoded, err := proto.Marshal(result)
			require.NoError(t, err)
			decoded := &telemetrypb.FleetNodeTelemetryResult{}
			require.NoError(t, proto.Unmarshal(encoded, decoded))
			require.Equal(t, tc.uptime, decoded.UptimeSeconds)
			if tc.uptime == nil || tc.observedAt.IsZero() || tc.observedAt.After(after) {
				require.Nil(t, decoded.UptimeObservationAge)
			} else {
				require.NotNil(t, decoded.UptimeObservationAge)
				require.NoError(t, decoded.UptimeObservationAge.CheckValid())
				age := decoded.UptimeObservationAge.AsDuration()
				require.GreaterOrEqual(t, age, before.Sub(tc.observedAt))
				require.LessOrEqual(t, age, after.Sub(tc.observedAt))
			}
			var payload modelsV2.DeviceMetrics
			require.NoError(t, json.Unmarshal(decoded.DeviceMetricsJson, &payload))
			require.Equal(t, tc.uptime, payload.UptimeSeconds)
			require.Equal(t, tc.observedAt, payload.UptimeObservedAt)
		})
	}
}
