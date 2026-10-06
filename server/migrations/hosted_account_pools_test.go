package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPoolRollbackPreservesNonemptyConfigurationData(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, root, account int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
	poolID, revisionID := uuid.NewString(), uuid.NewString()
	_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, poolID, org, account)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) VALUES($1,$2,$3,$4,1,'Choice','[{"url":"stratum+tcp://pool.example","username":"payout","worker_template":"","password_ciphertext":"encrypted","has_password":true}]')`, revisionID, org, account, poolID)
	require.NoError(t, err)
	down, err := migrations.Migrations.ReadFile("000163_hosted_account_pools.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "pool configuration data")
	var id string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT revision_id FROM hosted_account_pool_revision WHERE revision_id=$1`, revisionID).Scan(&id))
	require.Equal(t, revisionID, id)
	_, err = conn.ExecContext(ctx, `DELETE FROM organization WHERE id=$1`, org)
	require.NoError(t, err, "pool immutability permits organization cleanup after parent deletion")
	var remaining int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM hosted_account_pool_revision WHERE revision_id=$1`, revisionID).Scan(&remaining))
	require.Zero(t, remaining)
}
