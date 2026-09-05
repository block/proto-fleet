package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

// The release channel migration must round-trip: down removes every object
// it created (including the membership views), and up restores them.
func TestReleaseChannelsMigrationDownAndUp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	downSQL, err := migrations.Migrations.ReadFile("000146_release_channels.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000146_release_channels.up.sql")
	require.NoError(t, err)

	objects := []string{"release_channel", "release_channel_target", "release_channel_firmware", "firmware_rollout", "firmware_rollout_device", "release_channel_match", "release_channel_member"}
	exists := func(name string) bool {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM pg_class WHERE relname = $1 AND relkind IN ('r', 'v')`, name).Scan(&n))
		return n == 1
	}
	for _, name := range objects {
		require.True(t, exists(name), "%s should exist after migrating up", name)
	}

	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err)
	for _, name := range objects {
		require.False(t, exists(name), "%s should be gone after migrating down", name)
	}

	_, err = db.ExecContext(ctx, string(upSQL))
	require.NoError(t, err)
	for _, name := range objects {
		require.True(t, exists(name), "%s should exist after migrating up again", name)
	}
	// The views are queryable on an empty schema.
	var members int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM release_channel_member`).Scan(&members))
	require.Equal(t, 0, members)
}
