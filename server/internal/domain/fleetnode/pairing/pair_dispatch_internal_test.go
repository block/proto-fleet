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

func TestPairOnNodePartialAckPreservesReportedAndMissingResults(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(7)
	defer stream.Unregister()
	service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)
	go func() {
		cmd := <-stream.Outgoing
		registry.PublishPairResults(7, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "paired", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
	}()
	var batches [][]*gatewaypb.FleetNodePairResult
	err := service.PairOnNode(t.Context(), 7, []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: "paired"}, {DeviceIdentifier: "missing"}}, nil, 1, nil,
		func(results []*gatewaypb.FleetNodePairResult) error { batches = append(batches, results); return nil })
	require.NoError(t, err)
	require.Len(t, batches, 2, "PARTIAL must not introduce an empty callback")
	require.Len(t, batches[0], 1)
	assert.Equal(t, "paired", batches[0][0].GetDeviceIdentifier())
	assert.Equal(t, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, batches[0][0].GetOutcome())
	require.Len(t, batches[1], 1)
	assert.Equal(t, "missing", batches[1][0].GetDeviceIdentifier())
	assert.Equal(t, gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR, batches[1][0].GetOutcome())
}
