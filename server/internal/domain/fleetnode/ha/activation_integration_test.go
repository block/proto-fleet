package ha

import (
	"bytes"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestAlreadyEnabledGroupDoesNotRestartActivation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres/Timescale")
	}
	conn := dbtest.GetTestDB(t)
	_, err := conn.Exec(`INSERT INTO organization(id,org_id,name) VALUES(1,'activation-test','Activation')`)
	require.NoError(t, err)
	queries := sqlc.New(conn)
	node, err := queries.CreateFleetNode(t.Context(), sqlc.CreateFleetNodeParams{OrgID: 1, Name: "source", IdentityPubkey: bytes.Repeat([]byte{1}, 32), EncryptionPubkey: bytes.Repeat([]byte{2}, 32)})
	require.NoError(t, err)
	require.NoError(t, queries.CreateFleetNodeHAGroup(t.Context(), sqlc.CreateFleetNodeHAGroupParams{OwnerNodeID: node.ID, OrgID: 1, CredentialKeyFingerprint: bytes.Repeat([]byte{3}, 32)}))
	_, err = conn.Exec(`UPDATE fleet_node_ha_group SET enabled=true WHERE owner_node_id=$1`, node.ID)
	require.NoError(t, err)
	registry := control.NewRegistry()
	stream := registry.Register(node.ID)
	defer stream.Unregister()
	service := NewService(conn, registry)
	activated, err := service.activate(t.Context(), node.ID, 1)
	require.NoError(t, err)
	require.True(t, activated)
	select {
	case <-stream.Done:
		t.Fatal("a delayed setup worker disconnected the already-enabled source")
	default:
	}
	session, err := registry.Send(t.Context(), node.ID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "after-activation"}, nil, control.ReportKindDiscovery, nil)
	require.NoError(t, err, "a delayed setup worker must not leave dispatch paused")
	session.Close()
}
