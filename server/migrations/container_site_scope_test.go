package migrations_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestContainerPlacementClassAndLocks(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	conn := database.DB
	org := database.CreateSuperAdminUser().OrganizationID
	var building, rack int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO building(org_id,name) VALUES($1,'Building') RETURNING id`, org).Scan(&building))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO device_set(org_id,type,label) VALUES($1,'rack','Rack') RETURNING id`, org).Scan(&rack))
	begin := func(t *testing.T) *sql.Tx {
		t.Helper()
		tx, err := conn.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })
		_, err = tx.ExecContext(t.Context(), `SET LOCAL statement_timeout='5s'`)
		require.NoError(t, err)
		return tx
	}
	assertCode := func(t *testing.T, err error, code string) {
		t.Helper()
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, code, pgErr.Code)
	}
	insertRack := `INSERT INTO device_set_membership(org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES($1,$2,'rack',$3,$4)`

	for _, placement := range []string{"building", "rack"} {
		t.Run("reclassification rejects "+placement, func(t *testing.T) {
			device := database.CreateDevice(org, "proto")
			if placement == "building" {
				_, err = conn.ExecContext(t.Context(), `UPDATE device SET building_id=$1 WHERE id=$2`, building, device.DatabaseID)
			} else {
				_, err = conn.ExecContext(t.Context(), insertRack, org, rack, device.DatabaseID, device.ID)
			}
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, device.ID)
			assertCode(t, err, "23514")
			// Ordinary discovery refreshes must not acquire a device lock when
			// classification is unchanged, even for a placed conventional miner.
			locker := begin(t)
			_, err = locker.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, device.DatabaseID)
			require.NoError(t, err)
			refresh := begin(t)
			_, err = refresh.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=device_class WHERE device_identifier=$1`, device.ID)
			require.NoError(t, err)
			require.NoError(t, refresh.Commit())
		})
	}

	t.Run("rack placement is compatible with foreign key locks", func(t *testing.T) {
		device := database.CreateDevice(org, "proto")
		first, second := begin(t), begin(t)
		for _, tx := range []*sql.Tx{first, second} {
			_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR KEY SHARE`, device.DatabaseID)
			require.NoError(t, err)
		}
		_, err = second.ExecContext(t.Context(), insertRack, org, rack, device.DatabaseID, device.ID)
		require.NoError(t, err, "must not upgrade KEY SHARE to FOR UPDATE and block another membership writer")
		require.NoError(t, second.Commit())
	})

	for _, placement := range []string{"building", "rack"} {
		t.Run("classification racing "+placement, func(t *testing.T) {
			device := database.CreateDevice(org, "proto")
			placementTx, classTx := begin(t), begin(t)
			var placementPID, classPID int
			require.NoError(t, placementTx.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&placementPID))
			require.NoError(t, classTx.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&classPID))
			_, err = placementTx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR NO KEY UPDATE`, device.DatabaseID)
			require.NoError(t, err)
			classified := make(chan error, 1)
			go func() {
				_, classErr := classTx.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, device.ID)
				classified <- classErr
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				queryErr := conn.QueryRowContext(t.Context(), `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, placementPID, classPID).Scan(&blocked)
				return queryErr == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "classification must hold discovery and wait for placement's device lock")
			if placement == "building" {
				_, err = placementTx.ExecContext(t.Context(), `UPDATE device SET building_id=$1 WHERE id=$2`, building, device.DatabaseID)
			} else {
				_, err = placementTx.ExecContext(t.Context(), insertRack, org, rack, device.DatabaseID, device.ID)
			}
			assertCode(t, err, "40001")
			require.NoError(t, placementTx.Rollback())
			require.NoError(t, <-classified)
			require.NoError(t, classTx.Commit())
			// A complete placement retry observes the committed classification.
			if placement == "building" {
				_, err = conn.ExecContext(t.Context(), `UPDATE device SET building_id=$1 WHERE id=$2`, building, device.DatabaseID)
			} else {
				_, err = conn.ExecContext(t.Context(), insertRack, org, rack, device.DatabaseID, device.ID)
			}
			assertCode(t, err, "23514")
		})
	}

	t.Run("committed rack membership blocks waiting classification", func(t *testing.T) {
		device := database.CreateDevice(org, "proto")
		placementTx, classTx := begin(t), begin(t)
		_, err = placementTx.ExecContext(t.Context(), insertRack, org, rack, device.DatabaseID, device.ID)
		require.NoError(t, err)
		// Classification starts while the membership is still invisible to it.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		var placementPID, classPID int
		require.NoError(t, placementTx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&placementPID))
		require.NoError(t, classTx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&classPID))
		classified := make(chan error, 1)
		go func() {
			_, classErr := classTx.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, device.ID)
			classified <- classErr
		}()
		require.Eventually(t, func() bool {
			var blocked bool
			queryErr := conn.QueryRowContext(ctx, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, placementPID, classPID).Scan(&blocked)
			return queryErr == nil && blocked
		}, 3*time.Second, 10*time.Millisecond)
		require.NoError(t, placementTx.Commit())
		assertCode(t, <-classified, "23514")
	})
}
