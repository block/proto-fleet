package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceActivityLabelsMigrationDownAndUp(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	downSQL, err := migrations.Migrations.ReadFile("000151_maintenance_activity_labels.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000151_maintenance_activity_labels.up.sql")
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err)
	var prior sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT activity_display_label('maintenance.ticket_created', CAST(NULL AS TEXT), NULLIF('', ''), '{}'::jsonb, '')`).Scan(&prior))
	require.False(t, prior.Valid)

	_, err = db.ExecContext(ctx, string(upSQL))
	require.NoError(t, err)
	labels := map[string]string{
		"maintenance.ticket_created": "Created repair ticket", "maintenance.ticket_updated": "Updated repair ticket",
		"maintenance.ticket_deleted": "Deleted repair ticket", "maintenance.ticket_bulk_update": "Bulk updated repair tickets",
		"maintenance.comment_created": "Added repair ticket comment", "maintenance.comment_deleted": "Deleted repair ticket comment",
		"inventory.part_created": "Created inventory part", "inventory.part_updated": "Updated inventory part",
		"inventory.part_deleted": "Deleted inventory part", "inventory.parts_imported": "Imported inventory parts",
	}
	for eventType, want := range labels {
		var got string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT activity_display_label($1, CAST(NULL AS TEXT), NULLIF('', ''), '{}'::jsonb, '')`, eventType).Scan(&got))
		require.Equal(t, want, got)
	}
}
