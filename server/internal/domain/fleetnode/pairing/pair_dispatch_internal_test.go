package pairing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

func TestPairAllOnNodePagesPastBatchLimit(t *testing.T) {
	for _, count := range []int{1, MaxPairBatch, MaxPairBatch + 1, 3*MaxPairBatch + 7} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var credentials *pairingpb.Credentials
			if count > MaxPairBatch {
				credentials = &pairingpb.Credentials{Username: "root", Password: proto.String("test-password")}
			}
			store := pairAllTestStore(count)
			registry := control.NewRegistry()
			stream := registry.Register(7)
			defer stream.Unregister()
			service := NewService(store, nil, nil).WithProvisioning(nil, nil, registry)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			var reported []string
			go func() {
				done <- service.PairAllOnNode(ctx, 7, 20, credentials, nil, func(results []*gatewaypb.FleetNodePairResult) error {
					for _, result := range results {
						reported = append(reported, result.GetDeviceIdentifier())
					}
					return nil
				})
			}()
			var dispatched []string
			for {
				select {
				case cmd := <-stream.Outgoing:
					var payload gatewaypb.AgentCommand
					require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &payload))
					assert.True(t, proto.Equal(credentials, payload.GetPair().GetCredentials()), "every batch must retain the supplied credentials")
					targets := payload.GetPair().GetTargets()
					require.NotEmpty(t, targets)
					require.LessOrEqual(t, len(targets), MaxPairBatch)
					for _, target := range targets {
						dispatched = append(dispatched, target.GetDeviceIdentifier())
					}
					// No results: every target remains eligible in the store. A
					// stable cursor must still advance past these failed targets.
					stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
				case err := <-done:
					require.NoError(t, err)
					expected := make([]string, count)
					for i, device := range store.devices {
						expected[i] = device.DeviceIdentifier
					}
					assert.Equal(t, expected, dispatched)
					assert.ElementsMatch(t, expected, reported)
					return
				case <-ctx.Done():
					t.Fatal("pair-all did not complete")
				}
			}
		})
	}
}

func TestPairAllOnNodeStopsAfterCommandError(t *testing.T) {
	store := pairAllTestStore(MaxPairBatch + 1)
	registry := control.NewRegistry()
	stream := registry.Register(7)
	defer stream.Unregister()
	service := NewService(store, nil, nil).WithProvisioning(nil, nil, registry)
	done := make(chan error, 1)
	go func() {
		done <- service.PairAllOnNode(t.Context(), 7, 20, nil, nil, func([]*gatewaypb.FleetNodePairResult) error { return nil })
	}()
	select {
	case cmd := <-stream.Outgoing:
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_REPORT_FAILED})
	case <-time.After(5 * time.Second):
		t.Fatal("pair command was not dispatched")
	}
	select {
	case err := <-done:
		require.Error(t, err)
		require.Len(t, store.calls, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("pair-all did not stop after command failure")
	}
}

func TestPairAllOnNodeFinishesInFlightBatchAfterCancellation(t *testing.T) {
	store := pairAllTestStore(MaxPairBatch + 1)
	registry := control.NewRegistry()
	stream := registry.Register(7)
	defer stream.Unregister()
	service := NewService(store, nil, nil).WithProvisioning(nil, nil, registry)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	reported := 0
	go func() {
		done <- service.PairAllOnNode(ctx, 7, 20, nil, nil, func(results []*gatewaypb.FleetNodePairResult) error {
			reported += len(results)
			return nil
		})
	}()
	select {
	case cmd := <-stream.Outgoing:
		cancel()
		select {
		case err := <-done:
			t.Fatalf("in-flight command aborted: %v", err)
		default:
		}
		registry.PublishPairResults(7, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "miner-1", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
	case <-time.After(5 * time.Second):
		t.Fatal("pair command was not dispatched")
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, MaxPairBatch, reported)
		require.Len(t, store.calls, 1, "do not start another batch after operator cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("pair-all did not stop after finishing in-flight command")
	}
}

func TestPairAllOnNodeRejectsEmptySelection(t *testing.T) {
	service := NewService(pairAllTestStore(0), nil, nil)
	err := service.PairAllOnNode(t.Context(), 7, 20, nil, nil, nil)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func pairAllTestStore(count int) *pagingPairTargetStore {
	store := &pagingPairTargetStore{devices: make([]FleetNodeDiscoveredDevice, count)}
	for i := range store.devices {
		store.devices[i] = FleetNodeDiscoveredDevice{ID: int64(i + 1), DeviceIdentifier: fmt.Sprintf("miner-%d", i+1)}
	}
	return store
}

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
