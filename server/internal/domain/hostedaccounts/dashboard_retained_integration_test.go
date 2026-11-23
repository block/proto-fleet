package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func historyEpoch(t *testing.T, f fixture, at time.Time) {
	t.Helper()
	for _, query := range []string{"UPDATE hosted_account_history_epoch SET started_at=$1", "UPDATE hosted_account_history_summary_epoch SET started_at=$1"} {
		_, err := f.db.ExecContext(t.Context(), query, at)
		require.NoError(t, err)
	}
}

func refreshHostedHistory(t *testing.T, f fixture, start, end time.Time) {
	t.Helper()
	for _, view := range []string{"hosted_account_history_hourly", "hosted_account_history_daily"} {
		_, err := f.db.ExecContext(t.Context(), `CALL refresh_continuous_aggregate($1::regclass,$2::timestamptz,$3::timestamptz)`, view, start, end)
		require.NoError(t, err)
	}
}

func TestAccountDashboardRetainedSummaryEpochFollowsAllocationEpoch(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.Add(-30 * 24 * time.Hour)
	summaryEpoch := start.Add(15 * 24 * time.Hour)
	historyEpoch(t, f, start)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_history_summary_epoch SET started_at=$1`, summaryEpoch)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	for _, at := range []time.Time{summaryEpoch.Add(-time.Minute), summaryEpoch} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w)
 SELECT $1,device_identifier,100e12,2000 FROM device WHERE id=$2`, at, miner)
		require.NoError(t, err)
	}
	var before, after int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE time<$1),count(value) FILTER(WHERE time>=$1) FROM hosted_account_history_sample`, summaryEpoch).Scan(&before, &after))
	require.Zero(t, before, "existing ownership must not admit samples before summary capture launched")
	require.Positive(t, after, "the summary epoch itself is inclusive")
	refreshHostedHistory(t, f, start, end)
	request := dashboardRequest(client.AccountID, start, end, nil)
	request.BucketSeconds = int32(24 * time.Hour / time.Second)
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, summaryEpoch, result.HistoryAvailableFrom.AsTime())
	for _, reading := range result.Readings {
		if reading.Time.AsTime().Before(summaryEpoch) {
			require.Nil(t, reading.Summary.HashrateHs, "pre-launch days remain empty in a 30d request")
		}
	}
	require.Equal(t, 100e12, result.Readings[15].Summary.GetHashrateHs())
}

func TestAccountDashboardAllFleetRanges(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	end := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		span       time.Duration
		bucket     int32
		count      int
		resolution time.Duration
	}{
		{time.Hour, 90, 40, 0}, {24 * time.Hour, 90, 960, 0}, {7 * 24 * time.Hour, 900, 168, time.Hour},
		{30 * 24 * time.Hour, 2700, 30, 24 * time.Hour}, {365 * 24 * time.Hour, 32850, 365, 24 * time.Hour},
	} {
		req := dashboardRequest(client.AccountID, end.Add(-tc.span), end, nil)
		req.BucketSeconds = tc.bucket
		result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
		require.NoError(t, err)
		count := tc.count
		// Retained ranges start at their source period, adding a partial bucket.
		if tc.resolution > 0 && !end.Equal(end.Truncate(tc.resolution)) {
			count++
		}
		require.Len(t, result.Readings, count)
		for _, reading := range result.Readings {
			require.Nil(t, reading.Summary.HashrateHs, "unavailable history must remain absent")
			require.Nil(t, reading.Summary.PowerW)
			require.Zero(t, reading.UptimeStatus.HashingCount)
		}
	}
}

func TestAccountDashboardRetainedAllocationIsolationAndExpiry(t *testing.T) {
	for _, age := range []time.Duration{20 * 24 * time.Hour, 120 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			other := f.client(t, "Other", f.host, hostedaccounts.AllCapabilities)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
			day := time.Now().UTC().Truncate(24 * time.Hour).Add(-age)
			historyEpoch(t, f, day.Add(-24*time.Hour))
			boundary := day.Add(30 * time.Minute)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE asset_id=$3 AND kind=5`, day, boundary, miner)
			require.NoError(t, err)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 2, selected(5, miner, 1)))
			require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, 1, selected(5, miner, 2)))
			_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE asset_id=$2 AND kind=5 AND ended_at IS NULL`, boundary, miner)
			require.NoError(t, err)
			for _, sample := range []struct {
				at    time.Time
				hash  float64
				state int
			}{
				{day.Add(5 * time.Minute), 100e12, 3}, {day.Add(10 * time.Minute), 0, 0}, {boundary, 900e12, 2},
			} {
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT $1,device_identifier,$2,2000,55 FROM device WHERE id=$3`, sample.at, sample.hash, miner)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO miner_state_snapshots(time,org_id,device_identifier,state) SELECT $1,org_id,device_identifier,$2 FROM device WHERE id=$3`, sample.at, sample.state, miner)
				require.NoError(t, err)
			}
			var count int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&count))
			require.Positive(t, count, "capture must retain authorized source rows")
			refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_daily`).Scan(&count))
			require.Positive(t, count, "materialization must retain source rows")
			// Read genuine materialized summaries after raw history has gone away.
			_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
			require.NoError(t, err)
			query := func(account string, actor int64) *pb.GetAccountDashboardResponse {
				t.Helper()
				req := dashboardRequest(account, day, day.Add(24*time.Hour), nil)
				req.BucketSeconds = 86400
				result, err := f.svc.Dashboard(t.Context(), actor, f.org, req)
				require.NoError(t, err)
				require.Len(t, result.Readings, 1)
				return result
			}
			first := query(client.AccountID, f.user).Readings[0]
			require.Equal(t, 50e12, first.Summary.GetHashrateHs(), "other client's readings in the same source bucket stay isolated")
			require.Equal(t, 2000.0, first.Summary.GetPowerW())
			require.Equal(t, 40.0, first.Summary.GetEfficiencyJTh(), "efficiency retains the paired fleet ratio")
			require.Equal(t, 50.0, first.Summary.GetUptimePercent(), "chart uptime remains positive-hash sample percentage")
			require.EqualValues(t, 1, first.UptimeStatus.NotHashingCount, "latest saved state survives retention")
			require.EqualValues(t, 1, first.TemperatureStatus.OkCount)
			require.Equal(t, 900e12, query(other.AccountID, f.host).Readings[0].Summary.GetHashrateHs())
			require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.AllCapabilities, true))
			_, err = f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, day, day.Add(24*time.Hour), nil))
			require.Error(t, err, "suspension withdraws retained history too")
		})
	}
}

func TestAccountDashboardRejectsUnboundedRanges(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	end := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name   string
		span   time.Duration
		bucket int32
	}{
		{"empty range", 0, 3600}, {"longer than 366 days", 367 * 24 * time.Hour, 86400},
		{"bucket below one minute", time.Hour, 59}, {"bucket above one day", 30 * 24 * time.Hour, 86401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := dashboardRequest(client.AccountID, end.Add(-tc.span), end, nil)
			req.BucketSeconds = tc.bucket
			_, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
			require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
		})
	}
}

func TestAccountDashboardRetainedRevalidatesChangedIntervals(t *testing.T) {
	for _, age := range []time.Duration{20 * 24 * time.Hour, 120 * 24 * time.Hour} {
		for _, change := range []string{"recalled before source", "started after source"} {
			t.Run(age.String()+"/"+change, func(t *testing.T) {
				f := setup(t)
				client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
				miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
				day := time.Now().UTC().Truncate(24 * time.Hour).Add(-age)
				historyEpoch(t, f, day.Add(-24*time.Hour))
				_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, day, miner)
				require.NoError(t, err)
				at := day.Add(30 * time.Minute)
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,100e12,2000 FROM device WHERE id=$2`, at, miner)
				require.NoError(t, err)
				refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
				query := func() *pb.AccountDashboardReading {
					t.Helper()
					req := dashboardRequest(client.AccountID, day, day.Add(24*time.Hour), nil)
					req.BucketSeconds = 86400
					result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
					require.NoError(t, err)
					require.Len(t, result.Readings, 1)
					return result.Readings[0]
				}
				require.Equal(t, 100e12, query().Summary.GetHashrateHs(), "captured while authorized")
				// Change the interval after capture so only the read-side checks can exclude it.
				update, boundary := `UPDATE hosted_account_asset_interval SET ended_at=$1 WHERE kind=5 AND asset_id=$2`, at.Add(-10*time.Minute)
				if change == "started after source" {
					update, boundary = `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, at.Add(10*time.Minute)
				}
				_, err = f.db.ExecContext(t.Context(), update, boundary, miner)
				require.NoError(t, err)
				require.Nil(t, query().Summary.HashrateHs, "a retained source outside the current interval must disappear")
			})
		}
	}
}

func TestAccountDashboardRetainedModuleSourcesAndCoverage(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	f.legacyModuleHistory(t)
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day.Add(-24*time.Hour))
	began := day.Add(30 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=6 AND asset_id=$2`, began, module)
	require.NoError(t, err)
	at := day.Add(time.Hour)
	insert := func(at, oldest time.Time, missing bool) {
		t.Helper()
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) VALUES($1,$2,$2,100e12,2000,80,jsonb_build_object('hashrate',CASE WHEN $4 THEN '{}'::jsonb ELSE jsonb_build_object('reporting',1,'expected',1,'topology_complete',true,'oldest_at',$3::timestamptz,'newest_at',$2::timestamptz) END,'power',jsonb_build_object('reporting',1,'expected',1,'topology_complete',true,'oldest_at',$2::timestamptz,'newest_at',$2::timestamptz),'temperature',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, at, oldest, missing)
		require.NoError(t, err)
	}
	insert(at, at, false)
	insert(at.Add(time.Minute), day, true)
	insert(at.Add(2*time.Minute), day, false) // crossing allocation: hash excluded, independently fresh power retained
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,state,state_observed_at) VALUES($1,$2,'maintenance',$2),($1,$3,'active',$4)`, module, at, at.Add(time.Minute), day)
	require.NoError(t, err)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&count))
	require.Positive(t, count, "capture must retain authorized source rows")
	refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_daily`).Scan(&count))
	require.Positive(t, count, "materialization must retain source rows")
	req := dashboardRequest(client.AccountID, day, day.Add(24*time.Hour), nil)
	req.BucketSeconds = 86400
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
	require.NoError(t, err)
	summary := result.Readings[0].Summary
	require.Equal(t, 100e12, summary.GetHashrateHs())
	require.Equal(t, 2000.0, summary.GetPowerW())
	require.EqualValues(t, 1, summary.PartialModuleCount)
	require.Nil(t, summary.EfficiencyJTh, "authorized incomplete markers veto efficiency after retention")
	require.EqualValues(t, 1, result.Readings[0].UptimeStatus.NotHashingCount)
	require.Zero(t, result.Readings[0].UptimeStatus.HashingCount, "stale module health cannot become current within an aggregate")
	require.EqualValues(t, 1, result.Readings[0].TemperatureStatus.HotCount)
}

func TestAccountDashboardRetainedSiteMoveAndCurrentPolicy(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", 0, hostedaccounts.AllCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, client.AccountID, f.user, hostedaccounts.RoleViewer))
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var first, second int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'First','first') RETURNING id`, f.org).Scan(&first))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Second','second') RETURNING id`, f.org).Scan(&second))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, first, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, second, miner)
	require.NoError(t, err)
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-120 * 24 * time.Hour)
	historyEpoch(t, f, day.Add(-24*time.Hour))
	boundary := day.Add(time.Hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE device_id=$3 AND site_id=$4`, day, boundary, miner, first)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE device_id=$2 AND site_id=$3`, boundary, miner, second)
	require.NoError(t, err)
	for _, s := range []struct {
		at   time.Time
		site int64
		hash float64
	}{
		{day.Add(30 * time.Minute), first, 100e12}, {boundary.Add(30 * time.Minute), second, 300e12}, {day.Add(45 * time.Minute), second, 999e12},
	} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w) SELECT $1,device_identifier,$2,$3,3000 FROM device WHERE id=$4`, s.at, s.site, s.hash, miner)
		require.NoError(t, err)
	}
	refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
	query := func(actor int64) *pb.GetAccountDashboardResponse {
		t.Helper()
		req := dashboardRequest(client.AccountID, day, day.Add(24*time.Hour), nil)
		req.BucketSeconds = 86400
		result, err := f.svc.Dashboard(t.Context(), actor, f.org, req)
		require.NoError(t, err)
		return result
	}
	require.Equal(t, 200e12, query(f.user).Readings[0].Summary.GetHashrateHs())
	require.Equal(t, 200e12, query(f.host).Readings[0].Summary.GetHashrateHs(), "host preview reads the same authorized history")
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, client.AccountID, true, []int64{second}))
	require.Equal(t, 300e12, query(f.user).Readings[0].Summary.GetHashrateHs(), "current scope withdraws old-site readings")
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, client.AccountID, true, nil))
	require.Nil(t, query(f.user).Readings[0].Summary.HashrateHs)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, client.AccountID, true, []int64{first}))
	require.Equal(t, 100e12, query(f.user).Readings[0].Summary.GetHashrateHs(), "scope restoration uses retained site provenance")
}

func TestAccountDashboardRetainedNonAlignedAllocationBucket(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-20 * 24 * time.Hour)
	historyEpoch(t, f, day)
	began := day.Add(13*time.Hour + 30*time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, began, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,1e12,20 FROM device WHERE id=$2`, began.Add(15*time.Minute), miner)
	require.NoError(t, err)
	req := dashboardRequest(client.AccountID, day.Add(12*time.Hour+30*time.Minute), day.Add(15*time.Hour+30*time.Minute), nil)
	req.BucketSeconds = 3600
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
	require.NoError(t, err)
	require.Len(t, result.Readings, 4, "include whole intersecting source buckets")
	var measured []*pb.AccountDashboardReading
	for _, r := range result.Readings {
		require.GreaterOrEqual(t, r.Summary.UnknownCount, int64(0), "source and expected-allocation buckets must align")
		if r.Summary.HashrateHs != nil {
			measured = append(measured, r)
		}
	}
	require.Len(t, measured, 1, "the boundary sample must appear and other buckets must remain unavailable")
	require.Equal(t, 1e12, *measured[0].Summary.HashrateHs)
	require.Equal(t, day.Add(13*time.Hour), measured[0].Time.AsTime(), "retained timestamps identify the actual source bucket")
	require.EqualValues(t, 1, measured[0].Summary.AssetCount)
}

func TestAccountDashboardRetainedMixedBucketAfterClose(t *testing.T) {
	for _, age := range []time.Duration{20 * 24 * time.Hour, 120 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			affected := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			unaffected := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, affected, 0), selected(5, unaffected, 0)))
			day := time.Now().UTC().Truncate(24 * time.Hour).Add(-age)
			historyEpoch(t, f, day)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, day, client.ID)
			require.NoError(t, err)
			// Both writes see an open interval. The second source will lie beyond the
			// final close boundary, as can happen with clock skew or an uncommitted recall.
			for _, at := range []time.Time{day.Add(10 * time.Minute), day.Add(50 * time.Minute)} {
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,100e12,2000 FROM device WHERE id=$2`, at, affected)
				require.NoError(t, err)
			}
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,200e12,4000 FROM device WHERE id=$2`, day.Add(10*time.Minute), unaffected)
			require.NoError(t, err)
			refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
			_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=$1 WHERE kind=5 AND asset_id=$2`, day.Add(30*time.Minute), affected)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
			require.NoError(t, err)
			end := day.Add(time.Hour)
			bucketSeconds := int32(time.Hour / time.Second)
			if age > 90*24*time.Hour {
				end = day.Add(24 * time.Hour)
				bucketSeconds = int32(24 * time.Hour / time.Second)
			}
			req := dashboardRequest(client.AccountID, day, end, nil)
			req.BucketSeconds = bucketSeconds
			result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
			require.NoError(t, err)
			require.Len(t, result.Readings, 1)
			require.Equal(t, 200e12, result.Readings[0].Summary.GetHashrateHs(), "the affected interval's mixed bucket is unavailable; another allocation still reports")
			require.Equal(t, 4000.0, result.Readings[0].Summary.GetPowerW())
		})
	}
}

func TestAccountDashboardWideSummaryAuthorizesMetricsIndependently(t *testing.T) {
	for _, age := range []time.Duration{20 * 24 * time.Hour, 120 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			module, _ := f.miningModule(t)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
			f.legacyModuleHistory(t)
			day := time.Now().UTC().Truncate(24 * time.Hour).Add(-age)
			historyEpoch(t, f, day)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=6 AND asset_id=$2`, day, module)
			require.NoError(t, err)
			for _, at := range []time.Time{day.Add(10 * time.Minute), day.Add(50 * time.Minute)} {
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage)
    VALUES($1,$2,$2,100e12,2000,80,jsonb_build_object(
     'hashrate',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',1,'expected',1,'topology_complete',true),
     'power',jsonb_build_object('oldest_at',$3::timestamptz,'newest_at',$3::timestamptz,'reporting',1,'expected',1,'topology_complete',true),
     'temperature',jsonb_build_object('oldest_at',$3::timestamptz,'newest_at',$3::timestamptz)))`, module, at, day.Add(10*time.Minute))
				require.NoError(t, err)
			}
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,state,state_observed_at) VALUES($1,$2,'active',$2)`, module, day.Add(15*time.Minute))
			require.NoError(t, err)
			refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
			var hourlyRows, dailyRows int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM hosted_account_history_hourly),
 (SELECT count(*) FROM hosted_account_history_daily)`).Scan(&hourlyRows, &dailyRows))
			require.Equal(t, 1, hourlyRows, "all four metrics occupy one allocation/hour row")
			require.Equal(t, 1, dailyRows, "all four metrics occupy one allocation/day row")
			_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET ended_at=$1 WHERE kind=6 AND asset_id=$2`, day.Add(30*time.Minute), module)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
			require.NoError(t, err)
			end := day.Add(time.Hour)
			bucketSeconds := int32(time.Hour / time.Second)
			if age > 90*24*time.Hour {
				end = day.Add(24 * time.Hour)
				bucketSeconds = int32(24 * time.Hour / time.Second)
			}
			req := dashboardRequest(client.AccountID, day, end, nil)
			req.BucketSeconds = bucketSeconds
			result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
			require.NoError(t, err)
			require.Len(t, result.Readings, 1)
			reading := result.Readings[0]
			require.Nil(t, reading.Summary.HashrateHs, "hash evidence crosses the final close boundary")
			require.Equal(t, 2000.0, reading.Summary.GetPowerW(), "authorized power survives in the same wide row")
			require.Nil(t, reading.Summary.EfficiencyJTh)
			require.EqualValues(t, 1, reading.TemperatureStatus.HotCount)
			require.EqualValues(t, 1, reading.UptimeStatus.HashingCount)
		})
	}
}
