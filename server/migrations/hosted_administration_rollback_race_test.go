package migrations_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
)

func TestHostedAdministrationRollbackWaitsForAdministratorTransfer(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var org, user, root, customer int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, authz.Reconcile(ctx, conn))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO "user" (user_id, username, password_hash) VALUES ($1, $2, 'hash') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&user))
	_, err := conn.ExecContext(ctx, `INSERT INTO user_organization (user_id, organization_id, role_id) SELECT $1, $2, id FROM role WHERE organization_id=$2 AND builtin_key='SUPER_ADMIN'`, user, org)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO user_organization_role (user_id, organization_id, role_id, scope_type) SELECT $1, $2, id, 'org' FROM role WHERE organization_id=$2 AND builtin_key='SUPER_ADMIN'`, user, org)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities) VALUES ($1, $2, $3, 1, 'Customer', 31) RETURNING id`, uuid.NewString(), org, root).Scan(&customer))
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_membership (organization_id, account_id, user_id, role) VALUES ($1, $2, $3, 'account_administrator')`, org, customer, user)
	require.NoError(t, err)

	// The writer atomically replaces the last direct administrator with ancestor
	// authority. Its uncommitted changes are invisible to an unfenced precheck.
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = sqlc.New(writer).LockHostedAccountOrganization(ctx, org)
	require.NoError(t, err)
	var writerPID int
	require.NoError(t, writer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
	_, err = writer.ExecContext(ctx, `INSERT INTO hosted_account_ancestor_grant (organization_id, account_id, ancestor_id, capabilities) VALUES ($1, $2, $3, 9)`, org, customer, root)
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, `UPDATE hosted_account_membership SET revoked_at=now(), revision=revision+1 WHERE organization_id=$1 AND account_id=$2 AND user_id=$3`, org, customer, user)
	require.NoError(t, err)

	pid, done := startHostedAdministrationRollback(t, ctx, conn)
	// The old migration also waits, but only at DROP TABLE after accepting its
	// stale snapshot. Releasing the writer then wrongly permits data destruction.
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT $1 = ANY(pg_blocking_pids($2))`, writerPID, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	require.ErrorContains(t, <-done, "direct administrator")

	var grants, revoked int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_ancestor_grant WHERE organization_id=$1 AND account_id=$2`, org, customer).Scan(&grants))
	require.Equal(t, 1, grants, "rejected rollback preserves the newly committed ancestor authority")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_membership WHERE organization_id=$1 AND account_id=$2 AND revoked_at IS NOT NULL`, org, customer).Scan(&revoked))
	require.Equal(t, 1, revoked)
	var invitationsExist bool
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass('hosted_account_invitation') IS NOT NULL`).Scan(&invitationsExist))
	require.True(t, invitationsExist, "precheck refusal leaves the administration schema intact")
}

func TestHostedAdministrationRollbackIncludesConcurrentOrganizationCreation(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	creator, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer creator.Rollback()
	var org, user int64
	require.NoError(t, creator.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'Concurrent host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, creator.QueryRowContext(ctx, `INSERT INTO "user" (user_id, username, password_hash) VALUES ($1, $2, 'hash') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&user))
	_, err = creator.ExecContext(ctx, `INSERT INTO user_organization (user_id, organization_id, role_id) VALUES ($1, $2, NULL)`, user, org)
	require.NoError(t, err)

	pid, done := startHostedAdministrationRollback(t, ctx, conn)
	// A row-only fence would miss this uncommitted organization. The table fence
	// must wait for its creation before checking the new roleless affiliation.
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='organization'::regclass AND NOT granted)`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, creator.Commit())
	require.ErrorContains(t, <-done, "customer-only affiliations exist")
	var affiliations int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM user_organization WHERE organization_id=$1 AND user_id=$2 AND role_id IS NULL`, org, user).Scan(&affiliations))
	require.Equal(t, 1, affiliations, "rejected rollback preserves the affiliation committed before its fence")
}

func startHostedAdministrationRollback(t *testing.T, ctx context.Context, conn *sql.DB) (int, <-chan error) {
	t.Helper()
	down, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.down.sql")
	require.NoError(t, err)
	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	var pid int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	done := make(chan error, 1)
	go func() {
		_, execErr := tx.ExecContext(ctx, string(down))
		if execErr == nil {
			execErr = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		done <- execErr
	}()
	return pid, done
}
