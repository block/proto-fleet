package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestLoginSessionInsertCompletesWhileInvitationAcceptanceWaitsForUser(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.AllCapabilities)
	invites := invitations(f)
	issued, err := invites.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Hold the same user lock as login's short session-creation transaction.
	loginTx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = loginTx.Rollback() }()
	q := sqlc.New(loginTx)
	_, err = q.GetUserByIdForUpdate(ctx, f.host)
	require.NoError(t, err)
	var loginPID int
	require.NoError(t, loginTx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&loginPID))
	// Bound the FK insertion below before PostgreSQL's deadlock detector can
	// abort/retry acceptance and accidentally hide the conflicting lock mode.
	_, err = loginTx.ExecContext(ctx, "SET LOCAL lock_timeout = '200ms'")
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() {
		_, acceptErr := invites.Accept(ctx, issued.Token, f.user, f.org)
		finished <- acceptErr
	}()
	// Acceptance has acquired the organization serializer and is now waiting
	// on the actual user-lock query, rather than a goroutine timing guess.
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))
			AND query LIKE '%LockActiveHostedAccountUser%'
		)`, loginPID).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	sessions := session.NewService(session.Config{Duration: time.Hour, IDBytes: 32}, sqlstores.NewSQLSessionStore(f.db))
	created, insertErr := sessions.Create(db.WithTxQueries(ctx, q), f.host, f.org, "login", "127.0.0.1")
	if insertErr != nil {
		// Release the user lock and join acceptance even on the red failure path.
		_ = loginTx.Rollback()
	} else {
		require.NoError(t, loginTx.Commit())
	}
	select {
	case acceptErr := <-finished:
		require.NoError(t, acceptErr)
	case <-ctx.Done():
		t.Fatal("acceptance did not finish after login released its user lock")
	}
	require.NoError(t, insertErr, "login's organization FK check must complete while acceptance holds the serializer")
	stored, err := sessions.Validate(ctx, created.SessionID)
	require.NoError(t, err)
	require.Equal(t, f.host, stored.UserID)
}

func TestLoginSessionCreationRetriesDeadlockCause(t *testing.T) {
	f := setup(t)
	// The sequence survives rollback, so only the first real session insert
	// fails; the login-style transaction can then retry and commit once.
	_, err := f.db.ExecContext(t.Context(), `
		CREATE SEQUENCE session_retry_attempt;
		CREATE FUNCTION fail_first_session_insert() RETURNS trigger AS $$
		BEGIN
			IF nextval('session_retry_attempt') = 1 THEN
				RAISE EXCEPTION 'injected session deadlock' USING ERRCODE = '40P01';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER session_retry BEFORE INSERT ON session
		FOR EACH ROW EXECUTE FUNCTION fail_first_session_insert();`)
	require.NoError(t, err)
	users := sqlstores.NewSQLUserStore(f.db)
	sessions := session.NewService(session.Config{Duration: time.Hour, IDBytes: 32}, sqlstores.NewSQLSessionStore(f.db))
	attempts := 0
	err = sqlstores.NewSQLTransactor(f.db).RunInTx(t.Context(), func(ctx context.Context) error {
		attempts++
		if _, err := users.GetUserByIDForUpdate(ctx, f.host); err != nil {
			return err
		}
		_, err := sessions.Create(ctx, f.host, f.org, "login", "127.0.0.1")
		return err
	})
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM session WHERE user_id=$1 AND organization_id=$2`, f.host, f.org).Scan(&count))
	require.Equal(t, 1, count, "only the successful transaction persists a session")
}
