package pairing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
)

// Immutable inventory: every discovery has one existing node owner, as in SQL.
type nodePairTargetStore struct {
	fleetnodepairing.Store
	devices  map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice
	bindings map[int64][]fleetnodepairing.FleetNodeDevice
	// listErr fails discovery listing for a node; panicBindings panics when its
	// bindings are read; afterList runs once after the first discovery listing.
	listErr       map[int64]error
	panicBindings map[int64]bool
	afterList     func()
	afterListOnce sync.Once
}

func (s *nodePairTargetStore) ListFleetNodeDevices(_ context.Context, _ int64, nodeID *int64) ([]fleetnodepairing.FleetNodeDevice, error) {
	if s.panicBindings[*nodeID] {
		panic("bindings unavailable")
	}
	return s.bindings[*nodeID], nil
}

func (s *nodePairTargetStore) ListFleetNodeDiscoveredDevices(_ context.Context, _ int64, nodeID *int64, filter fleetnodepairing.FleetNodeDiscoveredDeviceFilter) ([]fleetnodepairing.FleetNodeDiscoveredDevice, error) {
	if err := s.listErr[*nodeID]; err != nil {
		return nil, err
	}
	if s.afterList != nil {
		defer s.afterListOnce.Do(s.afterList)
	}
	var out []fleetnodepairing.FleetNodeDiscoveredDevice
	for _, device := range s.devices[*nodeID] {
		if filter.Identifiers != nil && !slices.Contains(filter.Identifiers, device.DeviceIdentifier) {
			continue
		}
		if filter.CursorID != nil && device.ID <= *filter.CursorID {
			continue
		}
		out = append(out, device)
		if filter.Limit != nil && int64(len(out)) == *filter.Limit {
			break
		}
	}
	return out, nil
}

func TestPairFleetNodeDevicesDispatchesNodesInParallelAndBoundsBatches(t *testing.T) {
	for _, allDevices := range []bool{false, true} {
		t.Run(fmt.Sprintf("allDevices=%t", allDevices), func(t *testing.T) {
			// Arrange
			store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
			var selected []string
			for i := range 2*fleetnodepairing.MaxPairBatch + 1 {
				id := fmt.Sprintf("a-%03d", i)
				store.devices[7] = append(store.devices[7], fleetnodepairing.FleetNodeDiscoveredDevice{ID: int64(i + 1), DeviceIdentifier: id, IPAddress: "10.0.0.1"})
				selected = append(selected, id)
			}
			store.devices[8] = []fleetnodepairing.FleetNodeDiscoveredDevice{{ID: 300, DeviceIdentifier: "b", IPAddress: "10.0.0.2"}}
			selected = append(selected, "b", "a-000")
			if !allDevices {
				store.devices[8] = append(store.devices[8], store.devices[7][0])
			}
			registry := control.NewRegistry()
			a, b := registry.Register(7), registry.Register(8)
			defer a.Unregister()
			defer b.Unregister()
			for _, stream := range []*control.Stream{a, b} {
				stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
			}
			h := &Handler{
				discovery:        &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7, 8}},
				fleetNodePairing: fleetnodepairing.NewService(store, nil, nil).WithProvisioning(nil, nil, registry),
			}
			credentials := &pb.Credentials{Username: "root", Password: proto.String("secret")}
			type result struct {
				resp  *pb.PairResponse
				route fleetNodePairRoute
				err   error
			}
			done := make(chan result, 1)

			// Act
			go func() {
				selector := includeDevicesSelector(selected)
				if allDevices {
					selector = &minercommandv1.DeviceSelector{SelectionType: &minercommandv1.DeviceSelector_AllDevices{AllDevices: &minercommandv1.DeviceFilter{PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED}}}}
				}
				resp, route, err := h.pairFleetNodeDevices(t.Context(), 1, 2, &pb.PairRequest{Credentials: credentials, DeviceSelector: selector})
				done <- result{resp, route, err}
			}()
			var firstA *gatewaypb.ControlCommand
			select {
			case firstA = <-a.Outgoing:
			case <-time.After(2 * time.Second):
				t.Fatal("node A did not receive its first batch")
			}
			var commandB *gatewaypb.ControlCommand
			select {
			case commandB = <-b.Outgoing:
			case <-time.After(time.Second):
				t.Fatal("node B waited for node A's command to finish")
			}
			seen := map[string]bool{}
			complete := func(nodeID int64, stream *control.Stream, cmd *gatewaypb.ControlCommand, success bool) {
				var envelope gatewaypb.AgentCommand
				require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &envelope))
				targets := envelope.GetPair().GetTargets()
				require.LessOrEqual(t, len(targets), fleetnodepairing.MaxPairBatch)
				assert.True(t, proto.Equal(credentials, envelope.GetPair().GetCredentials()))
				var results []*gatewaypb.FleetNodePairResult
				for _, target := range targets {
					id := target.GetDeviceIdentifier()
					assert.False(t, seen[id], "dispatch repeated %s", id)
					seen[id] = true
					if success {
						results = append(results, &gatewaypb.FleetNodePairResult{DeviceIdentifier: id, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED})
					}
				}
				registry.PublishPairResults(nodeID, cmd.GetCommandId(), results)
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
			}
			complete(8, b, commandB, true)
			complete(7, a, firstA, false)
			for {
				select {
				case cmd := <-a.Outgoing:
					complete(7, a, cmd, false)
				case got := <-done:
					// Assert
					require.NoError(t, got.err)
					assert.True(t, got.route.remoteSucceeded)
					assert.Len(t, got.resp.GetFailedDeviceIds(), 257)
					assert.Len(t, seen, 258)
					if allDevices {
						assert.Len(t, got.route.routedAllDevices, 258)
					} else {
						assert.Len(t, got.route.routedExplicit, 258)
					}
					return
				case <-time.After(2 * time.Second):
					t.Fatal("pairing did not finish")
				}
			}
		})
	}
}

func TestPairFleetNodeDevicesWithoutConfigurationOnlyRepairsOwnedRoute(t *testing.T) {
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprintf("repair=%t", repair), func(t *testing.T) {
			// Arrange
			store := &nodePairTargetStore{
				devices:  map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{7: {{ID: 1, DeviceIdentifier: "miner", IPAddress: "10.0.0.1"}}},
				bindings: map[int64][]fleetnodepairing.FleetNodeDevice{},
			}
			if repair {
				store.devices[7][0].PairingStatus = fleetnodepairing.StatusAuthenticationNeeded
				store.bindings[7] = []fleetnodepairing.FleetNodeDevice{{DeviceIdentifier: "miner"}}
			}
			registry := control.NewRegistry()
			stream := registry.Register(7)
			defer stream.Unregister()
			h := &Handler{
				discovery:        &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}},
				fleetNodePairing: fleetnodepairing.NewService(store, nil, nil).WithProvisioning(nil, nil, registry),
			}
			done := make(chan fleetNodePairRoute, 1)

			// Act
			go func() {
				_, route, err := h.pairFleetNodeDevices(t.Context(), 1, 2, &pb.PairRequest{Credentials: &pb.Credentials{Password: proto.String("secret")}, DeviceSelector: includeDevicesSelector([]string{"miner"})})
				if err != nil {
					route.remoteErr = err
				}
				done <- route
			}()
			if repair {
				select {
				case cmd := <-stream.Outgoing:
					registry.PublishPairResults(7, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "miner", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}})
					stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
				case <-time.After(2 * time.Second):
					t.Fatal("owned authentication repair was not dispatched")
				}
			}
			select {
			case route := <-done:
				// Assert
				assert.Contains(t, route.routedExplicit, "miner")
				if repair {
					assert.NoError(t, route.remoteErr)
					assert.True(t, route.remoteSucceeded)
				} else {
					assert.Error(t, route.remoteErr, "new targets must still fail closed without ranges")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("pairing did not finish")
			}
			select {
			case cmd := <-stream.Outgoing:
				t.Fatalf("unexpected pair command %s", cmd.GetCommandId())
			default:
			}
		})
	}
}

func TestPairFleetNodeDevicesCompletesAcceptedCommandsAndStopsNewBatches(t *testing.T) {
	for _, stop := range []string{"command-error", "cancel"} {
		t.Run(stop, func(t *testing.T) {
			// Arrange
			store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
			var selected []string
			for i := range fleetnodepairing.MaxPairBatch + 1 {
				id := fmt.Sprintf("a-%03d", i)
				store.devices[7] = append(store.devices[7], fleetnodepairing.FleetNodeDiscoveredDevice{ID: int64(i + 1), DeviceIdentifier: id, IPAddress: "10.0.0.1"})
				selected = append(selected, id)
			}
			store.devices[8] = []fleetnodepairing.FleetNodeDiscoveredDevice{{ID: 200, DeviceIdentifier: "b", IPAddress: "10.0.0.2"}}
			selected = append(selected, "b")
			registry := control.NewRegistry()
			a, b := registry.Register(7), registry.Register(8)
			defer a.Unregister()
			defer b.Unregister()
			a.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
			b.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
			h := &Handler{discovery: &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7, 8}}, fleetNodePairing: fleetnodepairing.NewService(store, nil, nil).WithProvisioning(nil, nil, registry)}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type result struct {
				response *pb.PairResponse
				route    fleetNodePairRoute
				err      error
			}
			done := make(chan result, 1)

			// Act
			go func() {
				resp, route, err := h.pairFleetNodeDevices(ctx, 1, 2, &pb.PairRequest{DeviceSelector: includeDevicesSelector(selected)})
				done <- result{resp, route, err}
			}()
			var commandA, commandB *gatewaypb.ControlCommand
			select {
			case commandA = <-a.Outgoing:
			case <-time.After(2 * time.Second):
				t.Fatal("A did not dispatch")
			}
			select {
			case commandB = <-b.Outgoing:
			case <-time.After(2 * time.Second):
				t.Fatal("B did not dispatch")
			}
			code := gatewaypb.AckCode_ACK_CODE_REPORT_FAILED
			if stop == "cancel" {
				cancel()
				code = gatewaypb.AckCode_ACK_CODE_PARTIAL
			}
			a.PublishAck(&gatewaypb.ControlAck{CommandId: commandA.GetCommandId(), Code: code})
			select {
			case got := <-done:
				t.Fatalf("accepted B command aborted before results: %v", got.err)
			default:
			}
			registry.PublishPairResults(8, commandB.GetCommandId(), []*gatewaypb.FleetNodePairResult{{DeviceIdentifier: "b", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}})
			b.PublishAck(&gatewaypb.ControlAck{CommandId: commandB.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
			select {
			case got := <-done:
				// Assert
				require.NoError(t, got.err)
				assert.Error(t, got.route.remoteErr)
				assert.True(t, got.route.remoteSucceeded)
				assert.Len(t, got.response.GetFailedDeviceIds(), fleetnodepairing.MaxPairBatch+1)
			case <-time.After(2 * time.Second):
				t.Fatal("pairing did not finish")
			}
			select {
			case cmd := <-a.Outgoing:
				t.Fatalf("node A dispatched a new batch after %s: %s", stop, cmd.GetCommandId())
			default:
			}
		})
	}
}

type pairFanOutResult struct {
	resp  *pb.PairResponse
	route fleetNodePairRoute
	err   error
}

type pairTestNodes struct {
	handler  *Handler
	registry *control.Registry
	streams  map[int64]*control.Stream
}

// newPairTestNodes connects nodes whose discovery range is 10.0.0.0/24 and
// reports them to the handler in the given order.
func newPairTestNodes(t *testing.T, store *nodePairTargetStore, nodeIDs ...int64) *pairTestNodes {
	t.Helper()
	registry := control.NewRegistry()
	streams := map[int64]*control.Stream{}
	for _, id := range nodeIDs {
		stream := registry.Register(id)
		stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}})
		t.Cleanup(stream.Unregister)
		streams[id] = stream
	}
	return &pairTestNodes{
		handler: &Handler{
			discovery:        &stubFleetNodeDiscoveryRunner{nodeIDs: nodeIDs},
			fleetNodePairing: fleetnodepairing.NewService(store, nil, nil).WithProvisioning(nil, nil, registry),
		},
		registry: registry,
		streams:  streams,
	}
}

func (n *pairTestNodes) pair(ctx context.Context, selector *minercommandv1.DeviceSelector) <-chan pairFanOutResult {
	done := make(chan pairFanOutResult, 1)
	go func() {
		resp, route, err := n.handler.pairFleetNodeDevices(ctx, 1, 2, &pb.PairRequest{DeviceSelector: selector})
		done <- pairFanOutResult{resp, route, err}
	}()
	return done
}

func (n *pairTestNodes) nextCommand(t *testing.T, nodeID int64) (*gatewaypb.ControlCommand, []*pb.FleetNodePairTarget) {
	t.Helper()
	select {
	case cmd := <-n.streams[nodeID].Outgoing:
		var envelope gatewaypb.AgentCommand
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &envelope))
		return cmd, envelope.GetPair().GetTargets()
	case <-time.After(2 * time.Second):
		t.Fatalf("node %d did not receive a pair command", nodeID)
		return nil, nil
	}
}

func (n *pairTestNodes) publishPaired(nodeID int64, cmd *gatewaypb.ControlCommand, targets []*pb.FleetNodePairTarget) {
	results := make([]*gatewaypb.FleetNodePairResult, len(targets))
	for i, target := range targets {
		results[i] = &gatewaypb.FleetNodePairResult{DeviceIdentifier: target.GetDeviceIdentifier(), Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}
	}
	n.registry.PublishPairResults(nodeID, cmd.GetCommandId(), results)
}

func (n *pairTestNodes) ack(nodeID int64, cmd *gatewaypb.ControlCommand) {
	n.streams[nodeID].PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
}

func (n *pairTestNodes) assertNoCommand(t *testing.T, nodeID int64) {
	t.Helper()
	select {
	case cmd := <-n.streams[nodeID].Outgoing:
		t.Fatalf("node %d received an unexpected command %s", nodeID, cmd.GetCommandId())
	default:
	}
}

func awaitPairResult(t *testing.T, done <-chan pairFanOutResult) pairFanOutResult {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("pairing did not finish")
		return pairFanOutResult{}
	}
}

func addNodeDevices(store *nodePairTargetStore, nodeID int64, prefix, ip string, firstID, count int) []string {
	ids := make([]string, 0, count)
	for i := range count {
		id := fmt.Sprintf("%s-%03d", prefix, i)
		store.devices[nodeID] = append(store.devices[nodeID], fleetnodepairing.FleetNodeDiscoveredDevice{ID: int64(firstID + i), DeviceIdentifier: id, IPAddress: ip})
		ids = append(ids, id)
	}
	return ids
}

func allUnpairedSelector() *minercommandv1.DeviceSelector {
	return &minercommandv1.DeviceSelector{SelectionType: &minercommandv1.DeviceSelector_AllDevices{AllDevices: &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_UNPAIRED},
	}}}
}

func TestPairFleetNodeDevicesDeadlineReturnsUndispatchedTargetsAsFailed(t *testing.T) {
	// Arrange
	deadline := 500 * time.Millisecond
	previous := pairFleetNodeDeadline
	pairFleetNodeDeadline = deadline
	t.Cleanup(func() { pairFleetNodeDeadline = previous })
	store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
	selected := addNodeDevices(store, 7, "a", "10.0.0.1", 1, fleetnodepairing.MaxPairBatch+3)
	nodes := newPairTestNodes(t, store, 7)

	// Act
	done := nodes.pair(t.Context(), includeDevicesSelector(selected))
	cmd, targets := nodes.nextCommand(t, 7)
	time.Sleep(2 * deadline)
	nodes.publishPaired(7, cmd, targets)
	nodes.ack(7, cmd)
	got := awaitPairResult(t, done)

	// Assert
	require.NoError(t, got.err)
	assert.True(t, got.route.remoteSucceeded)
	assert.ErrorIs(t, got.route.remoteErr, context.DeadlineExceeded)
	assert.Equal(t, selected[fleetnodepairing.MaxPairBatch:], got.resp.GetFailedDeviceIds(), "the accepted batch paired; every undispatched target failed")
	nodes.assertNoCommand(t, 7)
}

func TestPairFleetNodeDevicesAssignsSharedDeviceToLowestNodeID(t *testing.T) {
	// Arrange
	store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
	addNodeDevices(store, 7, "shared", "10.0.0.1", 1, 1)
	addNodeDevices(store, 8, "shared", "10.0.0.1", 2, 1)
	nodes := newPairTestNodes(t, store, 8, 7)

	// Act
	done := nodes.pair(t.Context(), includeDevicesSelector([]string{"shared-000"}))
	cmd, targets := nodes.nextCommand(t, 7)
	nodes.publishPaired(7, cmd, targets)
	nodes.ack(7, cmd)
	got := awaitPairResult(t, done)

	// Assert
	require.NoError(t, got.err)
	assert.Empty(t, got.resp.GetFailedDeviceIds())
	assert.Contains(t, got.route.routedExplicit, "shared-000")
	nodes.assertNoCommand(t, 8)
}

func TestPairFleetNodeDevicesRecoversFromNodeWorkerPanic(t *testing.T) {
	// Arrange
	store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}, panicBindings: map[int64]bool{7: true}}
	// An address outside the node's range makes node 7 read its bindings, which panics.
	panicking := addNodeDevices(store, 7, "a", "10.9.9.9", 1, 1)
	healthy := addNodeDevices(store, 8, "b", "10.0.0.2", 10, 1)
	nodes := newPairTestNodes(t, store, 7, 8)

	// Act
	done := nodes.pair(t.Context(), includeDevicesSelector(append(panicking, healthy...)))
	cmd, targets := nodes.nextCommand(t, 8)
	nodes.publishPaired(8, cmd, targets)
	nodes.ack(8, cmd)
	got := awaitPairResult(t, done)

	// Assert
	require.NoError(t, got.err)
	assert.True(t, got.route.remoteSucceeded)
	assert.Error(t, got.route.remoteErr)
	assert.Equal(t, panicking, got.resp.GetFailedDeviceIds())
}

func TestPairFleetNodeDevicesContinuesPastOutOfScopeBatch(t *testing.T) {
	// Arrange
	store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
	outOfScope := addNodeDevices(store, 7, "a", "10.9.9.9", 1, fleetnodepairing.MaxPairBatch)
	inScope := addNodeDevices(store, 7, "b", "10.0.0.1", 500, 2)
	nodes := newPairTestNodes(t, store, 7)

	// Act
	done := nodes.pair(t.Context(), includeDevicesSelector(append(slices.Clone(outOfScope), inScope...)))
	cmd, targets := nodes.nextCommand(t, 7)
	nodes.publishPaired(7, cmd, targets)
	nodes.ack(7, cmd)
	got := awaitPairResult(t, done)

	// Assert
	require.NoError(t, got.err)
	assert.Len(t, targets, len(inScope))
	assert.True(t, got.route.remoteSucceeded)
	assert.True(t, fleetnodepairing.IsOutOfScopeError(got.route.remoteErr), "got %v", got.route.remoteErr)
	assert.Equal(t, outOfScope, got.resp.GetFailedDeviceIds())
}

func TestPairFleetNodeDevicesAllDevicesKeepsOtherNodesWhenOneNodeErrors(t *testing.T) {
	// Arrange
	store := &nodePairTargetStore{
		devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{},
		listErr: map[int64]error{7: errors.New("listing failed")},
	}
	addNodeDevices(store, 7, "a", "10.0.0.1", 1, 1)
	healthy := addNodeDevices(store, 8, "b", "10.0.0.2", 10, 2)
	nodes := newPairTestNodes(t, store, 7, 8)

	// Act
	done := nodes.pair(t.Context(), allUnpairedSelector())
	cmd, targets := nodes.nextCommand(t, 8)
	nodes.publishPaired(8, cmd, targets)
	nodes.ack(8, cmd)
	got := awaitPairResult(t, done)

	// Assert
	require.NoError(t, got.err, "another node's results must not be discarded")
	assert.True(t, got.route.allDevices)
	assert.True(t, got.route.remoteSucceeded)
	assert.Error(t, got.route.remoteErr)
	assert.Empty(t, got.resp.GetFailedDeviceIds())
	assert.Len(t, got.route.routedAllDevices, len(healthy))
	nodes.assertNoCommand(t, 7)
}

func TestPairFleetNodeDevicesAllDevicesReportsErrorWhenNoNodeCanPair(t *testing.T) {
	// Arrange
	store := &nodePairTargetStore{
		devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{},
		listErr: map[int64]error{7: errors.New("listing failed")},
	}
	nodes := newPairTestNodes(t, store, 7)

	// Act
	got := awaitPairResult(t, nodes.pair(t.Context(), allUnpairedSelector()))

	// Assert
	require.NoError(t, got.err)
	assert.True(t, got.route.allDevices, "the node error must flow through the all-devices route")
	assert.False(t, got.route.remoteSucceeded)
	assert.Error(t, got.route.remoteErr)
}

func TestPairFleetNodeDevicesAllDevicesStopsPaginationOnCancellation(t *testing.T) {
	// Arrange
	store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
	addNodeDevices(store, 7, "a", "10.0.0.1", 1, fleetnodepairing.MaxPairBatch+1)
	nodes := newPairTestNodes(t, store, 7)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Act
	done := nodes.pair(ctx, allUnpairedSelector())
	cmd, targets := nodes.nextCommand(t, 7)
	nodes.publishPaired(7, cmd, targets)
	cancel()
	nodes.ack(7, cmd)
	got := awaitPairResult(t, done)

	// Assert
	require.NoError(t, got.err)
	assert.True(t, got.route.remoteSucceeded, "the accepted page finished after cancellation")
	assert.ErrorIs(t, got.route.remoteErr, context.Canceled)
	assert.Empty(t, got.resp.GetFailedDeviceIds())
	assert.Len(t, got.route.routedAllDevices, fleetnodepairing.MaxPairBatch)
	nodes.assertNoCommand(t, 7)
}

func TestPairFleetNodeDevicesExplicitCancellationDuringResolutionDispatchesNothing(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}, afterList: cancel}
	selected := addNodeDevices(store, 7, "a", "10.0.0.1", 1, fleetnodepairing.MaxPairBatch+1)
	nodes := newPairTestNodes(t, store, 7)

	// Act
	got := awaitPairResult(t, nodes.pair(ctx, includeDevicesSelector(selected)))

	// Assert
	require.Error(t, got.err)
	assert.ErrorIs(t, got.err, context.Canceled)
	assert.Nil(t, got.resp)
	nodes.assertNoCommand(t, 7)
}
