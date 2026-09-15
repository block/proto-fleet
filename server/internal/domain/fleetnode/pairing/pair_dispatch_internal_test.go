package pairing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

func TestPairOnNodeRejectsOversizedBatchBeforeDispatch(t *testing.T) {
	targets := make([]*pairingpb.FleetNodePairTarget, MaxPairBatch+1)
	for i := range targets {
		targets[i] = &pairingpb.FleetNodePairTarget{DeviceIdentifier: "mac:device"}
	}

	err := (&Service{}).PairOnNode(t.Context(), 1, targets, nil, 1, nil, nil)

	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestPairOnNodeRequiresCommandProtocolV1(t *testing.T) {
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(7, "legacy", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_UNSPECIFIED)
	require.NoError(t, err)
	defer stream.Unregister()
	service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)

	err = service.PairOnNode(t.Context(), 7, []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: "miner-1"}}, nil, 1, nil, func([]*gatewaypb.FleetNodePairResult) error { return nil })

	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	select {
	case cmd := <-stream.Outgoing:
		t.Fatalf("legacy node received pairing command %q", cmd.GetCommandId())
	default:
	}
}
