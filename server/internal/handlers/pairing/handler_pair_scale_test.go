package pairing

import (
	"context"
	"fmt"
	"slices"
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
}

func (s *nodePairTargetStore) ListFleetNodeDevices(_ context.Context, _ int64, nodeID *int64) ([]fleetnodepairing.FleetNodeDevice, error) {
	return s.bindings[*nodeID], nil
}

func (s *nodePairTargetStore) ListFleetNodeDiscoveredDevices(_ context.Context, _ int64, nodeID *int64, filter fleetnodepairing.FleetNodeDiscoveredDeviceFilter) ([]fleetnodepairing.FleetNodeDiscoveredDevice, error) {
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
			store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
			var selected []string
			for i := range 257 {
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
				require.LessOrEqual(t, len(targets), 128)
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
	for _, configured := range []bool{false, true} {
		for _, repair := range []bool{false, true} {
			t.Run(fmt.Sprintf("empty-config=%t/repair=%t", configured, repair), func(t *testing.T) {
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
				if configured {
					stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{})
				}
				h := &Handler{
					discovery:        &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}},
					fleetNodePairing: fleetnodepairing.NewService(store, nil, nil).WithProvisioning(nil, nil, registry),
				}
				done := make(chan fleetNodePairRoute, 1)
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
}

func TestPairFleetNodeDevicesCompletesAcceptedCommandsAndStopsNewBatches(t *testing.T) {
	for _, stop := range []string{"command-error", "cancel"} {
		t.Run(stop, func(t *testing.T) {
			store := &nodePairTargetStore{devices: map[int64][]fleetnodepairing.FleetNodeDiscoveredDevice{}}
			var selected []string
			for i := range 129 {
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
				require.NoError(t, got.err)
				assert.Error(t, got.route.remoteErr)
				assert.True(t, got.route.remoteSucceeded)
				assert.Len(t, got.response.GetFailedDeviceIds(), 129)
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
