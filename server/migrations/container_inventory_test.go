package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestHashboardMigrationRoundTrip(t *testing.T) {
	db := testutil.GetTestDB(t)
	rollbackEmptyHashboardAllocations(t, db)
	rollbackEmptyClientTicketing(t, db)
	rollbackEmptyMiningGroups(t, db)
	testutil.RestoreLegacySummaryBootstraps(t, db,
		"001016_internal_ct1_metric_summaries",
		"001021_internal_hashboard_metric_summaries")
	diagnostics, err := migrations.Current.ReadFile("current/001037_internal_diagnostic_observations.down.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(diagnostics))
	require.NoError(t, err)
	observations, err := migrations.Current.ReadFile("current/001023_internal_hashboard_observation.down.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(observations))
	require.NoError(t, err)
	// Remove aggregates that depend on the raw board table before rolling it back.
	summaries, err := migrations.Current.ReadFile("current/001021_internal_hashboard_metric_summaries.down.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(summaries))
	require.NoError(t, err)
	for _, direction := range []string{"down", "up"} {
		script, err := migrations.Current.ReadFile("current/001004_internal_hashboards." + direction + ".sql")
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
		for _, table := range []string{"hashboard", "hashboard_identity", "hashboard_installation", "hashboard_slot", "hashboard_metrics", "hashboard_latest_metrics"} {
			var exists bool
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists))
			require.Equal(t, direction == "up", exists, table)
		}
	}
}

func TestModuleIssuesMigrationRoundTrip(t *testing.T) {
	db := testutil.GetTestDB(t)
	for _, direction := range []string{"down", "up"} {
		script, err := migrations.Current.ReadFile("current/001002_internal_container_module_issues." + direction + ".sql")
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='container_module' AND column_name IN ('issues','issues_observed_at','issues_source_times')").Scan(&count))
		if direction == "up" {
			require.Equal(t, 3, count)
		} else {
			require.Zero(t, count)
		}
	}
}
