package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
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
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ControlSessionId: "ha-session", HaOwnerNodeId: 7, OrgId: 1, CredentialKeyFingerprint: hello.GetCredentialKeyFingerprint()}}}); err != nil {
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
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ControlSessionId: "ha-session", HaOwnerNodeId: 7, OrgId: 1, CredentialKeyFingerprint: hello.GetCredentialKeyFingerprint()}}}); err != nil {
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
func TestHAStreamSafetyMarginRetiresSessionAndWorkers(t *testing.T) {
	fake := &haInterruptedStreamGateway{payload: telemetryCmd(t, validTelemetryRequest()), ack: make(chan *pb.ControlAck, 1)}
	mux := http.NewServeMux()
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, handler)
	server := testutil.NewH2CServer(t, mux)
	client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), server.URL, connect.WithGRPC())
	finished := make(chan error, 1)
	node := &RunCmd{credentials: &credentialCodec{key: bytes.Repeat([]byte{5}, 32)}, telemetry: haCancellationTelemetry{finished: finished}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.Error(t, node.runControlSession(ctx, discardLogger(t), client, &bootstrap.State{FleetNodeID: 7}))
	require.NoError(t, ctx.Err(), "expiry must retire the transport before parent cancellation")
	select {
	case cause := <-finished:
		require.ErrorIs(t, cause, errHAAuthorityLost)
	case <-time.After(time.Second):
		t.Fatal("authority expiry did not cancel the dispatched worker")
	}
	// Teardown may prevent delivery; any completion that won the race must
	// still carry the terminal interruption marker.
	node.waitForControlWorkers(discardLogger(t))
	select {
	case ack := <-fake.ack:
		require.Equal(t, pb.AckCode_ACK_CODE_COMMAND_INTERRUPTED, ack.GetCode())
	default:
	}
}

type haCancellationTelemetry struct{ finished chan<- error }

func (f haCancellationTelemetry) Fetch(ctx context.Context, _ *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	<-ctx.Done()
	f.finished <- context.Cause(ctx)
	return nil, fmt.Errorf("telemetry canceled: %w", ctx.Err())
}

type haLostResponsesGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler
	grant    bool
	requests atomic.Int32
}

func (f *haLostResponsesGateway) ControlStream(_ context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	first, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive lost-response hello: %w", err)
	}
	hello := first.GetHello()
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ControlSessionId: "lost-session", HaOwnerNodeId: 7, OrgId: 1, CredentialKeyFingerprint: hello.GetCredentialKeyFingerprint()}}}); err != nil {
		return fmt.Errorf("send lost-response acceptance: %w", err)
	}
	for {
		request, err := stream.Receive()
		if err != nil {
			return fmt.Errorf("receive lost-response lease request: %w", err)
		}
		f.requests.Add(1)
		if f.grant {
			f.grant = false
			if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_HaLease{HaLease: &pb.HALeaseResponse{RequestId: request.GetHaLease().GetRequestId(), OwnerNodeId: 7, ActiveNodeId: 7, LeaseEpoch: 1, Granted: true, InstanceId: hello.GetInstanceId(), ControlSessionId: "lost-session", OrgId: 1, LeaseDurationMs: 11000}}}); err != nil {
				return fmt.Errorf("send initial lease: %w", err)
			}
		}
		// Continue receiving upstream traffic while dropping all later responses.
	}
}

func TestHAStreamClosesWhenLeaseResponsesStop(t *testing.T) {
	for _, grant := range []bool{true, false} {
		t.Run(fmt.Sprintf("initial-grant-%t", grant), func(t *testing.T) {
			fake := &haLostResponsesGateway{grant: grant}
			mux := http.NewServeMux()
			path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
			mux.Handle(path, handler)
			server := testutil.NewH2CServer(t, mux)
			client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), server.URL, connect.WithGRPC())
			node := &RunCmd{credentials: &credentialCodec{key: bytes.Repeat([]byte{5}, 32)}}
			timeout := haMaxLease - haSafetyMargin + 2*time.Second
			if grant {
				timeout = 8 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			require.Error(t, node.runControlSession(ctx, discardLogger(t), client, &bootstrap.State{FleetNodeID: 7}), "local expiry must retire this session")
			require.NoError(t, ctx.Err(), "expiry must close a live transport before its parent is canceled")
			expectedRequests := int32(1)
			if grant {
				expectedRequests = 2
			}
			require.Equal(t, expectedRequests, fake.requests.Load(), "a missing response must not trigger another outstanding request")
		})
	}
}
