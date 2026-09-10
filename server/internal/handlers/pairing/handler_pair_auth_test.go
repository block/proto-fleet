package pairing

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

func TestPairRequiresMinerPairBeforeRouting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		perms []string
	}{
		{name: "no permissions"},
		{name: "fleet node manage alone", perms: []string{authz.PermFleetnodeManage}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: any target lookup or local pairing would hit a nil store.
			h := &Handler{
				discovery:        &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}},
				fleetNodePairing: fleetnodepairing.NewService(nil, nil, nil),
			}
			req := connect.NewRequest(&pb.PairRequest{DeviceSelector: includeDevicesSelector([]string{"miner-1"})})

			// Act
			resp, err := h.Pair(ctxWithPerms(tc.perms...), req)

			// Assert
			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err))
			assert.Nil(t, resp)
		})
	}
}

func TestPairWithMinerPairOnlyRoutesWithinOrganization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database-backed pairing authorization test in short mode")
	}

	// Arrange: two connected, confirmed nodes have the same miner identifier in
	// different organizations. Real SQL stores must select only the caller's row.
	db := dbtest.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name)
		VALUES (1, 'pair-org-1', 'Pair Org 1'), (2, 'pair-org-2', 'Pair Org 2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO fleet_node
		(id, org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
		VALUES (7, 1, 'node-1', 'key-1', $1, 'CONFIRMED'),
		       (8, 2, 'node-2', 'key-2', $1, 'CONFIRMED')`,
		[]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO discovered_device
		(org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		VALUES (1, 'miner-1', '10.0.0.10', '4028', 'tcp', 'virtual', TRUE, 7),
		       (2, 'miner-1', '10.0.0.20', '4028', 'tcp', 'virtual', TRUE, 8)`)
	require.NoError(t, err)
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(7, "session-1", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1)
	require.NoError(t, err)
	defer stream.Unregister()
	otherStream, err := registry.RegisterAuthenticated(8, "session-2", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1)
	require.NoError(t, err)
	defer otherStream.Unregister()
	enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(db)
	transactor := sqlstores.NewSQLTransactor(db)
	enrollmentSvc := enrollment.NewService(enrollmentStore, nil, transactor, nil)
	nodePairingSvc := fleetnodepairing.NewService(sqlstores.NewSQLFleetNodePairingStore(db), enrollmentStore, transactor).
		WithProvisioning(nil, nil, registry)
	// No cloud service: this selection must be fulfilled entirely by the node.
	h := NewHandler(nil, discovery.NewService(registry, enrollmentSvc), nodePairingSvc)
	type pairResult struct {
		resp *connect.Response[pb.PairResponse]
		err  error
	}
	done := make(chan pairResult, 1)

	// Act: exercise the normal Pair handler with miner:pair, not fleetnode:manage.
	go func() {
		resp, pairErr := h.Pair(ctxWithPerms(authz.PermMinerPair), connect.NewRequest(&pb.PairRequest{
			DeviceSelector: includeDevicesSelector([]string{"miner-1"}),
		}))
		done <- pairResult{resp: resp, err: pairErr}
	}()

	// Assert dispatch and persistence metadata stay scoped to the caller.
	select {
	case cmd := <-stream.Outgoing:
		var envelope gatewaypb.AgentCommand
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &envelope))
		targets := envelope.GetPair().GetTargets()
		require.Len(t, targets, 1)
		assert.Equal(t, "miner-1", targets[0].GetDeviceIdentifier())
		assert.Equal(t, "10.0.0.10", targets[0].GetIpAddress())
		results, meta, admitErr := registry.AdmitAndScopePairResults(7, cmd.GetCommandId(), []*gatewaypb.FleetNodePairResult{{
			DeviceIdentifier: "miner-1", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED,
		}})
		require.NoError(t, admitErr)
		require.Len(t, results, 1)
		assert.Equal(t, int64(1), meta.OrgID)
		require.NotNil(t, meta.AssignedBy)
		assert.Equal(t, int64(1), *meta.AssignedBy)
		registry.PublishPairResults(7, cmd.GetCommandId(), results)
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK})
	case result := <-done:
		t.Fatalf("Pair returned before dispatch: %v", result.err)
	case cmd := <-otherStream.Outgoing:
		t.Fatalf("another organization's node received command %q", cmd.GetCommandId())
	case <-time.After(5 * time.Second):
		t.Fatal("Pair did not dispatch a command")
	}
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.NotNil(t, result.resp)
		assert.Empty(t, result.resp.Msg.GetFailedDeviceIds())
	case <-time.After(5 * time.Second):
		t.Fatal("Pair did not finish after acknowledgement")
	}
	select {
	case cmd := <-otherStream.Outgoing:
		t.Fatalf("another organization's node received command %q", cmd.GetCommandId())
	default:
	}
}
