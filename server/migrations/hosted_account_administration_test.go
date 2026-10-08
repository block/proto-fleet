package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
)

func TestHostedAdministrationMigrationRoundTripAndRolelessAffiliation(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, user int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, authz.Reconcile(ctx, conn))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO "user" (user_id, username, password_hash) VALUES ($1, $2, 'hash') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&user))
	_, err := conn.ExecContext(ctx, `INSERT INTO user_organization (user_id, organization_id, role_id) VALUES ($1, $2, NULL)`, user, org)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "customer-only affiliations exist")
	var affiliations int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_organization WHERE user_id = $1 AND organization_id = $2 AND role_id IS NULL`, user, org).Scan(&affiliations))
	require.Equal(t, 1, affiliations, "rejected rollback preserves customer affiliations")
	_, err = conn.ExecContext(ctx, `UPDATE user_organization SET role_id = (SELECT id FROM role WHERE organization_id = $2 AND builtin_key = 'FIELD_TECH') WHERE user_id = $1 AND organization_id = $2`, user, org)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var role int64
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT role_id FROM user_organization WHERE user_id = $1 AND organization_id = $2`, user, org).Scan(&role))
	var assignments int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_organization_role WHERE user_id = $1`, user).Scan(&assignments))
	require.Zero(t, assignments, "rollback must not invent organization permissions")
	up, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
}

func TestHostedAncestorGrantsRejectSiblingAndForeignAccounts(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, root, client, sibling, child int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id = $1 AND depth = 0`, org).Scan(&root))
	for _, target := range []*int64{&client, &sibling} {
		require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities) VALUES ($1, $2, $3, 1, 'Client', 31) RETURNING id`, uuid.NewString(), org, root).Scan(target))
	}
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities) VALUES ($1, $2, $3, 2, 'Subclient', 31) RETURNING id`, uuid.NewString(), org, client).Scan(&child))
	for _, ancestor := range []int64{root, client} {
		_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_ancestor_grant (organization_id, account_id, ancestor_id, capabilities) VALUES ($1, $2, $3, 9)`, org, child, ancestor)
		require.NoError(t, err)
	}
	for _, ancestor := range []int64{child, sibling} {
		_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_ancestor_grant (organization_id, account_id, ancestor_id, capabilities) VALUES ($1, $2, $3, 9)`, org, child, ancestor)
		require.Error(t, err)
	}
	var otherOrg, otherRoot int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'Other host') RETURNING id`, uuid.NewString()).Scan(&otherOrg))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id = $1 AND depth = 0`, otherOrg).Scan(&otherRoot))
	_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_ancestor_grant (organization_id, account_id, ancestor_id, capabilities) VALUES ($1, $2, $3, 9)`, org, child, otherRoot)
	require.Error(t, err)
}

func TestHostedAdministrationRollbackPreservesSoleAncestorAdministrator(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, user, root, customer int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, authz.Reconcile(ctx, conn))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id = $1 AND depth = 0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO "user" (user_id, username, password_hash) VALUES ($1, $2, 'hash') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&user))
	_, err := conn.ExecContext(ctx, `INSERT INTO user_organization (user_id, organization_id, role_id) SELECT $1, $2, id FROM role WHERE organization_id = $2 AND builtin_key = 'SUPER_ADMIN'`, user, org)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO user_organization_role (user_id, organization_id, role_id, scope_type) SELECT $1, $2, id, 'org' FROM role WHERE organization_id = $2 AND builtin_key = 'SUPER_ADMIN'`, user, org)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities) VALUES ($1, $2, $3, 1, 'Customer', 15) RETURNING id`, uuid.NewString(), org, root).Scan(&customer))
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_ancestor_grant (organization_id, account_id, ancestor_id, capabilities) VALUES ($1, $2, $3, 9)`, org, customer, root)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "direct administrator")
	var grants int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_ancestor_grant WHERE organization_id=$1 AND account_id=$2`, org, customer).Scan(&grants))
	require.Equal(t, 1, grants, "rollback must reject before dropping ancestor authority")
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_membership (organization_id, account_id, user_id, role) VALUES ($1, $2, $3, 'account_administrator')`, org, customer, user)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
}
