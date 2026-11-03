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

func registerConfiguredNode(reg *control.Registry, nodeID int64) *control.Stream {
	stream := reg.Register(nodeID)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/8", "192.168.0.0/16"}})
	return stream
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
	stream := registerConfiguredNode(reg, nodeID)
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
	err := svc.RunOnNode(context.Background(), nodeID, "Fleet Node 7", ipListReq([]string{"10.0.0.5"}, []string{"4028"}), collectBatches(&got))

	// Assert
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "auto:1", got[0].GetDeviceIdentifier())
}

func TestRunOnNode_OverwritesNodeReportedFleetNodeID(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID, spoofedID = int64(7), int64(99)
	stream := registerConfiguredNode(reg, nodeID)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		reg.PublishBatch(nodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
			Devices: []*pairingpb.Device{
				{DeviceIdentifier: "auto:1", IpAddress: "10.0.0.5", Port: "4028", FleetNodeId: spoofedID},
				{DeviceIdentifier: "auto:2", IpAddress: "10.0.0.6", Port: "4028"},
			},
		})
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	}()
	var got []*pairingpb.Device

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, "Fleet Node 7", ipListReq([]string{"10.0.0.5", "10.0.0.6"}, []string{"4028"}), collectBatches(&got))

	// Assert
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, device := range got {
		assert.Equal(t, nodeID, device.GetFleetNodeId())
	}
}

func TestRunOnNode_RejectsIPListRangesBeforeDispatch(t *testing.T) {
	for _, raw := range []string{"10.0.0.1-2", "10.0-1.0-1.1", "10.0.0.1-10.0.1.2", "10.0.0.0/24"} {
		t.Run(raw, func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			stream := registerConfiguredNode(reg, 7)
			defer stream.Unregister()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var got []*pairingpb.DiscoverResponse

			err := svc.RunOnNode(ctx, 7, "Fleet Node 7", ipListReq([]string{"10.0.0.1", raw}, []string{"4028"}), collectResponses(&got))

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
	for _, code := range []gatewaypb.AckCode{gatewaypb.AckCode_ACK_CODE_PARTIAL, gatewaypb.AckCode_ACK_CODE_SCAN_FAILED, gatewaypb.AckCode_ACK_CODE_REPORT_FAILED, gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED} {
		t.Run(code.String(), func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			stream := registerConfiguredNode(reg, 8)
			defer stream.Unregister()
			go func() {
				cmd := <-stream.Outgoing
				reg.PublishBatch(8, cmd.GetCommandId(), &pairingpb.DiscoverResponse{Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:2", IpAddress: "10.0.0.6", Port: "4028"}}})
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: code, ErrorMessage: "stopped early"})
			}()
			var got []*pairingpb.DiscoverResponse
			err := svc.RunOnNode(t.Context(), 8, "Fleet Node 8", ipListReq([]string{"10.0.0.6"}, []string{"4028"}), collectResponses(&got))
			require.NoError(t, err)
			require.Len(t, got, 2)
			require.Len(t, got[0].GetDevices(), 1)
			assert.Equal(t, "auto:2", got[0].GetDevices()[0].GetDeviceIdentifier())
			assert.Contains(t, got[1].GetWarning(), "Fleet Node 8:")
			if code == gatewaypb.AckCode_ACK_CODE_PARTIAL {
				assert.Contains(t, got[1].GetWarning(), "stopped early")
			} else if code == gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED {
				assert.Contains(t, got[1].GetWarning(), "rejected discovery credentials")
			} else {
				assert.Equal(t, "Fleet Node 8: discovery failed", got[1].GetWarning())
			}
		})
	}
}

func TestRunOnNode_PartialWithoutDetailStillWarns(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := registerConfiguredNode(reg, 8)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
	}()
	var got []*pairingpb.DiscoverResponse
	require.NoError(t, svc.RunOnNode(t.Context(), 8, "Fleet Node 8", ipListReq([]string{"10.0.0.6"}, nil), collectResponses(&got)))
	require.Len(t, got, 1)
	assert.Equal(t, "Fleet Node 8: discovery completed partially", got[0].GetWarning())
}

func TestRunOnNode_DisconnectBeforeAckWarns(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := registerConfiguredNode(reg, 9)
	go func() { <-stream.Outgoing; stream.Unregister() }()
	var got []*pairingpb.DiscoverResponse
	err := svc.RunOnNode(t.Context(), 9, "Fleet Node 9", ipListReq([]string{"10.0.0.7"}, nil), collectResponses(&got))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].GetWarning(), "Fleet Node 9:")
	assert.Contains(t, got[0].GetWarning(), "control stream closed")
}

func TestRunOnNode_NoActiveStreamWarns(t *testing.T) {
	svc := NewService(control.NewRegistry(), stubLister{})
	var got []*pairingpb.DiscoverResponse
	err := svc.RunOnNode(t.Context(), 404, "Fleet Node", ipListReq([]string{"10.0.0.8"}, nil), collectResponses(&got))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Fleet Node: fleet node has no active control stream", got[0].GetWarning())
}

func TestRunOnNode_RequiresCommandProtocolV1(t *testing.T) {
	reg := control.NewRegistry()
	stream, err := reg.RegisterAuthenticated(7, "legacy", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_UNSPECIFIED)
	require.NoError(t, err)
	defer stream.Unregister()
	svc := NewService(reg, stubLister{})
	var got []*pairingpb.DiscoverResponse
	err = svc.RunOnNode(t.Context(), 7, "Fleet Node 7", ipListReq([]string{"10.0.0.5"}, nil), collectResponses(&got))
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

func TestRunOnNode_BadRequestRemainsError(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := registerConfiguredNode(reg, 8)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_BAD_REQUEST})
	}()
	var got []*pairingpb.DiscoverResponse
	err := svc.RunOnNode(t.Context(), 8, "Fleet Node 8", ipListReq([]string{"10.0.0.6"}, nil), collectResponses(&got))
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Empty(t, got)
}

func TestRunOnNode_WarningSendFailureIsTerminal(t *testing.T) {
	for _, code := range []gatewaypb.AckCode{gatewaypb.AckCode_ACK_CODE_PARTIAL, gatewaypb.AckCode_ACK_CODE_SCAN_FAILED} {
		t.Run(code.String(), func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			stream := registerConfiguredNode(reg, 8)
			defer stream.Unregister()
			go func() {
				cmd := <-stream.Outgoing
				stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: code})
			}()
			sendErr := errors.New("stream closed")
			sends := 0
			err := svc.RunOnNode(t.Context(), 8, "Fleet Node 8", ipListReq([]string{"10.0.0.6"}, nil), func(*pairingpb.DiscoverResponse) error { sends++; return sendErr })
			require.ErrorIs(t, err, sendErr)
			assert.Equal(t, 1, sends)
		})
	}
}

func TestRunOnNode_CancellationDoesNotWarn(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	stream := registerConfiguredNode(reg, 8)
	defer stream.Unregister()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { <-stream.Outgoing; cancel() }()
	var got []*pairingpb.DiscoverResponse
	require.NoError(t, svc.RunOnNode(ctx, 8, "Fleet Node 8", ipListReq([]string{"10.0.0.6"}, nil), collectResponses(&got)))
	assert.Empty(t, got)
}

func TestDiscoveryNodeIDs_IntersectsStatusAndConnection(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	lister := stubLister{nodes: []enrollment.FleetNodeListing{
		{FleetNode: enrollment.FleetNode{ID: 1, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
		{FleetNode: enrollment.FleetNode{ID: 2, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
		{FleetNode: enrollment.FleetNode{ID: 3, EnrollmentStatus: enrollment.FleetNodeStatusPending}},
		{FleetNode: enrollment.FleetNode{ID: 4, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
	}}
	svc := NewService(reg, lister)
	s1 := registerConfiguredNode(reg, 1)
	defer s1.Unregister()
	s3 := registerConfiguredNode(reg, 3)
	defer s3.Unregister()
	s4, err := reg.RegisterAuthenticated(4, "legacy", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_UNSPECIFIED)
	require.NoError(t, err)
	defer s4.Unregister()

	// Act
	got, err := svc.DiscoveryNodeIDs(context.Background(), 1)

	// Assert
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{1, 4}, got)
}

func TestDiscoveryNodeIDs_ExcludesHAStandby(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	activeID, standbyID := int64(1), int64(2)
	pair := func(n enrollment.FleetNode) enrollment.FleetNodeListing {
		return enrollment.FleetNodeListing{FleetNode: n, HAActiveNodeID: &activeID, HAStandbyNodeID: &standbyID}
	}
	lister := stubLister{nodes: []enrollment.FleetNodeListing{
		pair(enrollment.FleetNode{ID: 1, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}),
		pair(enrollment.FleetNode{ID: 2, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}),
		{FleetNode: enrollment.FleetNode{ID: 3, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}},
	}}
	svc := NewService(reg, lister)
	for _, nodeID := range []int64{1, 2, 3} {
		stream := registerConfiguredNode(reg, nodeID)
		defer stream.Unregister()
	}

	// Act
	got, err := svc.DiscoveryNodeIDs(context.Background(), 1)

	// Assert
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{1, 3}, got)
}

func TestRunOnNode_PreservesIPRangeRequest(t *testing.T) {
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(22)
	stream := registerConfiguredNode(reg, nodeID)
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

	err := svc.RunOnNode(context.Background(), nodeID, "Fleet Node 7", req, func(*pairingpb.DiscoverResponse) error { return nil })

	require.NoError(t, err)
	assert.True(t, proto.Equal(req.GetIpRange(), <-received))
}

func TestRunOnNode_OnBatchErrorIsTerminal(t *testing.T) {
	// Arrange: the agent emits a batch; the caller's onBatch reports its stream gone.
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{})
	const nodeID = int64(11)
	stream := registerConfiguredNode(reg, nodeID)
	defer stream.Unregister()
	go func() {
		cmd := <-stream.Outgoing
		reg.PublishBatch(nodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
			Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:x", IpAddress: "10.0.0.5", Port: "4028"}},
		})
	}()
	sentinel := errors.New("operator stream gone")

	// Act
	err := svc.RunOnNode(context.Background(), nodeID, "Fleet Node 7", ipListReq([]string{"10.0.0.5"}, []string{"4028"}), func(*pairingpb.DiscoverResponse) error {
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
	stream := registerConfiguredNode(reg, 12)
	defer stream.Unregister()
	go func() { <-stream.Outgoing }()
	var got []*pairingpb.DiscoverResponse
	err := svc.RunOnNode(t.Context(), 12, "Fleet Node 12", ipListReq([]string{"10.0.0.5"}, nil), collectResponses(&got))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].GetWarning(), "Fleet Node 12:")
	assert.Contains(t, got[0].GetWarning(), "timed out")
}

func TestDiscoveryNodeIDs_PropagatesListError(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	svc := NewService(reg, stubLister{err: errors.New("db unavailable")})

	// Act
	_, err := svc.DiscoveryNodeIDs(context.Background(), 1)

	// Assert
	require.Error(t, err)
}

func TestRunOnNode_InterpretsLocalSubnetFlag(t *testing.T) {
	for _, tc := range []struct {
		name        string
		target      string
		localSubnet bool
		wantTarget  string
	}{
		{name: "sentinel stays on node", target: netscan.LocalSubnetTarget, wantTarget: netscan.LocalSubnetTarget},
		{name: "flag normalizes to sentinel", target: "10.0.0.0/28", localSubnet: true, wantTarget: netscan.LocalSubnetTarget},
		{name: "explicit target stays unchanged", target: "10.0.0.0/28", wantTarget: "10.0.0.0/28"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := control.NewRegistry()
			svc := NewService(reg, stubLister{})
			const nodeID = int64(21)
			stream := registerConfiguredNode(reg, nodeID)
			stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1-10.0.0.100"}})
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

			err := svc.RunOnNode(context.Background(), nodeID, "Fleet Node 7", req, func(*pairingpb.DiscoverResponse) error { return nil })

			require.NoError(t, err)
			assert.Equal(t, tc.wantTarget, <-gotTarget)
		})
	}
}

func TestDiscoveryReadiness_WarnsWithoutDispatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  *gatewaypb.DiscoveryConfig
		warning string
	}{
		{"older node", nil, "upgrade"},
		{"unconfigured node", &gatewaypb.DiscoveryConfig{}, "configure"},
		{"invalid config", &gatewaypb.DiscoveryConfig{Ranges: []string{"8.8.8.8"}}, "invalid"},
		{"invalid report shape", &gatewaypb.DiscoveryConfig{Ranges: []string{""}}, "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			reg := control.NewRegistry()
			stream := reg.Register(7)
			defer stream.Unregister()
			stream.SetDiscoveryConfig(tc.config)
			svc := NewService(reg, stubLister{nodes: []enrollment.FleetNodeListing{{FleetNode: enrollment.FleetNode{ID: 7, EnrollmentStatus: enrollment.FleetNodeStatusConfirmed}}}})
			candidates, err := svc.DiscoveryNodeIDs(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, []int64{7}, candidates)
			var responses []*pairingpb.DiscoverResponse

			// Act
			err = svc.RunOnNode(t.Context(), 7, "Fleet Node 7", autoNetworkScanReq(nil), collectResponses(&responses))

			// Assert
			require.NoError(t, err)
			require.Len(t, responses, 1)
			require.Contains(t, responses[0].GetWarning(), "Fleet Node 7:")
			require.Contains(t, responses[0].GetWarning(), tc.warning)
			select {
			case cmd := <-stream.Outgoing:
				t.Fatalf("unready node received command %q", cmd.GetCommandId())
			default:
			}
		})
	}
}

func TestRunOnNode_HostnameReportsStayWithinAssignedRanges(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	stream := reg.Register(7)
	defer stream.Unregister()
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.5"}})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	checked := make(chan bool, 1)
	go func() {
		select {
		case cmd := <-stream.Outgoing:
			var envelope gatewaypb.AgentCommand
			if proto.Unmarshal(cmd.GetPayload(), &envelope) != nil {
				checked <- false
				return
			}
			scope, ok := reg.ReportScopeFor(7, cmd.GetCommandId())
			checked <- ok && scope("10.0.0.5", "4028") && !scope("10.0.0.6", "4028") && !scope("10.0.0.5", "80") && proto.Equal(envelope.GetDiscover(), ipListReq([]string{"10.0.0.6", "miner.lan"}, []string{"4028"}))
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
		case <-ctx.Done():
			checked <- false
		}
	}()

	// Act
	err := NewService(reg, nil).RunOnNode(ctx, 7, "Fleet Node", ipListReq([]string{"10.0.0.6", "miner.lan"}, []string{"4028"}), func(*pairingpb.DiscoverResponse) error { return nil })

	// Assert
	require.NoError(t, err)
	require.True(t, <-checked, "node resolves hostname; server constrains its report to assigned IPs and requested ports")
}

func TestRunOnNode_ForwardsMixedScopeRequestInOneCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *pairingpb.DiscoverRequest
	}{
		{"list", ipListReq([]string{"10.0.0.5", "10.0.0.201", "192.168.1.5"}, []string{"4028"})},
		{"range", ipRangeReq("10.0.0.5", "10.0.1.255", []string{"4028"})},
		{"network", networkScanReq("10.0.0.0/23", []string{"4028"})},
		{"automatic4096", autoNetworkScanReq([]string{"4028"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			reg := control.NewRegistry()
			stream := reg.Register(7)
			defer stream.Unregister()
			ranges := []string{"10.0.0.5-10.0.0.200", "10.0.2.1"}
			if tc.name == "automatic4096" {
				ranges = []string{"10.0.0.0/20"}
			}
			stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: ranges})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			commands := make(chan *pairingpb.DiscoverRequest, 32)
			scopes := make(chan control.ReportScope, 32)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					select {
					case cmd := <-stream.Outgoing:
						var envelope gatewaypb.AgentCommand
						if err := proto.Unmarshal(cmd.GetPayload(), &envelope); err != nil {
							return
						}
						commands <- envelope.GetDiscover()
						scope, _ := reg.ReportScopeFor(7, cmd.GetCommandId())
						scopes <- scope
						stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
					case <-ctx.Done():
						return
					}
				}
			}()

			err := NewService(reg, nil).RunOnNode(ctx, 7, "Fleet Node 7", req, func(*pairingpb.DiscoverResponse) error { return nil })
			cancel()
			<-done

			require.NoError(t, err)
			require.Len(t, commands, 1)
			require.True(t, proto.Equal(req, <-commands), "the original target must reach the node unchanged")
			scope := <-scopes
			require.True(t, scope("10.0.0.5", "4028"))
			require.False(t, scope("192.168.1.5", "4028"), "even requested reports must be inside the node's local ranges")
			require.False(t, scope("10.0.0.5", "80"), "reports must use requested ports")
			if req.GetNetworkScan().GetTarget() != netscan.LocalSubnetTarget {
				require.False(t, scope("10.0.2.1", "4028"), "local reports must also be inside the original request")
				require.False(t, scope("10.0.0.201", "4028"), "requested reports must also be inside the node's local ranges")
			}
		})
	}
}
