package hostedaccounts_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type observedDeactivationStore struct {
	*sqlstores.SQLUserStore
	locked chan struct{}
	once   sync.Once
}

func (s *observedDeactivationStore) LockAndCountSoleHostedAccountAdministrations(ctx context.Context, orgID, userID int64) (int64, error) {
	count, err := s.SQLUserStore.LockAndCountSoleHostedAccountAdministrations(ctx, orgID, userID)
	if err == nil {
		s.once.Do(func() { close(s.locked) })
	}
	return count, err
}

func TestRoleReplacementCompletesWhileDeactivationWaits(t *testing.T) {
	f := setup(t)
	store := sqlstores.NewSQLUserStore(f.db)
	fieldRole, err := store.GetBuiltinRoleForOrg(t.Context(), f.org, "FIELD_TECH")
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: fieldRole.ID, ScopeType: "org"})
	require.NoError(t, err)
	newRole, err := store.GetBuiltinRoleForOrg(t.Context(), f.org, "ADMIN")
	require.NoError(t, err)
	host, err := store.GetUserByID(t.Context(), f.host)
	require.NoError(t, err)
	target, err := store.GetUserByID(t.Context(), f.user)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Hold the same assignment/user locks as UpdateUserRole before its write.
	roleTx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = roleTx.Rollback() })
	q := sqlc.New(roleTx)
	current, err := q.GetOrgScopeAssignmentForUser(ctx, sqlc.GetOrgScopeAssignmentForUserParams{UserID: f.user, OrganizationID: f.org})
	require.NoError(t, err)
	var rolePID int
	require.NoError(t, roleTx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&rolePID))
	observed := &observedDeactivationStore{SQLUserStore: store, locked: make(chan struct{})}
	svc := domainauth.NewService(store, observed, sqlstores.NewSQLTransactor(f.db), nil, nil, nil, nil, authz.NewPermissionResolver(f.db))
	deactivateCtx := authn.SetInfo(ctx, &session.Info{UserID: f.host, OrganizationID: f.org, ExternalUserID: host.UserID, Username: host.Username})
	finished := make(chan error, 1)
	go func() {
		_, deactivateErr := svc.DeactivateUser(deactivateCtx, &authv1.DeactivateUserRequest{UserId: target.UserID})
		finished <- deactivateErr
	}()
	select {
	case <-observed.locked:
	case <-ctx.Done():
		t.Fatal("deactivation did not acquire the organization lock")
	}
	// Observe the actual database wait, rather than relying on goroutine timing.
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))
			AND query LIKE '%GetOrgScopeAssignmentForUser%'
		)`, rolePID).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	// This insert needs an organization foreign-key KEY SHARE lock. It must
	// complete while deactivation holds the organization serialization lock.
	roleCtx := db.WithTxQueries(ctx, q)
	require.NoError(t, store.UpdateUserOrganizationRole(roleCtx, f.user, f.org, current.AssignmentID, newRole.ID))
	require.NoError(t, roleTx.Commit())
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("deactivation did not finish after role replacement committed")
	}
	var deleted bool
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL FROM "user" WHERE id = $1`, f.user).Scan(&deleted))
	require.True(t, deleted)
}

type retryingDeactivationStore struct {
	*sqlstores.SQLUserStore
	attempts int
}

func (s *retryingDeactivationStore) LockAndCountSoleHostedAccountAdministrations(ctx context.Context, orgID, userID int64) (int64, error) {
	s.attempts++
	if s.attempts == 1 {
		return 0, &pgconn.PgError{Code: "40P01", Message: "injected organization lock deadlock"}
	}
	return s.SQLUserStore.LockAndCountSoleHostedAccountAdministrations(ctx, orgID, userID)
}

func TestDeactivationPreservesRetryableOrganizationLockErrors(t *testing.T) {
	f := setup(t)
	store := sqlstores.NewSQLUserStore(f.db)
	host, err := store.GetUserByID(t.Context(), f.host)
	require.NoError(t, err)
	target, err := store.GetUserByID(t.Context(), f.user)
	require.NoError(t, err)
	retrying := &retryingDeactivationStore{SQLUserStore: store}
	svc := domainauth.NewService(store, retrying, sqlstores.NewSQLTransactor(f.db), nil, nil, nil, nil, authz.NewPermissionResolver(f.db))
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: f.host, OrganizationID: f.org, ExternalUserID: host.UserID, Username: host.Username})
	_, err = svc.DeactivateUser(ctx, &authv1.DeactivateUserRequest{UserId: target.UserID})
	require.NoError(t, err)
	require.Equal(t, 2, retrying.attempts)
}

func TestRoleReplacementPreservesRetryableWriteErrors(t *testing.T) {
	for _, phase := range []struct{ name, table, event string }{
		{"legacy-role", "user_organization", "UPDATE"},
		{"unassign", "user_organization_role", "UPDATE"},
		{"assign", "user_organization_role", "INSERT"},
	} {
		t.Run(phase.name, func(t *testing.T) {
			f := setup(t)
			store := sqlstores.NewSQLUserStore(f.db)
			oldRole, err := store.GetBuiltinRoleForOrg(t.Context(), f.org, "FIELD_TECH")
			require.NoError(t, err)
			current, err := sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: oldRole.ID, ScopeType: "org"})
			require.NoError(t, err)
			newRole, err := store.GetBuiltinRoleForOrg(t.Context(), f.org, "ADMIN")
			require.NoError(t, err)
			// The sequence survives rollback, so the real PostgreSQL write fails
			// once and the transaction's second attempt can commit.
			_, err = f.db.ExecContext(t.Context(), fmt.Sprintf(`
				CREATE SEQUENCE role_retry_attempt;
				CREATE FUNCTION fail_first_role_write() RETURNS trigger AS $$
				BEGIN
					IF nextval('role_retry_attempt') = 1 THEN
						RAISE EXCEPTION 'injected role write deadlock' USING ERRCODE = '40P01';
					END IF;
					RETURN NEW;
				END;
				$$ LANGUAGE plpgsql;
				CREATE TRIGGER role_retry BEFORE %s ON %s
				FOR EACH ROW EXECUTE FUNCTION fail_first_role_write();`, phase.event, phase.table))
			require.NoError(t, err)
			attempts := 0
			err = sqlstores.NewSQLTransactor(f.db).RunInTx(t.Context(), func(ctx context.Context) error {
				attempts++
				return store.UpdateUserOrganizationRole(ctx, f.user, f.org, current.ID, newRole.ID)
			})
			require.NoError(t, err)
			require.Equal(t, 2, attempts)
			assignments, err := sqlc.New(f.db).ListAssignmentsForUser(t.Context(), sqlc.ListAssignmentsForUserParams{UserID: f.user, OrganizationID: f.org})
			require.NoError(t, err)
			require.Len(t, assignments, 1)
			require.Equal(t, newRole.ID, assignments[0].RoleID)
		})
	}
}
