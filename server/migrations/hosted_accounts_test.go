package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
)

func TestHostedAccountsMigrationBackfillsRootsAndRollsBack(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	down, err := migrations.Migrations.ReadFile("000160_hosted_accounts.down.sql")
	require.NoError(t, err)
	up, err := migrations.Migrations.ReadFile("000160_hosted_accounts.up.sql")
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
