package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountKeyRollbackNeverWidensLiveCredentials(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org, user, root, account, key int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO "user"(user_id,username,password_hash) VALUES($1,$2,'hash') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&user))
	_, err := conn.ExecContext(ctx, `INSERT INTO user_organization(user_id,organization_id,role_id) VALUES($1,$2,NULL)`, user, org)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO api_key(key_id,name,prefix,key_hash,user_id,organization_id,subject_kind) VALUES($1,'Customer','test-prefix',$2,$3,$4,'account') RETURNING id`, uuid.NewString(), uuid.NewString(), user, org).Scan(&key))
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_key(api_key_id,organization_id,account_id,capabilities) VALUES($1,$2,$3,1)`, key, org, account)
	require.NoError(t, err)
	down, err := migrations.Migrations.ReadFile("000162_hosted_account_keys.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "revoke all account API keys")
	var kind string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT subject_kind FROM api_key WHERE id=$1 AND revoked_at IS NULL`, key).Scan(&kind))
	require.Equal(t, "account", kind)
	var bound int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM hosted_account_key WHERE api_key_id=$1`, key).Scan(&bound))
	require.Equal(t, 1, bound)
	_, err = conn.ExecContext(ctx, `UPDATE api_key SET revoked_at=now() WHERE id=$1`, key)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT subject_kind FROM api_key WHERE id=$1 AND revoked_at IS NOT NULL`, key).Scan(&kind))
	require.Equal(t, "user", kind, "only already-revoked credentials may survive old schema")
	up, err := migrations.Migrations.ReadFile("000162_hosted_account_keys.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
}
