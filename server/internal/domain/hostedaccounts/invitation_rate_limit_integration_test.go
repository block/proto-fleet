package hostedaccounts

import (
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

// minuteRows selects per-client minute counters, excluding the hourly and
// deployment counters stored in the same table.
const minuteRows = `client_hash <> 'deployment' AND client_hash NOT LIKE 'hourly:%'`

func TestInvitationRateLimitIsolatesClientsAndSurvivesServiceReplacement(t *testing.T) {
	svc, conn, token := registrationFixture(t)
	// Many outstanding invitations must not reduce anyone's request allowance.
	_, err := conn.ExecContext(t.Context(), `
INSERT INTO hosted_account_invitation(invitation_id,organization_id,account_id,inviter_id,authority_fingerprint,token_hash,role,expires_at)
SELECT gen_random_uuid()::text,organization_id,account_id,inviter_id,authority_fingerprint,
       'population-' || n::text,role,expires_at
FROM hosted_account_invitation CROSS JOIN generate_series(1,999) n`)
	require.NoError(t, err)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET attempts=60`)
	require.NoError(t, err)
	other := NewInvitationService(svc.accounts, svc.sessions)
	require.True(t, fleeterror.IsResourceExhaustedError(other.CheckClientRateLimit(t.Context(), "192.0.2.1")))
	require.NoError(t, other.CheckClientRateLimit(t.Context(), "192.0.2.2"), "an abusive client cannot spend another client's allowance")
	_, err = other.Preview(t.Context(), token)
	require.NoError(t, err)
	_, err = other.Register(t.Context(), token, "independent-recipient", "password123", "", "192.0.2.2")
	require.NoError(t, err, "a different client can complete onboarding despite the exhausted client")
}

func TestInvitationRateLimitSerializesConcurrentReplicas(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	_, err := conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET attempts=55`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	start := make(chan struct{})
	for range 12 {
		other := NewInvitationService(svc.accounts, svc.sessions)
		wg.Go(func() {
			<-start
			results <- other.CheckClientRateLimit(t.Context(), "192.0.2.1")
		})
	}
	close(start)
	wg.Wait()
	close(results)
	admitted := 0
	for err := range results {
		if err == nil {
			admitted++
		} else {
			require.True(t, fleeterror.IsResourceExhaustedError(err), "%v", err)
		}
	}
	require.Equal(t, 5, admitted, "replicas cannot multiply one client's allowance")
	var attempts int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT attempts FROM hosted_invitation_rate_limit WHERE `+minuteRows).Scan(&attempts))
	require.Equal(t, 60, attempts)
}

func TestInvitationRateLimitResetsWindowAndPrunesIdleClients(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	_, err := conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET attempts=60,window_started_at=statement_timestamp()-interval '1 minute'`)
	require.NoError(t, err)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	var attempts int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT attempts FROM hosted_invitation_rate_limit WHERE `+minuteRows).Scan(&attempts))
	require.Equal(t, 1, attempts)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO hosted_invitation_rate_limit (client_hash,window_started_at,attempts) VALUES ('idle',statement_timestamp()-interval '1 hour',60)`)
	require.NoError(t, err)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.2"))
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit WHERE `+minuteRows).Scan(&count))
	require.Equal(t, 2, count, "only the two recent clients remain")
}

func TestInvitationRateLimitCanonicalizesTrustedClientAddresses(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	_, err := conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET attempts=60`)
	require.NoError(t, err)
	require.True(t, fleeterror.IsResourceExhaustedError(svc.CheckClientRateLimit(t.Context(), "::ffff:192.0.2.1")))
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "2001:db8:1234:5678::1"))
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET attempts=60`)
	require.NoError(t, err)
	require.True(t, fleeterror.IsResourceExhaustedError(svc.CheckClientRateLimit(t.Context(), "2001:db8:1234:5678::2")), "IPv6 rotation within a /64 shares the allowance")
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "2001:db8:1234:5679::1"))
	for _, invalid := range []string{"", "unknown", "fe80::1%en0"} {
		require.True(t, fleeterror.IsFailedPreconditionError(svc.CheckClientRateLimit(t.Context(), invalid)))
	}
}

func TestInvitationRateLimitBoundsLockWaitWithoutBlockingOtherClients(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(t.Context(), `SELECT client_hash FROM hosted_invitation_rate_limit WHERE `+minuteRows+` FOR UPDATE`)
	require.NoError(t, err)
	started := time.Now()
	err = svc.CheckClientRateLimit(t.Context(), "192.0.2.1")
	require.Error(t, err)
	require.Less(t, time.Since(started), 4*time.Second)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.2"), "another client never waits for the locked client")
	require.NoError(t, tx.Rollback())
	var attempts int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT sum(attempts) FROM hosted_invitation_rate_limit WHERE `+minuteRows).Scan(&attempts))
	require.Equal(t, 2, attempts, "a timed-out update records no request")
}

func TestInvitationRateLimitFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	_, err := conn.ExecContext(t.Context(), `DROP TABLE hosted_invitation_rate_limit`)
	require.NoError(t, err)
	require.Error(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
}

func TestInvitationCreationBoundsLifetime(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	var orgID, hostID int64
	var accountID string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT i.organization_id,i.inviter_id,a.account_id FROM hosted_account_invitation i JOIN hosted_account a ON a.id=i.account_id`).Scan(&orgID, &hostID, &accountID))
	_, err := svc.Create(t.Context(), hostID, orgID, accountID, RoleViewer, 25*time.Hour)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	issued, err := svc.Create(t.Context(), hostID, orgID, accountID, RoleViewer, 0)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), issued.Invitation.ExpiresAt, time.Second)
}

func TestInvitationRateLimitSkipsLockedIdleClientsDuringCleanup(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	_, err := conn.ExecContext(t.Context(), `INSERT INTO hosted_invitation_rate_limit (client_hash,window_started_at,attempts) VALUES ('idle',statement_timestamp()-interval '2 hours',60)`)
	require.NoError(t, err)
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(t.Context(), `SELECT client_hash FROM hosted_invitation_rate_limit WHERE client_hash='idle' FOR UPDATE`)
	require.NoError(t, err)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"), "a locked idle counter must not block another client's admission or cleanup")
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit WHERE `+minuteRows).Scan(&count))
	require.Equal(t, 2, count, "the locked idle row is skipped")
	require.NoError(t, tx.Rollback())
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_invitation_rate_limit WHERE `+minuteRows).Scan(&count))
	require.Equal(t, 1, count, "cleanup reclaims the idle row once it is unlocked")
}

func TestInvitationDeploymentLimitBoundsGuessesAcrossClients(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	_, err := conn.ExecContext(t.Context(), `
INSERT INTO hosted_invitation_rate_limit (client_hash,window_started_at,attempts) VALUES ('deployment',statement_timestamp(),$1)
ON CONFLICT (client_hash) DO UPDATE SET window_started_at=EXCLUDED.window_started_at, attempts=EXCLUDED.attempts`, invitationDeploymentAttemptLimit-3)
	require.NoError(t, err)
	for _, client := range []string{"192.0.2.1", "192.0.2.2", "2001:db8::1"} {
		require.NoError(t, svc.CheckClientRateLimit(t.Context(), client))
	}
	require.True(t, fleeterror.IsResourceExhaustedError(svc.CheckClientRateLimit(t.Context(), "192.0.2.3")),
		"rotating clients cannot exceed the deployment-wide allowance")
	other := NewInvitationService(svc.accounts, svc.sessions)
	require.True(t, fleeterror.IsResourceExhaustedError(other.CheckClientRateLimit(t.Context(), "192.0.2.4")),
		"replicas share the deployment-wide allowance")
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET window_started_at=statement_timestamp()-interval '1 hour' WHERE client_hash='deployment'`)
	require.NoError(t, err)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.5"), "the allowance resets after an hour")
	var attempts int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT attempts FROM hosted_invitation_rate_limit WHERE client_hash='deployment'`).Scan(&attempts))
	require.Equal(t, 1, attempts)
}

func TestInvitationThrottledClientsDoNotSpendTheDeploymentAllowance(t *testing.T) {
	svc, conn, _ := registrationFixture(t)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	_, err := conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET attempts=$1 WHERE `+minuteRows, invitationClientAttemptLimit)
	require.NoError(t, err)
	for range 5 {
		require.True(t, fleeterror.IsResourceExhaustedError(svc.CheckClientRateLimit(t.Context(), "192.0.2.1")))
	}
	var attempts int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT attempts FROM hosted_invitation_rate_limit WHERE client_hash='deployment'`).Scan(&attempts))
	require.Equal(t, 1, attempts)
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT attempts FROM hosted_invitation_rate_limit WHERE client_hash LIKE 'hourly:%'`).Scan(&attempts))
	require.Equal(t, 1, attempts, "requests the minute window rejects do not spend the hourly allowance")
}

func TestInvitationHourlyClientLimitKeepsOneClientFromDrainingTheDeployment(t *testing.T) {
	require.GreaterOrEqual(t, invitationDeploymentAttemptLimit/invitationClientHourlyAttemptLimit, 10,
		"draining the deployment allowance must take at least ten clients")
	svc, conn, _ := registrationFixture(t)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"))
	_, err := conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET attempts=$1 WHERE client_hash LIKE 'hourly:%'`, invitationClientHourlyAttemptLimit)
	require.NoError(t, err)
	require.True(t, fleeterror.IsResourceExhaustedError(svc.CheckClientRateLimit(t.Context(), "192.0.2.1")),
		"a client that spent its hourly share is throttled although its minute window has room")
	var deployment int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT attempts FROM hosted_invitation_rate_limit WHERE client_hash='deployment'`).Scan(&deployment))
	require.Equal(t, 1, deployment, "the throttled request does not spend the deployment allowance")
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.2"), "another client keeps its own allowance")
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_invitation_rate_limit SET window_started_at=statement_timestamp()-interval '1 hour' WHERE client_hash LIKE 'hourly:%'`)
	require.NoError(t, err)
	require.NoError(t, svc.CheckClientRateLimit(t.Context(), "192.0.2.1"), "the hourly allowance resets after an hour")
}
