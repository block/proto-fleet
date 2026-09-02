package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
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

	const metadata = `{"lane_name":"Canary","model":"Rig","firmware_version":"1.4.4"}`

	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err)
	var priorLabel sql.NullString
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('rollout_review_ready', CAST(NULL AS TEXT), NULL, $1::jsonb, '')`, metadata,
	).Scan(&priorLabel))
	require.False(t, priorLabel.Valid)

	_, err = db.ExecContext(ctx, string(upSQL))
	require.NoError(t, err)
	var label string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('rollout_review_ready', CAST(NULL AS TEXT), NULL, $1::jsonb, '')`, metadata,
	).Scan(&label))
	require.Equal(t, "Firmware rollout ready for review: Canary Rig → 1.4.4", label)

	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('rollout_aborted', CAST(NULL AS TEXT), NULL, '{}'::jsonb, '')`,
	).Scan(&label))
	require.Equal(t, "Aborted firmware rollout", label)

	// Unrelated event types still resolve through the previous function.
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('cli_reset_password', CAST(NULL AS TEXT), NULL, '{"target_username":"owner"}'::jsonb, '')`,
	).Scan(&label))
	require.Equal(t, "Break-glass password reset for owner", label)
}
