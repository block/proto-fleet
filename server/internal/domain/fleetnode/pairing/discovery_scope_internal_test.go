package pairing

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/assert"
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

func TestPairOnNodeDispatchesInScopeTargetsAndReportsTheRest(t *testing.T) {
	// Arrange
	registry := control.NewRegistry()
	stream := registry.Register(7)
	defer stream.Unregister()
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
	service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)
	targets := []*pairingpb.FleetNodePairTarget{
		{DeviceIdentifier: "in-scope", IpAddress: "10.0.0.1"},
		{DeviceIdentifier: "out-of-scope", IpAddress: "10.0.1.1"},
		{DeviceIdentifier: "stale-endpoint", IpAddress: "10.0.0.2", Endpoints: []*commonpb.DeviceEndpoint{{Host: "10.0.1.2"}}},
	}
	var mu sync.Mutex
	var results []*gatewaypb.FleetNodePairResult
	done := make(chan error, 1)

	// Act
	go func() {
		done <- service.PairOnNode(t.Context(), 7, targets, nil, 20, nil, func(batch []*gatewaypb.FleetNodePairResult) error {
			mu.Lock()
			defer mu.Unlock()
			results = append(results, batch...)
			return nil
		})
	}()
	var dispatched *gatewaypb.AgentCommand
	select {
	case cmd := <-stream.Outgoing:
		dispatched = &gatewaypb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), dispatched))
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.CommandId, Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
	case err := <-done:
		t.Fatalf("batch rejected instead of dispatching its in-scope target: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("pair command did not dispatch")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("pair command did not complete")
	}

	// Assert
	require.Len(t, dispatched.GetPair().GetTargets(), 1)
	assert.Equal(t, "in-scope", dispatched.GetPair().GetTargets()[0].GetDeviceIdentifier())
	mu.Lock()
	defer mu.Unlock()
	failed := map[string]string{}
	for _, result := range results {
		assert.Equal(t, gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR, result.GetOutcome())
		failed[result.GetDeviceIdentifier()] = result.GetErrorMessage()
	}
	assert.Contains(t, failed, "out-of-scope")
	assert.Contains(t, failed, "stale-endpoint")
	assert.Equal(t, outOfScopeMessage, failed["out-of-scope"])
}
