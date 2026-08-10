package sqlstores_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func newAlertRuleConfigStore(t *testing.T) *sqlstores.SQLAlertRuleConfigStore {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	return sqlstores.NewSQLAlertRuleConfigStore(testutil.GetTestDB(t))
}

func offlineRuleConfigFixture(name string) alerts.RuleConfig {
	return alerts.RuleConfig{Name: name, DurationSeconds: 1800, Offline: &alerts.OfflineRuleConfig{}}
}

func TestAlertRuleConfigStoreRoundTrip(t *testing.T) {
	store := newAlertRuleConfigStore(t)
	ctx := context.Background()

	cfg := offlineRuleConfigFixture("Offline too long")
	cfg.Scope = &alerts.RuleScope{SiteIDs: []int64{3}, DeviceIDs: []string{"dev-a"}}
	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-a", cfg))
	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-b", offlineRuleConfigFixture("Other")))

	got, err := store.GetConfig(ctx, 7, "pfu-a")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, cfg, *got)

	// Lists are bounded to the requested UIDs; unrequested and cross-org rows stay out.
	listed, err := store.ListConfigs(ctx, 7, []string{"pfu-a", "pfu-missing"})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, cfg, listed["pfu-a"])
	other, err := store.ListConfigs(ctx, 8, []string{"pfu-a"})
	require.NoError(t, err)
	assert.Empty(t, other)

	require.NoError(t, store.DeleteConfig(ctx, 7, "pfu-a"))
	gone, err := store.GetConfig(ctx, 7, "pfu-a")
	require.NoError(t, err)
	assert.Nil(t, gone)
}

// Rolling back past 000134 while scoped rules exist would silently break their
// persisted Grafana SQL (the compiled queries reference fleet_device_placement),
// so the down migration must refuse until every scoped rule is unscoped or
// deleted; scope-less rows must not block it.
func TestFleetDevicePlacementDownRefusesWithScopedRules(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLAlertRuleConfigStore(db)
	ctx := context.Background()

	downSQL, err := migrations.Migrations.ReadFile("000134_fleet_device_placement.down.sql")
	require.NoError(t, err)

	scoped := offlineRuleConfigFixture("Scoped")
	scoped.Scope = &alerts.RuleScope{SiteIDs: []int64{3}}
	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-scoped", scoped))
	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-unscoped", offlineRuleConfigFixture("Org-wide")))

	_, err = db.ExecContext(ctx, string(downSQL))
	require.ErrorContains(t, err, "scoped alert rules exist")
	var viewExists bool
	require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass('fleet_device_placement') IS NOT NULL").Scan(&viewExists))
	assert.True(t, viewExists, "the refused rollback must leave the view in place")

	require.NoError(t, store.DeleteConfig(ctx, 7, "pfu-scoped"))
	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err, "scope-less configs must not block the rollback")
}

// A standard multi-step rollback with no configs left runs 000135's down
// (drops the empty table) and then 000134's down: the 134 guard must tolerate
// the already-dropped table — PL/pgSQL prepares statements on first execution,
// so its EXISTS probe must stay unreachable when to_regclass reports the table
// gone — and still drop the view.
func TestScopeMigrationsRollBackWhenEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := context.Background()

	down135, err := migrations.Migrations.ReadFile("000135_alert_rule_config.down.sql")
	require.NoError(t, err)
	down134, err := migrations.Migrations.ReadFile("000134_fleet_device_placement.down.sql")
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, string(down135))
	require.NoError(t, err, "an empty config table must drop cleanly")
	_, err = db.ExecContext(ctx, string(down134))
	require.NoError(t, err, "the guard must tolerate the table 000135's down already dropped")

	var viewExists bool
	require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass('fleet_device_placement') IS NOT NULL").Scan(&viewExists))
	assert.False(t, viewExists, "the rollback must actually drop the view")
}

// The pre-135 server cannot read this table, so rolling back past 000135 while
// any rule config exists would strand those rules uneditable; the down
// migration must refuse until the rows are gone, then drop the table cleanly.
func TestAlertRuleConfigDownRefusesWithRows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLAlertRuleConfigStore(db)
	ctx := context.Background()

	downSQL, err := migrations.Migrations.ReadFile("000135_alert_rule_config.down.sql")
	require.NoError(t, err)

	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-a", offlineRuleConfigFixture("Any config")))
	_, err = db.ExecContext(ctx, string(downSQL))
	require.ErrorContains(t, err, "delete the affected user rules")
	var tableExists bool
	require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass('alert_rule_config') IS NOT NULL").Scan(&tableExists))
	assert.True(t, tableExists, "the refused rollback must leave the table in place")

	require.NoError(t, store.DeleteConfig(ctx, 7, "pfu-a"))
	_, err = db.ExecContext(ctx, string(downSQL))
	require.NoError(t, err, "an empty table must drop cleanly")
}

// The sweep reclaims aged rows for rules missing from the live list, sparing
// fresh rows (an in-flight create stores its config before the rule exists)
// and other orgs' rows.
func TestAlertRuleConfigStoreSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLAlertRuleConfigStore(db)
	ctx := context.Background()

	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-live", offlineRuleConfigFixture("Live")))
	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-orphan-old", offlineRuleConfigFixture("Aged orphan")))
	require.NoError(t, store.UpsertConfig(ctx, 7, "pfu-orphan-new", offlineRuleConfigFixture("In-flight create")))
	require.NoError(t, store.UpsertConfig(ctx, 8, "pfu-other-org", offlineRuleConfigFixture("Other org")))
	_, err := db.ExecContext(ctx,
		`UPDATE alert_rule_config SET updated_at = now() - INTERVAL '2 hours' WHERE rule_uid IN ('pfu-live', 'pfu-orphan-old', 'pfu-other-org')`)
	require.NoError(t, err)

	n, err := store.SweepConfigs(ctx, 7, []string{"pfu-live"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	for uid, want := range map[string]bool{
		"pfu-live":       true,
		"pfu-orphan-old": false,
		"pfu-orphan-new": true,
	} {
		got, err := store.GetConfig(ctx, 7, uid)
		require.NoError(t, err)
		assert.Equal(t, want, got != nil, uid)
	}
	otherOrg, err := store.GetConfig(ctx, 8, "pfu-other-org")
	require.NoError(t, err)
	assert.NotNil(t, otherOrg, "sweep must stay org-scoped")
}
