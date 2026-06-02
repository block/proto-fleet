package enrollment_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	fleetnodeauth "github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	fleetnodeenrollment "github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func setupEnrollmentTest(t *testing.T) (*sql.DB, int64, int64, *fleetnodeenrollment.Service, *fleetnodeauth.Service) {
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
	enrollmentSvc := fleetnodeenrollment.NewService(enrollmentStore, apiKeySvc, transactor, nil)

	authStore := sqlstores.NewSQLFleetNodeAuthStore(db)
	authSvc := fleetnodeauth.NewService(authStore, enrollmentStore, apiKeySvc)

	return db, 1, 1, enrollmentSvc, authSvc
}

// TestEnrollmentHappyPath exercises the full server-side flow: code creation,
// agent registration, operator confirmation (issues api_key), handshake to
// session_token, session resolution.
func TestEnrollmentHappyPath(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, auth := setupEnrollmentTest(t)
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signingPubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	// Act 1: operator creates code
	code, expiresAt, err := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, code)
	require.True(t, expiresAt.After(time.Now()))

	// Act 2: agent registers with the code
	agent, _, err := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signingPubKey)
	require.NoError(t, err)
	require.Equal(t, fleetnodeenrollment.FleetNodeStatusPending, agent.EnrollmentStatus)
	require.Equal(t, orgID, agent.OrgID)

	// Act 3: operator confirms; api_key is issued
	apiKeyPlaintext, _, err := enrollment.Confirm(ctx, agent.ID, orgID)
	require.NoError(t, err)
	require.NotEmpty(t, apiKeyPlaintext)

	// Act 4: agent runs the handshake
	challenge, _, err := auth.BeginHandshake(ctx, apiKeyPlaintext, pubKey)
	require.NoError(t, err)
	signature := ed25519.Sign(privKey, challenge)
	sessionToken, _, err := auth.CompleteHandshake(ctx, challenge, signature)
	require.NoError(t, err)
	require.NotEmpty(t, sessionToken)

	// Act 5: session resolves to the same agent
	resolved, err := auth.ResolveSession(ctx, sessionToken)
	require.NoError(t, err)

	// Assert
	require.Equal(t, agent.ID, resolved.FleetNodeID)
	require.Equal(t, orgID, resolved.OrgID)
	require.Equal(t, "agent-1", resolved.Name)
}

func TestRegisterRejectsReplay(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, err := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	require.NoError(t, err)

	// Act
	_, _, err = enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	require.NoError(t, err)
	_, _, err2 := enrollment.RegisterFleetNode(ctx, code, "agent-2", pubKey, signing)

	// Assert
	require.Error(t, err2, "second register with the same code must fail")
}

func TestRegisterRejectsExpiredCode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, err := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE pending_enrollment SET expires_at = $1`, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, err)

	// Act
	_, _, err = enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)

	// Assert
	require.Error(t, err)
}

func TestCompleteHandshakeRejectsReplayedChallenge(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, auth := setupEnrollmentTest(t)
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, _ := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	apiKeyPlaintext, _, _ := enrollment.Confirm(ctx, agent.ID, orgID)
	challenge, _, err := auth.BeginHandshake(ctx, apiKeyPlaintext, pubKey)
	require.NoError(t, err)
	signature := ed25519.Sign(privKey, challenge)
	_, _, err = auth.CompleteHandshake(ctx, challenge, signature)
	require.NoError(t, err)

	// Act
	_, _, err2 := auth.CompleteHandshake(ctx, challenge, signature)

	// Assert
	require.Error(t, err2, "second CompleteHandshake with the same challenge must fail")
}

func TestBeginHandshakeRejectsMismatchedIdentityPubkey(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, auth := setupEnrollmentTest(t)
	enrolledPubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, _ := enrollment.RegisterFleetNode(ctx, code, "agent-1", enrolledPubKey, signing)
	apiKeyPlaintext, _, _ := enrollment.Confirm(ctx, agent.ID, orgID)
	differentPubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	// Act
	_, _, err := auth.BeginHandshake(ctx, apiKeyPlaintext, differentPubKey)

	// Assert
	require.Error(t, err)
}

func TestSweepExpiredEnrollments(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	_, _, err := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE pending_enrollment SET expires_at = $1`, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, err)

	// Act
	swept, err := enrollment.SweepExpired(ctx)

	// Assert
	require.NoError(t, err)
	require.Equal(t, int64(1), swept)
}

func TestConfirmRejectsExpiredEnrollment(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, err := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	require.NoError(t, err)
	agent, _, err := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE pending_enrollment SET expires_at = $1`, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, err)

	// Act
	_, _, err = enrollment.Confirm(ctx, agent.ID, orgID)

	// Assert
	require.Error(t, err)
}

func TestRevokeAgentLocksOutHandshake(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, auth := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, _ := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	apiKeyPlaintext, _, _ := enrollment.Confirm(ctx, agent.ID, orgID)

	// Act
	err := enrollment.RevokeFleetNode(ctx, agent.ID, orgID)

	// Assert
	require.NoError(t, err)
	_, _, handshakeErr := auth.BeginHandshake(ctx, apiKeyPlaintext, pubKey)
	require.Error(t, handshakeErr, "BeginHandshake must fail with revoked api_key")
}

func TestConcurrentBeginHandshakesYieldOneChallenge(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, userID, orgID, enrollment, auth := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, _ := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	apiKeyPlaintext, _, _ := enrollment.Confirm(ctx, agent.ID, orgID)

	// Act
	const callers = 10
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			_, _, _ = auth.BeginHandshake(ctx, apiKeyPlaintext, pubKey)
		}()
	}
	wg.Wait()

	// Assert
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fleet_node_auth_challenge WHERE fleet_node_id = $1`, agent.ID).Scan(&count))
	require.Equal(t, 1, count, "concurrent BeginHandshakes for one fleet node must leave exactly one challenge row")
}

func TestRevokeBeforeConfirmCannotBeResurrected(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, err := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	require.NoError(t, err)
	require.NoError(t, enrollment.RevokeFleetNode(ctx, agent.ID, orgID))

	// Act
	_, _, confirmErr := enrollment.Confirm(ctx, agent.ID, orgID)

	// Assert
	require.Error(t, confirmErr, "Confirm must reject a revoked agent")
}

func TestConcurrentCompleteHandshakesYieldOneSession(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, userID, orgID, enrollment, auth := setupEnrollmentTest(t)
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, _ := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	apiKeyPlaintext, _, _ := enrollment.Confirm(ctx, agent.ID, orgID)
	// Pre-mint challenges so multiple CompleteHandshake calls can race against
	// the latest one. Each BeginHandshake replaces the prior; the test then
	// races as many CompleteHandshakes as we have stored signatures.
	type signed struct {
		challenge []byte
		sig       []byte
	}
	const callers = 5
	signedChallenges := make([]signed, 0, callers)
	for range callers {
		ch, _, err := auth.BeginHandshake(ctx, apiKeyPlaintext, pubKey)
		require.NoError(t, err)
		signedChallenges = append(signedChallenges, signed{ch, ed25519.Sign(privKey, ch)})
	}

	// Act
	var wg sync.WaitGroup
	wg.Add(len(signedChallenges))
	for _, sc := range signedChallenges {
		go func(sc signed) {
			defer wg.Done()
			_, _, _ = auth.CompleteHandshake(ctx, sc.challenge, sc.sig)
		}(sc)
	}
	wg.Wait()

	// Assert
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM fleet_node_session WHERE fleet_node_id = $1`, agent.ID).Scan(&count))
	require.Equal(t, 1, count, "concurrent CompleteHandshakes for one fleet node must leave exactly one session row")
}

func TestCompleteHandshakeRaceWithRevokeReturnsUnauthenticated(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, auth := setupEnrollmentTest(t)
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, _ := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	apiKeyPlaintext, _, _ := enrollment.Confirm(ctx, agent.ID, orgID)
	challenge, _, err := auth.BeginHandshake(ctx, apiKeyPlaintext, pubKey)
	require.NoError(t, err)
	require.NoError(t, enrollment.RevokeFleetNode(ctx, agent.ID, orgID))

	// Act
	_, _, completeErr := auth.CompleteHandshake(ctx, challenge, ed25519.Sign(privKey, challenge))

	// Assert
	require.Error(t, completeErr)
	require.True(t, fleeterror.IsAuthenticationError(completeErr), "race with revoke must surface as Unauthenticated, not internal")
}

func TestConfirmRejectsAgentRevokedMidConfirm(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent, _, _ := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	// Simulate a concurrent RevokeFleetNode that lands between Confirm's reads
	// and its SetFleetNodeEnrollmentStatus update.
	_, err := db.Exec(`UPDATE fleet_node SET deleted_at = $1 WHERE id = $2`, time.Now().UTC(), agent.ID)
	require.NoError(t, err)

	// Act
	_, _, confirmErr := enrollment.Confirm(ctx, agent.ID, orgID)

	// Assert
	require.Error(t, confirmErr, "Confirm must reject when the agent is soft-deleted")
	var apiKeyCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM api_key WHERE fleet_node_id = $1`, agent.ID).Scan(&apiKeyCount))
	require.Equal(t, 0, apiKeyCount, "no api_key must be issued for a revoked/deleted fleet node")
}

func TestRegisterAgentDuplicateIdentityIsPrecondition(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code1, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	code2, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	_, _, err := enrollment.RegisterFleetNode(ctx, code1, "agent-1", pubKey, signing)
	require.NoError(t, err)

	// Act
	_, _, err2 := enrollment.RegisterFleetNode(ctx, code2, "agent-2", pubKey, signing)

	// Assert
	require.Error(t, err2)
	require.True(t, fleeterror.IsFailedPreconditionError(err2), "duplicate identity_pubkey must surface as FailedPrecondition, not internal")
}

func TestRevokeAgentFreesIdentityForReenrollment(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code1, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent1, _, err := enrollment.RegisterFleetNode(ctx, code1, "agent-1", pubKey, signing)
	require.NoError(t, err)
	require.NoError(t, enrollment.RevokeFleetNode(ctx, agent1.ID, orgID))

	// Act
	code2, _, _ := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	agent2, _, err := enrollment.RegisterFleetNode(ctx, code2, "agent-1", pubKey, signing)

	// Assert
	require.NoError(t, err, "re-enrollment with the same identity_pubkey + name must succeed after revoke soft-deletes the prior agent")
	require.NotEqual(t, agent1.ID, agent2.ID)
}

func TestConfirmRejectsBeforeRegister(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, _, orgID, enrollment, _ := setupEnrollmentTest(t)

	// Act
	_, _, err := enrollment.Confirm(ctx, 99999, orgID)

	// Assert
	require.Error(t, err)
	require.True(t, fleeterror.IsNotFoundError(err))
}

func TestListAgentsSurfacesAwaitingConfirmation(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, err := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	require.NoError(t, err)
	agent, _, err := enrollment.RegisterFleetNode(ctx, code, "agent-1", pubKey, signing)
	require.NoError(t, err)

	// Act
	listings, err := enrollment.ListFleetNodes(ctx, orgID)

	// Assert
	require.NoError(t, err)
	var found *fleetnodeenrollment.FleetNodeListing
	for i := range listings {
		if listings[i].ID == agent.ID {
			found = &listings[i]
			break
		}
	}
	require.NotNil(t, found, "registered agent must appear in ListFleetNodes")
	require.Equal(t, fleetnodeenrollment.FleetNodeStatusPending, found.EnrollmentStatus)
	require.Equal(t, fleetnodeenrollment.StatusAwaitingConfirmation, found.PendingEnrollmentStatus)
}

func TestUpdateLastSeenAdvancesTimestamp(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, userID, orgID, enrollment, _ := setupEnrollmentTest(t)
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, err := enrollment.CreateCode(ctx, userID, orgID, time.Hour)
	require.NoError(t, err)
	agent, _, err := enrollment.RegisterFleetNode(ctx, code, "agent-heartbeat", pubKey, signing)
	require.NoError(t, err)
	heartbeat := time.Now().UTC().Truncate(time.Second)

	// Act
	err = enrollment.UpdateLastSeen(ctx, agent.ID, orgID, heartbeat)

	// Assert
	require.NoError(t, err)
	var lastSeen sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT last_seen_at FROM fleet_node WHERE id = $1`, agent.ID).Scan(&lastSeen))
	require.True(t, lastSeen.Valid, "last_seen_at must be set after heartbeat")
	require.WithinDuration(t, heartbeat, lastSeen.Time, time.Second)
}

func TestUpdateLastSeenReturnsNotFoundForDeletedFleetNode(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, _, orgID, enrollment, _ := setupEnrollmentTest(t)

	// Act
	err := enrollment.UpdateLastSeen(ctx, 99999, orgID, time.Now().UTC())

	// Assert
	require.Error(t, err)
	require.True(t, fleeterror.IsNotFoundError(err))
}
