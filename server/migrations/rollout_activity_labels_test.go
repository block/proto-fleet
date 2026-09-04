package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestRolloutActivityLabelsMigrationDownAndUp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	downSQL, err := migrations.Migrations.ReadFile("000147_rollout_activity_labels.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000147_rollout_activity_labels.up.sql")
	require.NoError(t, err)

	const query = `SELECT activity_display_label('rollout_completed_with_failures', CAST(NULL AS TEXT), NULL,
		'{"channel_name":"Canary","model":"Rig","firmware_version":"2.0.0"}'::jsonb, '')`

	var label string
	require.NoError(t, db.QueryRowContext(ctx, query).Scan(&label))
	require.Equal(t, "Completed firmware update with failures: Canary Rig → 2.0.0", label)

	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err)
	var priorLabel sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, query).Scan(&priorLabel))
	require.False(t, priorLabel.Valid, "before 000147 the function has no rollout labels")

	_, err = db.ExecContext(ctx, string(upSQL))
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, query).Scan(&label))
	require.Equal(t, "Completed firmware update with failures: Canary Rig → 2.0.0", label)

	// Earlier labels still resolve through the preserved helper.
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('cli_reset_password', CAST(NULL AS TEXT), NULL, '{"target_username":"owner"}'::jsonb, '')`,
	).Scan(&label))
	require.Equal(t, "Break-glass password reset for owner", label)
}
