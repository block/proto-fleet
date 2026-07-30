package timescaledb_test

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ruleFile is the provisioned Grafana rules file relative to this package.
const ruleFile = "../../../monitoring/grafana/provisioning/alerting/proto-fleet-rules.yaml"

// loadRuleSQL returns the live rawSql for the named rule from the provisioning file, asserting it contains mustContain so tests run the exact deployed query, not a copy that can drift.
func loadRuleSQL(t *testing.T, title, mustContain string) string {
	return loadRuleSQLFrom(t, ruleFile, title, mustContain)
}

// loadRuleSQLFrom is loadRuleSQL for rules provisioned from another file, such as the system-monitoring overlay.
func loadRuleSQLFrom(t *testing.T, path, title, mustContain string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var doc struct {
		Groups []struct {
			Rules []struct {
				Title string `yaml:"title"`
				Data  []struct {
					Model struct {
						RawSQL string `yaml:"rawSql"`
					} `yaml:"model"`
				} `yaml:"data"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))

	for _, g := range doc.Groups {
		for _, r := range g.Rules {
			if r.Title == title {
				require.NotEmpty(t, r.Data, "rule has no data block")
				sql := r.Data[0].Model.RawSQL
				require.Contains(t, sql, mustContain)
				return sql
			}
		}
	}
	t.Fatalf("rule %q not found in %s", title, path)
	return ""
}

// loadIngestStalledRuleSQL extracts the live rawSql for the "Metric Ingest Stalled" rule.
func loadIngestStalledRuleSQL(t *testing.T) string {
	return loadRuleSQL(t, "Metric Ingest Stalled", "fleet_pollable_device_presence")
}

// seedOrg inserts organization -> discovered_device -> device -> device_pairing
// so the org appears in fleet_pollable_device_presence when status is a pollable
// pairing ('PAIRED'/'DEFAULT_PASSWORD'). Returns the organization's bigint id.
func seedOrg(t *testing.T, db *sql.DB, i int, status string) int64 {
	t.Helper()
	ctx := t.Context()
	slug := fmt.Sprintf("ingest-test-%d", i)

	var orgID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name)
		VALUES ($1, $1) RETURNING id`, slug).Scan(&orgID))

	var discID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO discovered_device (org_id, device_identifier, driver_name, ip_address, port, url_scheme)
		VALUES ($1, $2, 'antminer', '10.0.0.1', '80', 'http') RETURNING id`, orgID, slug).Scan(&discID))

	var deviceID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO device (device_identifier, mac_address, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		slug+"-dev", fmt.Sprintf("02:00:00:00:00:%02x", i), orgID, discID).Scan(&deviceID))

	_, err := db.ExecContext(ctx, `
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		VALUES ($1, $2::pairing_status_enum, now())`, deviceID, status)
	require.NoError(t, err)
	return orgID
}

// writeHeartbeat lands one fleet_telemetry_poll_total sample for orgID at the
// given age and materializes the continuous aggregate the rule reads.
func writeHeartbeat(t *testing.T, db *sql.DB, orgID int64, age time.Duration) {
	t.Helper()
	ctx := t.Context()
	_, err := db.ExecContext(ctx, `
		INSERT INTO notification_metric_sample (time, metric, organization_id, value)
		VALUES ($1, 'fleet_telemetry_poll_total', $2, 1)`,
		time.Now().Add(-age), fmt.Sprintf("%d", orgID))
	require.NoError(t, err)
	// NULL bounds refresh the whole eligible range; CALL must not run in a tx.
	_, err = db.ExecContext(ctx,
		`CALL refresh_continuous_aggregate('fleet_telemetry_poll_heartbeat', NULL, NULL)`)
	require.NoError(t, err)
}

// runRule executes the rule SQL and returns organization_id -> staleness_seconds.
func runRule(t *testing.T, db *sql.DB, rawSQL string) map[string]float64 {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), rawSQL)
	require.NoError(t, err)
	defer rows.Close()

	out := map[string]float64{}
	for rows.Next() {
		var org string
		var staleness float64
		require.NoError(t, rows.Scan(&org, &staleness))
		out[org] = staleness
	}
	require.NoError(t, rows.Err())
	return out
}

// TestMetricIngestStalledRule_PerOrg covers the multi-org behaviour the rule
// must get right: a healthy org stays Normal, an org with pollable miners but no
// heartbeat fires per-org (cold-start) even while a peer is healthy, and an org
// whose miners were removed is dropped from evaluation despite leftover stale
// heartbeat rows.
func TestMetricIngestStalledRule_PerOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadIngestStalledRuleSQL(t)

	healthy := seedOrg(t, db, 0, "PAIRED")   // pollable, fresh heartbeat
	coldStart := seedOrg(t, db, 1, "PAIRED") // pollable, no heartbeat
	removed := seedOrg(t, db, 2, "UNPAIRED") // not pollable, but stale heartbeat lingers

	writeHeartbeat(t, db, healthy, 2*time.Minute)
	writeHeartbeat(t, db, removed, 30*time.Minute)

	got := runRule(t, db, rawSQL)

	healthyOrg := fmt.Sprint(healthy)
	require.Contains(t, got, healthyOrg)
	require.Less(t, got[healthyOrg], 300.0, "healthy org should be below the staleness threshold")

	require.Contains(t, got, fmt.Sprint(coldStart))
	require.Equal(t, 1e9, got[fmt.Sprint(coldStart)], "org with no heartbeat must read as stale via COALESCE")

	require.NotContains(t, got, fmt.Sprint(removed), "non-pollable org's stale heartbeat must be suppressed")
	require.NotContains(t, got, "", "global sentinel must not appear while pollable miners exist")
}

// TestMetricIngestStalledRule_NoPollableMiners covers the fresh/emptied fleet:
// with no pollable miners anywhere the rule returns only the healthy sentinel,
// even when stale heartbeat rows from removed miners remain.
func TestMetricIngestStalledRule_NoPollableMiners(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadIngestStalledRuleSQL(t)

	removed := seedOrg(t, db, 0, "UNPAIRED")
	writeHeartbeat(t, db, removed, 30*time.Minute)

	got := runRule(t, db, rawSQL)
	require.Equal(t, map[string]float64{"": 0}, got, "expected only the healthy global sentinel")
}
