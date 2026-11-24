package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPoolRollbackIsAtomicWithoutOuterTransaction(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pool     bool
		revision bool
		profile  bool
	}{
		{name: "configuration only", pool: true},
		{name: "configuration with revision and profile", pool: true, revision: true, profile: true},
		{name: "unselected profile only", profile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := dbtest.GetTestDB(t)
			rollbackEmptyMiningGroups(t, conn)
			ctx := t.Context()
			var org, root, account int64
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Host') RETURNING id`, uuid.NewString()).Scan(&org))
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
			poolID, revisionID := uuid.NewString(), uuid.NewString()
			if tc.pool {
				_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, poolID, org, account)
				require.NoError(t, err)
			}
			if tc.revision {
				_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) VALUES($1,$2,$3,$4,1,'Choice','[{"url":"stratum+tcp://pool.example","username":"payout","worker_template":"","password_ciphertext":"encrypted","has_password":true}]')`, revisionID, org, account, poolID)
				require.NoError(t, err)
			}
			if tc.profile {
				var selected any
				if tc.revision {
					selected = revisionID
				}
				_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_profile(organization_id,account_id,revision_id) VALUES($1,$2,$3)`, org, account, selected)
				require.NoError(t, err)
			}

			down, err := migrations.Current.ReadFile("current/001007_internal_hosted_account_pools.down.sql")
			require.NoError(t, err)
			// Preparing rejects multiple top-level statements, preventing any
			// destructive DDL from running in a separate autocommit statement
			// after the refusal guard fails, as plain psql would allow.
			rollback, err := conn.PrepareContext(ctx, string(down))
			require.NoError(t, err, "rollback must be a single guarded SQL statement")
			defer rollback.Close()
			_, err = rollback.ExecContext(ctx)
			require.ErrorContains(t, err, "pool configuration data")

			var schemaIntact bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT
 to_regclass('hosted_account_pool') IS NOT NULL AND
 to_regclass('hosted_account_pool_revision') IS NOT NULL AND
 to_regclass('hosted_account_pool_profile') IS NOT NULL AND
 to_regprocedure('keep_hosted_account_pool_revision_immutable()') IS NOT NULL AND
 to_regprocedure('valid_hosted_account_pool_slots(jsonb)') IS NOT NULL`).Scan(&schemaIntact))
			require.True(t, schemaIntact, "a failed guard must preserve all pool tables and functions")
			if tc.pool {
				var stored string
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT pool_id FROM hosted_account_pool WHERE pool_id=$1`, poolID).Scan(&stored))
				require.Equal(t, poolID, stored)
			}
			if tc.revision {
				var stored, ciphertext string
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT revision_id,slots->0->>'password_ciphertext' FROM hosted_account_pool_revision WHERE revision_id=$1`, revisionID).Scan(&stored, &ciphertext))
				require.Equal(t, revisionID, stored)
				require.Equal(t, "encrypted", ciphertext)
			}
			if tc.profile {
				var selected string
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT COALESCE(revision_id,'') FROM hosted_account_pool_profile WHERE organization_id=$1 AND account_id=$2`, org, account).Scan(&selected))
				if tc.revision {
					require.Equal(t, revisionID, selected)
				} else {
					require.Empty(t, selected)
				}
			}

			_, err = conn.ExecContext(ctx, `DELETE FROM organization WHERE id=$1`, org)
			require.NoError(t, err, "pool immutability permits organization cleanup after parent deletion")
			_, err = rollback.ExecContext(ctx)
			require.NoError(t, err, "empty pool tables must permit standalone rollback")
			var schemaDropped bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT
 to_regclass('hosted_account_pool') IS NULL AND
 to_regclass('hosted_account_pool_revision') IS NULL AND
 to_regclass('hosted_account_pool_profile') IS NULL AND
 to_regprocedure('keep_hosted_account_pool_revision_immutable()') IS NULL AND
 to_regprocedure('valid_hosted_account_pool_slots(jsonb)') IS NULL`).Scan(&schemaDropped))
			require.True(t, schemaDropped)
			up, err := migrations.Current.ReadFile("current/001007_internal_hosted_account_pools.up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(up))
			require.NoError(t, err, "the pool migration must reapply after an empty rollback")
		})
	}
}

func TestPoolRollbackRefusesAWriterCommittedWhileWaitingForItsLock(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var org, root, account int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Host') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
	downConn, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer downConn.Close()
	var downPID int
	require.NoError(t, downConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	poolID := uuid.NewString()
	_, err = writer.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, poolID, org, account)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001007_internal_hosted_account_pools.down.sql")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := downConn.ExecContext(ctx, string(down))
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted AND mode='AccessExclusiveLock')`, downPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "rollback must wait for the uncommitted pool writer")
	require.NoError(t, writer.Commit())
	select {
	case err := <-done:
		require.ErrorContains(t, err, "pool configuration data")
	case <-ctx.Done():
		t.Fatal("rollback did not finish after the writer committed")
	}
	var stored string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT pool_id FROM hosted_account_pool WHERE pool_id=$1`, poolID).Scan(&stored))
	require.Equal(t, poolID, stored)
}
