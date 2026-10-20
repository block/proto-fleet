package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func dashboardRequest(account string, start, end time.Time, owner *string) *pb.GetAccountDashboardRequest {
	return &pb.GetAccountDashboardRequest{AccountId: account, StartTime: timestamppb.New(start), EndTime: timestamppb.New(end), BucketSeconds: 14400, OwnerAccountId: owner}
}
func TestAccountDashboardHistoryAttributionRecallAndSiteRestoration(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Delegated", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	sibling := f.client(t, "Other customer", f.host, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Allocated site','allocated-site') RETURNING id`, f.org).Scan(&site))
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(5, miner, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 2, selected(5, miner, 2)))
	require.NoError(t, f.allocate(t, f.host, f.root, sibling.AccountID, 1, selected(5, miner, 3)))
	// Place the actual mutation-generated intervals at deterministic historical
	// boundaries. Production creates these atomically with each ledger revision.
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-4 * time.Hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE client_id=$3 AND subclient_id IS NULL`, start.Add(time.Hour), start.Add(2*time.Hour), client.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE subclient_id=$3`, start.Add(2*time.Hour), start.Add(3*time.Hour), child.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start.Add(3*time.Hour), sibling.ID)
	require.NoError(t, err)
	for _, sample := range []struct {
		at   time.Time
		hash float64
	}{{start.Add(30 * time.Minute), 900e12}, {start.Add(90 * time.Minute), 100e12}, {start.Add(150 * time.Minute), 200e12}, {start.Add(210 * time.Minute), 900e12}} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c,site_id) SELECT $1,device_identifier,$2,3000,60,$3 FROM device WHERE id=$4`, sample.at, sample.hash, site, miner)
		require.NoError(t, err)
	}
	query := func(account string, actor int64, owner *string) *pb.GetAccountDashboardResponse {
		result, err := f.svc.Dashboard(t.Context(), actor, f.org, dashboardRequest(account, start, end, owner))
		require.NoError(t, err)
		require.Len(t, result.Readings, 1)
		return result
	}
	all := query(client.AccountID, f.user, nil)
	require.Zero(t, all.Totals.AssetCount, "recall does not retain current allocation")
	require.NotNil(t, all.HistoryAvailableFrom)
	require.Equal(t, 150e12, all.Readings[0].Summary.GetHashrateHs(), "unknown and other-client periods cannot enter a crossing bucket")
	require.EqualValues(t, 1, all.Readings[0].Summary.AssetCount, "parent must not double-count delegated leaf")
	require.Equal(t, 100e12, query(client.AccountID, f.user, &client.AccountID).Readings[0].Summary.GetHashrateHs())
	require.Equal(t, 200e12, query(client.AccountID, f.user, &child.AccountID).Readings[0].Summary.GetHashrateHs())
	require.Equal(t, 200e12, query(child.AccountID, f.host, nil).Readings[0].Summary.GetHashrateHs())
	require.Len(t, all.Sites, 1, "history-only site must remain selectable after recall")
	_, err = f.svc.AssetDetail(t.Context(), f.user, f.org, &pb.GetAccountAssetDetailRequest{AccountId: client.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: miner}})
	require.True(t, fleeterror.IsNotFoundError(err), "retained history grants no current details: %v", err)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, client.AccountID, true, nil))
	revoked := query(client.AccountID, f.user, nil)
	require.Nil(t, revoked.Readings[0].Summary.HashrateHs)
	require.Empty(t, revoked.Sites)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, client.AccountID, true, []int64{site}))
	require.Equal(t, 150e12, query(client.AccountID, f.user, nil).Readings[0].Summary.GetHashrateHs(), "permission restoration restores retained samples")
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, client.AccountID, f.host, hostedaccounts.RoleViewer))
	_, err = f.svc.Dashboard(t.Context(), f.host, f.org, dashboardRequest(client.AccountID, start, end, &child.AccountID))
	require.Error(t, err, "viewer cannot probe delegation")
}

func TestAccountDashboardNoBackfillMissingMetricsAndTypedModules(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, controller := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0), selected(6, module, 0)))
	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,999e12,99999 FROM device WHERE id IN ($2,$3)`, start.Add(time.Minute), miner, controller)
	require.NoError(t, err)
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.NoError(t, err)
	require.Nil(t, result.Readings[0].Summary.HashrateHs, "current ownership cannot infer old ownership")
	// Establish both typed allocations before a sample; no miner sample is present.
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,coverage) VALUES($1,$2,$2,100e12,2000,jsonb_build_object('hashrate',jsonb_build_object('reporting',1,'expected',2,'topology_complete',true,'oldest_at',$2::timestamptz,'newest_at',$2::timestamptz),'power',jsonb_build_object('reporting',1,'expected',2,'topology_complete',true,'oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, start.Add(20*time.Minute))
	require.NoError(t, err)
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start.Add(10*time.Minute), end, nil))
	require.NoError(t, err)
	summary := result.Readings[0].Summary
	require.EqualValues(t, 2, summary.AssetCount)
	require.Equal(t, 100e12, summary.GetHashrateHs(), "controller measurements never substitute for module readings")
	require.Equal(t, 2000.0, summary.GetPowerW())
	require.EqualValues(t, 1, summary.PowerReportingCount)
	require.Nil(t, summary.EfficiencyJTh, "missing selected leaf and partial module coverage invalidate efficiency")
}

func TestAccountDashboardSiteMoveCrossingBucketAndDeletedIdentifierReuse(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var first, second int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'First','first') RETURNING id`, f.org).Scan(&first))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Second','second') RETURNING id`, f.org).Scan(&second))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, first, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, second, miner)
	require.NoError(t, err)
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-2 * time.Hour)
	boundary := start.Add(time.Hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE device_id=$3 AND site_id=$4`, start, boundary, miner, first)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE device_id=$2 AND site_id=$3`, boundary, miner, second)
	require.NoError(t, err)
	for _, sample := range []struct {
		at   time.Time
		site int64
		hash float64
	}{{start.Add(30 * time.Minute), first, 100e12}, {boundary.Add(30 * time.Minute), second, 300e12}, {start.Add(45 * time.Minute), second, 999e12}} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w) SELECT $1,device_identifier,$2,$3,3000 FROM device WHERE id=$4`, sample.at, sample.site, sample.hash, miner)
		require.NoError(t, err)
	}
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.NoError(t, err)
	summary := result.Readings[0].Summary
	require.Equal(t, 200e12, summary.GetHashrateHs(), "both authorized placements contribute; mismatched site stamp does not")
	require.EqualValues(t, 1, summary.AssetCount)
	require.EqualValues(t, 1, summary.UptimeReportingCount, "site move never duplicates the leaf")
	require.Equal(t, 15.0, summary.GetEfficiencyJTh())
	request := dashboardRequest(client.AccountID, start, end, nil)
	request.SiteId = &first
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, 100e12, result.Readings[0].Summary.GetHashrateHs())
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET deleted_at=now() WHERE id=$1`, miner)
	require.NoError(t, err)
	var ended time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT ended_at FROM hosted_account_asset_interval WHERE device_id=$1 ORDER BY started_at DESC LIMIT 1`, miner).Scan(&ended))
	// A future sample reusing this legacy identifier must not enter the retained
	// customer's history after the physical device's deletion boundary.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w) SELECT $1,device_identifier,$2,999e12,99999 FROM device WHERE id=$3`, ended.Add(time.Second), second, miner)
	require.NoError(t, err)
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, ended.Add(time.Minute), nil))
	require.NoError(t, err)
	require.Equal(t, 200e12, result.Readings[0].Summary.GetHashrateHs())
}

func TestAccountDashboardTypedIDCollisionAndCompleteCoverage(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Module owner", f.user, hostedaccounts.AllCapabilities)
	other := f.client(t, "Miner owner", f.host, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, controller := f.miningModule(t)
	require.Equal(t, miner, module, "fixture deliberately exercises overlapping numeric IDs")
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, 1, selected(5, miner, 0)))
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-time.Hour)
	sample := start.Add(30 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE organization_id=$2`, start, f.org)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,999e12,99999 FROM device WHERE id IN ($2,$3)`, sample, miner, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) VALUES($1,$2,$2,100e12,2000,55,jsonb_build_object('hashrate',jsonb_build_object('reporting',2,'expected',2,'topology_complete',true,'oldest_at',$2::timestamptz,'newest_at',$2::timestamptz),'power',jsonb_build_object('reporting',2,'expected',2,'topology_complete',true,'oldest_at',$2::timestamptz,'newest_at',$2::timestamptz),'temperature',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, sample)
	require.NoError(t, err)
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.NoError(t, err)
	summary := result.Readings[0].Summary
	require.Equal(t, 100e12, summary.GetHashrateHs())
	require.Equal(t, 2000.0, summary.GetPowerW())
	require.Equal(t, 55.0, summary.GetTemperatureC())
	require.Equal(t, 20.0, summary.GetEfficiencyJTh())
	require.EqualValues(t, 1, summary.ModuleCount)
	require.Zero(t, summary.MinerCount)
	// Raw requests support one day; earlier unavailable buckets stay explicit.
	request := dashboardRequest(client.AccountID, end.Add(-24*time.Hour), end, nil)
	request.BucketSeconds = 3600
	request.StartTime.Nanos = 321
	request.EndTime.Nanos = 654
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, result.Readings, 24)
	require.Nil(t, result.Readings[0].Summary.HashrateHs)
	require.Equal(t, 100e12, result.Readings[len(result.Readings)-1].Summary.GetHashrateHs(), "sub-microsecond request timestamps use the same bucket keys as PostgreSQL")
	request.StartTime = timestamppb.New(end.Add(-24*time.Hour - time.Second))
	_, err = f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.True(t, fleeterror.IsInvalidArgumentError(err), "longer ranges require source-authorized long-term storage")
}

func TestAccountDashboardHistoryAvailabilityTracksRetention(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	end := time.Now().UTC().Truncate(time.Second)
	oldEpoch := end.Add(-30 * 24 * time.Hour)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_history_epoch SET started_at=$1`, oldEpoch)
	require.NoError(t, err)
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, end.Add(-time.Hour), end, nil))
	require.NoError(t, err)
	require.Equal(t, result.GeneratedAt.AsTime().Add(-10*24*time.Hour), result.HistoryAvailableFrom.AsTime())
	newEpoch := end.Add(-time.Hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_history_epoch SET started_at=$1`, newEpoch)
	require.NoError(t, err)
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, end.Add(-time.Hour), end, nil))
	require.NoError(t, err)
	require.Equal(t, newEpoch, result.HistoryAvailableFrom.AsTime(), "the launch boundary still limits history when it is newer than retention")
}

func TestAccountModuleHistoryUsesCollectionScanWithIndependentSources(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	start := end.Add(-3 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start.Add(30*time.Second), client.ID)
	require.NoError(t, err)
	// A cached PSU pins the stored sample time before the chart even though
	// independently dated board hashrate and temperature remain fresh.
	stored := start.Add(-time.Hour)
	insert := func(collected, hashOld, hashNew, tempAt time.Time, hash, temperature float64) {
		t.Helper()
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) VALUES($1,$2,$3,$7,NULL,$8,jsonb_build_object('hashrate',jsonb_build_object('oldest_at',$4::timestamptz,'newest_at',$5::timestamptz,'reporting',1,'expected',1,'topology_complete',true),'power',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz,'reporting',0,'expected',1,'topology_complete',true),'temperature',jsonb_build_object('oldest_at',$6::timestamptz,'newest_at',$6::timestamptz)))`, module, stored, collected, hashOld, hashNew, tempAt, hash, temperature)
		require.NoError(t, err)
	}
	insert(start.Add(90*time.Second), start.Add(time.Minute), start.Add(time.Minute), start.Add(75*time.Second), 100e12, 55)
	insert(end.Add(30*time.Second), end.Add(-30*time.Second), end.Add(-30*time.Second), end.Add(-20*time.Second), 200e12, 65)
	insert(start.Add(10*time.Second), start.Add(-30*time.Second), start.Add(-30*time.Second), start.Add(-20*time.Second), 900e12, 900)
	insert(start.Add(110*time.Second), start.Add(20*time.Second), start.Add(90*time.Second), end.Add(10*time.Second), 900e12, 900)
	request := dashboardRequest(client.AccountID, start, end, nil)
	request.BucketSeconds = 60
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, result.Readings, 3)
	require.Nil(t, result.Readings[0].Summary.HashrateHs, "out-of-range and previous-owner sources remain excluded")
	require.Nil(t, result.Readings[0].Summary.TemperatureC)
	require.Equal(t, 100e12, result.Readings[1].Summary.GetHashrateHs(), "fresh sources survive an old stored PSU time; crossing ownership windows do not")
	require.Equal(t, 55.0, result.Readings[1].Summary.GetTemperatureC())
	require.Equal(t, 200e12, result.Readings[2].Summary.GetHashrateHs(), "late collection retains an in-range source")
	require.Equal(t, 65.0, result.Readings[2].Summary.GetTemperatureC(), "out-of-range temperature sources do not enter the collection bucket")
	for _, reading := range result.Readings {
		require.Nil(t, reading.Summary.PowerW, "stale PSU sources remain unavailable")
		require.Nil(t, reading.Summary.EfficiencyJTh)
	}
	var indexed bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE tablename='container_module_metrics' AND indexname='idx_container_module_metrics_module_collected_at' AND indexdef LIKE '%(module_id, collected_at DESC)%')`).Scan(&indexed))
	require.True(t, indexed, "collection scans have an equipment/collection index")
}

func TestAccountCurrentReadingsRejectPreviousOwnerSamples(t *testing.T) {
	f := setup(t)
	first := f.client(t, "First", f.host, hostedaccounts.AllCapabilities)
	second := f.client(t, "Second", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 1, selected(5, miner, 0)))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT clock_timestamp(),device_identifier,200e12,4000,65 FROM device WHERE id=$1`, miner)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_status(device_id,status,status_timestamp) VALUES($1,'ACTIVE',clock_timestamp())`, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 2, selected(5, miner, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, second.AccountID, 1, selected(5, miner, 2)))
	assertUnavailable := func() {
		t.Helper()
		summary, err := f.svc.MiningSummary(t.Context(), f.user, f.org, second.AccountID)
		require.NoError(t, err)
		require.EqualValues(t, 1, summary.Totals.AssetCount)
		require.Nil(t, summary.Totals.HashrateHs, "new owner cannot see cached previous-owner readings")
		detail, err := f.svc.AssetDetail(t.Context(), f.user, f.org, &pb.GetAccountAssetDetailRequest{AccountId: second.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: miner}})
		require.NoError(t, err)
		require.Nil(t, detail.Asset.PowerW)
		require.Nil(t, detail.Asset.ObservedAt)
		require.Equal(t, "unknown", detail.Asset.State)
		page, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: second.AccountID, PageSize: 1})
		require.NoError(t, err)
		require.Nil(t, page.Assets[0].HashrateHs)
	}
	assertUnavailable()
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w,temp_c) SELECT clock_timestamp(),device_identifier,100e12,2000,55 FROM device WHERE id=$1`, miner)
	require.NoError(t, err)
	detail, err := f.svc.AssetDetail(t.Context(), f.user, f.org, &pb.GetAccountAssetDetailRequest{AccountId: second.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: miner}})
	require.NoError(t, err)
	require.Equal(t, 100e12, detail.Asset.GetHashrateHs())
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, second.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.user, second.AccountID, child.AccountID, 1, selected(5, miner, 3)))
	delegated, err := f.svc.AssetDetail(t.Context(), f.host, f.org, &pb.GetAccountAssetDetailRequest{AccountId: child.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: miner}})
	require.NoError(t, err)
	require.Nil(t, delegated.Asset.HashrateHs, "delegation cannot expose the previous direct-owner sample")
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT clock_timestamp(),device_identifier,50e12,1000 FROM device WHERE id=$1`, miner)
	require.NoError(t, err)
	delegated, err = f.svc.AssetDetail(t.Context(), f.host, f.org, &pb.GetAccountAssetDetailRequest{AccountId: child.AccountID, Asset: &pb.MiningAssetReference{Kind: 5, Id: miner}})
	require.NoError(t, err)
	require.Equal(t, 50e12, delegated.Asset.GetHashrateHs())
	require.NoError(t, f.allocate(t, f.user, second.AccountID, child.AccountID, 2, selected(5, miner, 4)))
	assertUnavailable()
}

func TestAccountDashboardModuleUsesIndependentSourceWindows(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-2 * time.Hour)
	boundary := start.Add(time.Hour)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, boundary, client.ID)
	require.NoError(t, err)
	// The collection belongs to this client, but hash sources precede assignment,
	// power straddles it, and only the temperature has a fully authorized window.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) VALUES($1,$2,$2,900e12,9000,55,jsonb_build_object('hashrate',jsonb_build_object('reporting',1,'expected',1,'topology_complete',true,'oldest_at',$3::timestamptz,'newest_at',$3::timestamptz),'power',jsonb_build_object('reporting',1,'expected',1,'topology_complete',true,'oldest_at',$3::timestamptz,'newest_at',$4::timestamptz),'temperature',jsonb_build_object('oldest_at',$4::timestamptz,'newest_at',$4::timestamptz)))`, module, end.Add(-time.Minute), boundary.Add(-time.Minute), boundary.Add(time.Minute))
	require.NoError(t, err)
	request := dashboardRequest(client.AccountID, start, end, nil)
	request.BucketSeconds = 1800
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Nil(t, result.Readings[3].Summary.HashrateHs, "collection time cannot authorize earlier hash sources")
	require.Nil(t, result.Readings[3].Summary.PowerW, "cross-assignment source window must be unavailable")
	require.Nil(t, result.Readings[3].Summary.TemperatureC, "collection bucket must not receive source-time temperature")
	require.Equal(t, 55.0, result.Readings[2].Summary.GetTemperatureC(), "authorized metric is attributed to its source bucket")
}

func TestAccountCurrentModuleRejectsCachedAndCrossingSources(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	before := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	var boundary time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=6 AND asset_id=$1 AND ended_at IS NULL`, module).Scan(&boundary))
	publish := func(hashOld, hashNew, powerOld, powerNew time.Time) {
		t.Helper()
		at := time.Now().UTC()
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,coverage) VALUES($1,$2,$2,100e12,2000,55,jsonb_build_object('hashrate',jsonb_build_object('reporting',1,'expected',1,'topology_complete',true,'oldest_at',$3::timestamptz,'newest_at',$4::timestamptz),'power',jsonb_build_object('reporting',1,'expected',1,'topology_complete',true,'oldest_at',$5::timestamptz,'newest_at',$6::timestamptz)))`, module, at, hashOld, hashNew, powerOld, powerNew)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), `UPDATE container_module_latest_metrics l SET time=m.time,collected_at=m.collected_at,hashrate_hs=m.hashrate_hs,power_w=m.power_w,coverage=m.coverage FROM container_module_metrics m WHERE l.module_id=$1 AND m.module_id=l.module_id AND m.time=$2`, module, at)
		require.NoError(t, err)
	}
	detail := func() *pb.AccountAsset {
		t.Helper()
		result, err := f.svc.AssetDetail(t.Context(), f.user, f.org, &pb.GetAccountAssetDetailRequest{AccountId: client.AccountID, Asset: &pb.MiningAssetReference{Kind: 6, Id: module}})
		require.NoError(t, err)
		return result.Asset
	}
	publish(before, before, before, boundary)
	asset := detail()
	require.Nil(t, asset.HashrateHs)
	require.Nil(t, asset.PowerW)
	require.Nil(t, asset.ObservedAt, "collection timestamp cannot expose cached state or sources")
	require.Equal(t, "unknown", asset.State, "pre-assignment module status is not current-client status")
	now := time.Now().UTC()
	publish(before, now, now, now)
	asset = detail()
	require.Nil(t, asset.HashrateHs, "one earlier contributor invalidates the whole metric")
	require.Nil(t, asset.Coverage.Hashrate.OldestAt, "unauthorized source-window metadata must be redacted")
	require.Nil(t, asset.Coverage.Hashrate.NewestAt)
	require.Zero(t, asset.Coverage.Hashrate.Reporting)
	require.Equal(t, 2000.0, asset.GetPowerW(), "independently authorized metric remains measured")
	require.Nil(t, asset.EfficiencyJTh)
	require.Nil(t, asset.TemperatureC, "unknown source bounds remain unavailable")
	publish(now, now, now, now)
	asset = detail()
	require.Equal(t, 100e12, asset.GetHashrateHs())
	require.Equal(t, 2000.0, asset.GetPowerW())
	_, err := f.db.ExecContext(t.Context(), `UPDATE container_module SET state_observed_at=clock_timestamp() WHERE id=$1`, module)
	require.NoError(t, err)
	asset = detail()
	require.Equal(t, "hashing", asset.State, "new authorized module state becomes visible")
	require.NotNil(t, asset.ObservedAt)
	require.NotNil(t, asset.Coverage.Hashrate.NewestAt)
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM container_module_metrics WHERE module_id=$1`, module)
	require.NoError(t, err)
	asset = detail()
	require.Nil(t, asset.HashrateHs, "latest cache without its stamped source row cannot prove physical placement")
	require.Nil(t, asset.PowerW)
}

func TestAccountHistoryExactAllocationBoundary(t *testing.T) {
	f := setup(t)
	first := f.client(t, "First", f.user, hostedaccounts.AllCapabilities)
	second := f.client(t, "Second", f.host, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 1, selected(5, miner, 0)))
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 2, selected(5, miner, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, second.AccountID, 1, selected(5, miner, 2)))
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-time.Hour)
	boundary := start.Add(30 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE client_id=$3`, start, boundary, first.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, boundary, second.ID)
	require.NoError(t, err)
	for _, sample := range []struct {
		at   time.Time
		hash float64
	}{{boundary.Add(-time.Microsecond), 100e12}, {boundary, 200e12}} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT $1,device_identifier,$2,2000 FROM device WHERE id=$3`, sample.at, sample.hash, miner)
		require.NoError(t, err)
	}
	for _, test := range []struct {
		account string
		actor   int64
		hash    float64
	}{{first.AccountID, f.user, 100e12}, {second.AccountID, f.host, 200e12}} {
		result, err := f.svc.Dashboard(t.Context(), test.actor, f.org, dashboardRequest(test.account, start, end, &test.account))
		require.NoError(t, err)
		require.Equal(t, test.hash, result.Readings[0].Summary.GetHashrateHs(), "half-open ownership intervals must assign the boundary sample only to its new owner")
	}
}

func TestAccountModuleHistoryRangeUsesSourcesAndRejectsInvalidEfficiency(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	start := end.Add(-3 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start.Add(-time.Hour), client.ID)
	require.NoError(t, err)
	insert := func(collected, hashAt, powerAt time.Time, hash any) {
		t.Helper()
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,coverage) VALUES($1,$2,$2,$5,2000,jsonb_build_object('hashrate',jsonb_build_object('oldest_at',$3::timestamptz,'newest_at',$3::timestamptz,'reporting',1,'expected',1,'topology_complete',true),'power',jsonb_build_object('oldest_at',$4::timestamptz,'newest_at',$4::timestamptz,'reporting',1,'expected',1,'topology_complete',true)))`, module, collected, hashAt, powerAt, hash)
		require.NoError(t, err)
	}
	insert(start.Add(30*time.Second), start.Add(-30*time.Second), start.Add(30*time.Second), 900e12)
	insert(end.Add(30*time.Second), end.Add(-30*time.Second), end.Add(-90*time.Second), 100e12)
	request := dashboardRequest(client.AccountID, start, end, nil)
	request.BucketSeconds = 60
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Nil(t, result.Readings[0].Summary.HashrateHs, "in-range collection cannot import an earlier source")
	require.Equal(t, 2000.0, result.Readings[0].Summary.GetPowerW())
	require.Equal(t, 2000.0, result.Readings[1].Summary.GetPowerW(), "power uses its independent source bucket")
	require.Equal(t, 100e12, result.Readings[2].Summary.GetHashrateHs(), "source before end survives collection just after end")
	require.Nil(t, result.Readings[2].Summary.PowerW)
	// A full single bucket containing valid and invalid observations may retain
	// measured subtotals, but cannot claim complete efficiency coverage.
	insert(start.Add(45*time.Second), start.Add(45*time.Second), start.Add(45*time.Second), 100e12)
	insert(start.Add(47*time.Second), start.Add(47*time.Second), start.Add(47*time.Second), nil)
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Nil(t, result.Readings[0].Summary.EfficiencyJTh, "known missing metric observations invalidate completeness beside valid samples")
	insert(start.Add(50*time.Second), start.Add(50*time.Second), start.Add(50*time.Second), -100e12)
	insert(start.Add(55*time.Second), start.Add(55*time.Second), start.Add(55*time.Second), "NaN")
	result, err = f.svc.Dashboard(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Equal(t, 100e12, result.Readings[0].Summary.GetHashrateHs())
	require.Nil(t, result.Readings[0].Summary.EfficiencyJTh, "invalid observations invalidate complete per-asset coverage even beside valid readings")
}

func TestAccountCurrentMetricSortUsesOnlyAuthorizedSources(t *testing.T) {
	f := setup(t)
	first := f.client(t, "First", f.host, hostedaccounts.AllCapabilities)
	second := f.client(t, "Second", f.user, hostedaccounts.AllCapabilities)
	old := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	fresh := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 1, selected(5, old, 0)))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT clock_timestamp(),device_identifier,999e12,9999 FROM device WHERE id=$1`, old)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 2, selected(5, old, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, second.AccountID, 1, selected(5, old, 2), selected(5, fresh, 0)))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT clock_timestamp(),device_identifier,100e12,2000 FROM device WHERE id=$1`, fresh)
	require.NoError(t, err)
	for _, field := range []commonpb.SortField{commonpb.SortField_SORT_FIELD_HASHRATE, commonpb.SortField_SORT_FIELD_POWER} {
		page, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: second.AccountID, PageSize: 1, Sort: &commonpb.SortConfig{Field: field, Direction: commonpb.SortDirection_SORT_DIRECTION_DESC}})
		require.NoError(t, err)
		require.Equal(t, fresh, page.Assets[0].Id, "previous-owner telemetry must not affect SQL ordering")
	}
}

func TestAccountDashboardRetainsAllocatedCountsOutsideNarrowedPolicy(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, controller := f.miningModule(t)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Allocated site','allocated') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id IN ($2,$3)`, site, miner, controller)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0), selected(6, module, 0)))
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-time.Hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w) SELECT $1,device_identifier,site_id,100e12,3000 FROM device WHERE id=$2`, start.Add(30*time.Minute), miner)
	require.NoError(t, err)
	before, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.NoError(t, err)
	require.EqualValues(t, 2, before.Totals.AssetCount)
	require.Equal(t, 100e12, before.Readings[0].Summary.GetHashrateHs())
	require.Len(t, before.Sites, 1)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, client.AccountID, true, nil))
	after, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.NoError(t, err)
	require.EqualValues(t, 2, after.Totals.AssetCount, "policy changes must not erase existing allocations")
	require.EqualValues(t, 1, after.Totals.MinerCount)
	require.EqualValues(t, 1, after.Totals.ModuleCount)
	require.EqualValues(t, 2, after.Totals.OutsidePolicyCount)
	require.EqualValues(t, 2, after.Totals.UnknownCount)
	require.Nil(t, after.Totals.HashrateHs)
	require.Nil(t, after.Totals.PowerW)
	require.Empty(t, after.Sites, "outside-policy counts must not disclose site identity")
	require.Len(t, after.Readings, 1)
	require.Zero(t, after.Readings[0].Summary.AssetCount)
	require.Nil(t, after.Readings[0].Summary.HashrateHs, "policy changes revoke historical readings")
}
