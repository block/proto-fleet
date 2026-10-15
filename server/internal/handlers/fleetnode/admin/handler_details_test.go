package admin_test

import (
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
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
)

func TestListFleetNodes_HostMetadataAndEnrollment(t *testing.T) {
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
	_, err = gw.UploadHeartbeat(ctx, connect.NewRequest(req))
	require.NoError(t, err)

	resp, err := h.handler.ListFleetNodes(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.FleetNodes, 1)
	summary := resp.Msg.FleetNodes[0]
	require.Equal(t, []string{"192.168.1.7", "fd00::7"}, summary.GetIpAddresses())
	require.Equal(t, "v0.5.1-internal.3", summary.GetVersion())
	require.Equal(t, "op", summary.GetEnrolledBy())
	require.Equal(t, node.CreatedAt.UTC(), summary.GetCreatedAt().AsTime())

	redacted, err := h.handler.ListFleetNodes(h.ctxWithPerms(authz.PermMinerPair), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)
	data, err := protojson.Marshal(redacted.Msg.FleetNodes[0])
	require.NoError(t, err)
	require.NotContains(t, string(data), "ipAddresses")
	require.NotContains(t, string(data), "version")
	require.NotContains(t, string(data), "enrolledBy")

	_, err = h.db.Exec(`UPDATE pending_enrollment SET confirmed_by=NULL WHERE id=$1`, pendingID)
	require.NoError(t, err)
	_, err = gw.UploadHeartbeat(ctx, connect.NewRequest(&gatewaypb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)
	resp, err = h.handler.ListFleetNodes(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodesRequest{}))
	require.NoError(t, err)
	summary = resp.Msg.FleetNodes[0]
	require.Empty(t, summary.GetEnrolledBy())
	require.Empty(t, summary.GetIpAddresses())
	require.Empty(t, summary.GetVersion())
}
