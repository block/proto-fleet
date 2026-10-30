package hostedaccounts_test

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestRetainedHashboardHistorySplitsReplacementAndCorrectsSamples(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	old, replacement := boards[0].Id, boards[1].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, old, 0)))
	require.NoError(t, f.svc.ReplaceHashboard(t.Context(), f.host, f.org, &pb.ReplaceAccountHashboardRequest{
		AccountId:        f.root,
		OldBoard:         &pb.HashboardAllocationSelection{Id: old, ExpectedRevision: 1},
		ReplacementBoard: &pb.HashboardAllocationSelection{Id: replacement},
	}))
	start := time.Now().UTC().Truncate(24 * time.Hour).Add(-48 * time.Hour)
	boundary := start.Add(30 * time.Minute)
	end := start.Add(48 * time.Hour)
	historyEpoch(t, f, start)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE kind=7 AND asset_id=$3`, start, boundary, old)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=7 AND asset_id=$2`, boundary, replacement)
	require.NoError(t, err)
	for _, sample := range []struct {
		board int64
		at    time.Time
		hash  float64
	}{
		{old, start.Add(10 * time.Minute), 100e12},
		{old, boundary, 900e12},
		{replacement, boundary.Add(-time.Second), 900e12},
		{replacement, boundary.Add(time.Minute), 200e12},
	} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs,power_w,temperature_c)
		 SELECT $1,id,$2,$2,'active','',$3,1000,50 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, sample.board, sample.at, sample.hash)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_diagnostic_observation(hashboard_id,installation_id,collected_at,state_observed_at,state,mining_state)
		 SELECT $1,id,$2,$2,CASE WHEN $1::bigint=$3::bigint THEN 'maintenance' ELSE 'active' END,'' FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, sample.board, sample.at, old)
		require.NoError(t, err)
	}
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET hashrate_hs=150e12 WHERE hashboard_id=$1 AND time=$2`, old, start.Add(10*time.Minute))
	require.NoError(t, err)
	refreshHostedHistory(t, f, start, end)
	report, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.NoError(t, err)
	require.Equal(t, start, report.Readings[0].Time.AsTime())
	require.Equal(t, boundary, report.Readings[1].Time.AsTime())
	require.Equal(t, 150e12, report.Readings[0].Summary.GetHashrateHs())
	require.Equal(t, 200e12, report.Readings[1].Summary.GetHashrateHs())
	require.EqualValues(t, 1, report.Readings[0].UptimeStatus.NotHashingCount)
	require.EqualValues(t, 1, report.Readings[1].UptimeStatus.HashingCount)
	for _, reading := range report.Readings {
		require.EqualValues(t, 1, reading.Summary.HashboardCount)
		require.LessOrEqual(t, reading.Summary.HashrateReportingCount, int64(1))
	}
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hashboard_metrics WHERE hashboard_id=$1`, old)
	require.NoError(t, err)
	var remaining int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_history_sample WHERE source_id=5 AND origin_key=$1::bigint::text`, old).Scan(&remaining))
	require.Zero(t, remaining, "deleting raw evidence must remove its retained contribution")
}
