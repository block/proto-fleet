package timescaledb_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// systemRuleFile is the system-monitoring overlay rules file relative to this package.
const systemRuleFile = "../../../monitoring/grafana/system-monitoring/proto-fleet-system-rules.yaml"

const (
	cpuMetric       = "fleet_system_cpu_used_percent"
	diskMetric      = "fleet_system_disk_used_percent"
	heartbeatMetric = "fleet_system_heartbeat"
)

// seedActiveOrg inserts a bare organization so it appears in fleet_active_organization, returning its id rendered as the view renders it.
func seedActiveOrg(t *testing.T, db *sql.DB, i int) string {
	t.Helper()
	slug := fmt.Sprintf("system-test-%d", i)
	var orgID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO organization (org_id, name)
		VALUES ($1, $1) RETURNING id`, slug).Scan(&orgID))
	return fmt.Sprint(orgID)
}

// seedDeletedOrg inserts a soft-deleted organization that fleet_active_organization must exclude.
func seedDeletedOrg(t *testing.T, db *sql.DB, i int) string {
	t.Helper()
	slug := fmt.Sprintf("system-test-deleted-%d", i)
	var orgID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		INSERT INTO organization (org_id, name, deleted_at)
		VALUES ($1, $1, now()) RETURNING id`, slug).Scan(&orgID))
	return fmt.Sprint(orgID)
}

// writeSystemSample lands one host metric sample at the given age. Host metrics carry no org label, so organization_id keeps its empty default.
func writeSystemSample(t *testing.T, db *sql.DB, metric string, value float64, age time.Duration) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO notification_metric_sample (time, metric, value)
		VALUES ($1, $2, $3)`, time.Now().Add(-age), metric, value)
	require.NoError(t, err)
}

// clearSystemSamples wipes metric samples so each subtest arranges its own window in the shared per-test database.
func clearSystemSamples(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `DELETE FROM notification_metric_sample`)
	require.NoError(t, err)
}

// TestSystemMonitoringHostCPUHighRule covers the representative threshold rule: the newest in-window sample decides for every live org at once, and soft-deleted orgs never get an instance.
func TestSystemMonitoringHostCPUHighRule(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadRuleSQLFrom(t, systemRuleFile, "Host CPU High", cpuMetric)

	orgA := seedActiveOrg(t, db, 0)
	orgB := seedActiveOrg(t, db, 1)
	deleted := seedDeletedOrg(t, db, 2)

	t.Run("breach fans out to every live org", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, cpuMetric, 95, 2*time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Equal(t, map[string]float64{orgA: 95, orgB: 95}, got)
		require.NotContains(t, got, deleted, "soft-deleted org must not get an instance")
	})

	t.Run("newest sample below threshold wins over an older breach", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, cpuMetric, 95, 5*time.Minute)
		writeSystemSample(t, db, cpuMetric, 50, time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Empty(t, got, "ORDER BY time DESC LIMIT 1 must pick the recovered sample even with an older breach in the window")
	})

	t.Run("no samples in the window returns no rows", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, cpuMetric, 95, 20*time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Empty(t, got, "an out-of-window breach must fall to the noData path")
	})
}

// TestSystemMonitoringActiveOrganizationView pins the fan-out contract: live orgs appear with text ids matching the notification_metric_sample organization_id label, soft-deleted orgs do not.
func TestSystemMonitoringActiveOrganizationView(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	active := seedActiveOrg(t, db, 0)
	deleted := seedDeletedOrg(t, db, 1)

	// Act
	rows, err := db.QueryContext(t.Context(), `
		SELECT organization_id, pg_typeof(organization_id)::text
		FROM fleet_active_organization`)
	require.NoError(t, err)
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var id, typ string
		require.NoError(t, rows.Scan(&id, &typ))
		got[id] = typ
	}
	require.NoError(t, rows.Err())

	// Assert
	require.Equal(t, map[string]string{active: "text"}, got)
	require.NotContains(t, got, deleted, "soft-deleted org must be excluded")
}

// TestSystemMonitoringFleetHeartbeatStaleRule covers heartbeat staleness: a fresh beat reads small for every live org, and a fleet that has never beaten reads 1e9 via COALESCE instead of vanishing into noData.
func TestSystemMonitoringFleetHeartbeatStaleRule(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadRuleSQLFrom(t, systemRuleFile, "Fleet Heartbeat Stale", heartbeatMetric)

	orgA := seedActiveOrg(t, db, 0)
	orgB := seedActiveOrg(t, db, 1)

	t.Run("fresh heartbeat reads a small staleness per org", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, heartbeatMetric, 1, time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Len(t, got, 2)
		require.Contains(t, got, orgA)
		require.Contains(t, got, orgB)
		require.Less(t, got[orgA], 300.0, "a fresh beat must stay below the staleness threshold")
		require.Less(t, got[orgB], 300.0)
	})

	t.Run("no heartbeat rows read as 1e9 per org", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Equal(t, map[string]float64{orgA: 1e9, orgB: 1e9}, got, "COALESCE must cover the never-beaten fleet")
	})
}

// TestSystemMonitoringHostDiskStaleRule covers the disk-staleness gate: it fires only while the heartbeat is fresh, so a dead collector stays the Fleet Heartbeat Stale rule's page.
func TestSystemMonitoringHostDiskStaleRule(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadRuleSQLFrom(t, systemRuleFile, "Host Disk Monitoring Stalled", diskMetric)

	orgA := seedActiveOrg(t, db, 0)
	orgB := seedActiveOrg(t, db, 1)

	t.Run("no disk samples accrue staleness from heartbeat history", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, heartbeatMetric, 1, 14*time.Minute)
		writeSystemSample(t, db, heartbeatMetric, 1, time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Len(t, got, 2)
		require.InDelta(t, 840, got[orgA], 30, "a never-reporting disk gauge must age from the first in-window heartbeat, not jump to a sentinel")
		require.InDelta(t, 840, got[orgB], 30)
	})

	t.Run("no disk samples on a fresh boot stay below the condition", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, heartbeatMetric, 1, time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Len(t, got, 2)
		require.Less(t, got[orgA], 720.0, "a just-booted collector must not start the pending timer immediately")
		require.Less(t, got[orgB], 720.0)
	})

	t.Run("fresh heartbeat with a stale disk sample fires per org", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, heartbeatMetric, 1, time.Minute)
		writeSystemSample(t, db, diskMetric, 42, 30*time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Len(t, got, 2)
		require.InDelta(t, 1800, got[orgA], 30, "a real reading must report its age, not the heartbeat-history fallback")
		require.Greater(t, got[orgB], 720.0)
	})

	t.Run("stale heartbeat suppresses the rule", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, heartbeatMetric, 1, 10*time.Minute)
		writeSystemSample(t, db, diskMetric, 42, 30*time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Empty(t, got, "a dead collector is the Fleet Heartbeat Stale rule's page, not this one")
	})

	t.Run("fresh heartbeat and fresh disk stays below the condition", func(t *testing.T) {
		// Arrange
		clearSystemSamples(t, db)
		writeSystemSample(t, db, heartbeatMetric, 1, time.Minute)
		writeSystemSample(t, db, diskMetric, 42, 2*time.Minute)

		// Act
		got := runRule(t, db, rawSQL)

		// Assert
		require.Len(t, got, 2)
		require.Less(t, got[orgA], 720.0)
		require.Less(t, got[orgB], 720.0)
	})
}
