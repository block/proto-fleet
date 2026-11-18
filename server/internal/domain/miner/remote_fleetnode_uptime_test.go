package miner

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRemoteFleetNodeUptimeUsesServerRequestStart(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(12)
	defer stream.Unregister()
	miner := newTestRemoteFleetNodeMiner(t, registry)
	requestStartedAt := time.Now().UTC()
	var clock atomic.Int64
	clock.Store(requestStartedAt.UnixNano())
	miner.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	results := make(chan metricsResult, 1)
	go func() {
		metrics, err := miner.GetDeviceMetrics(context.Background())
		results <- metricsResult{metrics: metrics, err: err}
	}()

	cmd := receiveRemoteCommand(t, stream)
	clock.Store(requestStartedAt.Add(4 * time.Second).UnixNano())
	result := telemetryResult("node-device")
	result.UptimeSeconds = new(uint64(0))
	result.UptimeObservationAge = durationpb.New(time.Second)
	publishTelemetryAck(t, stream, cmd.GetCommandId(), result)

	got := receiveMetricsResult(t, results)
	require.NoError(t, got.err)
	require.Equal(t, new(uint64(0)), got.metrics.UptimeSeconds)
	require.Equal(t, requestStartedAt.Add(-time.Second), got.metrics.UptimeObservedAt)
}

func TestFleetNodeTelemetryUptimeRoundTrip(t *testing.T) {
	requestStartedAt := time.Date(2026, 10, 15, 10, 0, 0, 123456789, time.UTC)
	for _, tc := range []struct {
		name   string
		uptime *uint64
		age    *durationpb.Duration
	}{
		{name: "unknown"},
		{name: "age without uptime", age: durationpb.New(time.Second)},
		{name: "fresh boot", uptime: new(uint64(0)), age: durationpb.New(time.Second)},
		{name: "running cached sample", uptime: new(uint64(3600)), age: durationpb.New(time.Minute)},
		{name: "zero age", uptime: new(uint64(1)), age: durationpb.New(0)},
	} {
		for _, payload := range []string{"scalars", "json"} {
			t.Run(tc.name+"/"+payload, func(t *testing.T) {
				result := &telemetrypb.FleetNodeTelemetryResult{
					Timestamp:            timestamppb.New(requestStartedAt.Add(time.Second)),
					UptimeSeconds:        tc.uptime,
					UptimeObservationAge: tc.age,
				}
				if payload == "json" {
					encoded, err := json.Marshal(modelsV2.DeviceMetrics{
						UptimeSeconds:    tc.uptime,
						UptimeObservedAt: requestStartedAt.Add(time.Hour),
					})
					require.NoError(t, err)
					result.DeviceMetricsJson = encoded
				}
				encoded, err := proto.Marshal(result)
				require.NoError(t, err)
				decoded := &telemetrypb.FleetNodeTelemetryResult{}
				require.NoError(t, proto.Unmarshal(encoded, decoded))
				decodedMetrics, err := decodeTelemetryDeviceMetrics(decoded.GetDeviceMetricsJson())
				require.NoError(t, err)
				metrics := telemetryResultToDeviceMetrics(decoded, decodedMetrics, requestStartedAt)
				require.Equal(t, tc.uptime, metrics.UptimeSeconds)
				if tc.uptime == nil || tc.age == nil {
					require.True(t, metrics.UptimeObservedAt.IsZero())
				} else {
					require.Equal(t, requestStartedAt.Add(-tc.age.AsDuration()), metrics.UptimeObservedAt)
				}
				require.Equal(t, result.Timestamp.AsTime(), metrics.Timestamp)
			})
		}
	}
}

func TestFleetNodeUptimeIgnoresNodeClockOffset(t *testing.T) {
	requestStartedAt := time.Now().UTC()
	for _, offset := range []time.Duration{-12 * time.Hour, 12 * time.Hour} {
		encoded, err := json.Marshal(modelsV2.DeviceMetrics{
			UptimeSeconds:    new(uint64(999)),
			UptimeObservedAt: requestStartedAt.Add(offset),
		})
		require.NoError(t, err)
		result := &telemetrypb.FleetNodeTelemetryResult{
			UptimeSeconds:        new(uint64(0)),
			UptimeObservationAge: durationpb.New(time.Minute),
		}
		decodedMetrics, err := decodeTelemetryDeviceMetrics(encoded)
		require.NoError(t, err)
		metrics := telemetryResultToDeviceMetrics(result, decodedMetrics, requestStartedAt)
		require.Equal(t, new(uint64(0)), metrics.UptimeSeconds)
		require.Equal(t, requestStartedAt.Add(-time.Minute), metrics.UptimeObservedAt)
	}
}

func TestFleetNodeInvalidUptimeAgeCannotUseJSONEvidence(t *testing.T) {
	now := time.Now().UTC()
	encoded, err := json.Marshal(modelsV2.DeviceMetrics{
		UptimeSeconds:    new(uint64(0)),
		UptimeObservedAt: now,
	})
	require.NoError(t, err)
	for _, age := range []*durationpb.Duration{nil, durationpb.New(-time.Second), {Nanos: 1_000_000_000}} {
		for _, payload := range [][]byte{nil, encoded} {
			decodedMetrics, err := decodeTelemetryDeviceMetrics(payload)
			require.NoError(t, err)
			metrics := telemetryResultToDeviceMetrics(&telemetrypb.FleetNodeTelemetryResult{
				UptimeSeconds:        new(uint64(0)),
				UptimeObservationAge: age,
			}, decodedMetrics, now)
			require.Nil(t, metrics.UptimeSeconds)
			require.True(t, metrics.UptimeObservedAt.IsZero())
		}
	}
}
