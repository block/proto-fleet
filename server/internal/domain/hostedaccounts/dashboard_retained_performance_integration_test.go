package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountDashboardRetainedYearRepresentativeEstate(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.Add(-365 * 24 * time.Hour)
	historyEpoch(t, f, start)
	for range 100 {
		id := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, id, 0)))
	}
	for range 20 {
		id, _ := f.miningModule(t)
		require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, id, 0)))
	}
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	// Seed synthetic chart history without duplicating millions of fixture rows
	// into the evidence ledger. Capture correctness and cost have dedicated tests.
	seedHistory := func(from, until time.Time, cadence string) {
		t.Helper()
		tx, seedErr := f.db.BeginTx(t.Context(), nil)
		require.NoError(t, seedErr)
		defer tx.Rollback()
		var captureTrigger string
		require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgrelid='hosted_account_history_sample'::regclass AND tgname='record_hosted_reporting_evidence'`).Scan(&captureTrigger))
		// DISABLE TRIGGER does not prevent TimescaleDB from enabling its copy on
		// new chunks. Remove and restore the definition within this transaction.
		_, seedErr = tx.ExecContext(t.Context(), `DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample`)
		require.NoError(t, seedErr)
		_, seedErr = tx.ExecContext(t.Context(), `INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at)
 SELECT sample,i.id,metric,CASE metric WHEN 1 THEN 1e12 WHEN 2 THEN 20 WHEN 3 THEN 55 ELSE 3 END,CASE WHEN metric IN (1,2) THEN true END,sample,sample,1,i.asset_id::text,sample,sample
 FROM hosted_account_asset_interval i CROSS JOIN generate_series($1::timestamptz,$2::timestamptz-$3::interval,$3::interval) sample CROSS JOIN generate_series(1,4) metric WHERE i.client_id=$4`, from, until, cadence, client.ID)
		require.NoError(t, seedErr)
		_, seedErr = tx.ExecContext(t.Context(), captureTrigger)
		require.NoError(t, seedErr)
		require.NoError(t, tx.Commit())
		var captured int
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&captured))
		require.Zero(t, captured, "synthetic chart fixtures must not populate the evidence ledger")
	}
	// One sample per metric/day creates the same wide-summary row cardinality as
	// dense polls, isolating read cost from a year's ingestion.
	seedHistory(start, end, "1 day")
	refreshHostedHistory(t, f, start, end)
	_, err = f.db.ExecContext(t.Context(), `SELECT drop_chunks('hosted_account_history_sample',older_than=>$1::timestamptz)`, end)
	require.NoError(t, err)
	req := dashboardRequest(client.AccountID, start, end, nil)
	req.BucketSeconds = 32850
	began := time.Now()
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, req)
	elapsed := time.Since(began)
	require.NoError(t, err)
	require.Len(t, result.Readings, 365)
	for _, r := range result.Readings {
		require.Equal(t, 160e12, r.Summary.GetHashrateHs())
		require.Equal(t, 3200.0, r.Summary.GetPowerW())
		require.Equal(t, 20.0, r.Summary.GetEfficiencyJTh())
		require.EqualValues(t, 160, r.Summary.AssetCount)
		require.EqualValues(t, 160, r.UptimeStatus.HashingCount)
	}
	require.Less(t, elapsed, 5*time.Second, "representative retained estate must fit the mining transaction deadline")
	t.Logf("retained daily history: 100 miners + 60 hashboards,365 days,58400 wide summary rows,365 returned buckets,API duration=%s", elapsed)
	// Exercise the real-time current daily tail at a representative 15s cadence.
	// Keep it unmaterialized so this cannot silently benchmark only saved groups.
	seedHistory(end, end.Add(24*time.Hour), "15 seconds")
	req.StartTime = timestamppb.New(start.Add(24 * time.Hour))
	req.EndTime = timestamppb.New(end.Add(24 * time.Hour))
	began = time.Now()
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, req)
	elapsed = time.Since(began)
	require.NoError(t, err)
	require.Len(t, result.Readings, 365)
	require.Equal(t, 160e12, result.Readings[364].Summary.GetHashrateHs())
	require.EqualValues(t, 160, result.Readings[364].UptimeStatus.HashingCount)
	require.Less(t, elapsed, 5*time.Second, "dense real-time summary tail must fit the mining transaction deadline")
	t.Logf("same retained estate plus 24h unmaterialized 15-second tail (3686400 normalized samples),API duration=%s", elapsed)
	// Capture must resume on the chunks created while seeding the dense tail.
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_history_sample SET value=value+1
 WHERE time=$1 AND metric=1 AND interval_id=(SELECT min(id) FROM hosted_account_asset_interval WHERE client_id=$2)`, end, client.ID)
	require.NoError(t, err)
	var captured int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_reporting_evidence`).Scan(&captured))
	require.Equal(t, 2, captured, "capture must retain before and after images once fixture seeding commits")
}
