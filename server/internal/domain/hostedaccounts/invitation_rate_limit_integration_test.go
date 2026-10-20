package hostedaccounts

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

func seedInvitationAttempts(t *testing.T, conn *sql.DB, count int, exposure int64) {
	t.Helper()
	_, err := conn.ExecContext(t.Context(), `INSERT INTO hosted_invitation_rate_limit_attempt(exposure) SELECT $2 FROM generate_series(1,$1)`, count, exposure)
	require.NoError(t, err)
}

func TestInvitationRateLimitCoversAllPathsAndSurvivesServiceReplacement(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	seedInvitationAttempts(t, conn, 999, 1)
	_, err := svc.Preview(t.Context(), "AAAA-AAAA")
	require.True(t, fleeterror.IsFailedPreconditionError(err), "unknown code spends the last attempt")
	other := NewInvitationService(svc.accounts, svc.sessions)
	other.hashPassword = func([]byte) ([]byte, error) { t.Fatal("throttled registration reached bcrypt"); return nil, nil }
	_, err = other.Preview(t.Context(), token)
	require.True(t, fleeterror.IsResourceExhaustedError(err))
	_, err = other.Accept(t.Context(), token, 0, 0)
	require.True(t, fleeterror.IsResourceExhaustedError(err), "authenticated redemption cannot bypass public preview budget")
	_, err = other.Register(t.Context(), token, "throttled-user", "password123", "", "192.0.2.33")
	require.True(t, fleeterror.IsResourceExhaustedError(err))
	requireNoRegistration(t, conn, "throttled-user")
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit_attempt SET attempted_at=statement_timestamp()-interval '1 hour'`)
	require.NoError(t, err)
	_, err = other.Preview(t.Context(), token)
	require.NoError(t, err)
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit_attempt`).Scan(&count))
	require.Equal(t, 1, count, "expired attempts are pruned; table size is bounded")
}

func TestInvitationRateLimitWeightsActiveInvitations(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	_, err := conn.ExecContext(t.Context(), `
INSERT INTO hosted_account_invitation(invitation_id,organization_id,account_id,inviter_id,authority_fingerprint,token_hash,role,expires_at)
SELECT gen_random_uuid()::text,organization_id,account_id,inviter_id,authority_fingerprint,
       'fixture-' || n::text,role,expires_at
FROM hosted_account_invitation CROSS JOIN generate_series(1,99) n`)
	require.NoError(t, err)
	seedInvitationAttempts(t, conn, 1, 99900)
	_, err = svc.Preview(t.Context(), "AAAA-AAAA")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	var exposure int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT sum(exposure) FROM hosted_invitation_rate_limit_attempt`).Scan(&exposure))
	require.Equal(t, int64(100000), exposure)
	_, err = svc.Preview(t.Context(), token)
	require.True(t, fleeterror.IsResourceExhaustedError(err), "target population exhausts exposure before the request-count cap")
}

func TestInvitationRateLimitUsesRollingHourAndSerializesConcurrentReplicas(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	seedInvitationAttempts(t, conn, 995, 1)
	_, err := conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit_attempt SET attempted_at=statement_timestamp()-interval '59 minutes'`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	start := make(chan struct{})
	for range 12 {
		other := NewInvitationService(svc.accounts, svc.sessions)
		wg.Go(func() {
			<-start
			_, err := other.Preview(t.Context(), "AAAA-AAAA")
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	admitted := 0
	for err := range results {
		if fleeterror.IsFailedPreconditionError(err) {
			admitted++
		} else {
			require.True(t, fleeterror.IsResourceExhaustedError(err), "%v", err)
		}
	}
	require.Equal(t, 5, admitted, "replica-local state and hour boundaries cannot multiply the shared budget")
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit_attempt`).Scan(&count))
	require.Equal(t, 1000, count)
}

func TestInvitationRateLimitExcludesExpiredRevokedAndAcceptedTargets(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	_, err := conn.ExecContext(t.Context(), `
INSERT INTO hosted_account_invitation(invitation_id,organization_id,account_id,inviter_id,authority_fingerprint,token_hash,role,created_at,expires_at,revoked_at,accepted_at,accepted_user_id)
SELECT gen_random_uuid()::text,organization_id,account_id,inviter_id,authority_fingerprint,
       'unavailable-' || n::text,role,statement_timestamp()-interval '2 hours',
       CASE WHEN n=1 THEN statement_timestamp()-interval '1 hour' ELSE expires_at END,
       CASE WHEN n=2 THEN statement_timestamp() END,
       CASE WHEN n=3 THEN statement_timestamp() END,
       CASE WHEN n=3 THEN inviter_id END
FROM hosted_account_invitation CROSS JOIN generate_series(1,3) n`)
	require.NoError(t, err)
	_, err = svc.Preview(t.Context(), token)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET revoked_at=statement_timestamp()`)
	require.NoError(t, err)
	_, err = svc.Preview(t.Context(), "AAAA-AAAA")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	var exposure int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT sum(exposure) FROM hosted_invitation_rate_limit_attempt`).Scan(&exposure))
	require.Equal(t, int64(2), exposure, "one live target costs one; zero live targets still cost one")
}

func TestInvitationRateLimitChargesRejectedRedemptionsAndSkipsMalformedCodes(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	_, err := svc.Preview(t.Context(), "malformed")
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit_attempt`).Scan(&count))
	require.Zero(t, count, "syntactically invalid values cannot reach a digest lookup")
	_, err = svc.Accept(t.Context(), token, 0, 0)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	_, err = svc.Register(t.Context(), token, "registration-host", "password123", "", "")
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit_attempt`).Scan(&count))
	require.Equal(t, 2, count, "wrong organization and occupied username do not refund successful lookups")
}

func TestInvitationRateLimitFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	_, err := conn.ExecContext(t.Context(), `DROP TABLE hosted_invitation_rate_limit`)
	require.NoError(t, err)
	_, err = svc.Preview(t.Context(), token)
	require.Error(t, err)
	_, err = svc.Accept(t.Context(), token, 0, 0)
	require.Error(t, err)
	_, err = svc.Register(t.Context(), token, "unavailable-budget", "password123", "", "")
	require.Error(t, err)
	requireNoRegistration(t, conn, "unavailable-budget")
}

func TestInvitationCreationUsesRateLimitLockAndBoundsLifetime(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	var orgID, hostID int64
	var accountID string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT i.organization_id,i.inviter_id,a.account_id FROM hosted_account_invitation i JOIN hosted_account a ON a.id=i.account_id`).Scan(&orgID, &hostID, &accountID))
	_, err := svc.Create(t.Context(), hostID, orgID, accountID, RoleViewer, 25*time.Hour)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	issued, err := svc.Create(t.Context(), hostID, orgID, accountID, RoleViewer, 0)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), issued.Invitation.ExpiresAt, time.Second)
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(t.Context(), `SELECT singleton FROM hosted_invitation_rate_limit FOR UPDATE`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = svc.Create(ctx, hostID, orgID, accountID, RoleViewer, time.Hour)
	require.Error(t, err, "issuance cannot change the target population while admission holds the lock")
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_invitation`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestInvitationCreationCapsActiveInvitationsPerCustomer(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	var orgID, hostID int64
	var customerID, root string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT i.organization_id,i.inviter_id,a.account_id FROM hosted_account_invitation i JOIN hosted_account a ON a.id=i.account_id`).Scan(&orgID, &hostID, &customerID))
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, orgID).Scan(&root))
	_, err := conn.ExecContext(t.Context(), `UPDATE hosted_account SET capabilities=$1 WHERE account_id=$2`, int64(AllCapabilities), customerID)
	require.NoError(t, err)
	subclient, err := svc.accounts.CreateChild(t.Context(), hostID, orgID, customerID, "Subclient", hostID, DefaultCapabilities)
	require.NoError(t, err)
	other, err := svc.accounts.CreateChild(t.Context(), hostID, orgID, root, "Other customer", hostID, DefaultCapabilities)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `
INSERT INTO hosted_account_invitation(invitation_id,organization_id,account_id,inviter_id,authority_fingerprint,token_hash,role,expires_at)
SELECT gen_random_uuid()::text,organization_id,account_id,inviter_id,authority_fingerprint,
       'customer-cap-' || n::text,role,expires_at
FROM hosted_account_invitation CROSS JOIN generate_series(1,$1) n`, customerInvitationLimit-2)
	require.NoError(t, err)
	_, err = svc.Create(t.Context(), hostID, orgID, subclient.AccountID, RoleViewer, time.Hour)
	require.NoError(t, err, "subclient invitations fill the customer's allowance")
	_, err = svc.Create(t.Context(), hostID, orgID, customerID, RoleViewer, time.Hour)
	require.True(t, fleeterror.IsResourceExhaustedError(err), "%v", err)
	_, err = svc.Create(t.Context(), hostID, orgID, subclient.AccountID, RoleViewer, time.Hour)
	require.True(t, fleeterror.IsResourceExhaustedError(err), "subclients share the customer's allowance")
	_, err = svc.Create(t.Context(), hostID, orgID, other.AccountID, RoleViewer, time.Hour)
	require.NoError(t, err, "one customer's pending invitations cannot block another customer")
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET revoked_at=statement_timestamp() WHERE token_hash='customer-cap-1'`)
	require.NoError(t, err)
	_, err = svc.Create(t.Context(), hostID, orgID, customerID, RoleViewer, time.Hour)
	require.NoError(t, err, "revocation frees capacity")
}

func TestInvitationLookupTimesOutWhileRateLimitLockIsHeld(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(t.Context(), `SELECT singleton FROM hosted_invitation_rate_limit FOR UPDATE`)
	require.NoError(t, err)
	started := time.Now()
	_, err = svc.Preview(t.Context(), token)
	elapsed := time.Since(started)
	require.Error(t, err)
	require.False(t, fleeterror.IsFailedPreconditionError(err), "a lock timeout is not an unavailable invitation: %v", err)
	require.Less(t, elapsed, 4*time.Second, "admission waits at most its two-second bound")
	require.Empty(t, svc.previewSlots, "a timed-out preview releases its slot")
	require.NoError(t, tx.Rollback())
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit_attempt`).Scan(&count))
	require.Zero(t, count, "a lookup that never acquired the lock records no charge")
	_, err = svc.Preview(t.Context(), token)
	require.NoError(t, err)
}
