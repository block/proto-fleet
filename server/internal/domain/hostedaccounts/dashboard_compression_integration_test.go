package hostedaccounts_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestAccountHistoryCompressionMigrationPreservesIndependentSources(t *testing.T) {
	f := setup(t)
	module, _ := f.miningModule(t)
	end := time.Now().UTC().Truncate(time.Hour).Add(-8 * time.Hour)
	start := end.Add(-time.Hour)
	// The partition key can be pinned by an older source. It cannot safely be
	// bounded using collection time, even when a board metric is fresh.
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) SELECT $1,$2::timestamptz-interval '6 days',sample,1e12,20,55,jsonb_build_object('hashrate',evidence,'power',evidence,'temperature',evidence) FROM generate_series($2::timestamptz-interval '2 days',$3::timestamptz-interval '15 seconds',interval '15 seconds') sample CROSS JOIN LATERAL (SELECT jsonb_build_object('oldest_at',sample,'newest_at',sample,'reporting',1,'expected',1,'topology_complete',true) evidence) source`, module, start, end)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001033_internal_hosted_history_compression.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001033_internal_hosted_history_compression.up.sql")
	require.NoError(t, err)
	// Begin with the previous compressed layout, then exercise an actual upgrade
	// of an already compressed chunk, not just the new hypertable default.
	_, err = f.db.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c,if_not_compressed=>true) FROM show_chunks('container_module_metrics') c`)
	require.NoError(t, err)
	encoded, err := json.Marshal([]map[string]any{{"kind": 6, "asset_id": module, "site_id": 0, "started_at": start.Add(-time.Hour)}})
	require.NoError(t, err)
	read := func() *miningProjectionDB {
		t.Helper()
		capture := &miningProjectionDB{DB: f.db}
		rows, err := sqlc.New(capture).ListHostedAccountHistoricalReadings(t.Context(), sqlc.ListHostedAccountHistoricalReadingsParams{StartTime: start, EndTime: end, BucketSeconds: 900, Intervals: encoded, HostAuthority: json.RawMessage("[]"), PolicySites: []int64{}})
		require.NoError(t, err)
		require.Len(t, rows, 4)
		for _, row := range rows {
			require.Equal(t, 1e12, row.HashrateHs)
			require.Equal(t, 20.0, row.PowerW)
			require.True(t, row.Complete)
		}
		return capture
	}
	before := read()
	_, err = f.db.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	after := read()
	var first string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT attname FROM timescaledb_information.compression_settings WHERE hypertable_name='container_module_metrics' AND orderby_column_index=1`).Scan(&first))
	require.Equal(t, "collected_at", first)
	// The compressed scan now pushes collection bounds into batch min/max tests.
	require.NotContains(t, string(before.plan), "_ts_meta_min_1")
	require.Contains(t, string(after.plan), "_ts_meta_min_1")
	require.Contains(t, string(after.plan), "_ts_meta_max_1")
	_, err = f.db.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	read()
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT attname FROM timescaledb_information.compression_settings WHERE hypertable_name='container_module_metrics' AND orderby_column_index=1`).Scan(&first))
	require.Equal(t, "time", first)
}
