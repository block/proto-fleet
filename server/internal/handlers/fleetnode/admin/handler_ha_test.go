package admin_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func (h *pairingHarness) withHA() {
	h.enrollment.WithHA(ha.NewService(h.db, sqlstores.NewSQLTransactor(h.db), h.registry, nil, nil))
}

func (h *pairingHarness) insertHAPair(t *testing.T, activeID, standbyID int64, ready bool, reason string) {
	t.Helper()
	_, err := h.db.Exec(`INSERT INTO fleet_node_ha_pair (org_id, active_node_id, standby_node_id, standby_ready, status_reason)
		VALUES ($1, $2, $3, $4, $5)`, h.orgID, activeID, standbyID, ready, reason)
	require.NoError(t, err)
}

func (h *pairingHarness) registerPendingStandby(t *testing.T, name string, targetID int64) int64 {
	t.Helper()
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	code, _, _, err := h.enrollment.CreateStandbyCode(context.Background(), 1, h.orgID, time.Hour, targetID)
	require.NoError(t, err)
	node, _, err := h.enrollment.RegisterFleetNode(context.Background(), code, name, pubKey, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	return node.ID
}

func TestCreateEnrollmentCode_StandbyRecordsTarget(t *testing.T) {
	h := newPairingHarness(t)
	h.withHA()
	targetID := h.createFleetNode(t, "admin-standby-code-target")

	resp, err := h.handler.CreateEnrollmentCode(h.adminCtx(), connect.NewRequest(&pb.CreateEnrollmentCodeRequest{StandbyForNodeId: &targetID}))

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.GetCode())
	var recorded sql.NullInt64
	require.NoError(t, h.db.QueryRow(`SELECT standby_for_node_id FROM pending_enrollment WHERE id = $1`, resp.Msg.GetPendingEnrollmentId()).Scan(&recorded))
	assert.Equal(t, sql.NullInt64{Int64: targetID, Valid: true}, recorded)
}

func TestCreateEnrollmentCode_StandbyRejectsTargetAlreadyInPair(t *testing.T) {
	h := newPairingHarness(t)
	h.withHA()
	activeID := h.createFleetNode(t, "admin-standby-code-active")
	standbyID := h.createFleetNode(t, "admin-standby-code-standby")
	h.insertHAPair(t, activeID, standbyID, true, "")

	for name, targetID := range map[string]int64{"active": activeID, "standby": standbyID} {
		t.Run(name, func(t *testing.T) {
			_, err := h.handler.CreateEnrollmentCode(h.adminCtx(), connect.NewRequest(&pb.CreateEnrollmentCodeRequest{StandbyForNodeId: &targetID}))

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode)
		})
	}
}

func TestListFleetNodes_ReportsHARoleAndPeer(t *testing.T) {
	h := newPairingHarness(t)
	h.withHA()
	activeID := h.createFleetNode(t, "admin-ha-active")
	standbyID := h.createFleetNode(t, "admin-ha-standby")
	h.insertHAPair(t, activeID, standbyID, true, "credential key transferred")
	standaloneID := h.createFleetNode(t, "admin-ha-standalone")
	pendingStandbyID := h.registerPendingStandby(t, "admin-ha-pending-standby", standaloneID)

	resp, err := h.handler.ListFleetNodes(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodesRequest{}))

	require.NoError(t, err)
	type haView struct {
		role     pb.FleetNodeHARole
		peerID   int64
		peerName string
		ready    bool
		reason   string
	}
	got := make(map[int64]haView, len(resp.Msg.GetFleetNodes()))
	var standalone *pb.FleetNodeSummary
	for _, n := range resp.Msg.GetFleetNodes() {
		got[n.GetFleetNodeId()] = haView{n.GetHaRole(), n.GetHaPeerNodeId(), n.GetHaPeerNodeName(), n.GetHaStandbyReady(), n.GetHaStatusReason()}
		if n.GetFleetNodeId() == standaloneID {
			standalone = n
		}
	}
	assert.Equal(t, map[int64]haView{
		activeID:         {pb.FleetNodeHARole_FLEET_NODE_HA_ROLE_ACTIVE, standbyID, "admin-ha-standby", true, "credential key transferred"},
		standbyID:        {pb.FleetNodeHARole_FLEET_NODE_HA_ROLE_STANDBY, activeID, "admin-ha-active", true, "credential key transferred"},
		pendingStandbyID: {pb.FleetNodeHARole_FLEET_NODE_HA_ROLE_STANDBY, standaloneID, "admin-ha-standalone", false, ""},
		standaloneID:     {},
	}, got)
	require.NotNil(t, standalone)
	assert.Nil(t, standalone.HaPeerNodeId, "a node outside a pair has no peer")
}

func TestListFleetNodes_PairingOnlyCallerDoesNotTreatOfflineStandbyAsDegradedCoverage(t *testing.T) {
	h := newPairingHarness(t)
	activeID := h.createFleetNode(t, "admin-ha-coverage-active")
	standbyID := h.createFleetNode(t, "admin-ha-coverage-standby")
	h.insertHAPair(t, activeID, standbyID, true, "")
	stream := h.registry.Register(activeID)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"}})
	defer stream.Unregister()

	resp, err := h.handler.ListFleetNodes(h.ctxWithPerms(authz.PermMinerPair), connect.NewRequest(&pb.ListFleetNodesRequest{}))

	require.NoError(t, err)
	require.Len(t, resp.Msg.GetFleetNodes(), 2)
	for _, node := range resp.Msg.GetFleetNodes() {
		assert.Zero(t, node.GetFleetNodeId(), "pairing-only projections remain redacted")
		assert.False(t, node.GetDiscoveryCoverageDegraded())
	}
}

func TestDiscoverOnFleetNode_StandbyReturnsFailedPrecondition(t *testing.T) {
	h := newPairingHarness(t)
	activeID := h.createFleetNode(t, "admin-discover-ha-active")
	standbyID := h.createFleetNode(t, "admin-discover-ha-standby")
	h.insertHAPair(t, activeID, standbyID, true, "")
	stream := h.registry.Register(standbyID)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"}})
	defer stream.Unregister()
	client := startAdminServer(t, h)

	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: standbyID,
		Request:     ipListDiscoverRequest("10.0.0.5"),
	}))
	require.NoError(t, err)
	for resp.Receive() {
	}

	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(resp.Err()))
	select {
	case <-stream.Outgoing:
		t.Fatal("discovery was dispatched to the standby")
	default:
	}
}

func TestDiscoverOnFleetNode_ActiveNodeOfPairIsNotBlocked(t *testing.T) {
	h := newPairingHarness(t)
	activeID := h.createFleetNode(t, "admin-discover-ha-active-ok")
	standbyID := h.createFleetNode(t, "admin-discover-ha-standby-ok")
	h.insertHAPair(t, activeID, standbyID, true, "")
	client := startAdminServer(t, h)

	resp, err := client.DiscoverOnFleetNode(context.Background(), connect.NewRequest(&pb.DiscoverOnFleetNodeRequest{
		FleetNodeId: activeID,
		Request:     ipListDiscoverRequest("10.0.0.5"),
	}))
	require.NoError(t, err)
	var warnings []string
	for resp.Receive() {
		warnings = append(warnings, resp.Msg().GetResponse().GetWarning())
	}

	require.NoError(t, resp.Err())
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "has no active control stream")
}

func TestPairDiscoveredDevicesOnFleetNode_StandbyReturnsFailedPrecondition(t *testing.T) {
	h := newPairingHarness(t)
	activeID := h.createFleetNode(t, "admin-pairdisc-ha-active")
	standbyID := h.createFleetNode(t, "admin-pairdisc-ha-standby")
	h.insertHAPair(t, activeID, standbyID, true, "")
	h.insertDiscoveredForNode(t, standbyID, "mac:ha-standby-1")
	stream := h.registry.Register(standbyID)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"}})
	defer stream.Unregister()
	client := startAdminServer(t, h)

	resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       standbyID,
		DeviceIdentifiers: []string{"mac:ha-standby-1"},
	}))
	require.NoError(t, err)
	for resp.Receive() {
	}

	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(resp.Err()))
	select {
	case <-stream.Outgoing:
		t.Fatal("pairing was dispatched to the standby")
	default:
	}
}

func TestPairDiscoveredDevicesOnFleetNode_ActiveNodeOfPairIsNotBlocked(t *testing.T) {
	h := newPairingHarness(t)
	activeID := h.createFleetNode(t, "admin-pairdisc-ha-active-ok")
	standbyID := h.createFleetNode(t, "admin-pairdisc-ha-standby-ok")
	h.insertHAPair(t, activeID, standbyID, true, "")
	stream := h.registry.Register(activeID)
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"}})
	defer stream.Unregister()
	client := startAdminServer(t, h)

	resp, err := client.PairDiscoveredDevicesOnFleetNode(context.Background(), connect.NewRequest(&pb.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       activeID,
		DeviceIdentifiers: []string{"mac:does-not-exist"},
	}))
	require.NoError(t, err)
	for resp.Receive() {
	}

	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(resp.Err()))
}

func ipListDiscoverRequest(ip string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{
		Mode: &pairingpb.DiscoverRequest_IpList{
			IpList: &pairingpb.IPListModeRequest{IpAddresses: []string{ip}, Ports: []string{"4028"}},
		},
	}
}
