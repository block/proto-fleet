package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func discoverIPList(ips, ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: ips, Ports: ports},
		},
	}
}

// Drives runControlLoop until one ack lands, then cancels.
func runControlLoopOnce(t *testing.T, cmd *RunCmd, fake *controlFakeGateway) {
	t.Helper()
	state := &bootstrap.State{FleetNodeID: 7}
	client := newControlClient(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	require.Eventually(t, func() bool { return fake.ackCount() > 0 }, 4*time.Second, 20*time.Millisecond)
	cancel()
	<-done
}

func TestControlLoop_AcksAndReports(t *testing.T) {
	happyDisc := &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
		"10.0.0.5|4028":    {DeviceIdentifier: "auto:1", IpAddress: "10.0.0.5", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
		"2001:db8::1|4028": {DeviceIdentifier: "auto:v6", IpAddress: "2001:db8::1", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
		"192.168.1.4|4028": {DeviceIdentifier: "auto:r1", IpAddress: "192.168.1.4", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
		"192.168.1.5|4028": {DeviceIdentifier: "auto:r2", IpAddress: "192.168.1.5", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
	}}

	tooManyIPs := make([]string, maxIPsPerCommand+1)
	for i := range tooManyIPs {
		tooManyIPs[i] = fmt.Sprintf("10.0.%d.%d", i/256, i%256)
	}
	tooManyPorts := make([]string, maxPortsPerIP+1)
	for i := range tooManyPorts {
		tooManyPorts[i] = fmt.Sprintf("%d", 4000+i)
	}

	cases := []struct {
		name          string
		discoverer    discoverer
		behavior      controlFakeBehavior
		request       *pairingpb.DiscoverRequest
		rawPayload    []byte
		wantSucceeded bool
		wantCode      pb.AckCode
		wantErrSubstr string
		wantDevices   int
	}{
		{
			name:          "iplist happy path",
			discoverer:    happyDisc,
			request:       discoverIPList([]string{"10.0.0.5"}, []string{"4028"}),
			wantSucceeded: true,
			wantCode:      pb.AckCode_ACK_CODE_OK,
			wantDevices:   1,
		},
		{
			name:          "iplist normalizes scoped and canonical ipv6",
			discoverer:    happyDisc,
			request:       discoverIPList([]string{"fe80::1%eth0", "fe80::1", "2001:0DB8::1"}, []string{"4028"}),
			wantSucceeded: true,
			wantCode:      pb.AckCode_ACK_CODE_OK,
			wantDevices:   1,
		},
		{
			name:          "iplist all unusable",
			request:       discoverIPList([]string{"fe80::1%eth0", "fe80::1"}, []string{"4028"}),
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_BAD_REQUEST,
			wantErrSubstr: "no usable ip_addresses",
		},
		{
			name:          "iplist too many ips",
			request:       discoverIPList(tooManyIPs, []string{"4028"}),
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_BAD_REQUEST,
			wantErrSubstr: "too many ip_addresses",
		},
		{
			name:          "iplist too many ports",
			request:       discoverIPList([]string{"10.0.0.1"}, tooManyPorts),
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_BAD_REQUEST,
			wantErrSubstr: "too many ports",
		},
		{
			name:          "mdns rejected",
			request:       &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_Mdns{Mdns: &pairingpb.MDNSModeRequest{}}},
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_AGENT_INCAPABLE,
			wantErrSubstr: "mdns",
		},
		{
			name:          "nmap port-range bypass rejected",
			request:       &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_Nmap{Nmap: &pairingpb.NmapModeRequest{Target: "10.0.0.1", Ports: []string{"1-65535"}}}},
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_BAD_REQUEST,
			wantErrSubstr: "invalid port",
		},
		{
			name:          "iprange expands and probes",
			discoverer:    happyDisc,
			request:       &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{IpRange: &pairingpb.IPRangeModeRequest{StartIp: "192.168.1.1", EndIp: "192.168.1.5", Ports: []string{"4028"}}}},
			wantSucceeded: true,
			wantCode:      pb.AckCode_ACK_CODE_OK,
			wantDevices:   2,
		},
		{
			name:          "corrupt payload",
			rawPayload:    []byte{0xFF, 0xFE},
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_BAD_REQUEST,
			wantErrSubstr: "decode AgentCommand",
		},
		{
			name:          "envelope with no command kind",
			rawPayload:    []byte{}, // valid empty AgentCommand: no oneof arm set
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_BAD_REQUEST,
			wantErrSubstr: "no recognized command kind",
		},
		{
			name:          "report upload failure",
			discoverer:    happyDisc,
			behavior:      controlFakeBehavior{reportErr: connect.NewError(connect.CodeUnavailable, errors.New("upload boom"))},
			request:       discoverIPList([]string{"10.0.0.5"}, []string{"4028"}),
			wantSucceeded: false,
			wantCode:      pb.AckCode_ACK_CODE_REPORT_FAILED,
			wantErrSubstr: "report devices",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			cmd := &RunCmd{discoverer: tc.discoverer}
			if cmd.discoverer == nil {
				cmd.discoverer = &stubDiscoverer{}
			}
			fake := &controlFakeGateway{}
			fake.setBehavior(tc.behavior)
			if tc.request != nil {
				fake.queue(discoverPayload(t, tc.request))
			} else {
				fake.queue(tc.rawPayload)
			}

			// Act
			runControlLoopOnce(t, cmd, fake)

			// Assert
			acks := fake.acksCopy()
			require.Len(t, acks, 1)
			assert.Equal(t, tc.wantSucceeded, acks[0].GetSucceeded(), "ack=%+v", acks[0])
			assert.Equal(t, tc.wantCode, acks[0].GetCode())
			if tc.wantErrSubstr != "" {
				assert.Contains(t, acks[0].GetErrorMessage(), tc.wantErrSubstr)
			}
			reports := fake.reportsCopy()
			switch {
			case tc.wantCode == pb.AckCode_ACK_CODE_OK:
				require.Len(t, reports, 1)
				assert.Len(t, reports[0].GetDevices(), tc.wantDevices)
				assert.Equal(t, acks[0].GetCommandId(), reports[0].GetCommandId(), "ack and report must share command_id")
			case tc.wantCode == pb.AckCode_ACK_CODE_REPORT_FAILED:
				require.Len(t, reports, 1, "REPORT_FAILED implies the report was attempted")
			default:
				assert.Empty(t, reports, "failure before the report stage must not have produced a report")
			}
		})
	}
}

func TestResolveAndValidatePorts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		supplied  []string
		defaults  []string
		want      []string
		wantErr   bool
		errSubstr string
	}{
		{name: "valid single port", supplied: []string{"4028"}, want: []string{"4028"}},
		{name: "uses defaults when empty", supplied: nil, defaults: []string{"80", "4028"}, want: []string{"80", "4028"}},
		{name: "rejects range bypass", supplied: []string{"1-65535"}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects comma bypass", supplied: []string{"80,443,8080"}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects protocol prefix", supplied: []string{"T:80"}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects whitespace", supplied: []string{" 4028 "}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects service name", supplied: []string{"http"}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects zero", supplied: []string{"0"}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects 65536", supplied: []string{"65536"}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects negative", supplied: []string{"-1"}, wantErr: true, errSubstr: "invalid port"},
		{name: "rejects over-cap count", supplied: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}, wantErr: true, errSubstr: "too many ports"},
		{name: "dedupes", supplied: []string{"80", "80", "4028"}, want: []string{"80", "4028"}},
		{name: "normalizes leading zeros to canonical form", supplied: []string{"080"}, want: []string{"80"}},
		{name: "normalizes plus prefix to canonical form", supplied: []string{"+80"}, want: []string{"80"}},
		{name: "dedupes equivalent non-canonical inputs", supplied: []string{"80", "080", "+80"}, want: []string{"80"}},
		{name: "rejects all-empty when no defaults", supplied: nil, defaults: []string{}, wantErr: true, errSubstr: "non-empty"},
		{name: "validates plugin defaults", supplied: nil, defaults: []string{"1-65535"}, wantErr: true, errSubstr: "invalid port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			r := &RunCmd{discoverer: &stubDiscoverer{ports: tc.defaults}}

			// Act
			got, err := r.resolveAndValidatePorts(context.Background(), tc.supplied)

			// Assert
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExpandIPv4Range(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		start   string
		end     string
		max     int
		want    []string
		wantErr string
	}{
		{name: "skip network and gateway", start: "192.168.1.0", end: "192.168.1.3", max: 100, want: []string{"192.168.1.2", "192.168.1.3"}},
		{name: "keep loopback .0 and .1", start: "127.0.0.0", end: "127.0.0.2", max: 100, want: []string{"127.0.0.0", "127.0.0.1", "127.0.0.2"}},
		{name: "single host", start: "10.0.0.5", end: "10.0.0.5", max: 100, want: []string{"10.0.0.5"}},
		{name: "end before start", start: "10.0.0.5", end: "10.0.0.1", max: 100, wantErr: "must be >="},
		{name: "range collapses to empty", start: "192.168.1.0", end: "192.168.1.1", max: 100, wantErr: "only covers network/gateway"},
		{name: "exceeds cap", start: "10.0.0.0", end: "10.0.0.99", max: 16, wantErr: "exceeds the limit"},
		{name: "invalid start", start: "not-an-ip", end: "10.0.0.5", max: 100, wantErr: "not a valid IP address"},
		{name: "ipv6 rejected", start: "::1", end: "::2", max: 100, wantErr: "IPv4 required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got, err := expandIPv4Range(tc.start, tc.end, tc.max)

			// Assert
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// Regression: streaming must use the daemon ctx, not cmdCtx, or every
// partial discovery dies at the gateway call when commandTimeout fires.
func TestControlLoop_PartialResultsSurviveScanDeadline(t *testing.T) {
	prevTimeout := commandTimeout
	commandTimeout = 200 * time.Millisecond
	t.Cleanup(func() { commandTimeout = prevTimeout })

	// Arrange: first IP fast, later IPs block past commandTimeout.
	disc := &delayingStubDiscoverer{
		fast: map[string]*pb.DiscoveredDeviceReport{
			"10.0.0.4|4028": {DeviceIdentifier: "auto:fast", IpAddress: "10.0.0.4", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
		},
		blockingIPs: map[string]bool{
			"10.0.0.5": true,
			"10.0.0.6": true,
			"10.0.0.7": true,
		},
	}
	cmd := &RunCmd{discoverer: disc}
	state := &bootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	fake.queue(discoverPayload(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{
				IpAddresses: []string{"10.0.0.4", "10.0.0.5", "10.0.0.6", "10.0.0.7"},
				Ports:       []string{"4028"},
			},
		},
	}))
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	require.Eventually(t, func() bool { return fake.ackCount() > 0 }, 4*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	// Assert: fast IP reported, ack signals PARTIAL.
	reports := fake.reportsCopy()
	require.Len(t, reports, 1, "expected one batch even after scan deadline")
	devices := reports[0].GetDevices()
	require.Len(t, devices, 1)
	assert.Equal(t, "auto:fast", devices[0].GetDeviceIdentifier())

	acks := fake.acksCopy()
	require.Len(t, acks, 1)
	assert.False(t, acks[0].GetSucceeded(), "truncated scan must not ack succeeded")
	assert.Equal(t, pb.AckCode_ACK_CODE_PARTIAL, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "command deadline")
	assert.Contains(t, acks[0].GetErrorMessage(), "1 partial")
}

type delayingStubDiscoverer struct {
	fast        map[string]*pb.DiscoveredDeviceReport
	blockingIPs map[string]bool
}

func (s *delayingStubDiscoverer) Probe(ctx context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
	if s.blockingIPs[ip] {
		<-ctx.Done()
		return nil, fmt.Errorf("delaying stub probe cancelled: %w", ctx.Err())
	}
	if r, ok := s.fast[ip+"|"+port]; ok {
		return r, nil
	}
	return nil, nil
}

func (s *delayingStubDiscoverer) DefaultDiscoveryPorts(_ context.Context) []string {
	return []string{"4028"}
}

func TestControlLoop_PermanentErrorPropagates(t *testing.T) {
	// Arrange
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	state := &bootstrap.State{FleetNodeID: 11}

	fake := &controlFakeGateway{}
	fake.setBehavior(controlFakeBehavior{rejectWithCode: connect.CodeNotFound})
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	err := cmd.runControlLoop(ctx, client, state, discardLogger(t))

	// Assert
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestControlLoop_UnimplementedFallsBackToHeartbeatOnly(t *testing.T) {
	// Arrange
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	state := &bootstrap.State{FleetNodeID: 12}

	fake := &controlFakeGateway{}
	fake.setBehavior(controlFakeBehavior{rejectWithCode: connect.CodeUnimplemented})
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	err := cmd.runControlLoop(ctx, client, state, discardLogger(t))

	// Assert
	require.NoError(t, err)
}

func TestControlLoop_ReconnectsAfterStreamEOF(t *testing.T) {
	// Arrange
	cmd := &RunCmd{discoverer: &stubDiscoverer{}}
	state := &bootstrap.State{FleetNodeID: 9}

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
	ports  []string
}

func (s *stubDiscoverer) Probe(_ context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
	if r, ok := s.probes[ip+"|"+port]; ok {
		// Clone: fanOutProbes stamps ip/port onto the returned report, so handing
		// out a shared pointer would race when commands run concurrently.
		cloned, _ := proto.Clone(r).(*pb.DiscoveredDeviceReport)
		return cloned, nil
	}
	return nil, nil
}

func (s *stubDiscoverer) DefaultDiscoveryPorts(_ context.Context) []string {
	if s.ports != nil {
		return s.ports
	}
	return []string{"4028"}
}

type controlFakeBehavior struct {
	closeAfterAccepted bool
	// closeOnSignal lets tests force a server-side stream close at a precise
	// moment (e.g., after the agent has started executing a command). The
	// fake closes its ControlStream handler when this channel becomes ready.
	closeOnSignal  <-chan struct{}
	rejectWithCode connect.Code
	reportErr      error
	pairReportErr  error
	pairRejected   int64
}

type pendingCommand struct {
	id      string
	payload []byte
}

type controlFakeGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	mu          sync.Mutex
	pending     []pendingCommand
	hellos      int32
	acks        []*pb.ControlAck
	reports     []*pb.ReportDiscoveredDevicesRequest
	pairReports []*pb.ReportPairedDevicesRequest
	behavior    controlFakeBehavior
}

func (f *controlFakeGateway) queue(payload []byte) {
	f.mu.Lock()
	f.pending = append(f.pending, pendingCommand{id: "test-cmd", payload: payload})
	f.mu.Unlock()
}

func (f *controlFakeGateway) queueWithID(id string, payload []byte) {
	f.mu.Lock()
	f.pending = append(f.pending, pendingCommand{id: id, payload: payload})
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
	reportErr := f.behavior.reportErr
	f.mu.Unlock()
	if reportErr != nil {
		return nil, reportErr
	}
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{AcceptedCount: int64(len(req.Msg.GetDevices()))}), nil
}

func (f *controlFakeGateway) ReportPairedDevices(_ context.Context, req *connect.Request[pb.ReportPairedDevicesRequest]) (*connect.Response[pb.ReportPairedDevicesResponse], error) {
	f.mu.Lock()
	f.pairReports = append(f.pairReports, req.Msg)
	reportErr := f.behavior.pairReportErr
	rejected := f.behavior.pairRejected
	f.mu.Unlock()
	if reportErr != nil {
		return nil, reportErr
	}
	return connect.NewResponse(&pb.ReportPairedDevicesResponse{
		AcceptedCount: int64(len(req.Msg.GetResults())) - rejected,
		RejectedCount: rejected,
	}), nil
}

func (f *controlFakeGateway) pairReportsCopy() []*pb.ReportPairedDevicesRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*pb.ReportPairedDevicesRequest, len(f.pairReports))
	copy(out, f.pairReports)
	return out
}

func (f *controlFakeGateway) ControlStream(ctx context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	f.mu.Lock()
	rejectCode := f.behavior.rejectWithCode
	f.mu.Unlock()
	if rejectCode != 0 {
		atomic.AddInt32(&f.hellos, 1)
		return connect.NewError(rejectCode, fmt.Errorf("fake gateway rejected ControlStream"))
	}
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
	closeOnSignal := f.behavior.closeOnSignal
	pending := f.pending
	f.pending = nil
	f.mu.Unlock()
	if closeNow {
		return nil
	}

	for _, p := range pending {
		if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Command{Command: &pb.ControlCommand{
			CommandId: p.id,
			Payload:   p.payload,
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
		case <-closeOnSignal:
			// Test triggered a server-side stream close. nil chan blocks
			// forever, so this case is inert unless closeOnSignal was set.
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

// discoverPayload marshals a discovery request inside the AgentCommand envelope, the
// way DiscoverOnFleetNode now sends it over the ControlStream.
func discoverPayload(t *testing.T, req *pairingpb.DiscoverRequest) []byte {
	t.Helper()
	return mustMarshal(t, &pairingpb.AgentCommand{
		Command: &pairingpb.AgentCommand_Discover{Discover: req},
	})
}

func TestSynthesizeIdentifier(t *testing.T) {
	t.Parallel()

	dev := func(mac, serial, driver, model string) *discoverymodels.DiscoveredDevice {
		return &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{
			MacAddress: mac, SerialNumber: serial, DriverName: driver, Model: model,
		}}
	}

	cases := []struct {
		name string
		dev  *discoverymodels.DiscoveredDevice
		want string
	}{
		{name: "mac wins", dev: dev("aa:bb:cc:dd:ee:ff", "SN1", "antminer", "S19"), want: "mac:aa:bb:cc:dd:ee:ff"},
		{name: "serial when no mac", dev: dev("", "SN1", "antminer", "S19"), want: "serial:SN1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act: mac/serial wins regardless of the fleet node id.
			got := synthesizeIdentifier(tc.dev, "10.0.0.1", "80", 1)

			// Assert
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("auto fallback keys on the probed endpoint, not the plugin's", func(t *testing.T) {
		t.Parallel()
		// Driver left mac/serial blank: identity comes from the trusted probed
		// endpoint passed in, so devices at different endpoints stay distinct.
		blank := dev("", "", "antminer", "S19")

		// Act
		first := synthesizeIdentifier(blank, "10.0.0.7", "4028", 1)
		second := synthesizeIdentifier(blank, "10.0.0.7", "4028", 1)

		// Assert
		assert.True(t, strings.HasPrefix(first, "auto:"), "auto:* prefix, got %q", first)
		assert.Equal(t, first, second, "same device + endpoint + node must re-key identically across scans")
		assert.NotEqual(t, first, synthesizeIdentifier(blank, "10.0.0.8", "4028", 1), "a different probed endpoint must differ")
	})

	t.Run("auto fallback is scoped per fleet node", func(t *testing.T) {
		t.Parallel()
		// Two nodes on overlapping RFC1918 space probe the same endpoint for
		// distinct miners; the synthesized auto: key must differ by node so the
		// server's upsert guard doesn't silently drop the second node's device.
		blank := dev("", "", "antminer", "S19")

		// Act
		node1 := synthesizeIdentifier(blank, "192.168.1.20", "80", 1)
		node2 := synthesizeIdentifier(blank, "192.168.1.20", "80", 2)

		// Assert
		assert.NotEqual(t, node1, node2, "same endpoint on different fleet nodes must yield distinct auto: ids")
		assert.Equal(t, node1, synthesizeIdentifier(blank, "192.168.1.20", "80", 1), "a given fleet node id must be deterministic")
	})
}

func TestReportFromDiscovered(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dev  *discoverymodels.DiscoveredDevice
		want *pb.DiscoveredDeviceReport
	}{
		{
			name: "explicit identifier passes through",
			dev: &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{
				DeviceIdentifier: "drv-explicit-123",
				MacAddress:       "aa:bb:cc:dd:ee:ff",
				IpAddress:        "10.0.0.5",
				Port:             "4028",
			}},
			want: &pb.DiscoveredDeviceReport{
				DeviceIdentifier: "drv-explicit-123",
				IpAddress:        "10.0.0.5",
				Port:             "4028",
			},
		},
		{
			name: "synthesizes mac identifier and copies all fields",
			dev: &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{
				MacAddress:      "aa:bb:cc:dd:ee:ff",
				IpAddress:       "10.0.0.5",
				Port:            "4028",
				UrlScheme:       "http",
				DriverName:      "antminer",
				Model:           "S19",
				Manufacturer:    "Bitmain",
				FirmwareVersion: "v1",
			}},
			want: &pb.DiscoveredDeviceReport{
				DeviceIdentifier: "mac:aa:bb:cc:dd:ee:ff",
				IpAddress:        "10.0.0.5",
				Port:             "4028",
				UrlScheme:        "http",
				DriverName:       "antminer",
				Model:            "S19",
				Manufacturer:     "Bitmain",
				FirmwareVersion:  "v1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act: ip/port are the trusted probed endpoint.
			got := reportFromDiscovered(tc.dev, "10.0.0.5", "4028", 1)

			// Assert
			assert.True(t, proto.Equal(got, tc.want), "got=%v want=%v", got, tc.want)
		})
	}
}

// blockingDiscoverer holds Probe open per-IP so tests can observe Receive
// drain and ctx-cancel behavior while a command is in flight.
type blockingDiscoverer struct {
	mu      sync.Mutex
	started map[string]chan struct{}
	release map[string]chan struct{}
}

func newBlockingDiscoverer(ips ...string) *blockingDiscoverer {
	d := &blockingDiscoverer{started: map[string]chan struct{}{}, release: map[string]chan struct{}{}}
	for _, ip := range ips {
		d.started[ip] = make(chan struct{}, 1)
		d.release[ip] = make(chan struct{})
	}
	return d
}

func (b *blockingDiscoverer) Probe(ctx context.Context, ip, _ string) (*pb.DiscoveredDeviceReport, error) {
	b.mu.Lock()
	start, ok := b.started[ip]
	release := b.release[ip]
	b.mu.Unlock()
	if !ok {
		return nil, nil
	}
	select {
	case start <- struct{}{}:
	default:
	}
	select {
	case <-release:
		return &pb.DiscoveredDeviceReport{DeviceIdentifier: "auto:" + ip, IpAddress: ip, Port: "4028", UrlScheme: "http", DriverName: "antminer"}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("blocking discoverer cancelled: %w", ctx.Err())
	}
}

func (b *blockingDiscoverer) DefaultDiscoveryPorts(_ context.Context) []string {
	return []string{"4028"}
}

func (b *blockingDiscoverer) waitStarted(t *testing.T, ip string) {
	t.Helper()
	b.mu.Lock()
	start := b.started[ip]
	b.mu.Unlock()
	select {
	case <-start:
	case <-time.After(2 * time.Second):
		t.Fatalf("probe for %s never started", ip)
	}
}

func (b *blockingDiscoverer) release1(ip string) {
	b.mu.Lock()
	ch := b.release[ip]
	b.mu.Unlock()
	close(ch)
}

func TestControlLoop_SecondConcurrentDiscoveryGetsBusy(t *testing.T) {
	// Arrange: discovery is single-flight per node. The first discovery blocks in its
	// probe, holding the exclusive discovery slot.
	disc := newBlockingDiscoverer("10.0.0.1")
	cmd := &RunCmd{discoverer: disc}
	state := &bootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	for _, id := range []string{"disc-1", "disc-2"} {
		fake.queueWithID(id, discoverPayload(t, &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.1"}, Ports: []string{"4028"}},
			},
		}))
	}
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()

	// Assert: the second concurrent discovery is rejected BUSY while the first holds
	// the exclusive discovery slot.
	require.Eventually(t, func() bool {
		for _, ack := range fake.acksCopy() {
			if ack.GetCommandId() == "disc-2" {
				return ack.GetCode() == pb.AckCode_ACK_CODE_BUSY && !ack.GetSucceeded()
			}
		}
		return false
	}, 3*time.Second, 20*time.Millisecond, "second concurrent discovery should be rejected BUSY")

	cancel()
	<-done
}

func TestControlLoop_CtxCancelDuringInFlightUnblocks(t *testing.T) {
	// Arrange: probe blocks until ctx cancellation.
	disc := newBlockingDiscoverer("10.0.0.99")
	cmd := &RunCmd{discoverer: disc}
	state := &bootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	fake.queueWithID("cmd-stuck", discoverPayload(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.99"}, Ports: []string{"4028"}},
		},
	}))
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	disc.waitStarted(t, "10.0.0.99")
	cancel()

	// Assert: ctx-cancel propagates through the per-probe ctx so the loop returns.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runControlLoop did not return after ctx cancel")
	}
}

func TestFanOutProbes_SupervisorReturnsPartialOnStuckPlugin(t *testing.T) {
	// Shrink the supervisor budget into a unit-test window.
	prev := perProbeTimeout
	perProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { perProbeTimeout = prev })

	// Arrange: a probe that ignores ctx would pin the call without the supervisor.
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	probe := func(ctx context.Context, ip, _ string) (*pb.DiscoveredDeviceReport, error) {
		if ip == "10.0.0.1" {
			return &pb.DiscoveredDeviceReport{DeviceIdentifier: "auto:fast", IpAddress: ip, Port: "4028", UrlScheme: "http", DriverName: "antminer"}, nil
		}
		<-stuck
		return nil, nil
	}

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoints := []endpoint{{ip: "10.0.0.1", port: "4028"}, {ip: "10.0.0.2", port: "4028"}}

	start := time.Now()
	result, truncated := fanOutProbes(ctx, endpoints, 2, probe, discardLogger(t))
	elapsed := time.Since(start)

	// Assert: capped wall-clock, fast probe still reports, truncated set.
	require.LessOrEqual(t, elapsed, perProbeTimeout*2+time.Second, "fanOutProbes must return within the supervisor budget even with a stuck plugin")
	require.Len(t, result, 1)
	assert.True(t, truncated, "supervisor-fired batch must be flagged truncated")
	assert.Equal(t, "auto:fast", result[0].GetDeviceIdentifier())
}

func TestFanOutProbes_DropsInvalidReportInsteadOfPoisoningBatch(t *testing.T) {
	// Arrange: one plugin returns a healthy report, another returns one
	// that violates the gateway's url_scheme rule. Pre-fix, the bad report
	// would have flowed into ReportDiscoveredDevices and the server would
	// have rejected the whole batch, dropping the healthy device too.
	probe := func(_ context.Context, ip, _ string) (*pb.DiscoveredDeviceReport, error) {
		switch ip {
		case "10.0.0.1":
			return &pb.DiscoveredDeviceReport{
				DeviceIdentifier: "auto:good",
				IpAddress:        ip,
				Port:             "4028",
				UrlScheme:        "http",
				DriverName:       "antminer",
			}, nil
		case "10.0.0.2":
			return &pb.DiscoveredDeviceReport{
				DeviceIdentifier: "auto:bad",
				IpAddress:        ip,
				Port:             "4028",
				UrlScheme:        "http",
				DriverName:       "antminer",
				Model:            strings.Repeat("X", 300), // exceeds max_len=255
			}, nil
		}
		return nil, nil
	}

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoints := []endpoint{{ip: "10.0.0.1", port: "4028"}, {ip: "10.0.0.2", port: "4028"}}
	result, _ := fanOutProbes(ctx, endpoints, 2, probe, discardLogger(t))

	// Assert: only the gateway-valid report survives; the bad one is dropped.
	require.Len(t, result, 1)
	assert.Equal(t, "auto:good", result[0].GetDeviceIdentifier())
}

func TestControlLoop_SupervisorTruncatedScanAcksPartial(t *testing.T) {
	// Shrink the supervisor budget into a unit-test window.
	prev := perProbeTimeout
	perProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { perProbeTimeout = prev })

	// Arrange: one fast probe + one that ignores ctx; supervisor fires
	// before commandTimeout so cmdCtx stays alive.
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	disc := &ctxIgnoringDiscoverer{
		stuck: stuck,
		fast: map[string]*pb.DiscoveredDeviceReport{
			"10.0.0.1|4028": {DeviceIdentifier: "auto:fast", IpAddress: "10.0.0.1", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
		},
		stuckIPs: map[string]bool{"10.0.0.2": true},
	}
	cmd := &RunCmd{discoverer: disc}
	state := &bootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	fake.queueWithID("scan-1", discoverPayload(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.1", "10.0.0.2"}, Ports: []string{"4028"}},
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
	assert.Equal(t, pb.AckCode_ACK_CODE_PARTIAL, acks[0].GetCode())
	assert.Contains(t, acks[0].GetErrorMessage(), "supervisor")
}

// Ignores ctx for IPs in stuckIPs (blocks on `stuck`); fast for IPs in fast.
type ctxIgnoringDiscoverer struct {
	fast     map[string]*pb.DiscoveredDeviceReport
	stuckIPs map[string]bool
	stuck    chan struct{}
}

func (d *ctxIgnoringDiscoverer) Probe(_ context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
	if r, ok := d.fast[ip+"|"+port]; ok {
		return r, nil
	}
	if d.stuckIPs[ip] {
		<-d.stuck
	}
	return nil, nil
}

func (d *ctxIgnoringDiscoverer) DefaultDiscoveryPorts(_ context.Context) []string {
	return []string{"4028"}
}

func TestFanOutProbes_AcceptsNonHTTPPluginScheme(t *testing.T) {
	// Arrange: virtual plugin (and any future non-http plugin) reports its
	// own url_scheme. The gateway proto previously restricted url_scheme to
	// {"http","https"} which silently dropped these reports.
	probe := func(_ context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
		return &pb.DiscoveredDeviceReport{
			DeviceIdentifier: "auto:virt",
			IpAddress:        ip,
			Port:             port,
			UrlScheme:        "virtual",
			DriverName:       "virtual",
		}, nil
	}

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoints := []endpoint{{ip: "10.0.0.1", port: "4028"}}
	result, _ := fanOutProbes(ctx, endpoints, 1, probe, discardLogger(t))

	// Assert
	require.Len(t, result, 1)
	assert.Equal(t, "virtual", result[0].GetUrlScheme())
}

func TestFanOutProbes_OverridesPluginSuppliedEndpoint(t *testing.T) {
	// Arrange: plugin returns a report claiming a totally different
	// (ip, port) than the one we actually probed. A malicious or buggy
	// plugin would otherwise let the agent upload a spoofed endpoint
	// that poisons the server's discovery state.
	probe := func(_ context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
		return &pb.DiscoveredDeviceReport{
			DeviceIdentifier: "auto:" + ip,
			IpAddress:        "203.0.113.99", // not what we scanned
			Port:             "9999",
			UrlScheme:        "http",
			DriverName:       "antminer",
		}, nil
	}

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoints := []endpoint{{ip: "10.0.0.1", port: "4028"}}
	result, _ := fanOutProbes(ctx, endpoints, 1, probe, discardLogger(t))

	// Assert: report uses the scanned (ip, port), not what the plugin claimed.
	require.Len(t, result, 1)
	assert.Equal(t, "10.0.0.1", result[0].GetIpAddress())
	assert.Equal(t, "4028", result[0].GetPort())
}

func TestControlLoop_DroppedStreamCancelsInFlightScan(t *testing.T) {
	// Bump commandTimeout so the unfixed failure mode would visibly hang.
	prev := commandTimeout
	commandTimeout = 30 * time.Second
	t.Cleanup(func() { commandTimeout = prev })

	// Arrange: the probe blocks until released or until its ctx fires. The
	// fake holds the stream open after dispatching the command and closes
	// it on `streamClose`, so the test can drop the stream at a precise
	// moment (after observing the probe started).
	disc := newBlockingDiscoverer("10.0.0.42")
	cmd := &RunCmd{discoverer: disc}
	state := &bootstrap.State{FleetNodeID: 7}

	streamClose := make(chan struct{})
	fake := &controlFakeGateway{}
	fake.setBehavior(controlFakeBehavior{closeOnSignal: streamClose})
	fake.queueWithID("cmd-1", discoverPayload(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.42"}, Ports: []string{"4028"}},
		},
	}))
	client := newControlClient(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	disc.waitStarted(t, "10.0.0.42")
	close(streamClose)

	// Assert: with sessionCtx wiring, the dropped stream cancels the
	// in-flight probe via the session-scoped ctx, the worker exits within
	// the supervisor budget, runControlSession returns, and runControlLoop
	// backs off and reconnects. helloCount reaching 2 within 4 seconds
	// requires the unblock path -- without it, the loop's defer would
	// hang for commandTimeout (30s) before reconnect.
	require.Eventually(t, func() bool { return fake.helloCount() >= 2 }, 4*time.Second, 50*time.Millisecond)
	cancel()
	<-done
}

func TestControlLoop_DropsCommandWithInvalidCommandID(t *testing.T) {
	// Arrange: empty command_id violates the proto's min_len=1. The agent
	// must drop silently rather than ack -- echoing "" back in an ack
	// would itself fail buf-validate at the gateway and close the stream.
	// Queue a valid follow-up so we can prove via its ack that the loop
	// kept running normally past the dropped command.
	disc := &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
		"10.0.0.1|4028": {DeviceIdentifier: "auto:1", IpAddress: "10.0.0.1", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
	}}
	cmd := &RunCmd{discoverer: disc}
	state := &bootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	payload := discoverPayload(t, &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.1"}, Ports: []string{"4028"}},
		},
	})
	fake.queueWithID("", payload)     // invalid: empty command_id, drop silently
	fake.queueWithID("good", payload) // valid: this one should ack
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()
	require.Eventually(t, func() bool { return fake.ackCount() >= 1 }, 3*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	// Assert: exactly the valid command was acked.
	acks := fake.acksCopy()
	require.Len(t, acks, 1, "the empty-command_id command must be dropped silently")
	assert.Equal(t, "good", acks[0].GetCommandId())
}

func TestControlLoop_ConcurrentAcksSerialize(t *testing.T) {
	// Arrange: two commands. The worker's completion ack for cmd-A and the
	// receive loop's busy ack for cmd-C overlap on the same bidi stream;
	// without the lockedAcker wrapper, race detector flags concurrent
	// stream.Send. Using a fast (non-blocking) discoverer ensures the
	// worker actually completes and emits its ack.
	disc := &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
		"10.0.0.1|4028": {DeviceIdentifier: "auto:a", IpAddress: "10.0.0.1", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
		"10.0.0.2|4028": {DeviceIdentifier: "auto:b", IpAddress: "10.0.0.2", Port: "4028", UrlScheme: "http", DriverName: "antminer"},
	}}
	cmd := &RunCmd{discoverer: disc}
	state := &bootstrap.State{FleetNodeID: 7}

	fake := &controlFakeGateway{}
	for _, id := range []string{"cmd-A", "cmd-B", "cmd-C", "cmd-D"} {
		fake.queueWithID(id, discoverPayload(t, &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.1"}, Ports: []string{"4028"}},
			},
		}))
	}
	client := newControlClient(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- cmd.runControlLoop(ctx, client, state, discardLogger(t)) }()

	// Assert: every queued command lands an ack and -race stays clean.
	require.Eventually(t, func() bool { return fake.ackCount() >= 4 }, 4*time.Second, 20*time.Millisecond)
	cancel()
	<-done
}

func TestSendAck_TruncationPreservesUTF8Boundaries(t *testing.T) {
	// Arrange: 4-byte emoji + ASCII padding sized so the naive byte cut
	// would land in the middle of an emoji's continuation bytes.
	r := &RunCmd{}
	captured := &capturingAcker{}
	const emoji = "🚨" // 4 bytes
	// Choose padding so the cut at 4093 (maxAckErrorMessageBytes-3) lands
	// between bytes 2 and 3 of an emoji; the rune-aware truncator must
	// step back to the emoji's start.
	prefix := strings.Repeat("a", 4091)
	body := prefix + emoji + strings.Repeat("a", 4096)
	require.Greater(t, len(body), 4096)

	// Act
	r.sendAck(captured, "cmd-x", pb.AckCode_ACK_CODE_INTERNAL, body, discardLogger(t))

	// Assert
	require.Len(t, captured.sent, 1)
	got := captured.sent[0].GetAck().GetErrorMessage()
	assert.LessOrEqual(t, len(got), 4096)
	assert.True(t, utf8.ValidString(got), "truncated message must remain valid UTF-8")
	assert.True(t, strings.HasSuffix(got, "..."))
}

type capturingAcker struct {
	sent []*pb.ControlStreamRequest
}

func (c *capturingAcker) Send(req *pb.ControlStreamRequest) error {
	c.sent = append(c.sent, req)
	return nil
}
