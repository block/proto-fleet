package pairing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

func TestPairOnNodeRejectsOutsideLiveDiscoveryScopeBeforeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *gatewaypb.DiscoveryConfig
		target *pairingpb.FleetNodePairTarget
	}{
		{name: "missing live config", target: &pairingpb.FleetNodePairTarget{DeviceIdentifier: "miner", IpAddress: "10.0.0.1"}},
		{name: "empty live ranges", config: &gatewaypb.DiscoveryConfig{}, target: &pairingpb.FleetNodePairTarget{DeviceIdentifier: "miner", IpAddress: "10.0.0.1"}},
		{name: "outside range", config: &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}}, target: &pairingpb.FleetNodePairTarget{DeviceIdentifier: "miner", IpAddress: "10.0.1.1"}},
		{name: "outside group member", config: &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}}, target: &pairingpb.FleetNodePairTarget{DeviceIdentifier: "miner", IpAddress: "10.0.0.1", Endpoints: []*commonpb.DeviceEndpoint{{Host: "10.0.0.1"}, {Host: "10.0.1.1"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := control.NewRegistry()
			stream := registry.Register(7)
			defer stream.Unregister()
			stream.SetDiscoveryConfig(tc.config)
			service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			err := service.PairOnNode(ctx, 7, []*pairingpb.FleetNodePairTarget{tc.target}, nil, 20, nil, func([]*gatewaypb.FleetNodePairResult) error { return nil })
			require.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
			require.Empty(t, stream.Outgoing)
		})
	}
}
