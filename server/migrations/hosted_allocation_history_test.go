package migrations_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Tests of an earlier migration first remove the empty newer dependency.
func rollbackEmptyAllocationHistory(t *testing.T, conn *sql.DB) {
	t.Helper()
	rollbackEmptyHashboardAllocations(t, conn)
	summaryDown, err := migrations.Current.ReadFile("current/001044_internal_hosted_history_summaries.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(summaryDown))
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001031_internal_hosted_allocation_history.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
}

func TestAllocationHistoryMigrationProspectiveAndRollbackGuard(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	conn := database.DB
	ctx := t.Context()
	rollbackEmptyAllocationHistory(t, conn)
	var customer int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT $1,$2,id,1,'Customer',31 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), user.OrganizationID).Scan(&customer))
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id,updated_at) VALUES($1,5,$2,$2,$2,$3,now()-interval '10 days')`, user.OrganizationID, device.DatabaseID, customer)
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001031_internal_hosted_allocation_history.up.sql")
	require.NoError(t, err)
	before := time.Now().Add(-time.Second)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	var began, epoch time.Time
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT started_at FROM hosted_account_asset_interval WHERE client_id=$1`, customer).Scan(&began))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT started_at FROM hosted_account_history_epoch`).Scan(&epoch))
	require.Equal(t, epoch, began)
	require.True(t, began.After(before), "old ledger timestamp must not backfill ownership")
	down, err := migrations.Current.ReadFile("current/001031_internal_hosted_allocation_history.down.sql")
	require.NoError(t, err)
	rollback, err := conn.PrepareContext(ctx, string(down))
	require.NoError(t, err)
	defer rollback.Close()
	_, err = rollback.ExecContext(ctx)
	require.ErrorContains(t, err, "preserve established allocation intervals")
	_, err = conn.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=NULL,revision=revision+1 WHERE organization_id=$1`, user.OrganizationID)
	require.NoError(t, err)
	_, err = rollback.ExecContext(ctx)
	require.ErrorContains(t, err, "preserve established allocation intervals", "recall must not permit history destruction")
	var periods, open int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE ended_at IS NULL) FROM hosted_account_asset_interval`).Scan(&periods, &open))
	require.Equal(t, 1, periods)
	require.Zero(t, open)
	// Explicit fixture cleanup then proves the empty schema can round-trip.
	_, err = conn.ExecContext(ctx, `DELETE FROM hosted_account_asset_interval`)
	require.NoError(t, err)
	_, err = rollback.ExecContext(ctx)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(up))
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_asset_interval`).Scan(&periods))
	require.Zero(t, periods)
}

func TestAllocationHistoryMigrationFencesConcurrentRecall(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	conn := database.DB
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	rollbackEmptyAllocationHistory(t, conn)
	var customer int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT $1,$2,id,1,'Customer',31 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), user.OrganizationID).Scan(&customer))
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, user.OrganizationID, device.DatabaseID, customer)
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001031_internal_hosted_allocation_history.up.sql")
	require.NoError(t, err)
	barrier, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer barrier.Close()
	migration, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer migration.Close()
	writer, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer writer.Close()
	var migrationPID, writerPID int
	require.NoError(t, migration.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&migrationPID))
	require.NoError(t, writer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
	_, err = barrier.ExecContext(ctx, `SELECT pg_advisory_lock(2361030)`)
	require.NoError(t, err)
	// Pause after the seed, exactly where an unfenced recall could be lost.
	paused := strings.Replace(string(up), "CREATE FUNCTION record_hosted_account_asset_interval()", "SELECT pg_advisory_xact_lock(2361030);\nCREATE FUNCTION record_hosted_account_asset_interval()", 1)
	migrated := make(chan error, 1)
	go func() {
		tx, beginErr := migration.BeginTx(ctx, nil)
		if beginErr != nil {
			migrated <- beginErr
			return
		}
		defer tx.Rollback()
		if _, execErr := tx.ExecContext(ctx, paused); execErr != nil {
			migrated <- execErr
			return
		}
		migrated <- tx.Commit()
	}()
	defer func() {
		_, unlockErr := barrier.ExecContext(ctx, `SELECT pg_advisory_unlock(2361030)`)
		require.NoError(t, unlockErr)
		require.NoError(t, <-migrated)
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT wait_event='advisory' FROM pg_stat_activity WHERE pid=$1`, migrationPID).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "migration must reach the post-seed barrier")
	recalled := make(chan error, 1)
	go func() {
		_, err := writer.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=NULL,revision=revision+1 WHERE organization_id=$1`, user.OrganizationID)
		recalled <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := conn.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, migrationPID, writerPID).Scan(&blocked)
		return err == nil && blocked
	}, time.Second, 10*time.Millisecond, "recall must wait until history triggers are installed")
	_, err = barrier.ExecContext(ctx, `SELECT pg_advisory_unlock(2361030)`)
	require.NoError(t, err)
	require.NoError(t, <-recalled)
	var periods, open int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE ended_at IS NULL) FROM hosted_account_asset_interval WHERE client_id=$1`, customer).Scan(&periods, &open))
	require.Equal(t, 1, periods)
	require.Zero(t, open, "the recall after migration must close the seeded interval")
}

// Remove the newest dependent schema before testing a legacy migration.
func rollbackEmptyHashboardAllocations(t *testing.T, conn *sql.DB) {
	t.Helper()
	var activationInventory bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regprocedure('protect_activation_hashboard_owner()') IS NOT NULL`).Scan(&activationInventory))
	if activationInventory {
		source, err := migrations.Current.ReadFile("current/001098_internal_activation_pool_inventory.down.sql")
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(source))
		require.NoError(t, err, "remove empty activation inventory guards before historical rollback")
	}
	rollbackEmptyReportingLifecycle(t, conn)
	var exists bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('hosted_account_reporting_hour') IS NOT NULL`).Scan(&exists))
	if exists {
		hourDown, err := migrations.Current.ReadFile("current/001077_internal_hourly_reporting_evidence.down.sql")
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(hourDown))
		require.NoError(t, err, "remove only empty hourly reporting evidence before historical rollback")
	}
	var reportingInputs bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('hosted_reporting_inputs') IS NOT NULL`).Scan(&reportingInputs))
	if reportingInputs {
		source, err := migrations.Current.ReadFile("current/001071_internal_hosted_reporting_inputs.down.sql")
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(source))
		require.NoError(t, err, "remove only empty newer reporting inputs before historical rollback")
	}
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('hosted_account_reporting_evidence') IS NOT NULL`).Scan(&exists))
	if exists {
		evidenceDown, err := migrations.Current.ReadFile("current/001055_internal_hosted_reporting_evidence.down.sql")
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(evidenceDown))
		require.NoError(t, err)
	}
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('hosted_module_membership') IS NOT NULL`).Scan(&exists))
	if !exists {
		return
	}
	consumers, err := migrations.Current.ReadFile("current/001052_internal_hashboard_allocation_consumers.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(consumers))
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001049_internal_hashboard_allocations.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
}
