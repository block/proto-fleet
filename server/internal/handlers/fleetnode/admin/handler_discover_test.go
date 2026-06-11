package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func TestDiscoverOnFleetNode_StreamsBatchesAndStopsOnAck(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-1")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			var env pairingpb.AgentCommand
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &env))
			req := env.GetDiscover()
			ip := req.GetIpList().GetIpAddresses()
			require.Equal(t, []string{"10.0.0.5"}, ip)

			h.registry.PublishBatch(fleetNodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
				Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:abc", IpAddress: "10.0.0.5"}},
			})
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
		case <-time.After(2 * time.Second):
			t.Errorf("agent goroutine timed out waiting for command")
		}
	}()

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{
					IpAddresses: []string{"10.0.0.5"},
					Ports:       []string{"4028"},
				},
			},
		},
	}))
	require.NoError(t, err)

	// Assert
	var devices []*pairingpb.Device
	for resp.Receive() {
		devices = append(devices, resp.Msg().GetResponse().GetDevices()...)
	}
	require.NoError(t, resp.Err())
	require.NoError(t, resp.Close())
	require.Len(t, devices, 1)
	assert.Equal(t, "auto:abc", devices[0].GetDeviceIdentifier())
	<-agentDone
}

func TestDiscoverOnFleetNode_NoStreamReturnsFailedPrecondition(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-no-stream")
	client := startAdminServer(t, h)

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}},
			},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no batches before error")
	}

	// Assert
	streamErr := resp.Err()
	require.Error(t, streamErr)
	var connErr *connect.Error
	require.True(t, errors.As(streamErr, &connErr))
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())
}

func TestDiscoverOnFleetNode_FailedAckWithoutMessageReturnsError(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-failed-ack")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			// Failure ack with only a code set and no error_message, as a
			// buggy or custom agent might emit. Must still fail the command.
			stream.PublishAck(&gatewaypb.ControlAck{
				CommandId: cmd.GetCommandId(),
				Succeeded: false,
				Code:      gatewaypb.AckCode_ACK_CODE_BAD_REQUEST,
			})
		case <-time.After(2 * time.Second):
			t.Errorf("agent goroutine timed out waiting for command")
		}
	}()

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}, Ports: []string{"4028"}},
			},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no batches before error")
	}

	// Assert: BAD_REQUEST ack surfaces as InvalidArgument, not silent success.
	var connErr *connect.Error
	require.True(t, errors.As(resp.Err(), &connErr))
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
	<-agentDone
}

func TestDiscoverOnFleetNode_PartialAckCompletesSuccessfully(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-partial")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			// Agent uploads partial results, then acks PARTIAL (succeeded=false).
			h.registry.PublishBatch(fleetNodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
				Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:partial", IpAddress: "10.0.0.9"}},
			})
			stream.PublishAck(&gatewaypb.ControlAck{
				CommandId:    cmd.GetCommandId(),
				Succeeded:    false,
				Code:         gatewaypb.AckCode_ACK_CODE_PARTIAL,
				ErrorMessage: "scan exceeded command deadline; 1 partial report uploaded",
			})
		case <-time.After(2 * time.Second):
			t.Errorf("agent goroutine timed out waiting for command")
		}
	}()

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.9"}, Ports: []string{"4028"}},
			},
		},
	}))
	require.NoError(t, err)

	// Assert: partial results are delivered and the stream completes without error.
	var devices []*pairingpb.Device
	for resp.Receive() {
		devices = append(devices, resp.Msg().GetResponse().GetDevices()...)
	}
	require.NoError(t, resp.Err())
	require.NoError(t, resp.Close())
	require.Len(t, devices, 1)
	assert.Equal(t, "auto:partial", devices[0].GetDeviceIdentifier())
	<-agentDone
}

func TestDiscoverOnFleetNode_RejectsInconsistentSuccessAck(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-inconsistent-ack")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			// Inconsistent ack: Succeeded=true but a non-OK structured code.
			stream.PublishAck(&gatewaypb.ControlAck{
				CommandId: cmd.GetCommandId(),
				Succeeded: true,
				Code:      gatewaypb.AckCode_ACK_CODE_BAD_REQUEST,
			})
		case <-time.After(2 * time.Second):
			t.Errorf("agent goroutine timed out waiting for command")
		}
	}()

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}, Ports: []string{"4028"}},
			},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no batches before error")
	}

	// Assert: a non-OK code is a failure even though Succeeded=true.
	var connErr *connect.Error
	require.True(t, errors.As(resp.Err(), &connErr))
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
	<-agentDone
}

func TestDiscoverOnFleetNode_RejectsScanExceedingAgentCaps(t *testing.T) {
	tests := []struct {
		name    string
		request *pairingpb.DiscoverRequest
	}{
		{
			name: "too many ip addresses",
			request: &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: repeatString("10.0.0.5", 1025), Ports: []string{"4028"}},
			}},
		},
		{
			name: "too many ports",
			request: &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}, Ports: repeatString("4028", 11)},
			}},
		},
		{
			name: "too many nmap ports",
			request: &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_Nmap{
				Nmap: &pairingpb.NmapModeRequest{Target: "10.0.0.0/28", Ports: repeatString("4028", 11)},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newPairingHarness(t)
			fleetNodeID := h.createFleetNode(t, "admin-discover-caps")
			client := startAdminServer(t, h)

			// Act: oversized request must be rejected before dispatch.
			resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
				FleetNodeId: fleetNodeID,
				Request:     tc.request,
			}))
			require.NoError(t, err)
			for resp.Receive() {
				t.Fatal("expected no batches before error")
			}

			// Assert
			var connErr *connect.Error
			require.True(t, errors.As(resp.Err(), &connErr))
			assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
		})
	}
}

func TestDiscoverOnFleetNode_RejectsUnsupportedNmapTarget(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "ipv6 cidr", target: "2001:db8::/32"},
		{name: "ipv4 cidr broader than /22", target: "10.0.0.0/16"},
		{name: "leading dash flag", target: "-iL/etc/passwd"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newPairingHarness(t)
			fleetNodeID := h.createFleetNode(t, "admin-discover-nmap")
			client := startAdminServer(t, h)

			// Act: an unsupported nmap target is rejected before dispatch.
			resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
				FleetNodeId: fleetNodeID,
				Request: &pairingpb.DiscoverRequest{
					Mode: &pairingpb.DiscoverRequest_Nmap{
						Nmap: &pairingpb.NmapModeRequest{Target: tc.target, Ports: []string{"4028"}},
					},
				},
			}))
			require.NoError(t, err)
			for resp.Receive() {
				t.Fatal("expected no batches before error")
			}

			// Assert
			var connErr *connect.Error
			require.True(t, errors.As(resp.Err(), &connErr))
			assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
		})
	}
}

func repeatString(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestDiscoverOnFleetNode_RejectsMDNSMode(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-mdns")
	client := startAdminServer(t, h)

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_Mdns{Mdns: &pairingpb.MDNSModeRequest{}},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no batches before error")
	}

	// Assert
	var connErr *connect.Error
	require.True(t, errors.As(resp.Err(), &connErr))
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

func TestDiscoverOnFleetNode_NmapModePassesThrough(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-nmap")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	gotTarget := make(chan string, 1)
	go func() {
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			var env pairingpb.AgentCommand
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &env))
			req := env.GetDiscover()
			gotTarget <- req.GetNmap().GetTarget()
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
		case <-time.After(2 * time.Second):
			t.Errorf("timed out waiting for command")
		}
	}()

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_Nmap{Nmap: &pairingpb.NmapModeRequest{Target: "10.0.0.0/28", Ports: []string{"4028"}}},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
	}
	require.NoError(t, resp.Err())

	// Assert
	select {
	case target := <-gotTarget:
		assert.Equal(t, "10.0.0.0/28", target)
	case <-time.After(2 * time.Second):
		t.Fatal("agent never received Nmap command")
	}
}

func TestDiscoverOnFleetNode_NmapModeRejectsEmptyTarget(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-nmap-empty")
	client := startAdminServer(t, h)

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_Nmap{Nmap: &pairingpb.NmapModeRequest{}},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no batches before error")
	}

	// Assert
	var connErr *connect.Error
	require.True(t, errors.As(resp.Err(), &connErr))
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

func TestDiscoverOnFleetNode_ExpandsIPRangeIntoIPList(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-range")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	gotIPs := make(chan []string, 1)
	go func() {
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			var env pairingpb.AgentCommand
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &env))
			req := env.GetDiscover()
			gotIPs <- req.GetIpList().GetIpAddresses()
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
		case <-time.After(2 * time.Second):
			t.Errorf("timed out waiting for command")
		}
	}()

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpRange{
				IpRange: &pairingpb.IPRangeModeRequest{StartIp: "10.0.0.5", EndIp: "10.0.0.7", Ports: []string{"80"}},
			},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
	}
	require.NoError(t, resp.Err())

	// Assert
	select {
	case ips := <-gotIPs:
		assert.Equal(t, []string{"10.0.0.5", "10.0.0.6", "10.0.0.7"}, ips)
	case <-time.After(2 * time.Second):
		t.Fatal("agent never recorded IPs")
	}
}

func TestDiscoverOnFleetNode_RejectsUnconfirmedFleetNode(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-revoked")
	_, err := h.db.Exec(`UPDATE fleet_node SET enrollment_status = 'REVOKED' WHERE id = $1`, fleetNodeID)
	require.NoError(t, err)
	client := startAdminServer(t, h)

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}},
			},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no batches before error")
	}

	// Assert
	var connErr *connect.Error
	require.True(t, errors.As(resp.Err(), &connErr))
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())
}

func TestDiscoverOnFleetNode_RequiresAdminSession(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-viewer")
	srv := startAdminServerWithRole(t, h, "VIEWER")

	// Act
	resp, err := srv.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}}},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no response")
	}

	// Assert
	var connErr *connect.Error
	require.True(t, errors.As(resp.Err(), &connErr))
	assert.Equal(t, connect.CodePermissionDenied, connErr.Code())
}

func TestDiscoverOnFleetNode_TimesOutWhenAgentNeverResponds(t *testing.T) {
	// Arrange: register an agent stream but never publish batch or ack.
	// Override DiscoverCommandTimeout to a short window so the test
	// terminates quickly.
	prev := discovery.DiscoverCommandTimeout
	discovery.DiscoverCommandTimeout = 200 * time.Millisecond
	t.Cleanup(func() { discovery.DiscoverCommandTimeout = prev })

	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-timeout")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()
	// Drain the outgoing command so Send doesn't block, but never ack it.
	go func() { <-stream.Outgoing }()

	client := startAdminServer(t, h)

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpList{
				IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{"10.0.0.5"}},
			},
		},
	}))
	require.NoError(t, err)
	for resp.Receive() {
		t.Fatal("expected no batches before timeout")
	}

	// Assert
	var connErr *connect.Error
	require.True(t, errors.As(resp.Err(), &connErr))
	assert.Equal(t, connect.CodeDeadlineExceeded, connErr.Code())
}

func startAdminServer(t *testing.T, h *pairingHarness) fleetnodeadminv1connect.FleetNodeAdminServiceClient {
	return startAdminServerWithRole(t, h, "ADMIN")
}

func startAdminServerWithRole(t *testing.T, h *pairingHarness, role string) fleetnodeadminv1connect.FleetNodeAdminServiceClient {
	t.Helper()
	var perms []string
	if role == "ADMIN" || role == "SUPER_ADMIN" {
		perms = []string{authz.PermFleetnodeManage, authz.PermFleetnodeRead, authz.PermMinerPair}
	}
	injector := sessionInjector{role: role, orgID: h.orgID, userID: 1, perms: perms}
	mux := http.NewServeMux()
	mux.Handle(fleetnodeadminv1connect.NewFleetNodeAdminServiceHandler(
		h.handler,
		connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), injector),
	))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fleetnodeadminv1connect.NewFleetNodeAdminServiceClient(http.DefaultClient, srv.URL)
}

type sessionInjector struct {
	role   string
	orgID  int64
	userID int64
	perms  []string
}

func (s sessionInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		return next(s.inject(ctx), req)
	}
}

func (s sessionInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (s sessionInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(s.inject(ctx), conn)
	}
}

func (s sessionInjector) inject(ctx context.Context) context.Context {
	ctx = authn.SetInfo(ctx, &session.Info{Role: s.role, OrganizationID: s.orgID, UserID: s.userID})
	eff := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  s.perms,
	}})
	return middleware.WithEffectivePermissions(ctx, eff)
}
