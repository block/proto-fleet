package admin_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
)

func TestListFleetNodes_HARelationshipAndCountsSurviveFailover(t *testing.T) {
	h := newPairingHarness(t)
	ownerID := h.createFleetNode(t, "stable-device-owner")
	standbyID := h.createFleetNode(t, "replacement-active")
	deviceID := h.insertDevice(t)
	_, err := h.db.Exec(`INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, ownerID, deviceID, h.orgID)
	require.NoError(t, err)
	_, err = h.db.Exec(`INSERT INTO fleet_node_ha_group(owner_node_id,org_id,credential_key_fingerprint,enabled,active_node_id,instance_id,control_session_id,lease_epoch,lease_expires_at,acknowledged) VALUES($1,$2,$3,true,$4,'old-process','old-session',2,clock_timestamp()+interval '30 seconds',true)`, ownerID, h.orgID, bytes.Repeat([]byte{7}, 32), standbyID)
	require.NoError(t, err)
	_, err = h.db.Exec(`INSERT INTO fleet_node_ha_member(node_id,org_id,owner_node_id,ready,status_reason) VALUES($1,$3,$1,true,''),($2,$3,$1,true,'')`, ownerID, standbyID, h.orgID)
	require.NoError(t, err)
	stream := h.registry.Register(standbyID)
	defer stream.Unregister()
	coordinator := ha.NewService(h.db, h.registry)
	require.NoError(t, coordinator.ConnectStream(t.Context(), standbyID, h.orgID, &gatewaypb.ControlHello{HaCapable: true, InstanceId: "replacement", CredentialKeyFingerprint: bytes.Repeat([]byte{7}, 32)}, "replacement-session", stream))
	h.handler.WithHA(coordinator)
	resp, err := h.handler.ListFleetNodes(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)
	byID := make(map[int64]*pb.FleetNodeSummary)
	for _, node := range resp.Msg.FleetNodes {
		byID[node.GetFleetNodeId()] = node
	}
	require.EqualValues(t, 1, byID[ownerID].GetPairedDeviceCount())
	require.Zero(t, byID[standbyID].GetPairedDeviceCount())
	require.Equal(t, "standby", byID[standbyID].GetHaRole(), "a replacement session has no active authority until its grant is acknowledged")
	_, err = h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE owner_node_id=$1`, ownerID)
	require.NoError(t, err)
	grant, err := coordinator.Lease(t.Context(), standbyID, h.orgID, "replacement", "replacement-session", &gatewaypb.HALeaseRequest{RequestId: "acquire"})
	require.NoError(t, err)
	require.True(t, grant.GetGranted())
	_, err = coordinator.Lease(t.Context(), standbyID, h.orgID, "replacement", "replacement-session", &gatewaypb.HALeaseRequest{RequestId: "acknowledge", AcknowledgedEpoch: grant.GetLeaseEpoch()})
	require.NoError(t, err)
	resp, err = h.handler.ListFleetNodes(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)
	for _, node := range resp.Msg.FleetNodes {
		byID[node.GetFleetNodeId()] = node
	}
	require.Equal(t, "active", byID[standbyID].GetHaRole())
	require.EqualValues(t, 1, byID[ownerID].GetPairedDeviceCount())
	require.Zero(t, byID[standbyID].GetPairedDeviceCount())
	_, routable := h.registry.HARoute(ownerID)
	require.True(t, routable)
	require.Equal(t, ownerID, byID[standbyID].GetStandbyForNodeId())
	require.Equal(t, "stable-device-owner", byID[standbyID].GetStandbyForNodeName())
	// Both normal enrollments have already completed: this association must
	// come from persisted membership, not an awaiting-confirmation join.
	require.Nil(t, byID[standbyID].PendingEnrollmentId)
}

func TestListFleetNodes_HostMetadataAndEnrollment(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	_, err := h.db.Exec(`INSERT INTO "user" (id,user_id,username,password_hash) VALUES (2,'code-creator','creator','dummy')`)
	require.NoError(t, err)
	code, pendingID, _, err := h.enrollment.CreateCodeWithEnrollmentID(t.Context(), 2, h.orgID, time.Hour)
	require.NoError(t, err)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	node, _, err := h.enrollment.RegisterFleetNode(t.Context(), code, "node-details", pub, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = h.handler.ConfirmFleetNode(h.adminCtx(), connect.NewRequest(&pb.ConfirmFleetNodeRequest{FleetNodeId: node.ID, PendingEnrollmentId: pendingID}))
	require.NoError(t, err)

	_, err = h.db.Exec(`INSERT INTO pending_enrollment
 (code_hash,org_id,created_by,fleet_node_id,status,expires_at,consumed_at,confirmed_by,created_at)
 SELECT repeat('a',64),org_id,created_by,fleet_node_id,'CONFIRMED',expires_at,consumed_at - INTERVAL '1 day',2,created_at - INTERVAL '2 days'
 FROM pending_enrollment WHERE id=$1`, pendingID)
	require.NoError(t, err)

	req := &gatewaypb.UploadHeartbeatRequest{}
	require.NoError(t, protojson.Unmarshal([]byte(`{"sentAt":"2026-10-15T12:00:00Z","ipAddresses":["192.168.1.7","fd00::7"],"version":"v0.5.1-internal.3"}`), req))
	gw := gateway.NewHandler(h.enrollment, nil, nil, h.registry, nil)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{FleetNodeID: node.ID, OrgID: h.orgID})

	// Act
	_, err = gw.UploadHeartbeat(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	resp, err := h.handler.ListFleetNodes(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)

	// Assert
	require.Len(t, resp.Msg.FleetNodes, 1)
	summary := resp.Msg.FleetNodes[0]
	require.Equal(t, []string{"192.168.1.7", "fd00::7"}, summary.GetIpAddresses())
	require.Equal(t, "v0.5.1-internal.3", summary.GetVersion())
	require.Equal(t, "op", summary.GetEnrolledBy())
	require.Equal(t, node.CreatedAt.UTC(), summary.GetCreatedAt().AsTime())

	// Act
	redacted, err := h.handler.ListFleetNodes(h.ctxWithPerms(authz.PermMinerPair), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)

	// Assert
	data, err := protojson.Marshal(redacted.Msg.FleetNodes[0])
	require.NoError(t, err)
	require.NotContains(t, string(data), "ipAddresses")
	require.NotContains(t, string(data), "version")
	require.NotContains(t, string(data), "enrolledBy")

	// Arrange
	_, err = h.db.Exec(`UPDATE pending_enrollment SET confirmed_by=NULL WHERE id=$1`, pendingID)
	require.NoError(t, err)

	// Act
	_, err = gw.UploadHeartbeat(ctx, connect.NewRequest(&gatewaypb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)
	resp, err = h.handler.ListFleetNodes(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)

	// Assert
	summary = resp.Msg.FleetNodes[0]
	require.Empty(t, summary.GetEnrolledBy())
	require.Empty(t, summary.GetIpAddresses())
	require.Empty(t, summary.GetVersion())
}
