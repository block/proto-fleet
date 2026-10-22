package ha

import (
	"bytes"
	"context"
	"testing"
	"time"

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
	registry.BeginHAActivation(node.ID)
	activated, err := service.activate(t.Context(), node.ID, 1)
	require.NoError(t, err)
	require.True(t, activated)
	select {
	case <-stream.Done:
		t.Fatal("a delayed setup worker disconnected the already-enabled source")
	default:
	}
	_, err = registry.Send(t.Context(), node.ID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "before-grant"}, nil, control.ReportKindDiscovery, nil)
	require.Error(t, err, "a delayed worker must preserve an activation gate awaiting its first grant")
	require.True(t, stream.SetHAConnection(1, &pb.ControlHello{HaCapable: true, InstanceId: "source"}, "source-session"))
	route := control.RouteBinding{OwnerNodeID: node.ID, PhysicalNodeID: node.ID, Epoch: 1, InstanceID: "source", ControlSessionID: "source-session", ExpiresAt: time.Now().Add(time.Minute)}
	registry.SetHARoute(route)
	registry.SetHARouteResolver(func(context.Context, int64) (control.RouteBinding, error) { return route, nil })
	activated, err = service.activate(t.Context(), node.ID, 1)
	require.NoError(t, err)
	require.True(t, activated)
	session, err := registry.Send(t.Context(), node.ID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "after-activation"}, nil, control.ReportKindDiscovery, nil)
	require.NoError(t, err, "a delayed setup worker must preserve acknowledged dispatch")
	session.Close()

	// A separate unprepared group exercises the precommit failure cleanup.
	unprepared, err := queries.CreateFleetNode(t.Context(), sqlc.CreateFleetNodeParams{OrgID: 1, Name: "unprepared", IdentityPubkey: bytes.Repeat([]byte{4}, 32), EncryptionPubkey: bytes.Repeat([]byte{5}, 32)})
	require.NoError(t, err)
	require.NoError(t, queries.CreateFleetNodeHAGroup(t.Context(), sqlc.CreateFleetNodeHAGroupParams{OwnerNodeID: unprepared.ID, OrgID: 1, CredentialKeyFingerprint: bytes.Repeat([]byte{6}, 32)}))
	standalone := registry.Register(unprepared.ID)
	defer standalone.Unregister()
	activated, err = service.activate(t.Context(), unprepared.ID, 1)
	require.Error(t, err, "a group without a ready standby cannot enable")
	require.False(t, activated)
	registry.SetHARouteResolver(nil)
	session, err = registry.Send(t.Context(), unprepared.ID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "after-failure"}, nil, control.ReportKindDiscovery, nil)
	require.NoError(t, err, "failed activation must reopen standalone dispatch")
	session.Close()
}
