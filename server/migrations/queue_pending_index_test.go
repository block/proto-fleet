package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestQueuePendingIndexMigrationDownAndUp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	downSQL, err := migrations.Migrations.ReadFile("000152_queue_pending_created_index.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000152_queue_pending_created_index.up.sql")
	require.NoError(t, err)

	// Run directly on the connection: concurrent DDL cannot run in a transaction.
	for range 2 {
		_, err = db.ExecContext(ctx, string(downSQL))
		require.NoError(t, err)
		var absent bool
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT to_regclass('idx_queue_message_pending_created') IS NULL`).Scan(&absent))
		require.True(t, absent)

		_, err = db.ExecContext(ctx, string(upSQL))
		require.NoError(t, err)
		var valid, ready bool
		var keys, predicate string
		require.NoError(t, db.QueryRowContext(ctx, `
			SELECT indisvalid, indisready,
			       pg_get_indexdef(indexrelid, 1, true),
			       pg_get_expr(indpred, indrelid)
			FROM pg_index
			WHERE indexrelid = 'idx_queue_message_pending_created'::regclass
			  AND indrelid = 'queue_message'::regclass
			  AND indnkeyatts = 1
			  AND indnatts = 1
		`).Scan(&valid, &ready, &keys, &predicate))
		require.True(t, valid)
		require.True(t, ready)
		require.Equal(t, "created_at", keys)
		require.Equal(t, "(status = 'PENDING'::queue_status_enum)", predicate)

		// The existing per-device index is still needed by the anti-join.
		var existing bool
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT to_regclass('idx_queue_message_device_status_created') IS NOT NULL`).Scan(&existing))
		require.True(t, existing)
	}
}
