package control

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func haTestRegistry(t *testing.T) (*Registry, *Stream, RouteBinding) {
	t.Helper()
	r := NewRegistry()
	stream, err := r.RegisterAuthenticated(20, "bearer", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1)
	require.NoError(t, err)
	require.True(t, stream.SetHAConnection(1, &pb.ControlHello{HaCapable: true, InstanceId: "process"}, "session"))
	t.Cleanup(stream.Unregister)
	route := RouteBinding{OwnerNodeID: 10, PhysicalNodeID: 20, Epoch: 3, InstanceID: "process", ControlSessionID: "session", EncryptionPublicKey: []byte("recipient"), ExpiresAt: time.Now().Add(10 * time.Second)}
	r.SetHARoute(route)
	r.SetHARouteResolver(func(context.Context, int64) (RouteBinding, error) { return route, nil })
	return r, stream, route
}

func haTestCommand(t *testing.T, commandID string) *pb.ControlCommand {
	t.Helper()
	payload, err := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_MinerCommand{MinerCommand: &pb.MinerCommand{Action: &pb.MinerCommand_StopMining{StopMining: &pb.StopMiningAction{}}}}})
	require.NoError(t, err)
	return &pb.ControlCommand{CommandId: commandID, Payload: payload}
}

func TestHACommandsShareRenewalAndExpireWithoutDatabasePolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, stream, route := haTestRegistry(t)
		resolves := 0
		r.SetHARouteResolver(func(context.Context, int64) (RouteBinding, error) { resolves++; return route, nil })
		sessions := make([]*Session, 2)
		for i, name := range []string{"one", "two"} {
			var err error
			sessions[i], err = r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, haTestCommand(t, name), nil, ReportKindDiscovery, nil)
			require.NoError(t, err)
			defer sessions[i].Close()
			<-stream.Outgoing
		}
		time.Sleep(9 * time.Second)
		renewed := route
		renewed.ExpiresAt = time.Now().Add(10 * time.Second)
		r.SetHARoute(renewed)
		time.Sleep(2 * time.Second)
		for _, session := range sessions {
			select {
			case <-session.Done():
				t.Fatal("initial grant expiry canceled renewed command")
			default:
			}
		}
		require.Equal(t, 2, resolves, "accepted commands must not poll the database for lease renewal")
		time.Sleep(8 * time.Second)
		for _, session := range sessions {
			<-session.Done()
		}
		_, ok := r.HARoute(10)
		require.False(t, ok)
		r.SetHARoute(renewed)
		_, ok = r.HARoute(10)
		require.False(t, ok, "the expired snapshot itself cannot revive authority")
		// The database renewal committed before expiry, but publication was
		// delayed until after the local timer removed the original commands.
		renewed.ExpiresAt = time.Now().Add(time.Minute)
		r.SetHARoute(renewed)
		_, ok = r.HARoute(10)
		require.True(t, ok, "a later committed renewal must restore dispatch after local projection expiry")
		for _, session := range sessions {
			select {
			case <-session.Done():
			default:
				t.Fatal("a renewed projection revived an old command")
			}
		}
		route = renewed
		fresh, err := r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, haTestCommand(t, "fresh"), nil, ReportKindDiscovery, nil)
		require.NoError(t, err)
		defer fresh.Close()
		<-stream.Outgoing
		require.NoError(t, r.AdmitReport(20, "fresh", 1, ReportKindDiscovery))
		require.Error(t, r.AdmitReport(20, "one", 1, ReportKindDiscovery), "old command reports stay terminal")
	})
}

func TestHAOwnerRoutesToBoundMemberAndInterruptedSendIsTerminal(t *testing.T) {
	for _, acceptedAck := range []bool{false, true} {
		t.Run(map[bool]string{false: "interrupted", true: "accepted terminal result"}[acceptedAck], func(t *testing.T) {
			r, stream, _ := haTestRegistry(t)
			result := make(chan cmdResult, 1)
			go func() {
				ack, err := r.SendCommand(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, haTestCommand(t, "write"))
				result <- cmdResult{ack: ack, err: err}
			}()
			command := <-stream.Outgoing
			require.Equal(t, int64(10), command.GetHaOwnerNodeId())
			require.Equal(t, int64(3), command.GetHaLeaseEpoch())
			require.Equal(t, "process", command.GetIntendedInstanceId())
			if acceptedAck {
				stream.PublishAck(&pb.ControlAck{CommandId: "write", Code: pb.AckCode_ACK_CODE_OK, Succeeded: true})
			}
			stream.Unregister()
			got := <-result
			if acceptedAck {
				require.NoError(t, got.err)
				require.True(t, got.ack.GetSucceeded())
			} else {
				require.True(t, IsCommandInterrupted(got.err))
				require.False(t, IsCommandNotEnqueued(got.err))
			}
		})
	}
}

func TestHAAuthorityChangeStopsOldCommandsAndRejectsStaleSnapshots(t *testing.T) {
	r, stream, route := haTestRegistry(t)
	session, err := r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, haTestCommand(t, "old"), nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	next := route
	next.Epoch++
	r.SetHARoute(next)
	<-session.Done()
	r.SetHARoute(route)
	current, ok := r.HARoute(10)
	require.True(t, ok)
	require.Equal(t, next.Epoch, current.Epoch)
	_, err = r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, haTestCommand(t, "stale"), nil, ReportKindDiscovery, nil)
	require.Error(t, err, "old SQL route cannot dispatch into a different acknowledged epoch")
	replacement, err := r.RegisterAuthenticated(20, "bearer", pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1)
	require.NoError(t, err)
	defer replacement.Unregister()
	require.True(t, replacement.SetHAConnection(1, &pb.ControlHello{HaCapable: true, InstanceId: "new-process"}, "new-session"))
	r.SetHARoute(next)
	_, ok = r.HARoute(10)
	require.False(t, ok, "disconnected session must invalidate its projection")
	next.Epoch++
	next.InstanceID, next.ControlSessionID = "new-process", "new-session"
	r.SetHARoute(next)
	current, ok = r.HARoute(10)
	require.True(t, ok)
	require.Equal(t, "new-session", current.ControlSessionID)
	r.InvalidateHARoute(10)
	next.ExpiresAt = time.Now().Add(time.Minute)
	r.SetHARoute(next)
	_, ok = r.HARoute(10)
	require.False(t, ok, "explicit invalidation cannot be undone by the same snapshot")
}

func TestHAActivationDrainsAcceptedStandaloneWorkBeforeReconnect(t *testing.T) {
	r := NewRegistry()
	stream := r.Register(10)
	defer stream.Unregister()
	session, err := r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "accepted"}, nil, ReportKindPair, &PairMeta{OrgID: 1, Targets: map[string]struct{}{"miner": {}}})
	require.NoError(t, err)
	<-stream.Outgoing
	require.False(t, r.BeginHAActivation(10))
	_, err = r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "new"}, nil, ReportKindDiscovery, nil)
	require.Error(t, err)
	kept, meta, err := r.AdmitAndScopePairResults(10, "accepted", []*pb.FleetNodePairResult{{DeviceIdentifier: "miner"}})
	require.NoError(t, err)
	require.Len(t, kept, 1)
	require.Equal(t, int64(10), meta.OwnerNodeID)
	require.False(t, r.BeginHAActivation(10))
	session.Close()
	require.True(t, r.BeginHAActivation(10))
	raced := r.Register(10)
	defer raced.Unregister()
	r.FinishHAActivation(10)
	<-stream.Done
	<-raced.Done
	replacement := r.Register(10)
	defer replacement.Unregister()
	_, err = r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "unacknowledged"}, nil, ReportKindDiscovery, nil)
	require.Error(t, err, "activation remains gated until an acknowledged grant is published")
	r.AbortHAActivation(10)
	accepted, err := r.Send(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "aborted"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	accepted.Close()
}

func TestOldStreamCannotAcknowledgeReusedCommandID(t *testing.T) {
	r := NewRegistry()
	old := r.Register(1)
	current := r.Register(1)
	defer current.Unregister()
	session, err := r.Send(t.Context(), 1, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "reused"}, nil, ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-current.Outgoing
	old.PublishAck(&pb.ControlAck{CommandId: "reused", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
	select {
	case <-session.Events():
		t.Fatal("old stream acknowledged new command")
	default:
	}
	current.PublishAck(&pb.ControlAck{CommandId: "reused", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
	require.NotNil(t, (<-session.Events()).Ack)
}

func TestHAQueuedCiphertextCannotMoveToAnotherRecipient(t *testing.T) {
	for _, tc := range []struct {
		name     string
		marker   []byte
		original bool
		allowed  bool
	}{
		{name: "different marked recipient", marker: []byte("old-recipient")},
		{name: "historical ciphertext on original node", original: true, allowed: true},
		{name: "historical ciphertext after promotion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, stream, route := haTestRegistry(t)
			if tc.original {
				route.OwnerNodeID = route.PhysicalNodeID
				r.SetHARoute(route)
				r.SetHARouteResolver(func(context.Context, int64) (RouteBinding, error) { return route, nil })
			}
			env, err := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_MinerCommand{MinerCommand: &pb.MinerCommand{Action: &pb.MinerCommand_UpdateMinerPassword{UpdateMinerPassword: &pb.UpdateMinerPasswordAction{EncryptedPasswordUpdate: &pb.NodeEncryptedPayload{Ciphertext: []byte("ciphertext"), RecipientPublicKey: tc.marker}}}}}})
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() {
				_, err := r.SendCommand(t.Context(), route.OwnerNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "queued-password", Payload: env})
				result <- err
			}()
			if tc.allowed {
				command := <-stream.Outgoing
				stream.PublishAck(&pb.ControlAck{CommandId: command.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true})
				require.NoError(t, <-result)
			} else {
				require.True(t, IsCommandNotEnqueued(<-result))
				select {
				case <-stream.Outgoing:
					t.Fatal("ciphertext reached a different recipient")
				default:
				}
			}
		})
	}
}
