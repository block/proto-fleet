package sqlstores_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertNotification writes a notification_history row with an explicit received_at so the
// notification_active_sync trigger populates notification_active.received_at deterministically;
// received_at is the column the freshness gate filters on (starts_at/ends_at fall back to it).
func insertNotification(t *testing.T, db *sql.DB, orgID int64, fingerprint, status string, receivedAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO notification_history
			(received_at, alert_name, status, fingerprint, organization_id)
		VALUES ($1, 'Metric Ingest Stalled', $2, $3, $4)`,
		receivedAt, status, fingerprint, orgID,
	)
	require.NoError(t, err)
}

func TestNotificationHistoryStore_ListActive_FreshnessGate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	insertNotification(t, db, orgID, "fresh-firing", "firing", now.Add(-30*time.Minute))
	insertNotification(t, db, orgID, "stale-firing", "firing", now.Add(-3*time.Hour))
	insertNotification(t, db, orgID, "resolved-alert", "resolved", now.Add(-30*time.Minute))

	active, err := store.ListActive(t.Context(), orgID, 50)
	require.NoError(t, err)

	fingerprints := make([]string, 0, len(active))
	for _, n := range active {
		fingerprints = append(fingerprints, n.Fingerprint)
	}
	assert.Contains(t, fingerprints, "fresh-firing")
	assert.NotContains(t, fingerprints, "stale-firing", "alert not re-asserted within the window should be hidden")
	assert.NotContains(t, fingerprints, "resolved-alert", "resolved alert should not be active")
}
