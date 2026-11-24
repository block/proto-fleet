package command

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEquipmentWritableRequiresFreshKnownSupportedState(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name    string
		state   *pb.ContainerControls
		control string
		want    bool
	}{
		{"missing", nil, "dry_cooler", false},
		{"supported off", &pb.ContainerControls{ObservedAt: timestamppb.New(now), Supported: new(true), Controls: []*pb.ContainerControl{{Name: "dry_cooler", Enabled: new(false)}}}, "dry_cooler", true},
		{"unknown capability", &pb.ContainerControls{ObservedAt: timestamppb.New(now), Controls: []*pb.ContainerControl{{Name: "dry_cooler", Enabled: new(false)}}}, "dry_cooler", false},
		{"unknown state", &pb.ContainerControls{ObservedAt: timestamppb.New(now), Supported: new(true), Controls: []*pb.ContainerControl{{Name: "dry_cooler"}}}, "dry_cooler", false},
		{"expired", &pb.ContainerControls{ObservedAt: timestamppb.New(now.Add(-3 * time.Minute)), Supported: new(true), Controls: []*pb.ContainerControl{{Name: "dry_cooler", Enabled: new(false)}}}, "dry_cooler", false},
		{"breaker", &pb.ContainerControls{ObservedAt: timestamppb.New(now), MainCircuitBreakerSupported: new(true), MainCircuitBreakerClosed: new(false)}, "main_circuit_breaker", true},
		{"breaker unadvertised", &pb.ContainerControls{ObservedAt: timestamppb.New(now), MainCircuitBreakerClosed: new(false)}, "main_circuit_breaker", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, equipmentWritable(tc.state, sdk.ContainerControlRequest{Name: tc.control, Enabled: new(true)}, now))
		})
	}
}
