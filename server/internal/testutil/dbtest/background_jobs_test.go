package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPausedBackgroundJobsRemainRunnable(t *testing.T) {
	database := GetTestDB(t)
	var scheduled int
	require.NoError(t, database.QueryRowContext(t.Context(), `
		SELECT count(*) FROM timescaledb_information.jobs WHERE scheduled`).Scan(&scheduled))
	require.Zero(t, scheduled, "new test databases must start with automatic maintenance paused")
	_, err := database.ExecContext(t.Context(), `
		CREATE TABLE maintenance_fixture (runs integer);
		CREATE PROCEDURE maintenance_fixture_job(job_id integer, config jsonb)
		LANGUAGE SQL AS 'INSERT INTO maintenance_fixture VALUES (1)';`)
	require.NoError(t, err)
	var jobID int
	require.NoError(t, database.QueryRowContext(t.Context(), `
		SELECT add_job('maintenance_fixture_job', INTERVAL '1 hour',
			initial_start => now() + INTERVAL '1 hour')`).Scan(&jobID))

	PauseBackgroundJobs(t, database)
	require.NoError(t, database.QueryRowContext(t.Context(), `
		SELECT count(*) FROM timescaledb_information.jobs WHERE scheduled`).Scan(&scheduled))
	require.Zero(t, scheduled)

	// Pausing must retain the actual policy, so maintenance tests can still
	// exercise it at a deterministic point rather than substituting a mock.
	_, err = database.ExecContext(t.Context(), `CALL run_job($1)`, jobID)
	require.NoError(t, err)
	var runs int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM maintenance_fixture`).Scan(&runs))
	require.Equal(t, 1, runs)
}
