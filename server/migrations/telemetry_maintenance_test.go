package migrations_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestDeploymentTuningPreservesTelemetryMaintenanceSchedule(t *testing.T) {
	db := testutil.GetTestDB(t)
	scheduled := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	_, err := db.ExecContext(t.Context(), `SELECT alter_job(job_id,scheduled=>true,next_start=>$1) FROM telemetry_summary_source`, scheduled)
	require.NoError(t, err)
	script, err := os.ReadFile("../../deployment-files/run-fleet.sh")
	require.NoError(t, err)
	_, function, found := strings.Cut(string(script), "apply_database_tuning() {")
	require.True(t, found)
	_, sql, found := strings.Cut(function, "<<'SQL'\n")
	require.True(t, found)
	sql, _, found = strings.Cut(sql, "\nSQL")
	require.True(t, found)
	_, err = db.ExecContext(t.Context(), sql)
	require.NoError(t, err)
	rows, err := db.QueryContext(t.Context(), `SELECT js.next_start FROM telemetry_summary_source s JOIN timescaledb_information.job_stats js ON js.job_id=s.job_id`)
	require.NoError(t, err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var after time.Time
		require.NoError(t, rows.Scan(&after))
		require.True(t, scheduled.Equal(after), "deployment must preserve adaptive maintenance schedules")
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 4, count)
}
