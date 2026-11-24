package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
)

func TestHostedAccountsMigrationBackfillsRootsAndRollsBack(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	rollbackEmptyClientTicketing(t, conn)
	ctx := t.Context()
	rollbackEmptyMiningGroups(t, conn)
	rollbackEmptyAllocationHistory(t, conn)
	assetsDown, err := migrations.Current.ReadFile("current/001028_internal_hosted_account_assets.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(assetsDown))
	require.NoError(t, err)
	poolsDown, err := migrations.Current.ReadFile("current/001007_internal_hosted_account_pools.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(poolsDown))
	require.NoError(t, err)
	keysDown, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(keysDown))
	require.NoError(t, err)
	administrationDown, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(administrationDown))
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001003_internal_hosted_accounts.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001003_internal_hosted_accounts.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var org int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'Existing host') RETURNING id`, uuid.NewString()).Scan(&org))
	var unnamed int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name, deleted_at) VALUES ($1, '   ', CURRENT_TIMESTAMP) RETURNING id`, uuid.NewString()).Scan(&unnamed))
	_, err = conn.ExecContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, '')`, uuid.NewString())
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	var roots, memberships int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM hosted_account WHERE organization_id = $1 AND depth = 0 AND parent_id IS NULL`, org).Scan(&roots))
	require.Equal(t, 1, roots)
	var rootName string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT name FROM hosted_account WHERE organization_id = $1`, unnamed).Scan(&rootName))
	require.NotEmpty(t, rootName, "previously valid blank names must not prevent migration")
	_, err = conn.ExecContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, '')`, uuid.NewString())
	require.NoError(t, err, "new blank-name organizations also get a valid root")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM hosted_account_membership`).Scan(&memberships))
	require.Zero(t, memberships, "migration must not invent customer membership")
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account (account_id, organization_id, depth, name, capabilities) VALUES ($1, $2, 0, 'Duplicate root', 31)`, uuid.NewString(), org)
	require.Error(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	// The old organization schema remains usable and no deleted-table trigger survives.
	_, err = conn.ExecContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'After rollback')`, uuid.NewString())
	require.NoError(t, err)
}

func TestHostedAccountSchemaEnforcesHierarchyAndOrganization(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var orgA, orgB, rootA, rootB int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'A') RETURNING id`, uuid.NewString()).Scan(&orgA))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'B') RETURNING id`, uuid.NewString()).Scan(&orgB))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id = $1 AND depth = 0`, orgA).Scan(&rootA))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id = $1 AND depth = 0`, orgB).Scan(&rootB))
	for _, tc := range []struct {
		name   string
		parent int64
		depth  int
	}{
		{"cross organization", rootB, 1},
		{"skipped level", rootA, 2},
		{"too deep", rootA, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities) VALUES ($1, $2, $3, $4, 'Invalid', 15)`, uuid.NewString(), orgA, tc.parent, tc.depth)
			require.Error(t, err)
		})
	}
	var client int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities) VALUES ($1, $2, $3, 1, 'Client', 15) RETURNING id`, uuid.NewString(), orgA, rootA).Scan(&client))
	_, err := conn.ExecContext(ctx, `UPDATE hosted_account SET organization_id = $1, parent_id = $2 WHERE id = $3`, orgB, rootB, client)
	require.Error(t, err, "reparenting and organization changes are not supported")
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_membership (organization_id, account_id, user_id, role) VALUES ($1, $2, 999999, 'viewer')`, orgB, client)
	require.Error(t, err, "membership must reference a same-organization account and affiliated user")
	_, err = conn.ExecContext(ctx, `DELETE FROM organization WHERE id = $1`, orgA)
	require.NoError(t, err, "the account hierarchy must not block existing organization deletion")
	var remaining int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM hosted_account WHERE organization_id = $1`, orgA).Scan(&remaining))
	require.Zero(t, remaining)
}

func TestHostedAccountsMigrationBackfillsExistingAdmins(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	rollbackEmptyClientTicketing(t, conn)
	ctx := t.Context()
	rollbackEmptyMiningGroups(t, conn)
	rollbackEmptyAllocationHistory(t, conn)
	assetsDown, err := migrations.Current.ReadFile("current/001028_internal_hosted_account_assets.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(assetsDown))
	require.NoError(t, err)
	poolsDown, err := migrations.Current.ReadFile("current/001007_internal_hosted_account_pools.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(poolsDown))
	require.NoError(t, err)
	keysDown, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(keysDown))
	require.NoError(t, err)
	administrationDown, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(administrationDown))
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001003_internal_hosted_accounts.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001003_internal_hosted_accounts.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)

	// Model existing roles before the hosted-account permission is introduced.
	roles := []struct {
		name, key      string
		deleted        bool
		alreadyGranted bool
		wantGrant      bool
		id             int64
	}{
		{name: "existing admin", key: "ADMIN", wantGrant: true},
		{name: "already granted admin", key: "ADMIN", alreadyGranted: true, wantGrant: true},
		{name: "field tech", key: "FIELD_TECH"},
		{name: "custom role"},
		{name: "deleted admin", key: "ADMIN", deleted: true},
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO permission (key, description) VALUES ('account:manage', 'Existing grant')`)
	require.NoError(t, err)
	for i := range roles {
		role := &roles[i]
		var org int64
		require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id`, uuid.NewString(), role.name).Scan(&org))
		require.NoError(t, conn.QueryRowContext(ctx, `
			INSERT INTO role (name, organization_id, is_builtin, builtin_key, deleted_at)
			VALUES ($1, $2, $3, NULLIF($4, ''), CASE WHEN $5 THEN CURRENT_TIMESTAMP END)
			RETURNING id`, role.name, org, role.key != "", role.key, role.deleted).Scan(&role.id))
		_, err = conn.ExecContext(ctx, `INSERT INTO role_permission (role_id, permission_id) SELECT $1, id FROM permission WHERE key = 'miner:read'`, role.id)
		require.NoError(t, err)
		if role.alreadyGranted {
			_, err = conn.ExecContext(ctx, `INSERT INTO role_permission (role_id, permission_id) SELECT $1, id FROM permission WHERE key = 'account:manage'`, role.id)
			require.NoError(t, err)
		}
	}
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	for _, role := range roles {
		t.Run(role.name, func(t *testing.T) {
			var grants, total, retained int
			require.NoError(t, conn.QueryRowContext(ctx, `
				SELECT COUNT(*) FILTER (WHERE p.key = 'account:manage'), COUNT(*),
				       COUNT(*) FILTER (WHERE p.key = 'miner:read')
				FROM role_permission rp JOIN permission p ON p.id = rp.permission_id
				WHERE rp.role_id = $1`, role.id).Scan(&grants, &total, &retained))
			want := 0
			if role.wantGrant {
				want = 1
			}
			require.Equal(t, want, grants)
			require.Equal(t, 1, retained, "existing grants must remain intact")
			require.Equal(t, 1+want, total, "only account:manage may be added")
		})
	}

	var newOrg int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name) VALUES ($1, 'New host') RETURNING id`, uuid.NewString()).Scan(&newOrg))
	require.NoError(t, authz.Reconcile(ctx, conn))
	var newAdminGrant int
	require.NoError(t, conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM role_permission rp
		JOIN role r ON r.id = rp.role_id JOIN permission p ON p.id = rp.permission_id
		WHERE r.organization_id = $1 AND r.builtin_key = 'ADMIN' AND p.key = 'account:manage'`, newOrg).Scan(&newAdminGrant))
	require.Equal(t, 1, newAdminGrant, "new and existing ADMIN roles receive the same account permission")

	// Backfill once; later boot reconciliation must respect operator revocation.
	_, err = conn.ExecContext(ctx, `DELETE FROM role_permission WHERE role_id = $1 AND permission_id IN (SELECT id FROM permission WHERE key = 'account:manage')`, roles[0].id)
	require.NoError(t, err)
	require.NoError(t, authz.Reconcile(ctx, conn))
	var revokedGrant int
	require.NoError(t, conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM role_permission rp JOIN permission p ON p.id = rp.permission_id
		WHERE rp.role_id = $1 AND p.key = 'account:manage'`, roles[0].id).Scan(&revokedGrant))
	require.Zero(t, revokedGrant, "restart must not restore the operator's revoked permission")
}

func TestHostedAccountsMigrationIncludesOrganizationsCreatedBeforeItsFence(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	rollbackEmptyClientTicketing(t, conn)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	rollbackEmptyMiningGroups(t, conn)
	rollbackEmptyAllocationHistory(t, conn)
	assetsDown, err := migrations.Current.ReadFile("current/001028_internal_hosted_account_assets.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(assetsDown))
	require.NoError(t, err)
	poolsDown, err := migrations.Current.ReadFile("current/001007_internal_hosted_account_pools.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(poolsDown))
	require.NoError(t, err)
	keysDown, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(keysDown))
	require.NoError(t, err)
	administrationDown, err := migrations.Current.ReadFile("current/001005_internal_hosted_account_administration.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(administrationDown))
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001003_internal_hosted_accounts.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001003_internal_hosted_accounts.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)

	// An old server has created the organization and its seeded ADMIN, but has
	// not committed. Neither row is visible to the migration's snapshot yet.
	creator, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer creator.Rollback()
	var org, admin int64
	require.NoError(t, creator.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name) VALUES ($1, 'Concurrent host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, creator.QueryRowContext(ctx, `
		INSERT INTO role (name, organization_id, is_builtin, builtin_key)
		VALUES ('ADMIN', $1, TRUE, 'ADMIN') RETURNING id`, org).Scan(&admin))

	migration, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer migration.Rollback()
	var pid int
	require.NoError(t, migration.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	done := make(chan error, 1)
	go func() {
		_, execErr := migration.ExecContext(ctx, string(up))
		if execErr == nil {
			execErr = migration.Commit()
		}
		done <- execErr
	}()

	// Observe a real PostgreSQL lock wait instead of relying on sleep timing.
	// Without the early fence, DDL waits only after the ADMIN snapshot.
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_locks
			WHERE pid = $1 AND relation = 'organization'::regclass AND NOT granted)`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, creator.Commit())
	require.NoError(t, <-done)

	t.Run("ADMIN permission", func(t *testing.T) {
		var grants int
		require.NoError(t, conn.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM role_permission rp JOIN permission p ON p.id = rp.permission_id
			WHERE rp.role_id = $1 AND p.key = 'account:manage'`, admin).Scan(&grants))
		require.Equal(t, 1, grants, "the migration must include the organization committed before its fence")
	})
	t.Run("host root", func(t *testing.T) {
		var roots int
		require.NoError(t, conn.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM hosted_account WHERE organization_id = $1 AND depth = 0`, org).Scan(&roots))
		require.Equal(t, 1, roots, "the root snapshot must use the same organization fence")
	})
}
