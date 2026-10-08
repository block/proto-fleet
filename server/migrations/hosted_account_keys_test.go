package migrations_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
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
	down, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.down.sql")
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
	up, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
}

func TestAccountKeyRollbackIsAtomicWithoutOuterTransaction(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bound   bool
		expired bool
	}{
		{name: "live bound", bound: true},
		{name: "live unbound"},
		{name: "expired unrevoked", bound: true, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := dbtest.GetTestDB(t)
			ctx := t.Context()
			org, user, account := accountKeyRollbackScope(t, conn)
			var key int64
			keyHash := uuid.NewString()
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO api_key(key_id,name,prefix,key_hash,user_id,organization_id,subject_kind,expires_at)
VALUES($1,'Customer','test-prefix',$2,$3,$4,'account',CASE WHEN $5 THEN now()-interval '1 day' END) RETURNING id`, uuid.NewString(), keyHash, user, org, tc.expired).Scan(&key))
			if tc.bound {
				_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_key(api_key_id,organization_id,account_id,capabilities) VALUES($1,$2,$3,1)`, key, org, account)
				require.NoError(t, err)
			}
			down, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.down.sql")
			require.NoError(t, err)
			// Preparing rejects multiple top-level statements. Executing the
			// file this way ensures no destructive SQL can run as a separate
			// autocommit statement after a failed guard, as plain psql would allow.
			rollback, err := conn.PrepareContext(ctx, string(down))
			require.NoError(t, err, "rollback must be a single guarded SQL statement")
			defer rollback.Close()
			_, err = rollback.ExecContext(ctx)
			require.ErrorContains(t, err, "revoke all account API keys")
			var kind, storedHash string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT subject_kind,key_hash FROM api_key WHERE id=$1 AND revoked_at IS NULL`, key).Scan(&kind, &storedHash))
			require.Equal(t, "account", kind)
			require.Equal(t, keyHash, storedHash)
			var schemaIntact bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT
 to_regclass('hosted_account_key') IS NOT NULL AND
 to_regclass('hosted_account_key_descendant') IS NOT NULL AND
 to_regprocedure('check_hosted_account_key()') IS NOT NULL AND
 to_regprocedure('check_hosted_account_key_descendant()') IS NOT NULL AND
 (SELECT count(*) FROM pg_constraint WHERE conrelid='api_key'::regclass AND conname IN ('uq_api_key_org_id','ck_api_key_subject'))=2`).Scan(&schemaIntact))
			require.True(t, schemaIntact, "a failed guard must leave the account key schema intact")
			var bindings int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_key WHERE api_key_id=$1`, key).Scan(&bindings))
			if tc.bound {
				require.Equal(t, 1, bindings)
			} else {
				require.Zero(t, bindings)
			}
			_, err = conn.ExecContext(ctx, `UPDATE api_key SET revoked_at=now() WHERE id=$1`, key)
			require.NoError(t, err)
			_, err = rollback.ExecContext(ctx)
			require.NoError(t, err)
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT subject_kind,key_hash FROM api_key WHERE id=$1 AND revoked_at IS NOT NULL`, key).Scan(&kind, &storedHash))
			require.Equal(t, "user", kind)
			require.Equal(t, keyHash, storedHash)
			var tablesDropped bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass('hosted_account_key') IS NULL AND to_regclass('hosted_account_key_descendant') IS NULL`).Scan(&tablesDropped))
			require.True(t, tablesDropped)
		})
	}
}

func accountKeyRollbackScope(t *testing.T, conn *sql.DB) (org, user, account int64) {
	t.Helper()
	var root int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO organization(org_id,name) VALUES($1,'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO "user"(user_id,username,password_hash) VALUES($1,$2,'hash') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&user))
	_, err := conn.ExecContext(t.Context(), `INSERT INTO user_organization(user_id,organization_id,role_id) VALUES($1,$2,NULL)`, user, org)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
	return org, user, account
}

func waitForRollbackBlocker(t *testing.T, conn *sql.DB, waiter, blocker int, lockType string) {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var blocked bool
		err := conn.QueryRowContext(t.Context(), `SELECT $2 = ANY(pg_blocking_pids($1)) AND EXISTS (
 SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted AND locktype=$3
)`, waiter, blocker, lockType).Scan(&blocked)
		require.NoError(collect, err)
		require.True(collect, blocked, "migration must reach the database lock barrier")
	}, 10*time.Second, 10*time.Millisecond)
}

func TestAccountKeyRollbackWaitsForConcurrentCreator(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	org, user, account := accountKeyRollbackScope(t, conn)
	creator, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = creator.Rollback() }()
	var creatorPID, key int64
	require.NoError(t, creator.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&creatorPID))
	keyID, keyHash := uuid.NewString(), uuid.NewString()
	require.NoError(t, creator.QueryRowContext(ctx, `INSERT INTO api_key(key_id,name,prefix,key_hash,user_id,organization_id,subject_kind) VALUES($1,'Customer','test-prefix',$2,$3,$4,'account') RETURNING id`, keyID, keyHash, user, org).Scan(&key))
	_, err = creator.ExecContext(ctx, `INSERT INTO hosted_account_key(api_key_id,organization_id,account_id,capabilities) VALUES($1,$2,$3,1)`, key, org, account)
	require.NoError(t, err)
	rollback, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer rollback.Close()
	var rollbackPID int
	require.NoError(t, rollback.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&rollbackPID))
	down, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.down.sql")
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { _, err := rollback.ExecContext(ctx, string(down)); result <- err }()
	// On the old migration this waits at DROP, after its guard missed the
	// uncommitted key. The fixed migration must wait before the guard.
	waitForRollbackBlocker(t, conn, rollbackPID, int(creatorPID), "relation")
	require.NoError(t, creator.Commit())
	require.ErrorContains(t, <-result, "revoke all account API keys")
	var kind, storedHash string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT subject_kind,key_hash FROM api_key WHERE key_id=$1 AND revoked_at IS NULL`, keyID).Scan(&kind, &storedHash))
	require.Equal(t, "account", kind)
	require.Equal(t, keyHash, storedHash)
	var bound int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_key WHERE api_key_id=$1 AND account_id=$2`, key, account).Scan(&bound))
	require.Equal(t, 1, bound)
	_, err = conn.ExecContext(ctx, `UPDATE api_key SET revoked_at=now() WHERE id=$1`, key)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT subject_kind,key_hash FROM api_key WHERE id=$1 AND revoked_at IS NOT NULL`, key).Scan(&kind, &storedHash))
	require.Equal(t, "user", kind)
	require.Equal(t, keyHash, storedHash, "rollback retains only already-revoked credentials")
	up, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
}

// A rollback that has admitted only revoked keys must retain exclusion through
// conversion. A creator arriving later cannot commit an account credential in
// the old schema, even after rollback releases its lock.
func TestAccountKeyRollbackExcludesCreatorsThroughConversion(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	org, user, account := accountKeyRollbackScope(t, conn)
	var key int64
	keyHash := uuid.NewString()
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO api_key(key_id,name,prefix,key_hash,user_id,organization_id,subject_kind,revoked_at) VALUES($1,'Customer','test-prefix',$2,$3,$4,'account',now()) RETURNING id`, uuid.NewString(), keyHash, user, org).Scan(&key))
	_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_key(api_key_id,organization_id,account_id,capabilities) VALUES($1,$2,$3,1)`, key, org, account)
	require.NoError(t, err)
	// Pause the actual down script at credential conversion, using a test-only
	// trigger and a session lock held by a connection that touches no table.
	_, err = conn.ExecContext(ctx, `CREATE FUNCTION pause_account_key_conversion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.subject_kind='account' AND NEW.subject_kind='user' THEN
  PERFORM pg_advisory_xact_lock(173162, 1);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pause_account_key_conversion BEFORE UPDATE OF subject_kind ON api_key
FOR EACH ROW EXECUTE FUNCTION pause_account_key_conversion();`)
	require.NoError(t, err)
	barrier, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer barrier.Close()
	var barrierPID int
	require.NoError(t, barrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&barrierPID))
	_, err = barrier.ExecContext(ctx, `SELECT pg_advisory_lock(173162, 1)`)
	require.NoError(t, err)
	defer func() { _, _ = barrier.ExecContext(t.Context(), `SELECT pg_advisory_unlock(173162, 1)`) }()
	rollback, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer rollback.Close()
	var rollbackPID int
	require.NoError(t, rollback.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&rollbackPID))
	down, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.down.sql")
	require.NoError(t, err)
	rollbackResult := make(chan error, 1)
	go func() { _, err := rollback.ExecContext(ctx, string(down)); rollbackResult <- err }()
	waitForRollbackBlocker(t, conn, rollbackPID, barrierPID, "advisory")
	creator, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer creator.Close()
	var creatorPID int
	require.NoError(t, creator.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&creatorPID))
	lateKeyID := uuid.NewString()
	creatorResult := make(chan error, 1)
	go func() {
		_, err := creator.ExecContext(ctx, `INSERT INTO api_key(key_id,name,prefix,key_hash,user_id,organization_id,subject_kind) VALUES($1,'Late Customer','test-prefix',$2,$3,$4,'account')`, lateKeyID, uuid.NewString(), user, org)
		creatorResult <- err
	}()
	waitForRollbackBlocker(t, conn, creatorPID, rollbackPID, "relation")
	_, err = barrier.ExecContext(ctx, `SELECT pg_advisory_unlock(173162, 1)`)
	require.NoError(t, err)
	require.NoError(t, <-rollbackResult)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, <-creatorResult, &pgErr)
	require.Equal(t, "23514", pgErr.Code)
	require.Equal(t, "ck_api_key_subject", pgErr.ConstraintName)
	var lateKeys int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM api_key WHERE key_id=$1`, lateKeyID).Scan(&lateKeys))
	require.Zero(t, lateKeys)
	var kind, storedHash string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT subject_kind,key_hash FROM api_key WHERE id=$1 AND revoked_at IS NOT NULL`, key).Scan(&kind, &storedHash))
	require.Equal(t, "user", kind)
	require.Equal(t, keyHash, storedHash)
	up, err := migrations.Current.ReadFile("current/001006_internal_hosted_account_keys.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
}
