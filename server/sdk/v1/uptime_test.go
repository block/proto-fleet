package sdk

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDeviceMetricsUptimeRoundTrip(t *testing.T) {
	observedAt := time.Date(2026, 10, 15, 10, 0, 0, 123456789, time.UTC)
	for _, tc := range []struct {
		name       string
		uptime     *uint64
		observedAt time.Time
	}{
		{name: "unknown"},
		{name: "fresh boot", uptime: new(uint64(0)), observedAt: observedAt},
		{name: "running", uptime: new(uint64(3600)), observedAt: observedAt},
		{name: "missing observation time", uptime: new(uint64(1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Snapshot assembly can occur after a cached uptime sample was collected.
			metrics := DeviceMetrics{
				Timestamp:        observedAt.Add(time.Minute),
				UptimeSeconds:    tc.uptime,
				UptimeObservedAt: tc.observedAt,
			}
			wire := deviceMetricsToProto(metrics)
			encoded, err := proto.Marshal(wire)
			require.NoError(t, err)
			decoded := &pb.DeviceMetrics{}
			require.NoError(t, proto.Unmarshal(encoded, decoded))
			require.Equal(t, tc.uptime, decoded.UptimeSeconds)
			require.Equal(t, tc.observedAt.IsZero(), decoded.UptimeObservedAt == nil)
			roundTrip := deviceMetricsFromProto(decoded)
			require.Equal(t, tc.uptime, roundTrip.UptimeSeconds)
			require.Equal(t, tc.observedAt, roundTrip.UptimeObservedAt)
		})
	}
}

func TestDeviceMetricsInvalidUptimeObservationIsUnusable(t *testing.T) {
	metrics := deviceMetricsFromProto(&pb.DeviceMetrics{
		UptimeSeconds:    new(uint64(0)),
		UptimeObservedAt: &timestamppb.Timestamp{Nanos: -1},
	})
	require.True(t, metrics.UptimeObservedAt.IsZero())
}
