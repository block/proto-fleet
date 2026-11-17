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
	var generationPermission bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid=to_regclass('hosted_account') AND conname='hosted_account_capabilities_check' AND pg_get_constraintdef(oid) LIKE '%63%')`).Scan(&generationPermission))
	if generationPermission {
		source, err := migrations.Current.ReadFile("current/001091_internal_hosted_report_generation_permission.down.sql")
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(source))
		require.NoError(t, err, "restore the older capability catalog before historical rollback")
	}
	for _, stage := range []struct{ table, source string }{
		{"hosted_account_analysis_deletion", "001090_internal_account_analysis_deletion.down.sql"},
		{"hosted_report_queue", "001089_internal_hosted_reporting_delivery.down.sql"},
		{"hosted_account_analysis", "001088_internal_hosted_account_analyses.down.sql"},
		{"hosted_report_request_fulfillment", "001087_internal_hosted_report_request_fulfillment.down.sql"},
		{"hosted_report_queue", "001086_internal_hosted_report_queue.down.sql"},
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
