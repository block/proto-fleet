package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
)

type stubLister struct {
	nodes []enrollment.FleetNodeListing
	err   error
}

func (s stubLister) ListFleetNodes(context.Context, int64) ([]enrollment.FleetNodeListing, error) {
	return s.nodes, s.err
}

func collectBatches(dst *[]*pairingpb.Device) func(*pairingpb.DiscoverResponse) error {
	return func(b *pairingpb.DiscoverResponse) error {
		*dst = append(*dst, b.GetDevices()...)
		return nil
	}
}

func TestRunOnNode_ForwardsBatchesUntilAck(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(7)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		reg.PublishBatch(nodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
			Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:1", IpAddress: "10.0.0.5", Port: "4028"}},
		})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	}()
	var got []*pairingpb.Device

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, ipListReq([]string{"10.0.0.5"}, []string{"4028"}), collectBatches(&got))

	// Assert
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "auto:1", got[0].GetDeviceIdentifier())
}

func TestRunOnNode_RejectsIPListRangesBeforeDispatch(t *testing.T) {
	for _, raw := range []string{"10.0.0.1-2", "10.0-1.0-1.1", "10.0.0.1-10.0.1.2", "10.0.0.0/24"} {
		t.Run(raw, func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			stream := reg.Register(7)
			defer stream.Unregister()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var got []*pairingpb.DiscoverResponse

			err := svc.RunOnNode(ctx, 7, ipListReq([]string{"10.0.0.1", raw}, []string{"4028"}), collectResponses(&got))

			require.True(t, fleeterror.IsInvalidArgumentError(err), "error = %v", err)
			assert.Empty(t, got)
			select {
			case cmd := <-stream.Outgoing:
				t.Fatalf("invalid IP-list entry dispatched command %q", cmd.GetCommandId())
			default:
			}
		})
	}
}

func collectResponses(dst *[]*pairingpb.DiscoverResponse) func(*pairingpb.DiscoverResponse) error {
	return func(b *pairingpb.DiscoverResponse) error { *dst = append(*dst, b); return nil }
}

func TestRunOnNode_PartialAndFailedScanKeepResultsWithWarning(t *testing.T) {
	for _, code := range []gatewaypb.AckCode{gatewaypb.AckCode_ACK_CODE_PARTIAL, gatewaypb.AckCode_ACK_CODE_SCAN_FAILED, gatewaypb.AckCode_ACK_CODE_REPORT_FAILED} {
		t.Run(code.String(), func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			stream := reg.Register(8)
			defer stream.Unregister()
			go func() {
				cmd := <-stream.Outgoing
				reg.PublishBatch(8, cmd.GetCommandId(), &pairingpb.DiscoverResponse{Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:2", IpAddress: "10.0.0.6", Port: "4028"}}})
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: code, ErrorMessage: "stopped early"})
			}()
			var got []*pairingpb.DiscoverResponse
			err := svc.RunOnNode(t.Context(), 8, ipListReq([]string{"10.0.0.6"}, []string{"4028"}), collectResponses(&got))
			require.NoError(t, err)
			require.Len(t, got, 2)
			require.Len(t, got[0].GetDevices(), 1)
			assert.Equal(t, "auto:2", got[0].GetDevices()[0].GetDeviceIdentifier())
			assert.Contains(t, got[1].GetWarning(), "Fleet Node 8:")
			assert.Contains(t, got[1].GetWarning(), "stopped early")
		})
	}
}

func TestRunOnNode_PartialWithoutDetailStillWarns(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := reg.Register(8)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
	}()
	var got []*pairingpb.DiscoverResponse
	require.NoError(t, svc.RunOnNode(t.Context(), 8, ipListReq([]string{"10.0.0.6"}, nil), collectResponses(&got)))
	require.Len(t, got, 1)
	assert.Equal(t, "Fleet Node 8: discovery completed partially", got[0].GetWarning())
}

func TestRunOnNode_DisconnectBeforeAckWarns(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := reg.Register(9)
	go func() { <-stream.Outgoing; stream.Unregister() }()
	var got []*pairingpb.DiscoverResponse
	err := svc.RunOnNode(t.Context(), 9, ipListReq([]string{"10.0.0.7"}, nil), collectResponses(&got))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].GetWarning(), "Fleet Node 9:")
	assert.Contains(t, got[0].GetWarning(), "control stream closed")
}

func TestRunOnNode_NoActiveStreamWarns(t *testing.T) {
	svc := NewService(control.NewRegistry(), stubLister{})
	var got []*pairingpb.DiscoverResponse
	err := svc.RunOnNode(t.Context(), 404, ipListReq([]string{"10.0.0.8"}, nil), collectResponses(&got))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Fleet Node 404: fleet node has no active control stream", got[0].GetWarning())
}

func TestRunOnNode_RequiresCommandProtocolV1(t *testing.T) {
	reg := control.NewRegistry()
	stream, err := reg.RegisterAuthenticated(7, "legacy", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_UNSPECIFIED)
	require.NoError(t, err)
	defer stream.Unregister()
	svc := NewService(reg, stubLister{})
	var got []*pairingpb.DiscoverResponse
	err = svc.RunOnNode(t.Context(), 7, ipListReq([]string{"10.0.0.5"}, nil), collectResponses(&got))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].GetWarning(), "Fleet Node 7:")
	assert.Contains(t, got[0].GetWarning(), "upgrade")
	select {
	case cmd := <-stream.Outgoing:
		t.Fatalf("legacy node received discovery command %q", cmd.GetCommandId())
	default:
	}
}

func TestRunOnNode_RequestAndAuthenticationFailuresRemainErrors(t *testing.T) {
	for _, code := range []gatewaypb.AckCode{gatewaypb.AckCode_ACK_CODE_BAD_REQUEST, gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED} {
		t.Run(code.String(), func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			stream := reg.Register(8)
			defer stream.Unregister()
			go func() {
				cmd := <-stream.Outgoing
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: code})
			}()
			var got []*pairingpb.DiscoverResponse
			err := svc.RunOnNode(t.Context(), 8, ipListReq([]string{"10.0.0.6"}, nil), collectResponses(&got))
			require.Error(t, err)
			if code == gatewaypb.AckCode_ACK_CODE_BAD_REQUEST {
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			} else {
				assert.True(t, fleeterror.IsAuthenticationError(err))
			}
			assert.Empty(t, got)
		})
	}
}

func TestRunOnNode_WarningSendFailureIsTerminal(t *testing.T) {
	for _, code := range []gatewaypb.AckCode{gatewaypb.AckCode_ACK_CODE_PARTIAL, gatewaypb.AckCode_ACK_CODE_SCAN_FAILED} {
		t.Run(code.String(), func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			stream := reg.Register(8)
			defer stream.Unregister()
			go func() {
				cmd := <-stream.Outgoing
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: code})
			}()
			sendErr := errors.New("stream closed")
			sends := 0
			err := svc.RunOnNode(t.Context(), 8, ipListReq([]string{"10.0.0.6"}, nil), func(*pairingpb.DiscoverResponse) error { sends++; return sendErr })
			require.ErrorIs(t, err, sendErr)
			assert.Equal(t, 1, sends)
		})
	}
}

func TestRunOnNode_CancellationDoesNotWarn(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := reg.Register(8)
	defer stream.Unregister()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { <-stream.Outgoing; cancel() }()
	var got []*pairingpb.DiscoverResponse
	require.NoError(t, svc.RunOnNode(ctx, 8, ipListReq([]string{"10.0.0.6"}, nil), collectResponses(&got)))
	assert.Empty(t, got)
}

func TestEligibleNodeIDs_IntersectsStatusConnectionAndCompatibility(t *testing.T) {
	// Arrange: 1 = eligible, 2 = disconnected, 3 = pending, 4 = legacy protocol.
	reg := control.NewRegistry()
	lister := stubLister{nodes: []enrollment.FleetNodeListing{
		{FleetNode: enrollment.FleetNode{ID: 1, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
		{FleetNode: enrollment.FleetNode{ID: 2, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
		{FleetNode: enrollment.FleetNode{ID: 3, EnrollmentStatus: enrollment.FleetNodeStatusPending}},
		{FleetNode: enrollment.FleetNode{ID: 4, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
	}}
	svc := NewService(reg, lister)
	s1 := reg.Register(1)
	defer s1.Unregister()
	s3 := reg.Register(3)
	defer s3.Unregister()
	s4, err := reg.RegisterAuthenticated(4, "legacy", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_UNSPECIFIED)
	require.NoError(t, err)
	defer s4.Unregister()

	// Act
	got, err := svc.EligibleNodeIDs(context.Background(), 1)

	// Assert: only the confirmed AND connected node (order is unspecified).
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{1}, got)
}

func TestRunOnNode_PreservesIPRangeRequest(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(22)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	received := make(chan *pairingpb.IPRangeModeRequest, 1)
	go func() {
		cmd := <-stream.Outgoing
		var env gatewaypb.AgentCommand
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &env))
		received <- env.GetDiscover().GetIpRange()
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	}()
	req := &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{
		IpRange: &pairingpb.IPRangeModeRequest{
			StartIp: "192.168.1.10",
			EndIp:   "192.168.1.20",
			Ports:   []string{"4028", "8080"},
		},
	}}

	err := svc.RunOnNode(context.Background(), nodeID, req, func(*pairingpb.DiscoverResponse) error { return nil })

	require.NoError(t, err)
	assert.True(t, proto.Equal(req.GetIpRange(), <-received))
}

func TestRunOnNode_OnBatchErrorIsTerminal(t *testing.T) {
	// Arrange: the agent emits a batch; the caller's onBatch reports its stream gone.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(11)
	stream := reg.Register(nodeID)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		reg.PublishBatch(nodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
			Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:x", IpAddress: "10.0.0.5", Port: "4028"}},
		})
	}()
	sentinel := errors.New("operator stream gone")

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, ipListReq([]string{"10.0.0.5"}, []string{"4028"}), func(*pairingpb.DiscoverResponse) error {
		return sentinel
	})

	// Assert: an onBatch error terminates RunOnNode with that error.
	require.ErrorIs(t, err, sentinel)
}

func TestRunOnNode_TimesOutWhenAgentNeverAcks(t *testing.T) {
	prev := DiscoverCommandTimeout
	DiscoverCommandTimeout = 20 * time.Millisecond
	t.Cleanup(func() { DiscoverCommandTimeout = prev })
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := reg.Register(12)
	defer stream.Unregister()
	go func() { <-stream.Outgoing }()
	var got []*pairingpb.DiscoverResponse
	err := svc.RunOnNode(t.Context(), 12, ipListReq([]string{"10.0.0.5"}, nil), collectResponses(&got))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].GetWarning(), "Fleet Node 12:")
	assert.Contains(t, got[0].GetWarning(), "timed out")
}

func TestEligibleNodeIDs_PropagatesListError(t *testing.T) {
	// Arrange: the enrollment lookup fails.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{err: errors.New("db unavailable")})

	// Act
	_, err := svc.EligibleNodeIDs(context.Background(), 1)

	// Assert: the error propagates (fan-out treats it as best-effort upstream).
	require.Error(t, err)
}

func TestRunOnNode_InterpretsLocalSubnetFlag(t *testing.T) {
	for _, tc := range []struct {
		name        string
		target      string
		localSubnet bool
		wantTarget  string
	}{
		{name: "sentinel passes through", target: netscan.LocalSubnetTarget, wantTarget: netscan.LocalSubnetTarget},
		{name: "true uses node local subnet", target: "10.0.0.0/28", localSubnet: true, wantTarget: netscan.LocalSubnetTarget},
		{name: "false preserves target", target: "10.0.0.0/28", wantTarget: "10.0.0.0/28"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			const nodeID = int64(21)
			stream := reg.Register(nodeID)
			defer stream.Unregister()
			gotTarget := make(chan string, 1)
			go func() {
				cmd := <-stream.Outgoing
				var env gatewaypb.AgentCommand
				require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &env))
				gotTarget <- env.GetDiscover().GetNetworkScan().GetTarget()
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
			}()
			req := &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{NetworkScan: &pairingpb.NetworkScanModeRequest{
				Target:                  tc.target,
				UseFleetNodeLocalSubnet: tc.localSubnet,
			}}}

			err := svc.RunOnNode(context.Background(), nodeID, req, func(*pairingpb.DiscoverResponse) error { return nil })

			require.NoError(t, err)
			assert.Equal(t, tc.wantTarget, <-gotTarget)
		})
	}
}
