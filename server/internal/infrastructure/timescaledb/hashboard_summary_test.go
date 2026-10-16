package timescaledb

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestHashboardProvenanceUsesCollectionTopology(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	s := r.snapshot(time.Second, false, 100e12, 200e12)
	// Source time is in the previous hour; provenance belongs to this poll.
	s.Tanks[0].Modules[0].Hashboards[0].ObservedAt = r.base.Add(-time.Second)
	r.store(t, s)
	var missing int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics m
	 JOIN hashboard_installation i ON i.id=m.installation_id
	 JOIN container_module cm ON cm.id=i.module_id JOIN container_tank t ON t.id=cm.tank_id
	 JOIN container_topology_observation o ON o.device_id=t.device_id AND o.collected_at=m.collected_at
	 WHERE m.topology_version_id IS DISTINCT FROM o.version_id`).Scan(&missing))
	require.Zero(t, missing)
	var stamped int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics WHERE topology_version_id IS NOT NULL`).Scan(&stamped))
	require.Equal(t, 2, stamped)
	start, end := r.base.Add(-time.Hour), r.base.Add(time.Hour)
	testutil.RefreshContinuousAggregate(t, r.db, "hashboard_metrics_hourly", &start, &end)
	var bucket, first, last time.Time
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT bucket,first_collected_at,last_collected_at FROM hashboard_metrics_hourly WHERE bucket<$1`, r.base).Scan(&bucket, &first, &last))
	require.True(t, bucket.Equal(start))
	require.True(t, first.Equal(s.ObservedAt))
	require.True(t, last.Equal(s.ObservedAt))
}

func TestHashboardSummariesSurviveRawRetention(t *testing.T) {
	for _, age := range []time.Duration{40 * 24 * time.Hour, 400 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			r := newContainerRollupRig(t)
			r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-age)
			r.store(t, r.snapshot(time.Hour, false, 100e12, 200e12))
			r.store(t, r.snapshot(time.Hour+10*time.Second, false, 300e12, 200e12))
			start, end := r.base, r.base.Add(24*time.Hour)
			for _, tier := range []string{"hourly", "daily"} {
				testutil.RefreshContinuousAggregate(t, r.db, "hashboard_metrics_"+tier, &start, &end)
			}
			_, err := r.db.ExecContext(t.Context(), `SELECT drop_chunks('hashboard_metrics',older_than=>now()-INTERVAL '10 days')`)
			require.NoError(t, err)
			var board, org int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT hashboard_id,org_id FROM hashboard_identity WHERE kind='serial' AND value='board-a'`).Scan(&board, &org))
			resolution, err := sqlc.New(r.db).GetHistoryResolution(t.Context(), sqlc.GetHistoryResolutionParams{StartTime: start, EndTime: end})
			require.NoError(t, err)
			rows, err := sqlc.New(r.db).ListHashboardHistory(t.Context(), sqlc.ListHashboardHistoryParams{ResolutionSeconds: resolution,
				HashboardID: board, OrgID: org, Authority: []byte(`[{"org_wide":true}]`), StartTime: start, EndTime: end, PageLimit: 100,
			})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.True(t, rows[0].HashrateHs.Valid)
			require.Equal(t, 200e12, rows[0].HashrateHs.Float64)
			require.Equal(t, "unknown", rows[0].State)
			require.False(t, rows[0].Health.Valid, "summaries must not invent instantaneous diagnostics")
		})
	}
}

func TestHashboardSummaryBootstrapRetriesBeforeEnablingRetention(t *testing.T) {
	r := newContainerRollupRig(t)
	testutil.RestoreLegacySummaryBootstraps(t, r.db, "001021_internal_hashboard_metric_summaries")
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(time.Hour, false, 100e12, 200e12))
	var job, policies int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='initialize_hashboard_metric_summaries'`).Scan(&job))
	_, err := r.db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW hashboard_metrics_daily RENAME TO hashboard_metrics_daily_unavailable`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.Error(t, err)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='policy_retention' AND hypertable_name='hashboard_metrics'`).Scan(&policies))
	require.Zero(t, policies, "failed backfill must not enable raw deletion")
	_, err = r.db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW hashboard_metrics_daily_unavailable RENAME TO hashboard_metrics_daily`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.NoError(t, err)
	dbtest.PauseBackgroundJobs(t, r.db)
	for _, tier := range []struct{ table, retention string }{{"hashboard_metrics", "10 days"}, {"hashboard_metrics_hourly", "3 months"}, {"hashboard_metrics_daily", "3 years"}} {
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='policy_retention' AND hypertable_name=$1 AND (config->>'drop_after')::interval=$2::interval`, tier.table, tier.retention).Scan(&policies))
		require.Equal(t, 1, policies, tier.table)
	}
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('hashboard_metrics',older_than=>now()-INTERVAL '10 days')`)
	require.NoError(t, err)
	for _, tier := range []string{"hourly", "daily"} {
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics_`+tier).Scan(&policies))
		require.Equal(t, 2, policies)
	}
}

func TestHashboardSummaryMovesPaginationAndLegacyProvenance(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	// A physical board moves out and back in one source-time bucket.
	for n, moved := range []bool{false, true, false} {
		r.store(t, r.snapshot(time.Hour+time.Duration(n)*time.Minute, moved, 100e12, 200e12))
	}
	var board, org int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT hashboard_id,org_id FROM hashboard_identity WHERE kind='serial' AND value='board-a'`).Scan(&board, &org))
	// A legacy sample has no reconstructable version. Missing values stay missing;
	// measured zero contributes to the denominator, while NaN must not.
	_, err := r.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET topology_version_id=NULL,hashrate_hs=0,temperature_c=NULL,power_w='NaN' WHERE hashboard_id=$1 AND collected_at=$2`, board, r.base.Add(time.Hour))
	require.NoError(t, err)
	start, end := r.base.Add(time.Hour+time.Second), r.base.Add(2*time.Hour)
	refreshStart := r.base
	testutil.RefreshContinuousAggregate(t, r.db, "hashboard_metrics_hourly", &refreshStart, &end)
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('hashboard_metrics',older_than=>now()-INTERVAL '10 days')`)
	require.NoError(t, err)
	reader := fleetinventory.NewService(sqlc.New(r.db))
	req := &pb.ListHashboardHistoryRequest{HashboardId: board, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), PageSize: 1}
	installations := map[int64]bool{}
	for n := range 3 {
		response, err := reader.HashboardHistory(t.Context(), org, req, []fleetinventory.Scope{{OrgWide: true}})
		require.NoError(t, err)
		require.Len(t, response.Samples, 1)
		sample := response.Samples[0]
		require.Equal(t, int32(3600), sample.ResolutionSeconds)
		require.Equal(t, int64(1), sample.SampleCount)
		require.False(t, installations[sample.InstallationId])
		installations[sample.InstallationId] = true
		if n == 0 {
			require.Nil(t, sample.TopologyVersionId)
			require.NotNil(t, sample.HashrateHs)
			require.Zero(t, *sample.HashrateHs)
			require.Nil(t, sample.PowerW)
			require.Nil(t, sample.TemperatureC)
		} else {
			require.NotNil(t, sample.TopologyVersionId)
		}
		req.PageToken = response.NextPageToken
		require.Equal(t, n == 2, req.PageToken == "")
	}
	// Authorization remains tied to the historical installation, not current place.
	var site int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES ($1,'Historic','historic-summary') RETURNING id`, org).Scan(&site))
	_, err = r.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET site_id=$2 WHERE hashboard_id=$1 AND installed_at=$3`, board, site, r.base.Add(time.Hour))
	require.NoError(t, err)
	resolution, err := sqlc.New(r.db).GetHistoryResolution(t.Context(), sqlc.GetHistoryResolutionParams{StartTime: start, EndTime: end})
	require.NoError(t, err)
	rows, err := sqlc.New(r.db).ListHashboardHistory(t.Context(), sqlc.ListHashboardHistoryParams{ResolutionSeconds: resolution, HashboardID: board, OrgID: org, Authority: []byte(fmt.Sprintf(`[{"sites":[%d]}]`, site)), StartTime: start, EndTime: end, PageLimit: 100})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sql.NullInt64{}, rows[0].TopologyVersionID)
}
