package pairing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domainpairing "github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type stubFleetNodeDiscoveryRunner struct {
	mu          sync.Mutex
	nodeIDs     []int64
	eligibleErr error
	requests    []*pb.DiscoverRequest
	runErr      error
	warning     string
}

func (s *stubFleetNodeDiscoveryRunner) EligibleNodeIDs(context.Context, int64) ([]int64, error) {
	return s.nodeIDs, s.eligibleErr
}

func (s *stubFleetNodeDiscoveryRunner) RunOnNode(
	_ context.Context,
	nodeID int64,
	req *pb.DiscoverRequest,
	onBatch func(*pb.DiscoverResponse) error,
) error {
	cloned := proto.CloneOf(req)
	s.mu.Lock()
	s.requests = append(s.requests, cloned)
	s.mu.Unlock()
	if s.runErr != nil {
		return s.runErr
	}
	if err := onBatch(&pb.DiscoverResponse{Devices: []*pb.Device{
		{DeviceIdentifier: "shared", IpAddress: "192.168.1.10", Port: "4028"},
		{DeviceIdentifier: fmt.Sprintf("node-%d", nodeID), IpAddress: "192.168.1.11", Port: "4028"},
	}}); err != nil {
		return err
	}
	if s.warning != "" {
		if err := onBatch(&pb.DiscoverResponse{Warning: fmt.Sprintf("Fleet Node %d: %s", nodeID, s.warning)}); err != nil {
			return err
		}
	}
	return s.runErr
}

// ctxWithPerms builds the request context the auth interceptor would produce:
// session info plus the caller's effective org-scoped permissions.
func ctxWithPerms(perms ...string) context.Context {
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      "sess-1",
		UserID:         1,
		OrganizationID: 1,
		ExternalUserID: "user-1",
		Username:       "alice",
	}
	ctx := authn.SetInfo(context.Background(), info)
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(
		[]authz.Assignment{{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: perms}},
	))
}

func TestFleetNodeDiscoveryRequest(t *testing.T) {
	ipList := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10"}, Ports: []string{"4028"},
	}}}
	ipRange := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpRange{IpRange: &pb.IPRangeModeRequest{
		StartIp: "192.168.1.10", EndIp: "192.168.1.20", Ports: []string{"4028"},
	}}}
	nmapRequest := func(localSubnet bool) *pb.DiscoverRequest {
		return &pb.DiscoverRequest{
			Mode: &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{
				Target: "192.168.1.0/24", Ports: []string{"4028"}, UseFleetNodeLocalSubnet: localSubnet,
			}},
		}
	}

	assert.Same(t, ipList, fleetNodeDiscoveryRequest(ipList))
	assert.Same(t, ipRange, fleetNodeDiscoveryRequest(ipRange))
	assert.Nil(t, fleetNodeDiscoveryRequest(&pb.DiscoverRequest{
		Mode: &pb.DiscoverRequest_Mdns{Mdns: &pb.MDNSModeRequest{}},
	}))
	manual := nmapRequest(false)
	automatic := nmapRequest(true)
	assert.Same(t, manual, fleetNodeDiscoveryRequest(manual))
	assert.Same(t, automatic, fleetNodeDiscoveryRequest(automatic))
}

func TestDiscover_RejectsInvalidNodeRequestBeforeStartingSources(t *testing.T) {
	tests := []struct {
		name string
		req  *pb.DiscoverRequest
		want string
	}{
		{
			name: "range exceeds 1024 targets",
			req: &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpRange{IpRange: &pb.IPRangeModeRequest{
				StartIp: "10.0.0.2", EndIp: "10.0.4.2",
			}}},
			want: "ip range exceeds 1024 addresses",
		},
		{
			name: "public IP list target",
			req: &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{
				IpAddresses: []string{"8.8.8.8"},
			}}},
			want: "not a private",
		},
		{
			name: "manual subnet exceeds minimum prefix",
			req: &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{
				Target: "192.168.0.0/21",
			}}},
			want: "supported minimum /22",
		},
		{
			name: "automatic subnet still validates ports",
			req: &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{
				Target: "192.168.0.0/21", UseFleetNodeLocalSubnet: true, Ports: []string{"70000"},
			}}},
			want: "invalid port",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange: a nil local service and stream fail if discovery starts.
			runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}
			h := &Handler{discovery: runner}

			// Act
			err := h.Discover(ctxWithPerms(authz.PermMinerPair), connect.NewRequest(tt.req), nil)

			// Assert
			require.ErrorContains(t, err, tt.want)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Empty(t, runner.requests)
		})
	}
}

func TestDiscoverRequest_IPListTargetLimit(t *testing.T) {
	addresses := make([]string, 1024)
	for i := range addresses {
		addresses[i] = "192.168.1.10"
	}
	request := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{
		IpAddresses: addresses,
	}}}

	assert.NoError(t, protovalidate.Validate(request))
	request.GetIpList().IpAddresses = append(request.GetIpList().IpAddresses, "192.168.1.11")
	assert.Error(t, protovalidate.Validate(request))
}

func TestForwardDiscoverySources_FansOutManualRequestsAndDeduplicates(t *testing.T) {
	requests := []*pb.DiscoverRequest{
		{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}, Ports: []string{"4028"}}}},
		{Mode: &pb.DiscoverRequest_IpRange{IpRange: &pb.IPRangeModeRequest{StartIp: "192.168.1.10", EndIp: "192.168.1.20", Ports: []string{"4028"}}}},
		{Mode: &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{Target: "192.168.1.0/24", Ports: []string{"4028"}}}},
	}

	for _, req := range requests {
		t.Run(fmt.Sprintf("%T", req.GetMode()), func(t *testing.T) {
			runner := &stubFleetNodeDiscoveryRunner{
				nodeIDs: []int64{7, 8},
				warning: "node busy",
			}
			h := &Handler{discovery: runner}
			serverResults := make(chan *pb.DiscoverResponse, 1)
			serverResults <- &pb.DiscoverResponse{Devices: []*pb.Device{{
				DeviceIdentifier: "shared", IpAddress: "192.168.1.10", Port: "4028",
			}}}
			close(serverResults)
			var sent []*pb.Device
			var warnings []string
			fwd := newDedupForwarder(func(resp *pb.DiscoverResponse) error {
				sent = append(sent, resp.GetDevices()...)
				if resp.GetWarning() != "" {
					warnings = append(warnings, resp.GetWarning())
				}
				return nil
			}, nil)

			require.NoError(t, h.forwardDiscoverySources(ctxWithPerms(authz.PermMinerPair), 1, serverResults, req, fwd))
			assert.ElementsMatch(t, []string{"Fleet Node 7: node busy", "Fleet Node 8: node busy"}, warnings)

			require.Len(t, runner.requests, 2)
			for _, got := range runner.requests {
				assert.True(t, proto.Equal(req, got), "manual request must reach every node unchanged")
			}
			ids := make([]string, 0, len(sent))
			for _, device := range sent {
				ids = append(ids, device.GetDeviceIdentifier())
			}
			assert.ElementsMatch(t, []string{"shared", "node-7", "node-8"}, ids)
		})
	}
}

func TestForwardDiscoverySources_CanceledContextDoesNotDispatch(t *testing.T) {
	runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7, 8}}
	h := &Handler{discovery: runner}
	serverResults := make(chan *pb.DiscoverResponse)
	close(serverResults)
	ctx, cancel := context.WithCancel(ctxWithPerms(authz.PermMinerPair))
	cancel()
	fwd := newDedupForwarder(func(*pb.DiscoverResponse) error { return nil }, nil)

	require.NoError(t, h.forwardDiscoverySources(ctx, 1, serverResults, &pb.DiscoverRequest{
		Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}}},
	}, fwd))

	assert.Empty(t, runner.requests)
}

func TestForwardDiscoverySources_EligibleNodeLookupFailureKeepsServerResults(t *testing.T) {
	runner := &stubFleetNodeDiscoveryRunner{eligibleErr: errors.New("lookup failed")}
	h := &Handler{discovery: runner}
	serverResults := make(chan *pb.DiscoverResponse, 1)
	serverResults <- &pb.DiscoverResponse{Devices: []*pb.Device{{DeviceIdentifier: "server"}}}
	close(serverResults)
	var sent []*pb.Device
	var warnings []string
	fwd := newDedupForwarder(func(resp *pb.DiscoverResponse) error {
		sent = append(sent, resp.GetDevices()...)
		if resp.GetWarning() != "" {
			warnings = append(warnings, resp.GetWarning())
		}
		return nil
	}, nil)
	require.NoError(t, h.forwardDiscoverySources(ctxWithPerms(authz.PermMinerPair), 1, serverResults, &pb.DiscoverRequest{
		Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}}},
	}, fwd))
	assert.Empty(t, runner.requests)
	require.Len(t, sent, 1)
	assert.Equal(t, "server", sent[0].GetDeviceIdentifier())
	assert.Equal(t, []string{"Fleet Nodes: unable to list connected nodes"}, warnings)
}

func TestForwardDiscoverySources_AuthenticationAndLookupValidationErrorsAreTerminal(t *testing.T) {
	for _, sourceErr := range []error{fleeterror.NewInvalidArgumentError("invalid target"), fleeterror.NewUnauthenticatedError("expired session"), fleeterror.NewForbiddenError("no permission")} {
		for _, lookup := range []bool{false, true} {
			if !lookup && fleeterror.IsInvalidArgumentError(sourceErr) {
				continue // Node-specific target policy failures are source warnings.
			}
			t.Run(fmt.Sprintf("%v/lookup-%t", sourceErr, lookup), func(t *testing.T) {
				runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}, runErr: sourceErr}
				if lookup {
					runner.eligibleErr = sourceErr
				}
				h := &Handler{discovery: runner}
				serverResults := make(chan *pb.DiscoverResponse)
				close(serverResults)
				fwd := newDedupForwarder(func(resp *pb.DiscoverResponse) error { assert.Empty(t, resp.GetWarning()); return nil }, nil)
				err := h.forwardDiscoverySources(t.Context(), 1, serverResults, &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}}}}, fwd)
				require.ErrorIs(t, err, sourceErr)
			})
		}
	}
}

func TestForwardDiscoverySources_NodeTargetPolicyFailureKeepsLaterServerResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *pb.DiscoverRequest
	}{
		{"public address", &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"8.8.8.8"}}}}},
		{"broad private range", &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpRange{IpRange: &pb.IPRangeModeRequest{StartIp: "10.0.0.0", EndIp: "10.0.31.255"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}, runErr: fleeterror.NewInvalidArgumentError("target is outside this node's scan policy")}
			h := &Handler{discovery: runner}
			serverResults := make(chan *pb.DiscoverResponse)
			warningSent := make(chan struct{})
			go func() {
				defer close(serverResults)
				select {
				case <-warningSent:
				case <-ctx.Done():
					return
				}
				select {
				case serverResults <- &pb.DiscoverResponse{Devices: []*pb.Device{{DeviceIdentifier: "server"}}}:
				case <-ctx.Done():
				}
			}()
			var sent []*pb.DiscoverResponse
			fwd := newDedupForwarder(func(resp *pb.DiscoverResponse) error {
				sent = append(sent, resp)
				if resp.GetWarning() != "" {
					close(warningSent)
				}
				return nil
			}, cancel)
			require.NoError(t, h.forwardDiscoverySources(ctx, 1, serverResults, tc.req, fwd))
			require.Len(t, sent, 2)
			assert.Equal(t, "Fleet Node 7: target is outside this node's scan policy", sent[0].GetWarning())
			require.Len(t, sent[1].GetDevices(), 1)
			assert.Equal(t, "server", sent[1].GetDevices()[0].GetDeviceIdentifier())
		})
	}
}

func TestForwardDiscoverySources_StreamInvalidArgumentStaysTerminal(t *testing.T) {
	sendErr := fleeterror.NewInvalidArgumentError("stream rejected response")
	h := &Handler{discovery: &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}}
	fwd := newDedupForwarder(func(*pb.DiscoverResponse) error { return sendErr }, nil)
	err := h.forwardDiscoverySources(t.Context(), 1, nil, &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"10.0.0.1"}}}}, fwd)
	require.ErrorIs(t, err, sendErr)
	require.ErrorIs(t, fwd.failure(), sendErr)
}

func TestDiscover_ServerDefaultsUnavailableStillScansNodes(t *testing.T) {
	for _, defaults := range [][]string{nil, {"not-a-port"}} {
		t.Run(fmt.Sprint(defaults), func(t *testing.T) {
			runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}
			client := discoveryClientWithDefaultPorts(t, runner, defaults)
			stream, err := client.Discover(t.Context(), connect.NewRequest(&pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}}}}))
			require.NoError(t, err)
			var devices []*pb.Device
			var warnings []string
			for stream.Receive() {
				devices = append(devices, stream.Msg().GetDevices()...)
				if stream.Msg().GetWarning() != "" {
					warnings = append(warnings, stream.Msg().GetWarning())
				}
			}
			require.NoError(t, stream.Err())
			require.Len(t, runner.requests, 1)
			require.Len(t, devices, 2)
			require.Len(t, warnings, 1)
			assert.Contains(t, warnings[0], "Fleet Server: ")
			assert.Contains(t, warnings[0], "discovery ports")
			assert.NotContains(t, warnings[0], "FleetError:")
		})
	}
}

func TestDiscover_InvalidServerInputDoesNotFanOutWhenDefaultsUnavailable(t *testing.T) {
	for _, req := range []*pb.DiscoverRequest{
		{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}, Ports: []string{"bad"}}}},
		{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.0/24"}}}},
		{Mode: &pb.DiscoverRequest_IpRange{IpRange: &pb.IPRangeModeRequest{StartIp: "10.0.0.2", EndIp: "10.0.0.1"}}},
		{Mode: &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{Target: "not/a/target"}}},
	} {
		t.Run(req.String(), func(t *testing.T) {
			runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}
			client := discoveryClientWithDefaultPorts(t, runner, nil)
			stream, err := client.Discover(t.Context(), connect.NewRequest(req))
			if err == nil {
				assert.False(t, stream.Receive())
				err = stream.Err()
			}
			require.Error(t, err)
			assert.Empty(t, runner.requests)
		})
	}
}

func discoveryClientWithDefaultPorts(t *testing.T, runner *stubFleetNodeDiscoveryRunner, defaults []string) pairingv1connect.PairingServiceClient {
	t.Helper()
	caps := pairingmocks.NewMockCapabilitiesProvider(gomock.NewController(t))
	caps.EXPECT().GetDefaultDiscoveryPorts(gomock.Any()).Return(defaults).AnyTimes()
	h := &Handler{pairingSvc: domainpairing.NewService(nil, nil, nil, nil, nil, caps, nil, nil), discovery: runner}
	_, handler := pairingv1connect.NewPairingServiceHandler(h)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(ctxWithPerms(authz.PermMinerPair)))
	}))
	t.Cleanup(server.Close)
	return pairingv1connect.NewPairingServiceClient(server.Client(), server.URL)
}

func TestSelectedDeviceIdentifiers_IncludeDevices(t *testing.T) {
	selector := &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonv1.DeviceIdentifierList{DeviceIdentifiers: []string{"mac:a", "mac:b"}},
		},
	}

	assert.Equal(t, []string{"mac:a", "mac:b"}, selectedDeviceIdentifiers(selector))
}

func TestSelectedDeviceIdentifiers_NonIncludeDevices(t *testing.T) {
	selector := &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_AllDevices{AllDevices: &minercommandv1.DeviceFilter{}},
	}

	assert.Nil(t, selectedDeviceIdentifiers(selector))
}

func TestRemainingSelectedDeviceIdentifiers(t *testing.T) {
	selector := includeDevicesSelector([]string{"cloud", "node", "other-node"})
	routed := map[string]struct{}{
		"node":       {},
		"other-node": {},
	}

	assert.Equal(t, []string{"cloud"}, remainingSelectedDeviceIdentifiers(selector, routed))
}

func TestPendingSortedIdentifiers(t *testing.T) {
	sortedSelected := []string{"alpha", "bravo", "charlie"}
	pending := map[string]struct{}{
		"alpha":   {},
		"charlie": {},
	}

	assert.Equal(t, []string{"alpha", "charlie"}, pendingSortedIdentifiers(sortedSelected, pending))

	delete(pending, "alpha")
	assert.Equal(t, []string{"charlie"}, pendingSortedIdentifiers(sortedSelected, pending))

	delete(pending, "charlie")
	assert.Nil(t, pendingSortedIdentifiers(sortedSelected, pending))
}

func TestAllDevicesFilter(t *testing.T) {
	filter := &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
	}
	selector := &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_AllDevices{AllDevices: filter},
	}

	assert.Same(t, filter, allDevicesFilter(selector))
	assert.Nil(t, allDevicesFilter(&minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{IncludeDevices: &commonv1.DeviceIdentifierList{}},
	}))
	assert.NotNil(t, allDevicesFilter(&minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_AllDevices{AllDevices: &minercommandv1.DeviceFilter{}},
	}))
	assert.Nil(t, allDevicesFilter(includeDevicesSelector([]string{"mac:a"})))
}

func TestMergeAllDevicesPairFailuresSuppressesCloudFailuresForRemoteSuccess(t *testing.T) {
	route := fleetNodePairRoute{
		routedAllDevices: map[string]struct{}{
			"remote-ok":     {},
			"remote-failed": {},
		},
	}

	got := mergeAllDevicesPairFailures(
		[]string{"remote-failed"},
		[]string{"cloud-failed", "remote-ok", "remote-failed"},
		route,
	)

	assert.Equal(t, []string{"cloud-failed", "remote-failed"}, got)
}

func TestMergeAllDevicesPairFailuresKeepsCloudFailuresForCloudOnlyDevices(t *testing.T) {
	route := fleetNodePairRoute{routedAllDevices: map[string]struct{}{}}

	got := mergeAllDevicesPairFailures(nil, []string{"cloud-failed"}, route)

	assert.Equal(t, []string{"cloud-failed"}, got)
}

func TestRoutedTargetsFailed(t *testing.T) {
	routed := map[string]struct{}{"remote-failed": {}}

	assert.True(t, routedTargetsFailed(
		&pb.PairResponse{FailedDeviceIds: []string{"remote-failed"}},
		fleetNodePairRoute{},
		routed,
	))

	assert.False(t, routedTargetsFailed(
		&pb.PairResponse{FailedDeviceIds: []string{"remote-failed"}},
		fleetNodePairRoute{remoteSucceeded: true},
		routed,
	))

	assert.False(t, routedTargetsFailed(
		&pb.PairResponse{FailedDeviceIds: []string{"remote-failed"}},
		fleetNodePairRoute{remoteErr: errors.New("dispatch failed")},
		routed,
	))

	assert.False(t, routedTargetsFailed(
		&pb.PairResponse{},
		fleetNodePairRoute{},
		routed,
	))
}

func TestHandleAllDevicesCloudPairErrorRemoteOnlyFailures(t *testing.T) {
	resp, err := handleAllDevicesCloudPairError(
		&pb.PairResponse{FailedDeviceIds: []string{"remote-failed"}},
		fleetNodePairRoute{routedAllDevices: map[string]struct{}{"remote-failed": {}}},
		fleeterror.NewInvalidArgumentError("no devices match the selector"),
	)

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to pair any devices")
}

func TestHandleAllDevicesCloudPairErrorReturnsPartialRemoteSuccess(t *testing.T) {
	resp, err := handleAllDevicesCloudPairError(
		&pb.PairResponse{FailedDeviceIds: []string{"remote-failed"}},
		fleetNodePairRoute{
			routedAllDevices: map[string]struct{}{
				"remote-ok":     {},
				"remote-failed": {},
			},
			remoteSucceeded: true,
		},
		fleeterror.NewInvalidArgumentError("no devices match the selector"),
	)

	assert.NoError(t, err)
	assert.Equal(t, []string{"remote-failed"}, resp.Msg.GetFailedDeviceIds())
}

func TestHandleAllDevicesCloudPairErrorReturnsCloudFailureAfterRemoteSuccess(t *testing.T) {
	resp, err := handleAllDevicesCloudPairError(
		&pb.PairResponse{FailedDeviceIds: []string{"remote-failed"}},
		fleetNodePairRoute{
			routedAllDevices: map[string]struct{}{
				"remote-ok":     {},
				"remote-failed": {},
			},
			remoteSucceeded: true,
		},
		fleeterror.NewInternalError("cloud pairing failed"),
	)

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cloud pairing failed")
}

func TestHandleExplicitCloudPairErrorReturnsCloudFailureAfterRemoteSuccess(t *testing.T) {
	cloudErr := fleeterror.NewInternalError("telemetry scheduler failed")

	resp, err := handleExplicitCloudPairError(
		fleetNodePairRoute{remoteSucceeded: true},
		cloudErr,
	)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, cloudErr)
}

func TestHandleExplicitCloudPairErrorReturnsRemoteFailureWhenRemoteNeverSucceeded(t *testing.T) {
	remoteErr := errors.New("fleet node dispatch failed")
	cloudErr := fleeterror.NewInternalError("cloud failed")

	resp, err := handleExplicitCloudPairError(
		fleetNodePairRoute{remoteErr: remoteErr},
		cloudErr,
	)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, remoteErr)
}

func TestIsNoCloudPairTargetsError(t *testing.T) {
	assert.True(t, isNoCloudPairTargetsError(fleeterror.NewInvalidArgumentError("no devices match the selector")))
	assert.False(t, isNoCloudPairTargetsError(fleeterror.NewInvalidArgumentError("include_devices selector requires at least one device identifier")))
	assert.False(t, isNoCloudPairTargetsError(connect.NewError(connect.CodeInvalidArgument, errors.New("no devices match the selector"))))
}
