package gateway_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/testutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newHeartbeatHandler(t *testing.T) (*gateway.Handler, *sql.DB, int64) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name, miner_auth_private_key) VALUES (1, 'test-org', 'Test Org', 'dummy-key') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "user" (id, user_id, username, password_hash) VALUES (1, 'test-user', 'op', 'dummy') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(db)
	apiKeySvc := apikey.NewService(apiKeyStore, nil)
	transactor := sqlstores.NewSQLTransactor(db)
	enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(db)
	enrollmentSvc := enrollment.NewService(enrollmentStore, apiKeySvc, transactor, nil)
	authStore := sqlstores.NewSQLFleetNodeAuthStore(db)
	authSvc := auth.NewService(authStore, enrollmentStore, apiKeySvc)

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, err := enrollmentSvc.CreateCode(t.Context(), 1, 1, time.Hour)
	require.NoError(t, err)
	agent, _, err := enrollmentSvc.RegisterFleetNode(t.Context(), code, "agent-heartbeat", pubKey, signing)
	require.NoError(t, err)

	pairingStore := sqlstores.NewSQLFleetNodePairingStore(db)
	pairingSvc := pairing.NewService(pairingStore, enrollmentStore, transactor)

	return gateway.NewHandler(enrollmentSvc, authSvc, pairingSvc, control.NewRegistry()), db, agent.ID
}

func TestUploadHeartbeat_AdvancesLastSeen(t *testing.T) {
	// Arrange
	handler, db, fleetNodeID := newHeartbeatHandler(t)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{
		FleetNodeID: fleetNodeID,
		OrgID:       1,
		Name:        "agent-heartbeat",
	})
	req := connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()})

	// Act
	resp, err := handler.UploadHeartbeat(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetReceivedAt())
	assert.WithinDuration(t, time.Now(), resp.Msg.GetReceivedAt().AsTime(), 2*time.Second)
	var lastSeen sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT last_seen_at FROM fleet_node WHERE id = $1`, fleetNodeID).Scan(&lastSeen))
	require.True(t, lastSeen.Valid)
	assert.WithinDuration(t, time.Now(), lastSeen.Time, 2*time.Second)
}

func TestUploadHeartbeat_RejectsMissingSubject(t *testing.T) {
	// Arrange
	handler, _, _ := newHeartbeatHandler(t)
	req := connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()})

	// Act
	_, err := handler.UploadHeartbeat(t.Context(), req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fleet node subject")
}

func TestUploadHeartbeat_NotFoundForUnknownFleetNode(t *testing.T) {
	// Arrange
	handler, _, _ := newHeartbeatHandler(t)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{
		FleetNodeID: 99999,
		OrgID:       1,
		Name:        "ghost",
	})
	req := connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()})

	// Act
	_, err := handler.UploadHeartbeat(ctx, req)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}
