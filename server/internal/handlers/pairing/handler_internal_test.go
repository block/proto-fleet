package pairing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"buf.build/go/protovalidate"
	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/nmaptarget"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type stubFleetNodeDiscoveryRunner struct {
	mu       sync.Mutex
	nodeIDs  []int64
	requests []*pb.DiscoverRequest
	runErr   error
}

func (s *stubFleetNodeDiscoveryRunner) EligibleNodeIDs(context.Context, int64) ([]int64, error) {
	return s.nodeIDs, nil
}

func (s *stubFleetNodeDiscoveryRunner) RunOnNode(
	_ context.Context,
	nodeID int64,
	req *pb.DiscoverRequest,
	onBatch func(*pb.DiscoverResponse) error,
) error {
	cloned := &pb.DiscoverRequest{}
	proto.Merge(cloned, req)
	s.mu.Lock()
	s.requests = append(s.requests, cloned)
	s.mu.Unlock()
	if err := onBatch(&pb.DiscoverResponse{Devices: []*pb.Device{
		{DeviceIdentifier: "shared", IpAddress: "192.168.1.10", Port: "4028"},
		{DeviceIdentifier: fmt.Sprintf("node-%d", nodeID), IpAddress: "192.168.1.11", Port: "4028"},
	}}); err != nil {
		return err
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

func TestCallerCanManageFleetNodes(t *testing.T) {
	tests := []struct {
		name  string
		perms []string
		want  bool
	}{
		{
			// The fan-out regression: miner:pair alone (no fleetnode:manage) must
			// NOT unlock fleet-node discovery commands.
			name:  "miner:pair only does not grant fleet-node management",
			perms: []string{authz.PermMinerPair},
			want:  false,
		},
		{
			name:  "fleetnode:manage grants it",
			perms: []string{authz.PermMinerPair, authz.PermFleetnodeManage},
			want:  true,
		},
		{
			name:  "fleetnode:read alone does not grant it",
			perms: []string{authz.PermMinerPair, authz.PermFleetnodeRead},
			want:  false,
		},
		{
			name:  "no permissions",
			perms: nil,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctx := ctxWithPerms(tc.perms...)

			// Act
			got := callerCanManageFleetNodes(ctx)

			// Assert
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFleetNodeDiscoveryRequest(t *testing.T) {
	trueValue := true
	falseValue := false
	ipList := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.10"}, Ports: []string{"4028"},
	}}}
	ipRange := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_IpRange{IpRange: &pb.IPRangeModeRequest{
		StartIp: "192.168.1.10", EndIp: "192.168.1.20", Ports: []string{"4028"},
	}}}
	explicitNmap := func(flag *bool) *pb.DiscoverRequest {
		return &pb.DiscoverRequest{
			Mode: &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{
				Target: "192.168.1.0/24", Ports: []string{"4028"},
			}},
			UseFleetNodeLocalSubnet: flag,
		}
	}

	assert.Same(t, ipList, fleetNodeDiscoveryRequest(ipList, false))
	assert.Same(t, ipRange, fleetNodeDiscoveryRequest(ipRange, false))
	assert.Nil(t, fleetNodeDiscoveryRequest(&pb.DiscoverRequest{
		Mode: &pb.DiscoverRequest_Mdns{Mdns: &pb.MDNSModeRequest{}},
	}, false))

	manual := explicitNmap(&falseValue)
	assert.Same(t, manual, fleetNodeDiscoveryRequest(manual, true), "explicit false must override legacy inference")

	for _, tc := range []struct {
		req         *pb.DiscoverRequest
		legacyLocal bool
	}{
		{req: explicitNmap(&trueValue), legacyLocal: false},
		{req: explicitNmap(nil), legacyLocal: true},
	} {
		got := fleetNodeDiscoveryRequest(tc.req, tc.legacyLocal)
		assert.Equal(t, nmaptarget.LocalSubnetTarget, got.GetNmap().GetTarget())
		assert.Equal(t, []string{"4028"}, got.GetNmap().GetPorts())
	}

	legacyManual := explicitNmap(nil)
	assert.Same(t, legacyManual, fleetNodeDiscoveryRequest(legacyManual, false))
}

func TestDiscoverRequest_FleetNodeLocalSubnetRequiresNmap(t *testing.T) {
	falseValue := false
	for _, req := range []*pb.DiscoverRequest{
		{
			Mode:                    &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}}},
			UseFleetNodeLocalSubnet: &falseValue,
		},
		{
			Mode:                    &pb.DiscoverRequest_IpRange{IpRange: &pb.IPRangeModeRequest{StartIp: "192.168.1.10", EndIp: "192.168.1.20"}},
			UseFleetNodeLocalSubnet: &falseValue,
		},
		{
			Mode:                    &pb.DiscoverRequest_Mdns{Mdns: &pb.MDNSModeRequest{}},
			UseFleetNodeLocalSubnet: &falseValue,
		},
	} {
		assert.Error(t, protovalidate.Validate(req))
	}

	assert.NoError(t, protovalidate.Validate(&pb.DiscoverRequest{
		Mode:                    &pb.DiscoverRequest_Nmap{Nmap: &pb.NmapModeRequest{Target: "192.168.1.0/24"}},
		UseFleetNodeLocalSubnet: &falseValue,
	}))
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
				runErr:  errors.New("node busy"),
			}
			h := &Handler{discovery: runner}
			serverResults := make(chan *pb.DiscoverResponse, 1)
			serverResults <- &pb.DiscoverResponse{Devices: []*pb.Device{{
				DeviceIdentifier: "shared", IpAddress: "192.168.1.10", Port: "4028",
			}}}
			close(serverResults)
			var sent []*pb.Device
			fwd := newDedupForwarder(func(resp *pb.DiscoverResponse) error {
				sent = append(sent, resp.GetDevices()...)
				return nil
			}, nil)

			h.forwardDiscoverySources(ctxWithPerms(authz.PermFleetnodeManage), 1, serverResults, req, fwd)

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

func TestForwardDiscoverySources_WithoutFleetNodePermissionIsServerOnly(t *testing.T) {
	runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}
	h := &Handler{discovery: runner}
	serverResults := make(chan *pb.DiscoverResponse, 1)
	serverResults <- &pb.DiscoverResponse{Devices: []*pb.Device{{DeviceIdentifier: "server"}}}
	close(serverResults)
	var sent []*pb.Device
	fwd := newDedupForwarder(func(resp *pb.DiscoverResponse) error {
		sent = append(sent, resp.GetDevices()...)
		return nil
	}, nil)

	h.forwardDiscoverySources(ctxWithPerms(authz.PermMinerPair), 1, serverResults, &pb.DiscoverRequest{
		Mode: &pb.DiscoverRequest_IpList{IpList: &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.10"}}},
	}, fwd)

	assert.Empty(t, runner.requests)
	require.Len(t, sent, 1)
	assert.Equal(t, "server", sent[0].GetDeviceIdentifier())
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
