package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestCLIResetPasswordActivityLabelMigrationDownAndUp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	downSQL, err := migrations.Migrations.ReadFile("000142_cli_reset_password_activity_label.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000142_cli_reset_password_activity_label.up.sql")
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err)
	var priorLabel sql.NullString
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('cli_reset_password', CAST(NULL AS TEXT), NULL, '{"target_username":"owner"}'::jsonb, '')`,
	).Scan(&priorLabel))
	require.False(t, priorLabel.Valid)

	_, err = db.ExecContext(ctx, string(upSQL))
	require.NoError(t, err)
	var newLabel string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT activity_display_label('cli_reset_password', CAST(NULL AS TEXT), NULL, '{"target_username":"owner"}'::jsonb, '')`,
	).Scan(&newLabel))
	require.Equal(t, "Break-glass password reset for owner", newLabel)
}
