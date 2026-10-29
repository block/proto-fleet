package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestHostedHistoryCaptureMinerCorrectionsDoNotAddWeight(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, day, miner)
	require.NoError(t, err)
	for _, sample := range []struct {
		at   time.Time
		hash float64
	}{{day.Add(time.Minute), 100e12}, {day.Add(time.Minute), 200e12}, {day.Add(2 * time.Minute), 400e12}} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c)
 SELECT $1,device_identifier,$2,3000,55 FROM device WHERE id=$3
 ON CONFLICT(time,device_identifier) DO UPDATE SET hash_rate_hs=EXCLUDED.hash_rate_hs`, sample.at, sample.hash, miner)
		require.NoError(t, err)
	}
	refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
	var count int64
	var sum float64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT sum(hashrate_count),sum(hashrate_sum) FROM hosted_account_history_daily`).Scan(&count, &sum))
	require.EqualValues(t, 2, count, "a corrected raw observation replaces its prior weight")
	require.Equal(t, 600e12, sum)
	_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
	require.NoError(t, err)
	request := dashboardRequest(client.AccountID, day, day.Add(24*time.Hour), nil)
	request.BucketSeconds = 86400
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, 300e12, result.Readings[0].Summary.GetHashrateHs())
}

func TestHostedHistoryCaptureModuleCorrectionMovesIndependentSource(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=6 AND asset_id=$2`, day, module)
	require.NoError(t, err)
	collected := day.Add(time.Hour)
	stored := day.Add(10 * time.Minute) // A stale independent PSU chose the stored raw timestamp.
	first := collected.Add(-5 * time.Second)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage)
 VALUES($1,$2,$3,100e12,2000,55,jsonb_build_object(
 'hashrate',jsonb_build_object('oldest_at',$4::timestamptz,'newest_at',$4::timestamptz,'reporting',1,'expected',1,'topology_complete',true),
 'power',jsonb_build_object('oldest_at',$3::timestamptz,'newest_at',$3::timestamptz,'reporting',1,'expected',1,'topology_complete',true),
 'temperature',jsonb_build_object('oldest_at',$3::timestamptz,'newest_at',$3::timestamptz)))`, module, stored, collected, first)
	require.NoError(t, err)
	corrected := collected.Add(5 * time.Second)
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module_metrics SET hashrate_hs=200e12,
 coverage=jsonb_set(coverage,'{hashrate}',jsonb_build_object('oldest_at',$4::timestamptz,'newest_at',$4::timestamptz,'reporting',1,'expected',1,'topology_complete',true))
 WHERE module_id=$1 AND time=$2 AND collected_at=$3`, module, stored, collected, corrected)
	require.NoError(t, err)
	var count int64
	var at time.Time
	var value float64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),min(time),sum(value) FROM hosted_account_history_sample WHERE metric=1`).Scan(&count, &at, &value))
	require.EqualValues(t, 1, count)
	require.Equal(t, corrected, at.UTC(), "a corrected source must leave its previous chart bucket")
	require.Equal(t, 200e12, value)
	refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
	var bucket time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT bucket,hashrate_count FROM hosted_account_history_hourly`).Scan(&bucket, &count))
	require.Equal(t, day.Add(time.Hour), bucket.UTC())
	require.EqualValues(t, 1, count)
}

func TestHostedHistoryCaptureModuleStateRequiresCurrentAttribution(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
	historyEpoch(t, f, day)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=6 AND asset_id=$2`, day, module)
	require.NoError(t, err)
	at := day.Add(time.Hour)
	for _, observation := range []struct {
		observed, attribution time.Time
		state                 string
	}{
		{at, at.Add(time.Second), "error"},                   // reported for a previous attribution
		{at.Add(time.Minute), at.Add(time.Minute), "active"}, // boundary is inclusive
		{at.Add(2 * time.Minute), at, "offline"},
	} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_diagnostic_observation(module_id,collected_at,state,state_observed_at,attribution_since) VALUES($1,$2,$3,$2,$4)`, module, observation.observed, observation.state, observation.attribution)
		require.NoError(t, err)
	}
	rows, err := f.db.QueryContext(t.Context(), `SELECT time,value FROM hosted_account_history_sample WHERE metric=4 ORDER BY time`)
	require.NoError(t, err)
	defer rows.Close()
	type captured struct {
		at    time.Time
		state float64
	}
	var got []captured
	for rows.Next() {
		var c captured
		require.NoError(t, rows.Scan(&c.at, &c.state))
		c.at = c.at.UTC()
		got = append(got, c)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []captured{{at.Add(time.Minute), 3}, {at.Add(2 * time.Minute), 0}}, got, "only observations within the current attribution are captured")
}

func TestHostedHistorySummaryRollbackRemovesHistory(t *testing.T) {
	for _, withEvidence := range []bool{false, true} {
		name := "empty"
		if withEvidence {
			name = "retained"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			if withEvidence {
				client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
				miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
				day := time.Now().UTC().Truncate(24 * time.Hour).Add(-40 * 24 * time.Hour)
				historyEpoch(t, f, day)
				_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=5 AND asset_id=$2`, day, miner)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) SELECT $1,device_identifier,100e12 FROM device WHERE id=$2`, day.Add(time.Minute), miner)
				require.NoError(t, err)
				refreshHostedHistory(t, f, day, day.Add(24*time.Hour))
				_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, day.Add(24*time.Hour))
				require.NoError(t, err)
				var count int
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample`).Scan(&count))
				require.Zero(t, count)
			}
			down, err := migrations.Current.ReadFile("current/001044_internal_hosted_history_summaries.down.sql")
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), string(down))
			require.NoError(t, err)
			var removed bool
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT to_regclass('hosted_account_history_sample') IS NULL`).Scan(&removed))
			require.True(t, removed)

		})
	}
}
