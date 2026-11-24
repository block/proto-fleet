package miner

import (
	"testing"
	"time"

	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRemoteFreshnessUsesObservationAgeInsteadOfNodeWallClock(t *testing.T) {
	started := time.Now()
	for _, nodeSkew := range []time.Duration{-time.Hour, time.Hour} {
		result := &telemetrypb.FleetNodeTelemetryResult{
			DeviceIdentifier: "miner", Timestamp: timestamppb.New(started.Add(nodeSkew)),
			TelemetryObservationAge: durationpb.New(10 * time.Minute),
		}
		metrics := telemetryResultToDeviceMetrics(result, nil, started)
		require.Equal(t, started.Add(-10*time.Minute), metrics.FreshnessObservedAt)
		payload := []byte(`{"timestamp":"2099-01-01T00:00:00Z","freshness_observed_at":"2099-01-01T00:00:00Z"}`)
		decodedMetrics, err := decodeTelemetryDeviceMetrics(payload)
		require.NoError(t, err)
		metrics = telemetryResultToDeviceMetrics(result, decodedMetrics, started)
		require.Equal(t, started.Add(-10*time.Minute), metrics.FreshnessObservedAt)
		require.Equal(t, result.GetTimestamp().AsTime(), metrics.Timestamp)
	}
	for _, age := range []*durationpb.Duration{nil, durationpb.New(-time.Second), {Seconds: 315576000001}} {
		metrics := telemetryResultToDeviceMetrics(&telemetrypb.FleetNodeTelemetryResult{Timestamp: timestamppb.Now(), TelemetryObservationAge: age}, nil, started)
		require.True(t, metrics.FreshnessObservedAt.IsZero())
	}
}
