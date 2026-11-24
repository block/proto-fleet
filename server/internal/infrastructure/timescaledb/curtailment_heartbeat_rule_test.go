package timescaledb_test

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const runFleetScript = "../../../../deployment-files/run-fleet.sh"

// TestCurtailmentReconcilerStaleRule fires for every live organization while the
// singleton heartbeat is missing or old, and skips soft-deleted organizations.
func TestCurtailmentReconcilerStaleRule(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	rawSQL := loadRuleSQL(t, "Curtailment Reconciler Stale", "curtailment_reconciler_heartbeat")

	var live, deleted string
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name) VALUES ($1, 'heartbeat-live') RETURNING id::text`,
		uuid.NewString()).Scan(&live))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name, deleted_at) VALUES ($1, 'heartbeat-deleted', NOW()) RETURNING id::text`,
		uuid.NewString()).Scan(&deleted))
	setHeartbeat := func(age time.Duration) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO curtailment_reconciler_heartbeat (id, last_tick_at, last_tick_uuid)
			VALUES (1, $1, $2)
			ON CONFLICT (id) DO UPDATE SET last_tick_at = EXCLUDED.last_tick_at`,
			time.Now().Add(-age), uuid.New())
		require.NoError(t, err)
	}

	_, err := db.ExecContext(ctx, `DELETE FROM curtailment_reconciler_heartbeat`)
	require.NoError(t, err)
	got := runRule(t, db, rawSQL)
	require.Equal(t, 1e9, got[live], "a missing heartbeat must read as stale")
	require.NotContains(t, got, deleted, "soft-deleted organizations must not receive alert instances")

	setHeartbeat(5 * time.Second)
	got = runRule(t, db, rawSQL)
	require.Less(t, got[live], 60.0, "a fresh heartbeat must stay below the threshold")

	setHeartbeat(5 * time.Minute)
	got = runRule(t, db, rawSQL)
	require.Greater(t, got[live], 60.0, "an old heartbeat must exceed the threshold")
}

// TestPackagedRulesRunUnderGrafanaRoleGrants runs every standalone rule as a role
// holding only the grants run-fleet.sh gives the Grafana datasource, so a rule
// that reads an ungranted relation fails here instead of erroring in production.
func TestPackagedRulesRunUnderGrafanaRoleGrants(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	role := "grafana_rules_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := db.ExecContext(ctx, "CREATE ROLE "+role+" NOLOGIN NOINHERIT")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), "DROP OWNED BY "+role) //nolint:usetesting // Cleanup runs after the test context is canceled.
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), "DROP ROLE "+role) //nolint:usetesting // Cleanup runs after the test context is canceled.
		require.NoError(t, err)
	})
	for _, grant := range grafanaRoleGrants(t) {
		_, err := db.ExecContext(ctx, strings.ReplaceAll(grant, "${grafana_user}", role))
		require.NoError(t, err, grant)
	}

	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer func() {
		_, err := conn.ExecContext(context.Background(), "RESET ROLE") //nolint:usetesting // Reset pooled session state even after a failed assertion.
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}()
	_, err = conn.ExecContext(ctx, "SET ROLE "+role)
	require.NoError(t, err)

	for _, path := range []string{ruleFile, systemRuleFile} {
		for title, rawSQL := range packagedRuleSQL(t, path) {
			requireQueryRuns(t, conn, rawSQL, path+": "+title+" must run under the Grafana role grants")
		}
	}
}

func requireQueryRuns(t *testing.T, conn *sql.Conn, query, msg string) {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), query)
	require.NoError(t, err, msg)
	defer rows.Close()
	for rows.Next() {
	}
	require.NoError(t, rows.Err(), msg)
}

// grafanaRoleGrants extracts the schema and relation grants from
// run-fleet.sh's provision_grafana_db_role, still templated on ${grafana_user}.
func grafanaRoleGrants(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(runFleetScript)
	require.NoError(t, err)
	script := string(raw)
	start := strings.Index(script, "provision_grafana_db_role() {")
	require.NotEqual(t, -1, start, "provision_grafana_db_role not found")
	end := strings.Index(script[start:], "\n}\n")
	require.NotEqual(t, -1, end, "provision_grafana_db_role body not terminated")

	pattern := regexp.MustCompile(`(?s)GRANT\s+(?:USAGE\s+ON\s+SCHEMA\s+public|SELECT(?:\s*\([^)]*\))?\s+ON\s+\w+)\s+TO\s+"\$\{grafana_user\}";`)
	grants := pattern.FindAllString(script[start:start+end], -1)
	require.GreaterOrEqual(t, len(grants), 2, "expected schema usage and relation grants")
	return grants
}

func packagedRuleSQL(t *testing.T, path string) map[string]string {
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

	out := map[string]string{}
	for _, group := range doc.Groups {
		for _, rule := range group.Rules {
			for _, data := range rule.Data {
				if data.Model.RawSQL != "" {
					out[rule.Title] = data.Model.RawSQL
				}
			}
		}
	}
	require.NotEmpty(t, out, "no rule SQL found in %s", path)
	return out
}
