package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

// Exercise the real compressed layout and poll density, including the JSON
// contributor windows that dominate module history work.
func TestAccountHistoryDenseCompressedWindow(t *testing.T) {
	f := setup(t)
	end := time.Now().UTC().Truncate(time.Hour).Add(-8 * time.Hour)
	start := end.Add(-7 * 24 * time.Hour)
	intervals := []map[string]any{}
	miners := []int64{}
	for range 100 {
		id := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		miners = append(miners, id)
		var identifier string
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, id).Scan(&identifier))
		intervals = append(intervals, map[string]any{"kind": 5, "asset_id": id, "device_identifier": identifier, "site_id": 0, "started_at": start})
	}
	modules := []int64{}
	for range 20 {
		id, _ := f.miningModule(t)
		modules = append(modules, id)
		intervals = append(intervals, map[string]any{"kind": 6, "asset_id": id, "site_id": 0, "started_at": start})
	}
	minerIDs, err := json.Marshal(miners)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT sample,d.device_identifier,1e12,20,55 FROM device d CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample WHERE d.id IN (SELECT value::bigint FROM jsonb_array_elements_text($3::jsonb))`, start, end, minerIDs)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,device_identifier,state) SELECT sample,d.org_id,d.device_identifier,CASE WHEN d.id=$4 THEN 2 ELSE 3 END FROM device d CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample WHERE d.id IN (SELECT value::bigint FROM jsonb_array_elements_text($3::jsonb))`, start, end, minerIDs, miners[0])
	require.NoError(t, err)
	ids, err := json.Marshal(modules)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) SELECT id,sample,sample,1e12,20,55,jsonb_build_object('hashrate',evidence,'power',evidence,'temperature',evidence) FROM jsonb_array_elements_text($3::jsonb) ids(id_text) CROSS JOIN LATERAL (SELECT id_text::bigint id) asset CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample CROSS JOIN LATERAL (SELECT jsonb_build_object('oldest_at',sample,'newest_at',sample,'reporting',1,'expected',1,'topology_complete',true) evidence) source`, start, end, ids)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,state,state_observed_at) SELECT id_text::bigint,sample,'active',sample FROM jsonb_array_elements_text($3::jsonb) ids(id_text) CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample`, start, end, ids)
	require.NoError(t, err)
	for _, table := range []string{"device_metrics", "container_module_metrics", "miner_state_snapshots"} {
		_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c,if_not_compressed=>true) FROM show_chunks($1::regclass) c`, table)
		require.NoError(t, err)
	}
	encoded, err := json.Marshal(intervals)
	require.NoError(t, err)
	for _, duration := range []time.Duration{time.Hour, 24 * time.Hour} {
		t.Run(duration.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			capture := &miningProjectionDB{DB: f.db}
			rows, err := sqlc.New(capture).ListHostedAccountHistoricalReadings(ctx, sqlc.ListHostedAccountHistoricalReadingsParams{OrganizationID: f.org, StartTime: end.Add(-duration), EndTime: end, BucketStarts: historyTestBuckets(end.Add(-duration), end), Intervals: encoded, HostAuthority: json.RawMessage("[]"), PolicySites: []int64{}})
			require.NoError(t, err)
			require.Len(t, rows, int(duration/(15*time.Minute)))
			for _, row := range rows {
				require.EqualValues(t, 120, row.PairedReportingCount)
				require.EqualValues(t, 119, row.StatusHashingCount)
				require.EqualValues(t, 1, row.StatusBrokenCount)
			}
			var plans []struct {
				ExecutionTime float64 `json:"Execution Time"`
			}
			require.NoError(t, json.Unmarshal(capture.plan, &plans))
			require.Less(t, plans[0].ExecutionTime, 5000.0, "dense compressed history must fit the mining transaction deadline")
			t.Logf("120 assets (100 miners,20 modules),15-second metrics and state cadence,7 retained days with compressed numeric telemetry and miner snapshots,range=%s,execution=%.2fms", duration, plans[0].ExecutionTime)
			if directory := os.Getenv("HOSTED_HISTORY_PLAN_DIR"); directory != "" {
				require.NoError(t, os.WriteFile(directory+"/"+duration.String()+".json", capture.plan, 0600))
			}
		})
	}
}
