package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
)

func (h *pairingHarness) insertDiscoveredForNode(t *testing.T, fleetNodeID int64, identifier string) {
	t.Helper()
	_, err := h.db.Exec(
		`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		 VALUES ($1, $2, '10.0.0.7', '80', 'http', 'virtual', TRUE, $3)`,
		h.orgID, identifier, fleetNodeID,
	)
	require.NoError(t, err)
}

// nodeReportPaired simulates the node uploading pair results via the gateway
// ReportPairedDevices RPC -- the authoritative persistence path -- rather than
// poking the registry directly, so tests exercise the real persist+forward flow.
func (h *pairingHarness) nodeReportPaired(t *testing.T, fleetNodeID int64, commandID string, results []*gatewaypb.FleetNodePairResult) {
	t.Helper()
	gw := gateway.NewHandler(nil, nil, h.pairing, h.registry)
	ctx := authn.SetInfo(context.Background(), &auth.Subject{FleetNodeID: fleetNodeID, OrgID: h.orgID, Name: "agent"})
	_, err := gw.ReportPairedDevices(ctx, connect.NewRequest(&gatewaypb.ReportPairedDevicesRequest{CommandId: commandID, Results: results}))
	require.NoError(t, err)
}

func TestPairDiscoveredDevicesOnFleetNode_PairsAndStreamsResults(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-pairdisc-1")
	h.insertDiscoveredForNode(t, fleetNodeID, "mac:hp-1")
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
			var agentCmd gatewaypb.AgentCommand
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &agentCmd))
			pairReq := agentCmd.GetPair()
			require.NotNil(t, pairReq)
			require.Len(t, pairReq.GetTargets(), 1)
			assert.Equal(t, "mac:hp-1", pairReq.GetTargets()[0].GetDeviceIdentifier())

			h.nodeReportPaired(t, fleetNodeID, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{
				{DeviceIdentifier: "mac:hp-1", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: "sn-hp-1", MacAddress: "aa:bb:cc:11:22:33", Model: "S19", FirmwareVersion: "v2"},
			})
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
		case <-time.After(2 * time.Second):
			t.Errorf("agent goroutine timed out waiting for pair command")
		}
	}()

	// Act
	resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       fleetNodeID,
		DeviceIdentifiers: []string{"mac:hp-1"},
	}))
	require.NoError(t, err)

	// Assert: streamed result is PAIRED, and the device is persisted paired + bound.
	var results []*pb.DevicePairingResult
	for resp.Receive() {
		results = append(results, resp.Msg().GetResults()...)
	}
	require.NoError(t, resp.Err())
	require.NoError(t, resp.Close())
	require.Len(t, results, 1)
	assert.Equal(t, "mac:hp-1", results[0].GetDeviceIdentifier())
	assert.Equal(t, fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED, results[0].GetPairingStatus())
	<-agentDone

	var status string
	require.NoError(t, h.db.QueryRow(
		`SELECT dp.pairing_status FROM device d JOIN device_pairing dp ON dp.device_id = d.id WHERE d.device_identifier=$1 AND d.org_id=$2`,
		"mac:hp-1", h.orgID,
	).Scan(&status))
	assert.Equal(t, "PAIRED", status)

	var bound int
	require.NoError(t, h.db.QueryRow(
		`SELECT count(*) FROM device d JOIN fleet_node_device fnd ON fnd.device_id = d.id WHERE d.device_identifier=$1 AND fnd.fleet_node_id=$2`,
		"mac:hp-1", fleetNodeID,
	).Scan(&bound))
	assert.Equal(t, 1, bound)
}

func TestPairDiscoveredDevicesOnFleetNode_SynthesizesUnreportedTargets(t *testing.T) {
	// Arrange: two devices requested, but the node reports only one before its
	// (partial) ack. The operator must still get a terminal status for the device
	// the node never reported, rather than a silently-complete RPC.
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-pairdisc-partial")
	h.insertDiscoveredForNode(t, fleetNodeID, "mac:reported")
	h.insertDiscoveredForNode(t, fleetNodeID, "mac:dropped")
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
			h.nodeReportPaired(t, fleetNodeID, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{
				{DeviceIdentifier: "mac:reported", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: "sn-r", MacAddress: "aa:bb:cc:00:00:01"},
			})
			// PARTIAL: the batch timed out before "mac:dropped" was attempted.
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_PARTIAL})
		case <-time.After(2 * time.Second):
			t.Errorf("agent goroutine timed out waiting for pair command")
		}
	}()

	// Act
	resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       fleetNodeID,
		DeviceIdentifiers: []string{"mac:reported", "mac:dropped"},
	}))
	require.NoError(t, err)
	statuses := map[string]fleetmanagementv1.PairingStatus{}
	for resp.Receive() {
		for _, r := range resp.Msg().GetResults() {
			statuses[r.GetDeviceIdentifier()] = r.GetPairingStatus()
		}
	}
	require.NoError(t, resp.Err())
	require.NoError(t, resp.Close())
	<-agentDone

	// Assert: the reported device is PAIRED; the unreported one gets a synthesized
	// FAILED so the operator has a terminal status + retry signal for every target.
	assert.Equal(t, fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED, statuses["mac:reported"])
	assert.Equal(t, fleetmanagementv1.PairingStatus_PAIRING_STATUS_FAILED, statuses["mac:dropped"])
}

func TestPairDiscoveredDevicesOnFleetNode_DropsResultsOutsideRequestedTargets(t *testing.T) {
	// Arrange: two devices discovered by the node; the operator requests only one.
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-pairdisc-unrequested")
	h.insertDiscoveredForNode(t, fleetNodeID, "mac:req")
	h.insertDiscoveredForNode(t, fleetNodeID, "mac:sneaky")
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
			var agentCmd gatewaypb.AgentCommand
			require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &agentCmd))
			// The dispatched command must carry only the requested target.
			require.Len(t, agentCmd.GetPair().GetTargets(), 1)
			assert.Equal(t, "mac:req", agentCmd.GetPair().GetTargets()[0].GetDeviceIdentifier())
			// A compromised node reports PAIRED for an unrequested device it
			// previously discovered (not in the dispatched targets).
			h.nodeReportPaired(t, fleetNodeID, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{
				{DeviceIdentifier: "mac:sneaky", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: "sn-sneaky", MacAddress: "aa:bb:cc:11:22:02"},
			})
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
		case <-time.After(2 * time.Second):
			t.Errorf("agent goroutine timed out waiting for pair command")
		}
	}()

	// Act
	resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       fleetNodeID,
		DeviceIdentifiers: []string{"mac:req"},
	}))
	require.NoError(t, err)
	statuses := map[string]fleetmanagementv1.PairingStatus{}
	for resp.Receive() {
		for _, r := range resp.Msg().GetResults() {
			statuses[r.GetDeviceIdentifier()] = r.GetPairingStatus()
		}
	}
	require.NoError(t, resp.Err())
	require.NoError(t, resp.Close())
	<-agentDone

	// Assert: the unrequested device is dropped by the gateway scope (never created
	// or persisted, never streamed); the requested device the node never reported
	// gets a synthesized FAILED.
	_, sneakyStreamed := statuses["mac:sneaky"]
	assert.False(t, sneakyStreamed, "an out-of-scope result must not be streamed to the operator")
	assert.Equal(t, fleetmanagementv1.PairingStatus_PAIRING_STATUS_FAILED, statuses["mac:req"])

	var sneaky int
	require.NoError(t, h.db.QueryRow(
		`SELECT count(*) FROM device WHERE device_identifier=$1 AND org_id=$2 AND deleted_at IS NULL`,
		"mac:sneaky", h.orgID,
	).Scan(&sneaky))
	assert.Equal(t, 0, sneaky, "a result outside the requested targets must not create or pair a device")
}

func TestPairDiscoveredDevicesOnFleetNode_PersistsAfterOperatorDisconnect(t *testing.T) {
	// Arrange: the regression for the HIGH split-brain finding. The operator
	// dispatches, then disconnects before the node reports.
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-pairdisc-disconnect")
	h.insertDiscoveredForNode(t, fleetNodeID, "mac:dc-1")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()
	client := startAdminServer(t, h)

	// The operator call blocks until the server streams, so drive it in a goroutine
	// (a streaming client call doesn't return until the handler produces output).
	ctx, cancel := context.WithCancel(context.Background())
	opDone := make(chan struct{})
	go func() {
		defer close(opDone)
		rs, err := client.PairDiscoveredDevicesOnFleetNode(ctx, connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
			FleetNodeId:       fleetNodeID,
			DeviceIdentifiers: []string{"mac:dc-1"},
		}))
		if err != nil {
			return
		}
		for rs.Receive() { //nolint:revive // drain until the operator ctx is cancelled
		}
		_ = rs.Close()
	}()

	// Act: wait for the command to dispatch, then the operator disconnects.
	var cmd *gatewaypb.ControlCommand
	select {
	case c := <-stream.Outgoing:
		cmd = c
	case <-time.After(3 * time.Second):
		t.Fatal("pair command was not dispatched")
	}
	cancel()
	<-opDone

	// The node reports + acks AFTER the operator is gone. The command must still be
	// in flight (dispatched on a disconnect-immune context) so the gateway persists.
	h.nodeReportPaired(t, fleetNodeID, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{
		{DeviceIdentifier: "mac:dc-1", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: "sn-dc-1", MacAddress: "aa:bb:cc:dc:00:01"},
	})
	stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})

	// Assert: the device is persisted (PAIRED + bound) despite the disconnect.
	var status string
	require.NoError(t, h.db.QueryRow(
		`SELECT dp.pairing_status FROM device d JOIN device_pairing dp ON dp.device_id = d.id WHERE d.device_identifier=$1 AND d.org_id=$2`,
		"mac:dc-1", h.orgID,
	).Scan(&status))
	assert.Equal(t, "PAIRED", status)
	var bound int
	require.NoError(t, h.db.QueryRow(
		`SELECT count(*) FROM device d JOIN fleet_node_device fnd ON fnd.device_id = d.id WHERE d.device_identifier=$1 AND fnd.fleet_node_id=$2`,
		"mac:dc-1", fleetNodeID,
	).Scan(&bound))
	assert.Equal(t, 1, bound, "pairing must persist even if the operator disconnected")
}

func TestPairDiscoveredDevicesOnFleetNode_NoStreamReturnsFailedPrecondition(t *testing.T) {
	// Arrange: discovered device exists but the node has no active control stream.
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-pairdisc-nostream")
	h.insertDiscoveredForNode(t, fleetNodeID, "mac:ns-1")
	client := startAdminServer(t, h)

	// Act
	resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       fleetNodeID,
		DeviceIdentifiers: []string{"mac:ns-1"},
	}))
	require.NoError(t, err)
	for resp.Receive() {
	}

	// Assert
	require.Error(t, resp.Err())
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(resp.Err()))
}

func TestPairDiscoveredDevicesOnFleetNode_NoPairableTargetsReturnsInvalidArgument(t *testing.T) {
	// Arrange: a confirmed node with no discovered devices.
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-pairdisc-empty")
	stream := h.registry.Register(fleetNodeID)
	defer stream.Unregister()
	client := startAdminServer(t, h)

	// Act
	resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       fleetNodeID,
		DeviceIdentifiers: []string{"mac:does-not-exist"},
	}))
	require.NoError(t, err)
	for resp.Receive() {
	}

	// Assert
	require.Error(t, resp.Err())
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(resp.Err()))
}

func TestPairDiscoveredDevicesOnFleetNode_RequiresBothPermissions(t *testing.T) {
	tests := []struct {
		name  string
		perms []string
	}{
		{name: "no permissions (VIEWER)", perms: nil},
		{name: "fleetnode:manage only", perms: []string{authz.PermFleetnodeManage}},
		{name: "miner:pair only", perms: []string{authz.PermMinerPair}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newPairingHarness(t)
			fleetNodeID := h.createFleetNode(t, "admin-pairdisc-perm")
			h.insertDiscoveredForNode(t, fleetNodeID, "mac:perm-1")
			injector := sessionInjector{role: "ADMIN", orgID: h.orgID, userID: 1, perms: tc.perms}
			mux := http.NewServeMux()
			mux.Handle(fleetnodeadminv1connect.NewFleetNodeAdminServiceHandler(
				h.handler,
				connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), injector),
			))
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			client := fleetnodeadminv1connect.NewFleetNodeAdminServiceClient(http.DefaultClient, srv.URL)

			// Act
			resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
				FleetNodeId:       fleetNodeID,
				DeviceIdentifiers: []string{"mac:perm-1"},
			}))
			require.NoError(t, err)
			for resp.Receive() {
			}

			// Assert
			require.Error(t, resp.Err())
			assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(resp.Err()))
		})
	}
}

func TestListFleetNodeDiscoveredDevices_ReturnsAndFilters(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	nodeA := h.createFleetNode(t, "admin-list-a")
	nodeB := h.createFleetNode(t, "admin-list-b")
	h.insertDiscoveredForNode(t, nodeA, "mac:la-1")
	h.insertDiscoveredForNode(t, nodeB, "mac:lb-1")
	client := startAdminServer(t, h)

	// Act
	resp, err := client.ListFleetNodeDiscoveredDevices(context.Background(), connect.NewRequest(&pb.ListFleetNodeDiscoveredDevicesRequest{
		FleetNodeId: nodeA,
	}))

	// Assert
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.Msg.GetDevices()))
	for _, d := range resp.Msg.GetDevices() {
		ids = append(ids, d.GetDeviceIdentifier())
		assert.Equal(t, nodeA, d.GetFleetNodeId())
	}
	assert.Contains(t, ids, "mac:la-1")
	assert.NotContains(t, ids, "mac:lb-1")
}
