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
	for _, count := range []int{MaxPairBatch, MaxPairBatch + 1, 3*MaxPairBatch + 7} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var credentials *pairingpb.Credentials
			if count > MaxPairBatch {
				credentials = &pairingpb.Credentials{Username: "root", Password: proto.String("test-password")}
			}
			store := pairAllTestStore(count)
			registry := control.NewRegistry()
			stream := registry.Register(7)
			stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
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

func TestPairAllOnNodeContinuesAfterEntirePageOutsideDiscoveryScope(t *testing.T) {
	store := pairAllTestStore(MaxPairBatch + 1)
	for i := range MaxPairBatch {
		store.devices[i].IPAddress = "10.0.1.1"
	}
	registry := control.NewRegistry()
	stream := registry.Register(7)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
	defer stream.Unregister()
	service := NewService(store, nil, nil).WithProvisioning(nil, nil, registry)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var results []*gatewaypb.FleetNodePairResult
	go func() {
		done <- service.PairAllOnNode(ctx, 7, 20, nil, nil, func(batch []*gatewaypb.FleetNodePairResult) error {
			results = append(results, batch...)
			return fmt.Errorf("display disconnected")
		})
	}()
	select {
	case cmd := <-stream.Outgoing:
		var payload gatewaypb.AgentCommand
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &payload))
		require.Len(t, payload.GetPair().GetTargets(), 1)
		lastID := store.devices[MaxPairBatch].DeviceIdentifier
		assert.Equal(t, lastID, payload.GetPair().GetTargets()[0].GetDeviceIdentifier())
		registry.PublishPairResults(7, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: lastID, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
	case err := <-done:
		t.Fatalf("rejected first page stopped pagination: %v", err)
	case <-ctx.Done():
		t.Fatal("valid second page was not dispatched")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("pair-all did not finish")
	}
	require.Len(t, store.calls, 2)
	require.NotNil(t, store.calls[1].filter.CursorID)
	assert.Equal(t, int64(MaxPairBatch), *store.calls[1].filter.CursorID)
	require.Len(t, results, MaxPairBatch+1)
	for i, result := range results[:MaxPairBatch] {
		assert.Equal(t, store.devices[i].DeviceIdentifier, result.GetDeviceIdentifier())
		assert.Equal(t, gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR, result.GetOutcome())
		assert.Equal(t, outOfScopeMessage, result.GetErrorMessage())
	}
	assert.Equal(t, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, results[MaxPairBatch].GetOutcome())
}

func TestPairAllOnNodeStopsAfterCommandError(t *testing.T) {
	store := pairAllTestStore(MaxPairBatch + 1)
	registry := control.NewRegistry()
	stream := registry.Register(7)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
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
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
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

func TestPairAllOnNodeCancellationBeforeEnqueueStopsDispatch(t *testing.T) {
	store := pairAllTestStore(MaxPairBatch + 1)
	registry := control.NewRegistry()
	stream := registry.Register(7)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
	defer stream.Unregister()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Cancel after PairAllOnNode's final context check but before Registry.Send
	// enqueues the command. The real registry must still see the cancellation.
	sender := cancelBeforeEnqueueSender{Sender: registry, cancel: cancel, stream: stream}
	service := NewService(store, nil, nil).WithProvisioning(nil, nil, sender)

	err := service.PairAllOnNode(ctx, 7, 20, nil, nil, func([]*gatewaypb.FleetNodePairResult) error { return nil })

	assert.True(t, fleeterror.IsCanceledError(err), "enqueue cancellation must remain a canceled error: %v", err)
	require.Len(t, store.calls, 1)
	select {
	case cmd := <-stream.Outgoing:
		t.Fatalf("canceled request dispatched command %q", cmd.GetCommandId())
	default:
	}
}

type cancelBeforeEnqueueSender struct {
	control.Sender
	cancel context.CancelFunc
	stream *control.Stream
}

func (s cancelBeforeEnqueueSender) Send(ctx context.Context, nodeID int64, minimumVersion gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand, scope control.ReportScope, kind control.ReportKind, pair *control.PairMeta) (*control.Session, error) {
	s.cancel()
	session, err := s.Sender.Send(ctx, nodeID, minimumVersion, cmd, scope, kind, pair)
	if err == nil {
		// Let the regressed implementation finish rather than waiting 12 minutes.
		s.stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
	}
	return session, err
}

func TestPairAllOnNodeRejectsEmptySelection(t *testing.T) {
	service := NewService(pairAllTestStore(0), nil, nil)
	err := service.PairAllOnNode(t.Context(), 7, 20, nil, nil, nil)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func pairAllTestStore(count int) *pagingPairTargetStore {
	store := &pagingPairTargetStore{devices: make([]FleetNodeDiscoveredDevice, count)}
	for i := range store.devices {
		store.devices[i] = FleetNodeDiscoveredDevice{ID: int64(i + 1), DeviceIdentifier: fmt.Sprintf("miner-%d", i+1), IPAddress: "10.0.0.1"}
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
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
	service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)

	err = service.PairOnNode(t.Context(), 7, []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: "miner-1", IpAddress: "10.0.0.1"}}, nil, 1, nil, func([]*gatewaypb.FleetNodePairResult) error { return nil })

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
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
	defer stream.Unregister()
	service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)
	go func() {
		cmd := <-stream.Outgoing
		registry.PublishPairResults(7, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "paired", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
	}()
	var batches [][]*gatewaypb.FleetNodePairResult
	err := service.PairOnNode(t.Context(), 7, []*pairingpb.FleetNodePairTarget{{DeviceIdentifier: "paired", IpAddress: "10.0.0.1"}, {DeviceIdentifier: "missing", IpAddress: "10.0.0.2"}}, nil, 1, nil,
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

func TestPairTargetsOnNodeChunksPublicSelectionAndStopsNewCommands(t *testing.T) {
	for _, stop := range []string{"complete", "cancel", "command-error"} {
		t.Run(stop, func(t *testing.T) {
			registry := control.NewRegistry()
			stream := registry.Register(7)
			stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
			defer stream.Unregister()
			service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)
			credentials := &pairingpb.Credentials{Username: "root", Password: proto.String("secret")}
			targets := pairTargetsFromDiscoveredDevices(pairAllTestStore(8 * MaxPairBatch).devices)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			delivered := make(chan struct{}, 2*len(targets))
			reported := 0
			awaitDelivery := func() {
				t.Helper()
				select {
				case <-delivered:
				case <-time.After(2 * time.Second):
					t.Fatal("results of an accepted command were not delivered")
				}
			}
			pairedResults := func(targets []*pairingpb.FleetNodePairTarget) []*gatewaypb.FleetNodePairResult {
				results := make([]*gatewaypb.FleetNodePairResult, len(targets))
				for i, target := range targets {
					results[i] = &gatewaypb.FleetNodePairResult{DeviceIdentifier: target.GetDeviceIdentifier(), Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}
				}
				return results
			}

			go func() {
				done <- service.PairTargetsOnNode(ctx, 7, targets, credentials, 20, nil, func(results []*gatewaypb.FleetNodePairResult) error {
					reported += len(results)
					delivered <- struct{}{}
					return nil
				})
			}()
			var seen []string
			commands := 0
			for {
				select {
				case cmd := <-stream.Outgoing:
					commands++
					var payload gatewaypb.AgentCommand
					require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &payload))
					assert.True(t, proto.Equal(credentials, payload.GetPair().GetCredentials()))
					batch := payload.GetPair().GetTargets()
					require.LessOrEqual(t, len(batch), MaxPairBatch)
					for _, target := range batch {
						seen = append(seen, target.GetDeviceIdentifier())
					}
					code := gatewaypb.AckCode_ACK_CODE_PARTIAL
					if stop == "cancel" {
						registry.PublishPairResults(7, cmd.GetCommandId(), pairedResults(batch[:1]))
						awaitDelivery()
						cancel()
						registry.PublishPairResults(7, cmd.GetCommandId(), pairedResults(batch[1:]))
						awaitDelivery()
					}
					if stop == "command-error" {
						code = gatewaypb.AckCode_ACK_CODE_REPORT_FAILED
					}
					stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: code})
				case err := <-done:
					if stop == "complete" {
						require.NoError(t, err)
						assert.Equal(t, len(targets)/MaxPairBatch, commands)
						expected := make([]string, len(targets))
						for i, target := range targets {
							expected[i] = target.GetDeviceIdentifier()
						}
						assert.Equal(t, expected, seen)
					} else {
						require.Error(t, err)
						assert.Equal(t, 1, commands)
						assert.Len(t, seen, MaxPairBatch)
						if stop == "cancel" {
							assert.ErrorIs(t, err, context.Canceled)
						}
					}
					assert.Equal(t, len(seen), reported, "accepted batch has terminal results before returning")
					return
				case <-time.After(2 * time.Second):
					t.Fatal("explicit pairing did not finish")
				}
			}
		})
	}
}

func TestPairTargetsOnNodeSkipsOutOfScopeBatchAndReturnsItsError(t *testing.T) {
	registry := control.NewRegistry()
	stream := registry.Register(7)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
	defer stream.Unregister()
	service := NewService(nil, nil, nil).WithProvisioning(nil, nil, registry)
	targets := make([]*pairingpb.FleetNodePairTarget, 2*MaxPairBatch)
	for i := range targets {
		address := "10.0.0.1"
		if i < MaxPairBatch {
			address = "10.9.9.9"
		}
		targets[i] = &pairingpb.FleetNodePairTarget{DeviceIdentifier: fmt.Sprintf("miner-%d", i), IpAddress: address}
	}
	var reported []*gatewaypb.FleetNodePairResult
	done := make(chan error, 1)

	go func() {
		done <- service.PairTargetsOnNode(t.Context(), 7, targets, nil, 20, nil, func(results []*gatewaypb.FleetNodePairResult) error {
			reported = append(reported, results...)
			return nil
		})
	}()
	var dispatched []string
	select {
	case cmd := <-stream.Outgoing:
		var payload gatewaypb.AgentCommand
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &payload))
		for _, target := range payload.GetPair().GetTargets() {
			dispatched = append(dispatched, target.GetDeviceIdentifier())
		}
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
	case <-time.After(2 * time.Second):
		t.Fatal("in-scope batch was not dispatched after an out-of-scope batch")
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pairing did not finish")
	}

	assert.True(t, IsOutOfScopeError(err), "got %v", err)
	require.Len(t, dispatched, MaxPairBatch)
	assert.Equal(t, "miner-128", dispatched[0])
	assert.Len(t, reported, 2*MaxPairBatch, "every target has a terminal result")
}
