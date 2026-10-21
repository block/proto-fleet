package control

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestLongHACommandSurvivesRenewalButStopsOnLeadershipChange(t *testing.T) {
	r := NewRegistry()
	route := RouteBinding{OwnerNodeID: 10, PhysicalNodeID: 20, Epoch: 3, InstanceID: "process", ControlSessionID: "session", ExpiresAt: time.Now().Add(20 * time.Millisecond)}
	var changed atomic.Bool
	renewed := make(chan struct{}, 1)
	r.SetHARouteResolver(func(context.Context, int64) (RouteBinding, error) {
		next := route
		next.ExpiresAt = time.Now().Add(40 * time.Millisecond)
		if changed.Load() {
			next.Epoch++
		}
		select {
		case renewed <- struct{}{}:
		default:
		}
		return next, nil
	})
	ctx, cancel := r.leaseContext(t.Context(), route)
	defer cancel()
	select {
	case <-renewed:
	case <-time.After(time.Second):
		t.Fatal("initial grant was not checked for renewal")
	}
	require.NoError(t, ctx.Err(), "renewal must allow a long command to continue")
	changed.Store(true)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("leadership change did not cancel the old command")
	}
}

func TestHAOwnerRoutesToBoundMemberAndInterruptedSendIsTerminal(t *testing.T) {
	r := NewRegistry()
	stream := r.Register(20)
	defer stream.Unregister()
	r.SetHAConnection(20, 1, &pb.ControlHello{HaCapable: true, InstanceId: "process"}, "session")
	r.SetHARouteResolver(func(context.Context, int64) (RouteBinding, error) {
		return RouteBinding{OwnerNodeID: 10, PhysicalNodeID: 20, Epoch: 3, InstanceID: "process", ControlSessionID: "session", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})
	env, _ := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_MinerCommand{MinerCommand: &pb.MinerCommand{Action: &pb.MinerCommand_StopMining{StopMining: &pb.StopMiningAction{}}}}})
	result := make(chan error, 1)
	go func() {
		_, err := r.SendCommand(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "write", Payload: env})
		result <- err
	}()
	select {
	case command := <-stream.Outgoing:
		require.Equal(t, int64(10), command.GetHaOwnerNodeId())
		require.Equal(t, int64(3), command.GetHaLeaseEpoch())
		require.Equal(t, "process", command.GetIntendedInstanceId())
	case <-time.After(time.Second):
		t.Fatal("command was not routed to standby")
	}
	stream.Unregister()
	require.True(t, IsCommandInterrupted(<-result))
}

func TestHAAdoptionProtectsAlreadyEnqueuedStandaloneCommand(t *testing.T) {
	r := NewRegistry()
	stream := r.Register(10)
	defer stream.Unregister()
	result := make(chan error, 1)
	go func() {
		_, err := r.SendCommand(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "pre-adoption-write"})
		result <- err
	}()
	select {
	case <-stream.Outgoing:
	case <-time.After(time.Second):
		t.Fatal("standalone command was not enqueued")
	}
	r.AdoptHAOwner(10)
	stream.Unregister()
	require.True(t, IsCommandInterrupted(<-result), "the captured epoch-zero route must not make an adopted command retryable")
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
	r := NewRegistry()
	stream := r.Register(20)
	defer stream.Unregister()
	r.SetHAConnection(20, 1, &pb.ControlHello{HaCapable: true, InstanceId: "process"}, "session")
	r.SetHARouteResolver(func(context.Context, int64) (RouteBinding, error) {
		return RouteBinding{OwnerNodeID: 10, PhysicalNodeID: 20, Epoch: 2, InstanceID: "process", ControlSessionID: "session", EncryptionPublicKey: []byte("new-recipient"), ExpiresAt: time.Now().Add(time.Minute)}, nil
	})
	env, _ := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_MinerCommand{MinerCommand: &pb.MinerCommand{Action: &pb.MinerCommand_UpdateMinerPassword{UpdateMinerPassword: &pb.UpdateMinerPasswordAction{EncryptedPasswordUpdate: &pb.NodeEncryptedPayload{RecipientPublicKey: []byte("old-recipient")}}}}}})
	_, err := r.SendCommand(t.Context(), 10, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "queued-password", Payload: env})
	require.True(t, IsCommandNotEnqueued(err))
	select {
	case <-stream.Outgoing:
		t.Fatal("ciphertext reached a different recipient")
	default:
	}
}
