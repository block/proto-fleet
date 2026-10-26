package migrations_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Earlier schema rollback tests must first remove mining groups, which depend
// on hashboard installations, hosted pool revisions, and device write attempts.
func rollbackEmptyMiningGroups(t *testing.T, conn *sql.DB) {
	t.Helper()
	down, err := migrations.Current.ReadFile("current/001039_internal_mining_groups.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
}

func miningGroupRollbackFixture(t *testing.T) (*sql.DB, int64, int64) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	_, err = database.DB.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, device.ID)
	require.NoError(t, err)
	_, err = sqlc.New(database.DB).AdvanceContainerInventory(t.Context(), sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: device.ID, ObservedAt: time.Now()})
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id) VALUES($1,$2)`, device.DatabaseID, user.OrganizationID)
	require.NoError(t, err)
	return database.DB, user.OrganizationID, device.DatabaseID
}

func TestMiningGroupRollbackPreservesControllerState(t *testing.T) {
	for _, kind := range []string{"pending", "applying", "blocked", "bindings", "intent"} {
		t.Run(kind, func(t *testing.T) {
			conn, org, device := miningGroupRollbackFixture(t)
			ctx := t.Context()
			switch kind {
			case "pending", "applying", "blocked":
				_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status=$1 WHERE device_id=$2`, kind, device)
				require.NoError(t, err)
			case "bindings":
				_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status='complete',bindings='{"fleet-group":{"config_id":"revision"}}' WHERE device_id=$1`, device)
				require.NoError(t, err)
			case "intent":
				var tank, module, board, installation, account int64
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, device).Scan(&tank))
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'module',now(),now()) RETURNING id`, tank).Scan(&module))
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, org).Scan(&board))
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,now(),now()) RETURNING id`, board, module).Scan(&installation))
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&account))
				pool, revision := uuid.NewString(), uuid.NewString()
				_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, pool, org, account)
				require.NoError(t, err)
				_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) VALUES($1,$2,$3,$4,1,'Pools','[{"url":"stratum+tcp://pool.example","username":"worker","worker_template":"","password_ciphertext":"encrypted","has_password":true}]')`, revision, org, account, pool)
				require.NoError(t, err)
				_, err = conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, device)
				require.NoError(t, err)
				_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_pool_intent(organization_id,hashboard_id,device_id,installation_id,mac_address,account_id,pool_revision_id,operation_id) VALUES($1,$2,$3,$4,'02:00:00:00:00:01',$5,$6,$7)`, org, board, device, installation, account, revision, uuid.New())
				require.NoError(t, err)
			}
			down, err := migrations.Current.ReadFile("current/001039_internal_mining_groups.down.sql")
			require.NoError(t, err)
			// Preparing requires a single statement: no DROP can execute separately
			// after a refusal, even when a caller does not wrap it in a transaction.
			rollback, err := conn.PrepareContext(ctx, string(down))
			require.NoError(t, err)
			defer rollback.Close()
			_, err = rollback.ExecContext(ctx)
			require.ErrorContains(t, err, "mining group")
			var intact bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass('mining_group_request') IS NOT NULL AND to_regclass('hashboard_pool_intent') IS NOT NULL AND to_regclass('container_mining_group_state') IS NOT NULL AND EXISTS(SELECT 1 FROM permission WHERE key='miner:activate_mining_groups')`).Scan(&intact))
			require.True(t, intact)
			var status, bindings string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT status,bindings::text FROM container_mining_group_state WHERE device_id=$1`, device).Scan(&status, &bindings))
			if kind == "bindings" {
				require.Equal(t, "complete", status)
				require.Contains(t, bindings, "fleet-group")
			} else if kind == "intent" {
				var count int
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hashboard_pool_intent WHERE device_id=$1`, device).Scan(&count))
				require.Equal(t, 1, count)
			} else {
				require.Equal(t, kind, status)
			}
			_, err = conn.ExecContext(ctx, `DELETE FROM hashboard_pool_intent; DELETE FROM container_mining_group_state`)
			require.NoError(t, err)
			_, err = rollback.ExecContext(ctx)
			require.NoError(t, err, "empty state must permit rollback")
			up, err := migrations.Current.ReadFile("current/001039_internal_mining_groups.up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(up))
			require.NoError(t, err, "mining group migration must reapply")
		})
	}
}

func TestMiningGroupRollbackRefusesJournalCommittedWhileWaitingForLock(t *testing.T) {
	conn, _, device := miningGroupRollbackFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	downConn, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer downConn.Close()
	var downPID int
	require.NoError(t, downConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.ExecContext(ctx, `UPDATE container_mining_group_state SET status='pending' WHERE device_id=$1`, device)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001039_internal_mining_groups.down.sql")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := downConn.ExecContext(ctx, string(down))
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted AND mode='AccessExclusiveLock')`, downPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	select {
	case err := <-done:
		require.ErrorContains(t, err, "mining group")
	case <-ctx.Done():
		t.Fatal("rollback did not finish after the writer committed")
	}
	var status string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM container_mining_group_state WHERE device_id=$1`, device).Scan(&status))
	require.Equal(t, "pending", status)
}

func TestMiningGroupTransferRecoveryRollback(t *testing.T) {
	for _, test := range []struct {
		status   string
		failures int
		refused  bool
	}{
		{"idle", 0, false}, {"pending", 1, true}, {"applying", 1, true}, {"blocked", 1, true}, {"failed", 3, true}, {"complete", 3, false},
	} {
		t.Run(test.status, func(t *testing.T) {
			conn, _, device := miningGroupRollbackFixture(t)
			_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status=$1,transfer_failure_count=$2 WHERE device_id=$3`, test.status, test.failures, device)
			require.NoError(t, err)
			down, err := migrations.Current.ReadFile("current/001039_internal_mining_groups.down.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(down))
			if test.refused {
				require.ErrorContains(t, err, "recovery state")
				var count int
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT transfer_failure_count FROM container_mining_group_state WHERE device_id=$1`, device).Scan(&count))
				require.Equal(t, test.failures, count)
			} else {
				require.NoError(t, err)
				up, err := migrations.Current.ReadFile("current/001039_internal_mining_groups.up.sql")
				require.NoError(t, err)
				_, err = conn.ExecContext(t.Context(), string(up))
				require.NoError(t, err)
			}
		})
	}
}
