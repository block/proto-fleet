package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

type haStreamGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler
	hello chan *pb.ControlHello
	acks  chan *pb.ControlAck
}

func (f *haStreamGateway) ControlStream(_ context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	first, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("HA test stream: %w", err)
	}
	hello := first.GetHello()
	f.hello <- hello
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ControlSessionId: "ha-session"}}}); err != nil {
		return fmt.Errorf("HA test stream: %w", err)
	}
	request, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("HA test stream: %w", err)
	}
	if request.GetHaLease() == nil {
		return fmt.Errorf("expected initial lease request")
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_HaLease{HaLease: &pb.HALeaseResponse{RequestId: request.GetHaLease().GetRequestId(), OwnerNodeId: 7, ActiveNodeId: 7, LeaseEpoch: 1, Granted: true, InstanceId: hello.GetInstanceId(), ControlSessionId: "ha-session", OrgId: 1, LeaseDurationMs: 30000}}}); err != nil {
		return fmt.Errorf("HA test stream: %w", err)
	}
	for _, epoch := range []int64{2, 1} {
		cmd := &pb.ControlCommand{CommandId: fmt.Sprintf("c-%d", epoch), HaOwnerNodeId: 7, HaLeaseEpoch: epoch, IntendedInstanceId: hello.GetInstanceId(), ControlSessionId: "ha-session"}
		if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: cmd}}); err != nil {
			return fmt.Errorf("HA test stream: %w", err)
		}
		for {
			msg, err := stream.Receive()
			if err != nil {
				return fmt.Errorf("HA test stream: %w", err)
			}
			if ack := msg.GetAck(); ack != nil {
				f.acks <- ack
				break
			}
		}
	}
	return nil
}
func TestHAStreamAdvertisesAndRefusesBeforeDispatch(t *testing.T) {
	fake := &haStreamGateway{hello: make(chan *pb.ControlHello, 2), acks: make(chan *pb.ControlAck, 4)}
	mux := http.NewServeMux()
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, handler)
	server := testutil.NewH2CServer(t, mux)
	client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), server.URL, connect.WithGRPC())
	key := bytes.Repeat([]byte{5}, 32)
	fingerprint := sha256.Sum256(key)
	node := &RunCmd{credentials: &credentialCodec{key: key}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for range 2 {
		require.Error(t, node.runControlSession(ctx, discardLogger(t), client, &bootstrap.State{FleetNodeID: 7}))
		wrong, right := <-fake.acks, <-fake.acks
		require.Equal(t, pb.AckCode_ACK_CODE_HA_NOT_AUTHORIZED, wrong.GetCode())
		// Correct authority reaches the decoder, which rejects this empty envelope.
		require.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, right.GetCode())
	}
	first, second := <-fake.hello, <-fake.hello
	require.True(t, first.GetHaCapable())
	require.Equal(t, fingerprint[:], first.GetCredentialKeyFingerprint())
	require.NotEmpty(t, first.GetInstanceId())
	require.Equal(t, first.GetInstanceId(), second.GetInstanceId())
}

type haInterruptedStreamGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler
	payload []byte
	ack     chan *pb.ControlAck
}

func (f *haInterruptedStreamGateway) ControlStream(_ context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	first, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive interruption hello: %w", err)
	}
	hello := first.GetHello()
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ControlSessionId: "ha-session"}}}); err != nil {
		return fmt.Errorf("send interruption acceptance: %w", err)
	}
	request, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive interruption lease request: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_HaLease{HaLease: &pb.HALeaseResponse{RequestId: request.GetHaLease().GetRequestId(), OwnerNodeId: 7, ActiveNodeId: 7, LeaseEpoch: 1, Granted: true, InstanceId: hello.GetInstanceId(), ControlSessionId: "ha-session", OrgId: 1, LeaseDurationMs: 5200}}}); err != nil {
		return fmt.Errorf("send interruption lease: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: &pb.ControlCommand{CommandId: "interrupted", Payload: f.payload, HaOwnerNodeId: 7, HaLeaseEpoch: 1, IntendedInstanceId: hello.GetInstanceId(), ControlSessionId: "ha-session"}}}); err != nil {
		return fmt.Errorf("send interrupted command: %w", err)
	}
	for {
		message, err := stream.Receive()
		if err != nil {
			return fmt.Errorf("receive interrupted completion: %w", err)
		}
		if ack := message.GetAck(); ack != nil {
			f.ack <- ack
			return nil
		}
	}
}
func TestHAStreamSafetyMarginCancellationIsTerminalInterruption(t *testing.T) {
	fake := &haInterruptedStreamGateway{payload: telemetryCmd(t, validTelemetryRequest()), ack: make(chan *pb.ControlAck, 1)}
	mux := http.NewServeMux()
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, handler)
	server := testutil.NewH2CServer(t, mux)
	client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), server.URL, connect.WithGRPC())
	node := &RunCmd{credentials: &credentialCodec{key: bytes.Repeat([]byte{5}, 32)}, telemetry: waitingTelemetryFetcher{}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.Error(t, node.runControlSession(ctx, discardLogger(t), client, &bootstrap.State{FleetNodeID: 7}))
	ack := <-fake.ack
	require.Equal(t, pb.AckCode_ACK_CODE_COMMAND_INTERRUPTED, ack.GetCode(), "a backend cancellation after dispatch must not produce a retryable generic failure")
	require.False(t, ack.GetSucceeded())
	require.Empty(t, ack.GetPayload())
	// Pass the actual wire acknowledgement through the production remote-miner
	// adapter. Queue consumers use this typed marker to avoid replaying a write.
	remote, err := remotenode.New(remotenode.Config{Sender: haReceivedAckSender{ack: ack}, FleetNodeID: 7, DeviceIdentifier: "miner", DriverName: "antminer", IPAddress: "10.0.0.1", Port: "4028", URLScheme: "http"})
	require.NoError(t, err)
	require.True(t, control.IsCommandInterrupted(remote.StopMining(t.Context())))
}

type haReceivedAckSender struct{ ack *pb.ControlAck }

func (s haReceivedAckSender) SendCommand(context.Context, int64, pb.CommandProtocolVersion, *pb.ControlCommand) (*pb.ControlAck, error) {
	return s.ack, nil
}
