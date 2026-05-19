package fleetnodeadmin_test

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
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

func TestDiscoverOnFleetNode_StreamsBatchesAndStopsOnAck(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-discover-1")
	stream, err := h.registry.Register(fleetNodeID)
	require.NoError(t, err)
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
			var req pairingpb.DiscoverRequest
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &req))
			ip := req.GetIpList().GetIpAddresses()
			require.Equal(t, []string{"10.0.0.5"}, ip)

			h.registry.PublishBatch(fleetNodeID, cmd.GetCommandId(), &pairingpb.DiscoverResponse{
				Devices: []*pairingpb.Device{{DeviceIdentifier: "auto:abc", IpAddress: "10.0.0.5"}},
			})
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true})
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
	stream, err := h.registry.Register(fleetNodeID)
	require.NoError(t, err)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	gotTarget := make(chan string, 1)
	go func() {
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			var req pairingpb.DiscoverRequest
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &req))
			gotTarget <- req.GetNmap().GetTarget()
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true})
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
	stream, err := h.registry.Register(fleetNodeID)
	require.NoError(t, err)
	defer stream.Unregister()

	client := startAdminServer(t, h)

	gotIPs := make(chan []string, 1)
	go func() {
		select {
		case cmd, ok := <-stream.Outgoing:
			if !ok {
				return
			}
			var req pairingpb.DiscoverRequest
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &req))
			gotIPs <- req.GetIpList().GetIpAddresses()
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true})
		case <-time.After(2 * time.Second):
			t.Errorf("timed out waiting for command")
		}
	}()

	// Act
	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		Request: &pairingpb.DiscoverRequest{
			Mode: &pairingpb.DiscoverRequest_IpRange{
				IpRange: &pairingpb.IPRangeModeRequest{StartIp: "10.0.0.1", EndIp: "10.0.0.3", Ports: []string{"80"}},
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
		assert.Equal(t, []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, ips)
	case <-time.After(2 * time.Second):
		t.Fatal("agent never recorded IPs")
	}
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

func startAdminServer(t *testing.T, h *pairingHarness) fleetnodeadminv1connect.FleetNodeAdminServiceClient {
	return startAdminServerWithRole(t, h, domainAuth.AdminRoleName)
}

func startAdminServerWithRole(t *testing.T, h *pairingHarness, role string) fleetnodeadminv1connect.FleetNodeAdminServiceClient {
	t.Helper()
	injector := sessionInjector{role: role, orgID: h.orgID, userID: 1}
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
	return authn.SetInfo(ctx, &session.Info{Role: s.role, OrganizationID: s.orgID, UserID: s.userID})
}
