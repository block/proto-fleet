package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceAndInventoryActivityLabels(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
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
