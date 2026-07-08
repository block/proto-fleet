package sqlstores_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
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

// TestNotificationHistoryStore_InsertBatch_Chunks persists a multi-chunk batch and checks all rows land, org-less rows keep NULL org, and jsonb/timestamps round-trip.
func TestNotificationHistoryStore_InsertBatch_Chunks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	startsAt := time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Second)

	// 2100 rows spans three chunks (maxBatchRows = 1000), exercising the chunk boundary.
	const orgScoped = 2100
	const orgless = 3
	notifs := make([]*notificationhistory.Notification, 0, orgScoped+orgless)
	for i := range orgScoped {
		notifs = append(notifs, &notificationhistory.Notification{
			AlertName:      "Device Offline",
			Status:         "firing",
			Severity:       "critical",
			Fingerprint:    fmt.Sprintf("fp-%d", i),
			OrganizationID: &orgID,
			DeviceID:       fmt.Sprintf("device-%d", i),
			StartsAt:       &startsAt,
			Labels:         map[string]string{"device_id": fmt.Sprintf("device-%d", i)},
			Annotations:    map[string]string{"summary": "down"},
		})
	}
	for i := range orgless {
		// Unscoped self-monitoring alerts carry a nil org and must persist as NULL.
		notifs = append(notifs, &notificationhistory.Notification{
			AlertName:   "Metric Ingest Stalled",
			Status:      "firing",
			Fingerprint: fmt.Sprintf("internal-%d", i),
		})
	}

	require.NoError(t, store.InsertBatch(t.Context(), notifs))

	var gotScoped, gotNull int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM notification_history WHERE organization_id = $1`, orgID).Scan(&gotScoped))
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM notification_history WHERE organization_id IS NULL`).Scan(&gotNull))
	assert.Equal(t, orgScoped, gotScoped, "every org-scoped row in a multi-chunk batch persists")
	assert.Equal(t, orgless, gotNull, "org-less rows persist with NULL organization_id")

	// One row's jsonb + timestamp round-trip through jsonb_to_recordset.
	var gotStarts time.Time
	var gotLabel string
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT starts_at, labels->>'device_id' FROM notification_history WHERE fingerprint = 'fp-0'`).
		Scan(&gotStarts, &gotLabel))
	assert.True(t, startsAt.Equal(gotStarts), "starts_at round-trips: want %s got %s", startsAt, gotStarts)
	assert.Equal(t, "device-0", gotLabel, "labels jsonb round-trips")
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
