package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestControlLoop_RunsProbesAndReports(t *testing.T) {
	// Arrange
	disc := &stubDiscoverer{
		probes: map[string]*pb.DiscoveredDeviceReport{
			"10.0.0.5|4028": {DeviceIdentifier: "auto:1", IpAddress: "10.0.0.5", Port: "4028", UrlScheme: "http"},
		},
	}
	cmd := &RunCmd{discoverer: disc}
	state := &fleetnodebootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	fake.queue(mustMarshal(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}, Ports: []string{"4028"}},
		},
	}))
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() {
		done <- cmd.runControlLoop(ctx, client, state, discardLogger(t))
	}()

	require.Eventually(t, func() bool { return fake.ackCount() > 0 }, 3*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	// Assert
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.True(t, acks[0].GetSucceeded())
	require.Len(t, fake.reportsCopy(), 1)
	assert.Equal(t, "auto:1", fake.reportsCopy()[0].GetDevices()[0].GetDeviceIdentifier())
	commandID := acks[0].GetCommandId()
	assert.Equal(t, commandID, fake.reportsCopy()[0].GetCommandId())
}

func TestControlLoop_RejectsMDNSMode(t *testing.T) {
	// Arrange
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	state := &fleetnodebootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	fake.queue(mustMarshal(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_Mdns{Mdns: &pairingpb.MDNSModeRequest{}},
	}))
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() {
		done <- cmd.runControlLoop(ctx, client, state, discardLogger(t))
	}()
	require.Eventually(t, func() bool { return fake.ackCount() > 0 }, 3*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	// Assert
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Contains(t, acks[0].GetErrorMessage(), "mdns")
	assert.Empty(t, fake.reportsCopy())
}

func TestControlLoop_RejectsTooManyIPs(t *testing.T) {
	// Arrange
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	state := &fleetnodebootstrap.State{FleetNodeID: 7}

	tooMany := make([]string, maxIPsPerCommand+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("10.0.%d.%d", i/256, i%256)
	}
	fake := &controlFakeGateway{}
	fake.queue(mustMarshal(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: tooMany, Ports: []string{"4028"}},
		},
	}))
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	require.Eventually(t, func() bool { return fake.ackCount() > 0 }, 3*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	// Assert
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Contains(t, acks[0].GetErrorMessage(), "too many ip_addresses")
	assert.Empty(t, fake.reportsCopy())
}

func TestControlLoop_RejectsTooManyPorts(t *testing.T) {
	// Arrange
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	state := &fleetnodebootstrap.State{FleetNodeID: 7}

	ports := make([]string, maxPortsPerIP+1)
	for i := range ports {
		ports[i] = fmt.Sprintf("%d", 4000+i)
	}
	fake := &controlFakeGateway{}
	fake.queue(mustMarshal(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.1"}, Ports: ports},
		},
	}))
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	require.Eventually(t, func() bool { return fake.ackCount() > 0 }, 3*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	// Assert
	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded())
	assert.Contains(t, acks[0].GetErrorMessage(), "too many ports")
}

func TestControlLoop_ReconnectsAfterStreamEOF(t *testing.T) {
	// Arrange
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	state := &fleetnodebootstrap.State{FleetNodeID: 9}

	fake := &controlFakeGateway{}
	fake.setBehavior(controlFakeBehavior{closeAfterAccepted: true})
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() {
		done <- cmd.runControlLoop(ctx, client, state, discardLogger(t))
	}()

	require.Eventually(t, func() bool { return fake.helloCount() >= 2 }, 3*time.Second, 50*time.Millisecond)
	cancel()
	<-done

	// Assert: the loop reconnected at least once.
	assert.GreaterOrEqual(t, fake.helloCount(), 2)
}

type stubDiscoverer struct {
	probes map[string]*pb.DiscoveredDeviceReport
}

func (s *stubDiscoverer) Probe(_ context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
	if r, ok := s.probes[ip+"|"+port]; ok {
		return r, nil
	}
	return nil, nil
}

func (s *stubDiscoverer) DefaultDiscoveryPorts(_ context.Context) []string {
	return []string{"4028"}
}

type controlFakeBehavior struct {
	closeAfterAccepted bool
}

type controlFakeGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	mu       sync.Mutex
	pending  [][]byte
	hellos   int32
	acks     []*pb.ControlAck
	reports  []*pb.ReportDiscoveredDevicesRequest
	behavior controlFakeBehavior
}

func (f *controlFakeGateway) queue(payload []byte) {
	f.mu.Lock()
	f.pending = append(f.pending, payload)
	f.mu.Unlock()
}

func (f *controlFakeGateway) setBehavior(b controlFakeBehavior) {
	f.mu.Lock()
	f.behavior = b
	f.mu.Unlock()
}

func (f *controlFakeGateway) ackCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.acks) }
func (f *controlFakeGateway) acksCopy() []*pb.ControlAck {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*pb.ControlAck, len(f.acks))
	copy(out, f.acks)
	return out
}
func (f *controlFakeGateway) reportsCopy() []*pb.ReportDiscoveredDevicesRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*pb.ReportDiscoveredDevicesRequest, len(f.reports))
	copy(out, f.reports)
	return out
}
func (f *controlFakeGateway) helloCount() int { return int(atomic.LoadInt32(&f.hellos)) }

func (f *controlFakeGateway) ReportDiscoveredDevices(_ context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error) {
	f.mu.Lock()
	f.reports = append(f.reports, req.Msg)
	f.mu.Unlock()
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{AcceptedCount: int64(len(req.Msg.GetDevices()))}), nil
}

func (f *controlFakeGateway) UploadHeartbeat(_ context.Context, _ *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	return connect.NewResponse(&pb.UploadHeartbeatResponse{ReceivedAt: timestamppb.Now()}), nil
}

func (f *controlFakeGateway) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	first, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("recv hello: %w", err)
	}
	if first.GetHello() == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("expected hello"))
	}
	atomic.AddInt32(&f.hellos, 1)

	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{Accepted: &pb.ControlAccepted{ServerTime: timestamppb.Now()}}}); err != nil {
		return fmt.Errorf("send accepted: %w", err)
	}

	f.mu.Lock()
	closeNow := f.behavior.closeAfterAccepted
	pending := f.pending
	f.pending = nil
	f.mu.Unlock()
	if closeNow {
		return nil
	}

	for _, payload := range pending {
		if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: &pb.ControlCommand{
			CommandId: "test-cmd",
			Payload:   payload,
		}}}); err != nil {
			return fmt.Errorf("send command: %w", err)
		}
	}

	type recvResult struct {
		msg *pb.ControlStreamRequest
		err error
	}
	incoming := make(chan recvResult, 1)
	go func() {
		for {
			msg, err := stream.Receive()
			incoming <- recvResult{msg: msg, err: err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-incoming:
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					return nil
				}
				return r.err
			}
			if ack := r.msg.GetAck(); ack != nil {
				f.mu.Lock()
				f.acks = append(f.acks, ack)
				f.mu.Unlock()
			}
		}
	}
}

func newControlClient(t *testing.T, fake *controlFakeGateway) gatewayClient {
	t.Helper()
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	srv := testutil.NewH2CServer(t, mux)
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), srv.URL, connect.WithGRPC())
}

func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	require.NoError(t, err)
	return b
}
