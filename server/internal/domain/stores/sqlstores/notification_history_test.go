package sqlstores_test

import (
	"database/sql"
	"fmt"
	"strings"
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

func TestNotificationHistoryStore_ListExcludesResolvedRows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	insertNotification(t, db, orgID, "firing-alert", "firing", now.Add(-time.Minute))
	insertNotification(t, db, orgID, "resolved-alert", "resolved", now)

	history, err := store.List(t.Context(), orgID, nil, 50)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "firing-alert", history[0].Fingerprint)

	var storedRows int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM notification_history WHERE organization_id = $1`, orgID).Scan(&storedRows))
	assert.Equal(t, 2, storedRows, "resolution rows remain stored for active-alert lifecycle state")
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

func insertDeviceAlert(t *testing.T, db *sql.DB, orgID int64, alertName, ruleGroup, deviceID, summary string, receivedAt time.Time) {
	t.Helper()
	insertAlert(t, db, orgID, alertName, ruleGroup, deviceID, alertName+"-"+deviceID, summary, receivedAt)
}

func insertAlert(
	t *testing.T, db *sql.DB, orgID int64, alertName, ruleGroup, deviceID, fingerprint, summary string, receivedAt time.Time,
) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO notification_history
			(received_at, alert_name, status, severity, rule_group, fingerprint, organization_id, device_id, template, summary, starts_at)
		VALUES ($1, $2, 'firing', 'critical', $3, $4, $5, $6, 'offline', $7, $1)`,
		receivedAt, alertName, ruleGroup, fingerprint, orgID, deviceID, summary,
	)
	require.NoError(t, err)
}

func TestNotificationHistoryStore_ListActiveGroups_RollsUpPerAlert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	for i := range 3 {
		insertDeviceAlert(t, db, orgID, "Device Offline", "defaults", fmt.Sprintf("device-%d", i),
			fmt.Sprintf("device-%d is offline", i), now.Add(time.Duration(-10+i)*time.Minute))
	}
	insertDeviceAlert(t, db, orgID, "Device Temperature High", "defaults", "device-9", "too hot", now.Add(-5*time.Minute))
	// Stale and resolved rows are excluded, exactly as in ListActive.
	insertDeviceAlert(t, db, orgID, "Device Offline", "defaults", "device-stale", "stale", now.Add(-3*time.Hour))
	insertNotification(t, db, orgID, "resolved-alert", "resolved", now.Add(-30*time.Minute))

	groups, err := store.ListActiveGroups(t.Context(), orgID, 50)
	require.NoError(t, err)
	require.Len(t, groups, 2, "one row per alert, not per miner")

	// Widest blast radius first.
	assert.Equal(t, "Device Offline", groups[0].AlertName)
	assert.Equal(t, int64(3), groups[0].DeviceCount)
	assert.Equal(t, int64(3), groups[0].AlertCount)
	// Earliest start across the group: how long the alert has been firing somewhere in the fleet.
	assert.WithinDuration(t, now.Add(-10*time.Minute), groups[0].FirstStartedAt, time.Minute)

	assert.Equal(t, "Device Temperature High", groups[1].AlertName)
	assert.Equal(t, int64(1), groups[1].DeviceCount)
}

// A group with no miners has no per-miner drill-in to describe it, so the rollup carries one summary; a group
// with miners leaves its free text on the drill-in rows, which are gated on miner:read.
func TestNotificationHistoryStore_ListActiveGroups_SummarizesOnlyDeviceLessGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	insertAlert(t, db, orgID, "Curtailment Source Unreachable", "curtailment", "", "source-a",
		"maestro-a is unreachable", now.Add(-10*time.Minute))
	insertAlert(t, db, orgID, "Curtailment Source Unreachable", "curtailment", "", "source-b",
		"maestro-b is unreachable", now.Add(-2*time.Minute))
	insertDeviceAlert(t, db, orgID, "Device Offline", "defaults", "device-1", "device-1 is offline", now.Add(-5*time.Minute))
	// The header polls this rollup, so a runaway interpolation must not ship a whole TEXT column per group.
	insertAlert(t, db, orgID, "Runaway Summary", "curtailment", "", "source-c",
		strings.Repeat("x", 900), now.Add(-time.Minute))

	groups, err := store.ListActiveGroups(t.Context(), orgID, 50)
	require.NoError(t, err)
	require.Len(t, groups, 3)

	byName := make(map[string]notificationhistory.ActiveAlertGroup, len(groups))
	for _, g := range groups {
		byName[g.AlertName] = g
	}

	sourceGroup := byName["Curtailment Source Unreachable"]
	assert.Equal(t, int64(0), sourceGroup.DeviceCount)
	assert.Equal(t, int64(2), sourceGroup.AlertCount)
	// The newest instance, so the summary names the source that most recently went unreachable.
	assert.Equal(t, "maestro-b is unreachable", sourceGroup.Summary)
	// Carried unredacted: the handler, not the store, decides whether the template lets that text out.
	assert.Equal(t, "offline", sourceGroup.Template)

	assert.Empty(t, byName["Device Offline"].Summary, "a group with miners reports no free text here")
	assert.Len(t, byName["Runaway Summary"].Summary, 500)
}

// The trigger leaves an oversized label out of the active view (see 000136), and a real alert inserted alongside
// it still lands, so a forged label can't hide the fleet's state.
func TestNotificationActiveSync_SkipsOversizedIndexedLabels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)

	oversized := strings.Repeat("a", 4096)
	insertDeviceAlert(t, db, orgID, oversized, oversized, "device-1", "down", time.Now())
	// device_id is indexed raw by the rollup (000139), under a bound of its own.
	insertDeviceAlert(t, db, orgID, "MinerOffline", "proto-fleet-defaults", oversized, "down", time.Now())
	insertDeviceAlert(t, db, orgID, "MinerOffline", "proto-fleet-defaults", "device-2", "down", time.Now())

	rows, err := store.ListActive(t.Context(), orgID, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "MinerOffline", rows[0].AlertName)
	assert.Equal(t, "device-2", rows[0].DeviceID)

	var historyCount int
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM notification_history
		 WHERE LENGTH(alert_name) > 190 OR LENGTH(device_id) > 255`).Scan(&historyCount))
	assert.Equal(t, 2, historyCount, "history keeps the events the active view leaves out")
}

// The fallback key joins on chr(31), so a label carrying one hashes onto another identity: name "A<US>B" in
// group "C" lands on the key of name "A" in group "B<US>C". The trigger skips both rather than let one win.
func TestNotificationActiveSync_SkipsSeparatorBearingLabels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	// Fingerprintless, so these take the label-derived key the separator would make ambiguous.
	insertAlert(t, db, orgID, "Device Offline\x1fuser-rules", "defaults", "device-1", "", "forged", now)
	insertAlert(t, db, orgID, "Device Offline", "user-rules\x1fdefaults", "device-1", "", "forged", now)
	insertAlert(t, db, orgID, "Device Offline", "user-rules", "device-1", "", "real", now)

	rows, err := store.ListActive(t.Context(), orgID, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "real", rows[0].Summary)
}

// An empty fingerprint (which Grafana never sends) exercises the label-derived fallback key. It has to carry
// rule_group: on 000094's name-and-device key, two same-named rules on one device overwrote each other.
func TestNotificationActiveSync_FingerprintlessKeyIncludesRuleGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	insertAlert(t, db, orgID, "Device Offline", "user-rules", "device-1", "", "down per user rule",
		now.Add(-2*time.Minute))
	insertAlert(t, db, orgID, "Device Offline", "defaults", "device-1", "", "down per default rule",
		now.Add(-time.Minute))

	groups, err := store.ListActiveGroups(t.Context(), orgID, 50)
	require.NoError(t, err)
	require.Len(t, groups, 2, "one name in two rule groups is two rules, not one overwriting the other")
	for _, g := range groups {
		assert.Equal(t, int64(1), g.DeviceCount)
	}
	assert.ElementsMatch(t,
		[]string{"user-rules", "defaults"},
		[]string{groups[0].RuleGroup, groups[1].RuleGroup},
	)
}

// An alert whose rule carries no group label rolls up under "", so its drill-in must filter on "" exactly —
// treating that as "any group" would list another group's miners as this alert's blast radius.
func TestNotificationHistoryStore_ListActiveByAlert_EmptyRuleGroupMatchesExactly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	insertDeviceAlert(t, db, orgID, "Device Offline", "", "device-ungrouped", "down", now)
	insertDeviceAlert(t, db, orgID, "Device Offline", "defaults", "device-grouped", "down", now)

	ungrouped, err := store.ListActiveByAlert(t.Context(), orgID, notificationhistory.ActiveAlertFilter{
		AlertName: "Device Offline", RuleGroup: "", AfterKey: "", Limit: 50,
	})
	require.NoError(t, err)
	require.Len(t, ungrouped, 1)
	assert.Equal(t, "device-ungrouped", ungrouped[0].DeviceID)

	grouped, err := store.ListActiveByAlert(t.Context(), orgID, notificationhistory.ActiveAlertFilter{
		AlertName: "Device Offline", RuleGroup: "defaults", AfterKey: "", Limit: 50,
	})
	require.NoError(t, err)
	require.Len(t, grouped, 1)
	assert.Equal(t, "device-grouped", grouped[0].DeviceID)
}

func TestNotificationHistoryStore_ListActiveByAlert_PagesOneAlert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	db := testContext.DatabaseService.DB
	orgID := testContext.DatabaseService.CreateSuperAdminUser().OrganizationID
	store := sqlstores.NewSQLNotificationHistoryStore(db)
	now := time.Now()

	devices := make([]string, 0, 5)
	for i := range 5 {
		device := fmt.Sprintf("device-%d", i)
		devices = append(devices, device)
		insertDeviceAlert(t, db, orgID, "Device Offline", "defaults", device, "down", now.Add(-time.Minute))
	}
	insertDeviceAlert(t, db, orgID, "Device Temperature High", "defaults", "device-9", "too hot", now.Add(-time.Minute))

	first, err := store.ListActiveByAlert(t.Context(), orgID, notificationhistory.ActiveAlertFilter{
		AlertName: "Device Offline", RuleGroup: "defaults", AfterKey: "", Limit: 3,
	})
	require.NoError(t, err)
	require.Len(t, first, 3, "only the requested alert's instances, capped at the page size")
	for _, n := range first {
		assert.Equal(t, "Device Offline", n.AlertName)
	}

	// Alertmanager re-asserts the whole firing group between the two page reads, rewriting every row's
	// history_id: a cursor on that id would lift the unread rows above it and drop them from the second page.
	for _, device := range devices {
		insertDeviceAlert(t, db, orgID, "Device Offline", "defaults", device, "down", now)
	}

	cursor := first[len(first)-1].AlertKey
	second, err := store.ListActiveByAlert(t.Context(), orgID, notificationhistory.ActiveAlertFilter{
		AlertName: "Device Offline", RuleGroup: "defaults", AfterKey: cursor, Limit: 3,
	})
	require.NoError(t, err)
	require.Len(t, second, 2, "the keyset cursor continues past the first page without repeating it")

	paged := make([]string, 0, len(devices))
	for _, n := range append(first, second...) {
		assert.NotEmpty(t, n.AlertKey, "the page cursor is read off the row")
		paged = append(paged, n.DeviceID)
	}
	assert.ElementsMatch(t, devices, paged, "every affected miner is paged exactly once")

	// A mismatched rule group matches nothing, so a name reused across groups stays separated.
	otherGroup, err := store.ListActiveByAlert(t.Context(), orgID, notificationhistory.ActiveAlertFilter{
		AlertName: "Device Offline", RuleGroup: "other", AfterKey: "", Limit: 50,
	})
	require.NoError(t, err)
	assert.Empty(t, otherGroup)
}
