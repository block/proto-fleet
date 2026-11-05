package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

// Exercise the real compressed layout and poll density, including the JSON
// contributor windows that dominate module history work.
func TestAccountHistoryDenseCompressedWindow(t *testing.T) {
	phaseStart := time.Now()
	phase := func(name string) {
		t.Helper()
		t.Logf("dense fixture phase %s: %s", name, time.Since(phaseStart))
		phaseStart = time.Now()
	}
	f := setup(t)
	phase("database setup")
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
	phase("asset setup")
	// These are synthetic, unallocated source rows: the capture trigger's only
	// effect would be millions of unsuccessful allocation lookups. Check that
	// invariant before bypassing it, and restore it transactionally so an error
	// rolls back both the DDL and the fixture. Dedicated capture tests exercise
	// actual allocation, normalization and correction behavior.
	var allocated, samples int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset_interval`).Scan(&allocated))
	require.Zero(t, allocated)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&samples))
	require.Zero(t, samples)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	tables := []string{"device_metrics", "miner_state_snapshots", "container_module_metrics", "container_module_diagnostic_observation"}
	triggerDefs := make([]string, 0, len(tables))
	for _, table := range tables {
		var definition string
		require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgrelid=$1::regclass AND tgname='record_hosted_account_history' AND tgenabled='O'`, table).Scan(&definition))
		triggerDefs = append(triggerDefs, definition)
		// The table names are fixed constants above, not external input.
		_, err = tx.ExecContext(t.Context(), `DROP TRIGGER record_hosted_account_history ON `+table)
		require.NoError(t, err)
	}
	phase("disable capture for unallocated fixture")
	minerIDs, err := json.Marshal(miners)
	require.NoError(t, err)
	result, err := tx.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT sample,d.device_identifier,1e12,20,55 FROM device d CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample WHERE d.id IN (SELECT value::bigint FROM jsonb_array_elements_text($3::jsonb))`, start, end, minerIDs)
	require.NoError(t, err)
	inserted, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 100*7*24*60*4, inserted)
	phase("device metrics insert")
	result, err = tx.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,device_identifier,state) SELECT sample,d.org_id,d.device_identifier,CASE WHEN d.id=$4 THEN 2 ELSE 3 END FROM device d CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample WHERE d.id IN (SELECT value::bigint FROM jsonb_array_elements_text($3::jsonb))`, start, end, minerIDs, miners[0])
	require.NoError(t, err)
	inserted, err = result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 100*7*24*60*4, inserted)
	phase("miner state insert")
	ids, err := json.Marshal(modules)
	require.NoError(t, err)
	result, err = tx.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) SELECT id,sample,sample,1e12,20,55,jsonb_build_object('hashrate',evidence,'power',evidence,'temperature',evidence) FROM jsonb_array_elements_text($3::jsonb) ids(id_text) CROSS JOIN LATERAL (SELECT id_text::bigint id) asset CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample CROSS JOIN LATERAL (SELECT jsonb_build_object('oldest_at',sample,'newest_at',sample,'reporting',1,'expected',1,'topology_complete',true) evidence) source`, start, end, ids)
	require.NoError(t, err)
	inserted, err = result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 20*7*24*60*4, inserted)
	phase("module metrics insert")
	result, err = tx.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,state,state_observed_at) SELECT id_text::bigint,sample,'active',sample FROM jsonb_array_elements_text($3::jsonb) ids(id_text) CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-interval '15 seconds',interval '15 seconds') sample`, start, end, ids)
	require.NoError(t, err)
	inserted, err = result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 20*7*24*60*4, inserted)
	phase("module diagnostic insert")
	for _, definition := range triggerDefs {
		_, err = tx.ExecContext(t.Context(), definition)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	phase("restore capture and commit")
	for index, table := range tables {
		var definition string
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgrelid=$1::regclass AND tgname='record_hosted_account_history' AND tgenabled='O'`, table).Scan(&definition))
		require.Equal(t, triggerDefs[index], definition)
		// Timescale propagates the restored trigger to chunks created during the
		// load. Check them as well as the hypertable, not just the parent.
		var chunks, missing int
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),count(*) FILTER (WHERE NOT EXISTS
 (SELECT 1 FROM pg_trigger t WHERE t.tgrelid=format('%I.%I',c.chunk_schema,c.chunk_name)::regclass
 AND t.tgname='record_hosted_account_history' AND t.tgenabled='O')) FROM timescaledb_information.chunks c
 WHERE c.hypertable_schema='public' AND c.hypertable_name=$1`, table).Scan(&chunks, &missing))
		require.Positive(t, chunks, "fixture must create %s chunks", table)
		require.Zero(t, missing, "capture must be enabled on all %s fixture chunks", table)
	}
	phase("verify capture restoration")
	for _, table := range []string{"device_metrics", "container_module_metrics", "miner_state_snapshots"} {
		_, err = f.db.ExecContext(t.Context(), `SELECT compress_chunk(c,if_not_compressed=>true) FROM show_chunks($1::regclass) c`, table)
		require.NoError(t, err)
		phase("compress " + table)
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
	// Prove the restored triggers fire on the chunks created during bulk load.
	// Allocate one asset of each kind only after the measured history queries, so
	// the test continues to exercise exactly the original synthetic intervals.
	client := f.client(t, "Capture restoration", f.user, hostedaccounts.AllCapabilities)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miners[0], 0), selected(6, modules[0], 0)))
	f.legacyModuleHistory(t)
	historyEpoch(t, f, start)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	check, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer check.Rollback()
	at := end.Add(-time.Second) // Existing chunks, but not an existing 15s sample.
	_, err = check.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT $1,device_identifier,1e12,20,55 FROM device WHERE id=$2`, at, miners[0])
	require.NoError(t, err)
	_, err = check.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,device_identifier,state) SELECT $1,org_id,device_identifier,2 FROM device WHERE id=$2`, at, miners[0])
	require.NoError(t, err)
	_, err = check.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) VALUES($1,$2,$2,1e12,20,55,jsonb_build_object('hashrate',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',true),'power',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',true),'temperature',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',true)))`, modules[0], at)
	require.NoError(t, err)
	_, err = check.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,state,state_observed_at) VALUES($1,$2,'active',$2)`, modules[0], at)
	require.NoError(t, err)
	for source, expected := range map[int]int{1: 3, 2: 1, 3: 3, 4: 1} {
		var captured int
		require.NoError(t, check.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample WHERE source_id=$1 AND origin_time=$2`, source, at).Scan(&captured))
		require.Equal(t, expected, captured, "restored source %d trigger must capture existing chunks", source)
	}
	require.NoError(t, check.Rollback())
	phase("verify restored capture behavior")
}
