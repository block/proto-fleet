package timescaledb

import (
	"os"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerCompressionPolicies(t *testing.T) {
	r := newContainerRollupRig(t)
	for _, table := range []string{"container_plant_metrics", "container_tank_metrics", "container_module_metrics", "hashboard_metrics"} {
		t.Run(table, func(t *testing.T) {
			var enabled, hourlyChunks, policy bool
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT compression_enabled FROM timescaledb_information.hypertables WHERE hypertable_name=$1`, table).Scan(&enabled))
			require.True(t, enabled)
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT time_interval=INTERVAL '1 hour' FROM timescaledb_information.dimensions WHERE hypertable_name=$1`, table).Scan(&hourlyChunks))
			require.True(t, hourlyChunks)
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM timescaledb_information.jobs WHERE hypertable_name=$1 AND proc_name='policy_compression' AND (config->>'compress_after')::interval=INTERVAL '6 hours' AND schedule_interval=INTERVAL '1 hour')`, table).Scan(&policy))
			require.True(t, policy)
		})
	}
}

func TestContainerCompressionLateWrites(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
	r.db.SetMaxOpenConns(1) // Keep the decompression limit on every ingestion transaction.
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	// Three hours at ten-second intervals, with two boards per poll. Each hour
	// has 720 rows, below the limit; an unbounded stamp exceeds it.
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics
	 (hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs,topology_version_id)
	 SELECT hashboard_id,installation_id,time+n*INTERVAL '10 seconds',collected_at+n*INTERVAL '10 seconds',state,mining_state,hashrate_hs,topology_version_id
	 FROM hashboard_metrics CROSS JOIN generate_series(1,1079) n`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('hashboard_metrics') c`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SET timescaledb.max_tuples_decompressed_per_dml_transaction=1000`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET topology_version_id=NULL`)
	require.ErrorContains(t, err, "tuple decompression limit", "prove the fixture catches an unbounded write")

	// A current poll may carry source timestamps from a compressed day. Use a
	// different collection time, as a real delayed report would.
	s := r.snapshot(24*time.Hour, false, 300e12, 400e12)
	for i := range s.Tanks {
		s.Tanks[i].Modules[0].Hashboards[0].ObservedAt = r.base.Add(30 * time.Second)
	}
	r.store(t, s)
	var stamped int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics b
	 JOIN container_topology_observation o ON o.collected_at=b.collected_at AND o.version_id=b.topology_version_id
	 WHERE o.device_id=$1 AND b.time=$2 AND b.collected_at=$3`, r.databaseID, r.base.Add(30*time.Second), s.ObservedAt).Scan(&stamped))
	require.Equal(t, 2, stamped)

	// Exercise the stamp on already compressed rows too, independently of the
	// decompression caused by the preceding late insert.
	_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(c,if_not_compressed=>true) FROM show_chunks('hashboard_metrics') c`)
	require.NoError(t, err)
	var version int64
	var samples []byte
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT min(topology_version_id),jsonb_agg(jsonb_build_object('hashboard_id',hashboard_id,'time',time)) FROM hashboard_metrics WHERE collected_at=$1`, s.ObservedAt).Scan(&version, &samples))
	require.NoError(t, sqlc.New(r.db).StampHashboardTopology(t.Context(), sqlc.StampHashboardTopologyParams{VersionID: version, CollectedAt: s.ObservedAt, Samples: samples}))
}

func TestContainerCompressionTopologyReferences(t *testing.T) {
	r := newContainerRollupRig(t)
	r.db.SetMaxOpenConns(1)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-20 * 24 * time.Hour)
	for _, offset := range []time.Duration{0, time.Minute, 2 * time.Minute} {
		r.store(t, r.snapshot(offset, false, 100e12, 200e12))
	}
	_, err := r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('container_tank_metrics') c;
	 SELECT drop_chunks('device_metrics',older_than=>now()-INTERVAL '10 days');
	 SET timescaledb.max_tuples_decompressed_per_dml_transaction=1`)
	require.NoError(t, err)
	// A receipt without telemetry can be deleted even while other receipts have
	// compressed FK references in the same chunk.
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
	 SELECT device_id,version_id,collected_at+INTERVAL '30 seconds',complete FROM container_topology_observation WHERE collected_at=$1`, r.base)
	require.NoError(t, err)
	compact := func() {
		t.Helper()
		_, err := r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',100)`, r.databaseID)
		require.NoError(t, err)
	}
	compact()
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation`).Scan(&count))
	require.Equal(t, 3, count, "compressed tank rows must pin every receipt")
	// FK enforcement must still reject deletion of an observation referenced
	// only by compressed raw tanks (the middle receipt is not a latest value).
	_, err = r.db.ExecContext(t.Context(), `DELETE FROM container_topology_observation WHERE collected_at=$1`, r.base.Add(time.Minute))
	require.ErrorContains(t, err, "foreign key constraint")
	// Once raw retention expires, only anchor and latest references remain.
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('container_tank_metrics',older_than=>now()-INTERVAL '10 days');
	 UPDATE container_topology_compaction_pending SET next_check_at=now()`)
	require.NoError(t, err)
	compact()
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation`).Scan(&count))
	require.Equal(t, 2, count, "retention releases the interior receipt; anchor and latest survive")
}

func TestContainerCompressionRollbackPreservesData(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	require.NoError(t, sqlc.New(r.db).InsertContainerPlantMetrics(t.Context(), sqlc.InsertContainerPlantMetricsParams{DeviceIdentifier: r.deviceID, Time: r.base, Metrics: []byte(`{"power_w":4000}`)}))
	tables := []string{"container_plant_metrics", "container_tank_metrics", "container_module_metrics", "hashboard_metrics"}
	for _, table := range tables {
		_, err := r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks($1::regclass) c`, table)
		require.NoError(t, err)
	}
	for _, direction := range []string{"down", "up"} {
		migration, err := os.ReadFile("../../../migrations/current/001027_internal_container_telemetry_compression." + direction + ".sql")
		require.NoError(t, err)
		tx, err := r.db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })
		_, err = tx.ExecContext(t.Context(), string(migration))
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), `SELECT alter_job(job_id,scheduled=>false) FROM timescaledb_information.jobs WHERE scheduled`)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		for i, table := range tables {
			var count int
			var enabled bool
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count))
			want := 2
			if i == 0 {
				want = 1
			}
			require.Equal(t, want, count)
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT compression_enabled FROM timescaledb_information.hypertables WHERE hypertable_name=$1`, table).Scan(&enabled))
			require.Equal(t, direction == "up", enabled)
		}
	}
}

func TestContainerCompressionPolicyPreservesSamplesAndReducesSize(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
	// One hour of 15-second polls with two tanks/modules/boards. Vary readings
	// and retain real ingestion's coverage, topology and plant JSON payloads.
	store := func(offset time.Duration) {
		t.Helper()
		variation := float64(int(offset.Seconds()) % 17)
		s := r.snapshot(offset, false, (100+variation)*1e12, (200+variation)*1e12)
		s.Plant = &sdk.ContainerPlant{ObservedAt: s.ObservedAt, TotalPowerW: new(4000 + variation), InletTemperatureC: new(30 + variation/10), OutletTemperatureC: new(35 + variation/10)}
		r.store(t, s)
	}
	for i := range 240 {
		store(time.Duration(i) * 15 * time.Second)
	}
	for _, table := range []string{"container_plant_metrics", "container_tank_metrics", "container_module_metrics", "hashboard_metrics"} {
		var before, after string
		// Compare all stored fields, not only row counts, through compression.
		digest := `SELECT md5(string_agg(row_to_json(m)::text, '' ORDER BY row_to_json(m)::text)) FROM ` + table + ` m`
		require.NoError(t, r.db.QueryRowContext(t.Context(), digest).Scan(&before))
		var job int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='policy_compression' AND hypertable_name=$1`, table).Scan(&job))
		_, err := r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
		require.NoError(t, err)
		require.NoError(t, r.db.QueryRowContext(t.Context(), digest).Scan(&after))
		require.Equal(t, before, after, table)
		var uncompressed, compressed int64
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT before_compression_total_bytes,after_compression_total_bytes FROM hypertable_compression_stats($1::regclass)`, table).Scan(&uncompressed, &compressed))
		require.Positive(t, compressed)
		require.Less(t, compressed, uncompressed, table)
		t.Logf("%s: %d -> %d bytes (%.1f%% smaller)", table, uncompressed, compressed, 100*(1-float64(compressed)/float64(uncompressed)))
	}
	// All four ingestion paths accept late data in their compressed hour.
	store(time.Hour - time.Second)
	for _, table := range []string{"container_plant_metrics", "container_tank_metrics", "container_module_metrics", "hashboard_metrics"} {
		var count int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE time=$1`, r.base.Add(time.Hour-time.Second)).Scan(&count))
		want := 2
		if table == "container_plant_metrics" {
			want = 1
		}
		require.Equal(t, want, count, table)
	}
}

// The two requested boards must not decompress the other 498 boards in the
// same chunk. Exercise prepared/generic plans, as used by the running server.
func TestHashboardTopologyStampPrunesUnrelatedBoards(t *testing.T) {
	r := newContainerRollupRig(t)
	r.db.SetMaxOpenConns(1)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	_, err := r.db.ExecContext(t.Context(), `WITH boards AS (
  INSERT INTO hashboard(org_id,first_seen_at,last_seen_at)
  SELECT org_id,$2,$2 FROM device CROSS JOIN generate_series(1,498) WHERE id=$1 RETURNING id
 ) INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,removed_at)
 SELECT b.id,m.id,1,$2,$2,$2 FROM boards b CROSS JOIN LATERAL
 (SELECT m.id FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE t.device_id=$1 ORDER BY m.id LIMIT 1) m`, r.databaseID, r.base)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state)
 SELECT i.hashboard_id,i.id,$1::timestamptz+n*INTERVAL '15 seconds',$1::timestamptz+n*INTERVAL '15 seconds','hashing','hashing'
 FROM hashboard_installation i CROSS JOIN generate_series(0,239) n ON CONFLICT DO NOTHING`, r.base)
	require.NoError(t, err)
	// Neither a different source time from the same collection, nor the same
	// source time from another collection, belongs to the requested sample keys.
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state)
	 SELECT m.hashboard_id,m.installation_id,m.time+x.source_offset,m.collected_at+x.collection_offset,m.state,m.mining_state
	 FROM hashboard_metrics m CROSS JOIN (VALUES (INTERVAL '1 second',INTERVAL '0'),(INTERVAL '0',INTERVAL '1 second')) x(source_offset,collection_offset)
	 WHERE m.time=$1 AND m.hashboard_id IN (SELECT hashboard_id FROM hashboard_identity)`, r.base)
	require.NoError(t, err)
	var samples []byte
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT jsonb_agg(jsonb_build_object('hashboard_id',hashboard_id,'time',time)) FROM hashboard_metrics WHERE time=$1 AND collected_at=$1 AND hashboard_id IN (SELECT hashboard_id FROM hashboard_identity)`, r.base).Scan(&samples))
	var version int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO container_topology_version(device_id,fingerprint,topology) VALUES ($1,'stamp-pruning-test','{}') RETURNING id`, r.databaseID).Scan(&version))
	_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('hashboard_metrics') c;
 SET timescaledb.max_tuples_decompressed_per_dml_transaction=1000;
 SET plan_cache_mode=force_generic_plan`)
	require.NoError(t, err)
	q, err := sqlc.Prepare(t.Context(), r.db)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, q.Close()) })
	require.NoError(t, q.StampHashboardTopology(t.Context(), sqlc.StampHashboardTopologyParams{VersionID: version, CollectedAt: r.base, Samples: samples}))
	var stamped int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics WHERE topology_version_id=$1`, version).Scan(&stamped))
	require.Equal(t, 2, stamped)
	var planMode string
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SHOW plan_cache_mode`).Scan(&planMode))
	require.Equal(t, "force_generic_plan", planMode, "the stamp must restore the caller's planning mode")
	// Empty sample lists do not touch any compressed history.
	require.NoError(t, q.StampHashboardTopology(t.Context(), sqlc.StampHashboardTopologyParams{VersionID: version, CollectedAt: r.base, Samples: []byte(`[]`)}))
	// A delayed poll also runs the new stamp through the real ingestion transaction.
	s := r.snapshot(24*time.Hour, false, 100e12, 200e12)
	for i := range s.Tanks {
		s.Tanks[i].Modules[0].Hashboards[0].ObservedAt = r.base.Add(30 * time.Second)
	}
	r.store(t, s)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics WHERE collected_at=$1 AND topology_version_id IS NOT NULL`, s.ObservedAt).Scan(&stamped))
	require.Equal(t, 2, stamped)
}
