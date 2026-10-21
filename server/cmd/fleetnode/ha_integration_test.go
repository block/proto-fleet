package main

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	nodeauth "github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type haIntegrationTelemetry struct {
	nodeID int64
	calls  chan<- int64
}

func (f haIntegrationTelemetry) Fetch(_ context.Context, _ *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	f.calls <- f.nodeID
	return &telemetrypb.FleetNodeTelemetryResult{}, nil
}

// This crosses the actual database coordinator, gateway, bidirectional stream,
// node command decoder, cryptographic transfer, durable key installation, and startup reconnect.
// Only authentication and the final device telemetry fetch are test seams.
func TestHAIntegrationRealStreamsProvisionAndPromote(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres/Timescale")
	}
	database := dbtest.GetTestDB(t)
	_, err := database.Exec(`INSERT INTO organization(id,org_id,name) VALUES(1,'ha-node-streams','HA Node Streams')`)
	require.NoError(t, err)
	registry := control.NewRegistry()
	service := ha.NewService(database, registry)
	registry.SetHARouteResolver(func(ctx context.Context, ownerID int64) (control.RouteBinding, error) {
		route, err := service.Resolve(ctx, ownerID, 1)
		if err != nil {
			return control.RouteBinding{}, err
		}
		return route, nil
	})
	_, gatewayHandler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(gateway.NewHandler(nil, nil, nil, registry, nil).WithHA(service))
	// Authentication itself has its own integration tests. Supply each real
	// gateway stream the independently enrolled physical subject selected by its
	// test bearer token; all HA decisions still run through production code.
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		nodeID, parseErr := strconv.ParseInt(token, 10, 64)
		if parseErr != nil {
			http.Error(w, "invalid test node token", http.StatusUnauthorized)
			return
		}
		subject := &nodeauth.Subject{FleetNodeID: nodeID, OrgID: 1, SessionFingerprint: nodeauth.SessionFingerprint(token)}
		gatewayHandler.ServeHTTP(w, r.WithContext(authn.SetInfo(r.Context(), subject)))
	})
	server := testutil.NewH2CServer(t, authenticated)
	type localNode struct {
		state   *bootstrap.State
		path    string
		codec   *credentialCodec
		runtime *RunCmd
		cancel  context.CancelFunc
		done    chan error
	}
	nodes := make([]localNode, 3)
	calls := make(chan int64, 16)
	q := sqlc.New(database)
	for i := range nodes {
		identityPublic, identityPrivate, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		encryption, err := ecdh.X25519().GenerateKey(rand.Reader)
		require.NoError(t, err)
		enrolled, err := q.CreateFleetNode(t.Context(), sqlc.CreateFleetNodeParams{OrgID: 1, Name: fmt.Sprintf("stream-node-%d", i), IdentityPubkey: identityPublic, EncryptionPubkey: encryption.PublicKey().Bytes()})
		require.NoError(t, err)
		_, err = q.SetFleetNodeEnrollmentStatus(t.Context(), sqlc.SetFleetNodeEnrollmentStatusParams{ID: enrolled.ID, OrgID: 1, EnrollmentStatus: "CONFIRMED"})
		require.NoError(t, err)
		state := &bootstrap.State{FleetNodeID: enrolled.ID, ServerURL: server.URL, IdentityPublicKeyHex: hex.EncodeToString(identityPublic), IdentityPrivateKeyHex: hex.EncodeToString(identityPrivate), EncryptionPublicKeyHex: hex.EncodeToString(encryption.PublicKey().Bytes()), EncryptionPrivateKeyHex: hex.EncodeToString(encryption.Bytes())}
		path := bootstrap.StatePath(t.TempDir())
		codec, err := ensureCredentialCodec(path, state)
		require.NoError(t, err)
		nodes[i] = localNode{state: state, path: path, codec: codec, runtime: &RunCmd{discoverer: &stubDiscoverer{}, credentials: codec, haState: state, haStatePath: path, telemetry: haIntegrationTelemetry{nodeID: enrolled.ID, calls: calls}}}
	}
	t.Cleanup(func() {
		for i := range nodes {
			if nodes[i].cancel != nil {
				nodes[i].cancel()
			}
		}
		for i := range nodes {
			if nodes[i].done != nil {
				select {
				case err := <-nodes[i].done:
					require.NoError(t, err)
				case <-time.After(3 * time.Second):
					t.Errorf("node %d did not stop", i)
				}
			}
		}
	})
	start := func(index int) {
		node := &nodes[index]
		client, err := bootstrap.NewAuthenticatedGatewayClient(server.URL, func() string { return strconv.FormatInt(node.state.FleetNodeID, 10) })
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		node.cancel = cancel
		node.done = make(chan error, 1)
		go func() { node.done <- node.runtime.runControlLoop(ctx, client, node.state, discardLogger(t)) }()
	}
	ownerID := nodes[0].state.FleetNodeID
	sendTelemetry := func() (*pb.ControlAck, error) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		return registry.SendCommand(ctx, ownerID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: fmt.Sprintf("telemetry-%d", time.Now().UnixNano()), Payload: telemetryCmd(t, validTelemetryRequest())})
	}
	start(0)
	require.Eventually(t, func() bool { ack, err := sendTelemetry(); return err == nil && ack.GetSucceeded() }, 5*time.Second, 20*time.Millisecond, "standalone source must accept a real ordinary command")
	require.Equal(t, ownerID, <-calls)
	sourceCredentials, err := nodes[0].codec.Seal(sdk.SecretBundle{Kind: sdk.UsernamePassword{Username: "miner-user", Password: "persisted-password"}})
	require.NoError(t, err)
	originalIdentity := make([]string, 3)
	originalEncryption := make([]string, 3)
	for i := range nodes {
		originalIdentity[i] = nodes[i].state.IdentityPrivateKeyHex
		originalEncryption[i] = nodes[i].state.EncryptionPrivateKeyHex
	}
	confirm := func(index int) {
		require.NoError(t, sqlstores.NewSQLTransactor(database).RunInTx(t.Context(), func(ctx context.Context) error {
			return service.Confirm(ctx, nodes[index].state.FleetNodeID, 1, ownerID)
		}))
	}
	waitReady := func(index int) {
		require.Eventually(t, func() bool {
			var ready bool
			err := database.QueryRow(`SELECT ready FROM fleet_node_ha_member WHERE node_id=$1`, nodes[index].state.FleetNodeID).Scan(&ready)
			return err == nil && ready
		}, 20*time.Second, 20*time.Millisecond, "actual export/import stream chain must finish")
		persisted, exists, err := bootstrap.LoadState(nodes[index].path)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, hex.EncodeToString(nodes[0].codec.keySnapshot()), persisted.CredentialKeyHex)
		require.Equal(t, originalIdentity[index], persisted.IdentityPrivateKeyHex)
		require.Equal(t, originalEncryption[index], persisted.EncryptionPrivateKeyHex)
		require.Equal(t, ownerID, persisted.HACredentialOwnerNodeID)
		reloaded, err := ensureCredentialCodec(nodes[index].path, persisted)
		require.NoError(t, err)
		opened, err := reloaded.Open(sourceCredentials)
		require.NoError(t, err)
		require.Equal(t, sdk.UsernamePassword{Username: "miner-user", Password: "persisted-password"}, opened.Kind)

	}
	confirm(1)
	start(1)
	waitReady(1)
	require.Eventually(t, func() bool {
		route, err := service.Resolve(t.Context(), ownerID, 1)
		return err == nil && route.PhysicalNodeID == ownerID && route.Epoch > 0
	}, 15*time.Second, 20*time.Millisecond, "owner lease becomes routable after actual node epoch acknowledgement")
	confirm(2)
	start(2)
	waitReady(2)
	require.NotEqual(t, originalIdentity[0], originalIdentity[1])
	require.NotEqual(t, originalIdentity[1], originalIdentity[2])
	require.NotEqual(t, originalEncryption[0], originalEncryption[1])
	require.NotEqual(t, originalEncryption[1], originalEncryption[2])
	ack, err := sendTelemetry()
	require.NoError(t, err)
	require.True(t, ack.GetSucceeded())
	require.Equal(t, ownerID, <-calls)
	// Stop the old process before advancing the persisted expiry. This preserves
	// fencing without making the integration test wait an entire 30-second TTL.
	nodes[0].cancel()
	select {
	case err := <-nodes[0].done:
		require.NoError(t, err)
		nodes[0].done = nil
	case <-time.After(3 * time.Second):
		t.Fatal("old process did not stop")
	}
	require.Eventually(t, func() bool { _, _, connected := registry.HAConnection(ownerID); return !connected }, time.Second, 10*time.Millisecond)
	_, err = database.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE owner_node_id=$1`, ownerID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		route, err := service.Resolve(t.Context(), ownerID, 1)
		return err == nil && route.PhysicalNodeID == nodes[1].state.FleetNodeID
	}, 15*time.Second, 20*time.Millisecond, "oldest prepared standby must receive and acknowledge promotion")
	ack, err = sendTelemetry()
	require.NoError(t, err)
	require.True(t, ack.GetSucceeded())
	require.Equal(t, nodes[1].state.FleetNodeID, <-calls)
}
