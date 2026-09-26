package testutil

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

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
