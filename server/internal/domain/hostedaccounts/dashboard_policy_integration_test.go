package hostedaccounts

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestHostedHistorySummaryUsesStandardPolicies(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM telemetry_summary_source WHERE source_name='hosted_account_history_sample'`).Scan(&count))
	require.Zero(t, count, "hosted history has no recovery stream")
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.dimensions
 WHERE hypertable_name='hosted_account_history_sample' AND time_interval=INTERVAL '1 hour'`).Scan(&count))
	require.Equal(t, 1, count, "new samples use hourly chunks")
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs
 WHERE hypertable_name='hosted_account_history_sample' AND proc_name='policy_compression'
 AND (config->>'compress_after')::interval=INTERVAL '6 hours' AND schedule_interval=INTERVAL '1 hour'`).Scan(&count))
	require.Equal(t, 1, count, "compression runs hourly once each whole chunk is six hours old")
	for _, tc := range []struct {
		table         string
		duration      time.Duration
		months, years int
	}{
		{table: "hosted_account_history_sample", duration: dashboardHistoryRetention},
		{table: "hosted_account_history_hourly", months: dashboardHourlyRetentionMonths},
		{table: "hosted_account_history_daily", years: dashboardDailyRetentionYears},
	} {
		t.Run(tc.table, func(t *testing.T) {
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs j
 LEFT JOIN timescaledb_information.continuous_aggregates a ON a.materialization_hypertable_name=j.hypertable_name
 WHERE j.proc_name='policy_retention' AND COALESCE(a.view_name,j.hypertable_name)=$1
 AND (j.config->>'drop_after')::interval=make_interval(years=>$2,months=>$3,secs=>$4)`, tc.table, tc.years, tc.months, tc.duration.Seconds()).Scan(&count))
			require.Equal(t, 1, count, "retention must agree with dashboard tier selection")
		})
	}
	for _, tc := range []struct {
		table               string
		resolution, cadence time.Duration
	}{
		{"hosted_account_history_hourly", dashboardHourlyResolution, dashboardHourlyRefreshCadence},
		{"hosted_account_history_daily", dashboardDailyResolution, dashboardDailyRefreshCadence},
	} {
		t.Run(tc.table+" refresh", func(t *testing.T) {
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs j
 JOIN _timescaledb_catalog.continuous_agg a ON a.mat_hypertable_id=(j.config->>'mat_hypertable_id')::int
 WHERE j.proc_name='policy_refresh_continuous_aggregate' AND a.user_view_name=$1
 AND (j.config->>'start_offset')::interval=make_interval(secs=>$2)
 AND (j.config->>'end_offset')::interval=make_interval(secs=>$3)
 AND j.schedule_interval=make_interval(secs=>$4)`, tc.table, (dashboardHistoryRetention-dashboardDailyResolution).Seconds(), tc.resolution.Seconds(), tc.cadence.Seconds()).Scan(&count))
			require.Equal(t, 1, count, "refresh window, end offset and cadence must match the supported policy")
		})
	}
}
