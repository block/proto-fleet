package testutil

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// RestoreLegacySummaryBootstraps reinstalls bootstraps superseded by telemetry
// recovery so disposable databases can still test the historical migrations.
func RestoreLegacySummaryBootstraps(t *testing.T, db *sql.DB, names ...string) {
	t.Helper()
	for _, name := range names {
		raw, err := migrations.Current.ReadFile("current/" + name + ".up.sql")
		require.NoError(t, err)
		script := string(raw)
		start := strings.Index(script, "CREATE PROCEDURE initialize_")
		require.NotEqual(t, -1, start, "bootstrap missing from %s", name)
		_, err = db.ExecContext(t.Context(), script[start:]+`
	 SELECT alter_job(job_id,scheduled=>false) FROM timescaledb_information.jobs WHERE scheduled;`)
		require.NoError(t, err)
	}
}

// RefreshContinuousAggregate materializes a test fixture, allowing a scheduled
// policy refresh already running in the test database to finish first. Nil bounds
// refresh the whole eligible range; the CALL must not run inside a transaction.
func RefreshContinuousAggregate(t *testing.T, db *sql.DB, view string, start, end *time.Time) {
	t.Helper()
	const maxAttempts = 10
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := db.ExecContext(t.Context(),
			`CALL refresh_continuous_aggregate($1::regclass, $2::timestamptz, $3::timestamptz)`,
			view, start, end,
		)
		if err == nil {
			return
		}
		// pgx exposes refresh lock contention as 55P03. Other failures must
		// surface immediately rather than being hidden by a generic retry.
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" || attempt == maxAttempts {
			require.NoError(t, err)
		}
		select {
		case <-t.Context().Done():
			require.NoError(t, t.Context().Err())
		case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
		}
	}
}
