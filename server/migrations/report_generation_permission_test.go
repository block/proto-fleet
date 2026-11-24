package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReportGenerationPermissionMigrationPreservesAccessAndDenials(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	down, err := migrations.Current.ReadFile("current/001091_internal_hosted_report_generation_permission.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001091_internal_hosted_report_generation_permission.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var org, root, client, limited int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Generation migration') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Full client',31) RETURNING id`, uuid.NewString(), org, root).Scan(&client))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Limited client',9) RETURNING id`, uuid.NewString(), org, root).Scan(&limited))
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_ancestor_grant(organization_id,account_id,ancestor_id,capabilities) VALUES($1,$2,$4,31),($1,$3,$4,9)`, org, client, limited, root)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	var mask, revision int64
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities,authority_revision FROM hosted_account WHERE id=$1`, client).Scan(&mask, &revision))
	require.EqualValues(t, 63, mask)
	require.EqualValues(t, 2, revision)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities FROM hosted_account WHERE id=$1`, limited).Scan(&mask))
	require.EqualValues(t, 41, mask, "existing admin generation is preserved independently of other features")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities FROM hosted_account_ancestor_grant WHERE account_id=$1`, client).Scan(&mask))
	require.EqualValues(t, 63, mask, "complete source authority retains administrator delegation")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities FROM hosted_account_ancestor_grant WHERE account_id=$1`, limited).Scan(&mask))
	require.EqualValues(t, 9, mask, "partial grants remain exact")
	var newOrg int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'New host') RETURNING id`, uuid.NewString()).Scan(&newOrg))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities FROM hosted_account WHERE organization_id=$1 AND depth=0`, newOrg).Scan(&mask))
	require.EqualValues(t, 63, mask)
	_, err = conn.ExecContext(ctx, `UPDATE hosted_account SET capabilities=127 WHERE id=$1`, client)
	require.Error(t, err, "unknown capability bits remain invalid")
	_, err = conn.ExecContext(ctx, `UPDATE hosted_account SET capabilities=31 WHERE id=$1`, client)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "disabled report generation prevents rollback")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities FROM hosted_account WHERE id=$1`, client).Scan(&mask))
	require.EqualValues(t, 31, mask, "rejected rollback preserves the explicit denial")
	_, err = conn.ExecContext(ctx, `UPDATE hosted_account SET capabilities=63 WHERE id=$1`, client)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE hosted_account SET capabilities=31 WHERE id=$1`, root)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "disabled report generation prevents rollback", "a parent ceiling denial must survive rollback too")
	_, err = conn.ExecContext(ctx, `UPDATE hosted_account SET capabilities=63 WHERE id=$1`, root)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities FROM hosted_account WHERE id=$1`, limited).Scan(&mask))
	require.EqualValues(t, 9, mask)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT capabilities FROM hosted_account_ancestor_grant WHERE account_id=$1`, client).Scan(&mask))
	require.EqualValues(t, 31, mask)
}
