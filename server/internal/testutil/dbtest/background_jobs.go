package dbtest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PauseBackgroundJobs gives a test control over maintenance in its disposable
// database. Backdated fixtures and deliberate lock barriers must not race with
// automatic retention, refresh, or bootstrap jobs. Jobs remain available to
// CALL run_job explicitly; this does not change their configuration or policies.
func PauseBackgroundJobs(t *testing.T, database *sql.DB) {
	t.Helper()
	const timeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := database.ExecContext(ctx, `
			SELECT alter_job(job_id, scheduled => false, if_exists => true)
			FROM timescaledb_information.jobs WHERE scheduled`)
		require.NoError(c, err)
		// An already-running bootstrap can create more policies. Wait for it
		// to finish and repeat the pause before returning the database to tests.
		var idle bool
		err = database.QueryRowContext(ctx, `SELECT
			NOT EXISTS (SELECT 1 FROM timescaledb_information.job_stats WHERE job_status='Running')
			AND NOT EXISTS (SELECT 1 FROM timescaledb_information.jobs WHERE scheduled)`).Scan(&idle)
		require.NoError(c, err)
		require.True(c, idle, "background maintenance is still active")
	}, timeout, 10*time.Millisecond)
}
