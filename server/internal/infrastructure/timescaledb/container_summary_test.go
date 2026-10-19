package timescaledb

import (
	"fmt"
	"os"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestContainerEquipmentSummariesSurviveRawRetention(t *testing.T) {
	for _, age := range []time.Duration{40 * 24 * time.Hour, 400 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			r := newContainerRollupRig(t)
			r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-age)
			r.store(t, r.snapshot(time.Hour, false, 100e12, 200e12))
			r.store(t, r.snapshot(time.Hour+10*time.Second, false, 300e12, 200e12))
			// Refresh both summary tiers from compressed raw history before retention.
			for _, table := range []string{"device_metrics", "container_tank_metrics", "container_module_metrics"} {
				_, err := r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks($1::regclass) c`, table)
				require.NoError(t, err)
			}
			start, end := r.base, r.base.Add(24*time.Hour)
			for _, equipment := range []string{"container", "container_tank", "container_module"} {
				for _, tier := range []string{"hourly", "daily"} {
					testutil.RefreshContinuousAggregate(t, r.db, equipment+"_metrics_"+tier, &start, &end)
				}
			}
			// Exercise actual retention: remove the original chunks, not just rows.
			for _, table := range []string{"device_metrics", "container_tank_metrics", "container_module_metrics"} {
				_, err := r.db.ExecContext(t.Context(), `SELECT drop_chunks($1::regclass, older_than => now()-INTERVAL '10 days')`, table)
				require.NoError(t, err)
			}
			q := sqlc.New(r.db)
			var org, tank, module int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT d.org_id,t.id,m.id FROM device d JOIN container_tank t ON t.device_id=d.id JOIN container_module m ON m.tank_id=t.id WHERE d.id=$1 AND t.position=1`, r.databaseID).Scan(&org, &tank, &module))
			container, err := q.GetContainerPollHistory(t.Context(), sqlc.GetContainerPollHistoryParams{ContainerIdentifier: r.deviceID, OrgID: org, StartTime: start, EndTime: end, GranularitySeconds: 86400})
			require.NoError(t, err)
			require.Len(t, container, 4)
			for _, m := range container {
				if m.MeasurementType == 2 {
					require.Equal(t, 400e12, m.Average)
					require.Equal(t, 300e12, m.Minimum)
					require.Equal(t, 500e12, m.Maximum)
				}
			}
			tanks, err := q.GetContainerTankHistory(t.Context(), sqlc.GetContainerTankHistoryParams{TankID: tank, ContainerIdentifier: r.deviceID, OrgID: org, Authority: []byte(`[{"org_wide":true}]`), StartTime: start, EndTime: end, GranularitySeconds: 86400})
			require.NoError(t, err)
			require.Len(t, tanks, 4)
			for _, m := range tanks {
				if m.MeasurementType == 2 {
					require.Equal(t, 200.0, m.Average)
				}
			}
			resolution, err := q.GetHistoryResolution(t.Context(), sqlc.GetHistoryResolutionParams{StartTime: start, EndTime: end})
			require.NoError(t, err)
			modules, err := q.ListContainerModuleHistory(t.Context(), sqlc.ListContainerModuleHistoryParams{ResolutionSeconds: resolution, ModuleID: module, OrgID: org, Authority: []byte(`[{"org_wide":true}]`), StartTime: start, EndTime: end, PageLimit: 100})
			require.NoError(t, err)
			require.Len(t, modules, 1)
			require.True(t, modules[0].HashrateHs.Valid)
			require.Equal(t, 200e12, modules[0].HashrateHs.Float64)
		})
	}
}

// Migrations run in a transaction; the one-time Timescale job must refresh
// existing samples outside that transaction before enabling raw retention.
func TestContainerSummaryBootstrapAndPolicies(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-20 * 24 * time.Hour)
	var siteA, siteB int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) SELECT org_id,'A','bootstrap-a' FROM device WHERE id=$1 RETURNING id`, r.databaseID).Scan(&siteA))
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) SELECT org_id,'B','bootstrap-b' FROM device WHERE id=$1 RETURNING id`, r.databaseID).Scan(&siteB))
	_, err := r.db.ExecContext(t.Context(), `UPDATE device SET site_id=$2 WHERE id=$1`, r.databaseID, siteA)
	require.NoError(t, err)

	r.store(t, r.snapshot(time.Hour, false, 100e12, 200e12))
	oldTime := r.base.Add(time.Hour)
	recentTime := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	r.store(t, r.snapshot(recentTime.Sub(r.base), false, 100e12, 200e12))
	_, err = r.db.ExecContext(t.Context(), `UPDATE device SET site_id=$2 WHERE id=$1`, r.databaseID, siteB)
	require.NoError(t, err)
	for _, direction := range []string{"down", "up"} {
		migration, err := os.ReadFile("../../../migrations/current/001016_internal_ct1_metric_summaries." + direction + ".sql")
		require.NoError(t, err)
		// The down migration restores retention. Pause newly registered jobs
		// before committing so none can delete this backdated upgrade fixture.
		tx, err := r.db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })
		_, err = tx.ExecContext(t.Context(), string(migration))
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), `SELECT alter_job(job_id,scheduled=>false)
			FROM timescaledb_information.jobs WHERE scheduled`)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		var scheduled int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE scheduled`).Scan(&scheduled))
		require.Zero(t, scheduled, "migration-created policies must stay paused while the upgrade fixture is prepared")
		if direction == "down" {
			for _, table := range []string{"device_metrics"} {
				_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(chunk,if_not_compressed=>true) FROM show_chunks($1::regclass,older_than=>INTERVAL '1 day') chunk`, table)
				require.NoError(t, err)
			}
		}
	}
	for _, sampleTime := range []time.Time{oldTime, recentTime} {
		var restoredSites bool
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT bool_and(site_id IS NOT DISTINCT FROM $1) FROM container_module_metrics WHERE time=$2`, siteA, sampleTime).Scan(&restoredSites))
		require.True(t, restoredSites, "backfill must restore every module's original site at %s", sampleTime)
	}
	var jobs int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='policy_retention' AND hypertable_name IN ('device_metrics','container_tank_metrics','container_module_metrics')`).Scan(&jobs))
	require.Zero(t, jobs, "raw retention must wait for backfill")
	var job int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='initialize_container_metric_summaries'`).Scan(&job))
	_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.NoError(t, err)
	dbtest.PauseBackgroundJobs(t, r.db)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='initialize_container_metric_summaries'`).Scan(&jobs))
	require.Zero(t, jobs, "successful bootstrap removes its job")
	for _, table := range []string{"device_metrics", "container_tank_metrics", "container_module_metrics"} {
		_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks($1::regclass,older_than=>now()-INTERVAL '10 days')`, table)
		require.NoError(t, err)
	}

	for _, equipment := range []string{"container", "container_tank", "container_module"} {
		for _, tier := range []string{"hourly", "daily"} {
			bucketWidth := map[string]string{"hourly": "1 hour", "daily": "1 day"}[tier]
			var count int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+equipment+`_metrics_`+tier+` WHERE bucket=time_bucket($1::interval,$2::timestamptz)`, bucketWidth, oldTime).Scan(&count))
			if equipment == "container" {
				require.Zero(t, count, "compressed device history has no backfilled topology")
			} else {
				require.Positive(t, count, "bootstrap preserves 20-day-old %s %s", equipment, tier)
			}
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+equipment+`_metrics_`+tier+` WHERE bucket=time_bucket($1::interval,$2::timestamptz)`, bucketWidth, recentTime).Scan(&count))
			require.Positive(t, count, "summaries include recent %s %s", equipment, tier)
			var retention, refresh bool
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM timescaledb_information.jobs j JOIN timescaledb_information.continuous_aggregates a ON a.view_name=j.hypertable_name WHERE a.view_name=$1 AND j.proc_name='policy_retention' AND (j.config->>'drop_after')::interval=$2::interval), EXISTS(SELECT 1 FROM timescaledb_information.jobs j JOIN timescaledb_information.continuous_aggregates a ON a.view_name=j.hypertable_name WHERE a.view_name=$1 AND j.proc_name='policy_refresh_continuous_aggregate' AND (j.config->>'start_offset')::interval<INTERVAL '10 days')`, equipment+"_metrics_"+tier, map[string]string{"hourly": "3 months", "daily": "3 years"}[tier]).Scan(&retention, &refresh))
			require.True(t, retention, equipment+"_metrics_"+tier)
			require.True(t, refresh, "refresh must not revisit expired raw data")
		}
	}
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='policy_retention' AND hypertable_name IN ('device_metrics','container_tank_metrics','container_module_metrics') AND (config->>'drop_after')::interval=INTERVAL '10 days'`).Scan(&jobs))
	require.Equal(t, 3, jobs)
	var versions int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_version`).Scan(&versions))
	require.Positive(t, versions)
}

func TestContainerSummariesPreserveSiteBoundariesAndMissingValues(t *testing.T) {
	for _, age := range []time.Duration{40 * 24 * time.Hour, 400 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			r := newContainerRollupRig(t)
			r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-age)
			var org, siteA, siteB int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT org_id FROM device WHERE id=$1`, r.databaseID).Scan(&org))
			require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES ($1,'A','summary-a') RETURNING id`, org).Scan(&siteA))
			require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES ($1,'B','summary-b') RETURNING id`, org).Scan(&siteB))
			_, err := r.db.ExecContext(t.Context(), `UPDATE device SET site_id=$2 WHERE id=$1`, r.databaseID, siteA)
			require.NoError(t, err)
			r.store(t, r.snapshot(time.Hour, false, 100e12, 200e12))
			r.store(t, r.snapshot(time.Hour+10*time.Second, false, 300e12, 200e12))
			_, err = r.db.ExecContext(t.Context(), `UPDATE device SET site_id=$2 WHERE id=$1`, r.databaseID, siteB)
			require.NoError(t, err)
			missing := r.snapshot(time.Hour+20*time.Second, false, 500e12, 200e12)
			for _, rollup := range []*sdk.ContainerRollup{missing.Metrics, missing.Tanks[0].Metrics, missing.Tanks[0].Modules[0].Metrics.Rollup} {
				rollup.PowerW, rollup.EfficiencyJH = nil, nil
				rollup.Power.Reporting = 0
			}
			missing.Tanks[0].Modules[0].Metrics.PowerW = nil
			r.store(t, missing)
			// A later zero-power reading is present, unlike the missing reading above.
			zero := r.snapshot(25*time.Hour, false, 600e12, 200e12)
			for _, rollup := range []*sdk.ContainerRollup{zero.Metrics, zero.Tanks[0].Metrics, zero.Tanks[0].Modules[0].Metrics.Rollup} {
				rollup.PowerW = new(float64)
			}
			zero.Tanks[0].Modules[0].Metrics.PowerW = new(float64)
			r.store(t, zero)
			start, end := r.base, r.base.Add(48*time.Hour)
			for _, equipment := range []string{"container", "container_tank", "container_module"} {
				for _, tier := range []string{"hourly", "daily"} {
					testutil.RefreshContinuousAggregate(t, r.db, equipment+"_metrics_"+tier, &start, &end)
				}
			}
			for _, table := range []string{"device_metrics", "container_tank_metrics", "container_module_metrics"} {
				_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks($1::regclass,older_than=>now()-INTERVAL '10 days')`, table)
				require.NoError(t, err)
			}
			var tank, module int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT t.id,m.id FROM container_tank t JOIN container_module m ON m.tank_id=t.id WHERE t.device_id=$1 AND t.position=1`, r.databaseID).Scan(&tank, &module))
			q := sqlc.New(r.db)
			for _, scope := range []struct {
				name, authority    string
				hash, eff          float64
				hasPower, complete bool
			}{
				{"org", `[{"org_wide":true}]`, 300, 10, true, false},
				{"site A", fmt.Sprintf(`[{"sites":[%d]}]`, siteA), 200, 10, true, true},
				{"site B", fmt.Sprintf(`[{"sites":[%d]}]`, siteB), 500, 0, false, false},
			} {
				rows, err := q.GetContainerTankHistory(t.Context(), sqlc.GetContainerTankHistoryParams{TankID: tank, ContainerIdentifier: r.deviceID, OrgID: org, Authority: []byte(scope.authority), StartTime: start, EndTime: start.Add(24 * time.Hour), GranularitySeconds: 86400})
				require.NoError(t, err, scope.name)
				require.Len(t, rows, 4, scope.name)
				for _, m := range rows {
					switch m.MeasurementType {
					case 2:
						require.Equal(t, scope.hash, m.Average, scope.name)
					case 3:
						require.Equal(t, scope.hasPower, m.HasValues, scope.name)
						require.Equal(t, scope.complete, m.CoverageComplete, scope.name)
					case 4:
						require.InDelta(t, scope.eff, m.Average, 1e-9, "mean saved efficiency, not ratio of averaged inputs")
					}
				}
			}
			// Container charts remain limited to the current site, even within one bucket.
			containers, err := q.GetContainerPollHistory(t.Context(), sqlc.GetContainerPollHistoryParams{ContainerIdentifier: r.deviceID, OrgID: org, StartTime: start, EndTime: start.Add(24 * time.Hour), GranularitySeconds: 86400})
			require.NoError(t, err)
			require.Len(t, containers, 4)
			for _, m := range containers {
				if m.MeasurementType == 2 {
					require.Equal(t, 700e12, m.Average)
				}
			}
			resolution, err := q.GetHistoryResolution(t.Context(), sqlc.GetHistoryResolutionParams{StartTime: start, EndTime: end})
			require.NoError(t, err)
			p := sqlc.ListContainerModuleHistoryParams{ResolutionSeconds: resolution, ModuleID: module, OrgID: org, Authority: []byte(fmt.Sprintf(`[{"sites":[%d]}]`, siteB)), StartTime: start, EndTime: end, PageLimit: 1}
			modules, err := q.ListContainerModuleHistory(t.Context(), p)
			require.NoError(t, err)
			require.Len(t, modules, 1)
			require.Equal(t, 500e12, modules[0].HashrateHs.Float64)
			require.False(t, modules[0].PowerW.Valid)
			p.HasCursor = true
			p.AfterTime = modules[0].Time
			p.AfterCollectedAt = modules[0].CollectedAt
			modules, err = q.ListContainerModuleHistory(t.Context(), p)
			require.NoError(t, err)
			require.Len(t, modules, 1)
			require.True(t, modules[0].PowerW.Valid)
			require.Zero(t, modules[0].PowerW.Float64)
			var versions int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_version WHERE device_id=$1`, r.databaseID).Scan(&versions))
			require.Equal(t, 2, versions, "site provenance persists after all original samples expire")
		})
	}
}
