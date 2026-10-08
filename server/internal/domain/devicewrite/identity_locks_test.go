package devicewrite_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDeviceWriteIdentityLocksUseBoundedTransactionBudget(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	tx, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("serial:distinct-miner-%d", i)
	}
	require.NoError(t, sqlc.New(tx).LockDeviceWriteIdentities(t.Context(), keys))
	var locks int
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory'").Scan(&locks))
	require.Positive(t, locks)
	require.LessOrEqual(t, locks, 256, "identity admission must leave the shared lock pool available for other transactions")
	// More device admissions within the same transaction share the same budget.
	for i := range keys {
		keys[i] = fmt.Sprintf("serial:distinct-miner-%d", i+len(keys))
	}
	require.NoError(t, sqlc.New(tx).LockDeviceWriteIdentities(t.Context(), keys))
	var cumulative int
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory'").Scan(&cumulative))
	require.LessOrEqual(t, cumulative, 256)
}

func TestDeviceWriteIdentityLocksSerializeAdmissionAndDeletion(t *testing.T) {
	for _, admissionFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("admissionFirst=%t", admissionFirst), func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			service := devicewrite.NewService(database.DB)
			holder, err := database.DB.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = holder.Rollback() }()
			waiter, err := database.DB.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = waiter.Rollback() }()
			var waiterPID int
			require.NoError(t, waiter.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&waiterPID))
			admit := func(ctx context.Context, tx *sql.Tx) error {
				_, err := service.Reserve(db.WithTxQueries(ctx, sqlc.New(tx)), user.OrganizationID, device.DatabaseID)
				return err
			}
			deleteDevice := func(ctx context.Context, tx *sql.Tx) error {
				return devicewrite.PrepareDeletion(ctx, sqlc.New(tx), user.OrganizationID, []string{device.ID})
			}
			first, second := deleteDevice, admit
			if admissionFirst {
				first, second = admit, deleteDevice
			}
			require.NoError(t, first(t.Context(), holder))
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- second(ctx, waiter) }()
			// Observe the actual advisory wait, rather than mistaking a fence-row
			// wait or slow goroutine scheduling for identity mutual exclusion.
			require.Eventually(t, func() bool {
				var waiting bool
				err := database.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted)", waiterPID).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, holder.Commit())
			if admissionFirst {
				require.Error(t, <-finished, "deletion must reject the committed reservation")
			} else {
				require.NoError(t, <-finished)
				fence, err := sqlc.New(waiter).LockDeviceWriteFenceRecord(t.Context(), sqlc.LockDeviceWriteFenceRecordParams{DeviceID: device.DatabaseID, OrganizationID: user.OrganizationID})
				require.NoError(t, err)
				require.Equal(t, int64(2), fence.Epoch, "admission must see the committed deletion preparation epoch")
			}
		})
	}
}
