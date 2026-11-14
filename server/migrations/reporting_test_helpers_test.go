package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

// Historical rollback fixtures remove newer empty tables through their guarded
// down migrations. Retained report or artifact history still refuses removal.
func rollbackEmptyReportingLifecycle(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, stage := range []struct{ table, source string }{
		{"hosted_report_artifact", "001080_internal_hosted_report_artifacts.down.sql"},
		{"hosted_report", "001079_internal_hosted_reports.down.sql"},
	} {
		var exists bool
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, stage.table).Scan(&exists))
		if !exists {
			continue
		}
		source, err := migrations.Current.ReadFile("current/" + stage.source)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(source))
		require.NoError(t, err, "remove only empty newer reporting lifecycle before historical rollback")
	}
}
