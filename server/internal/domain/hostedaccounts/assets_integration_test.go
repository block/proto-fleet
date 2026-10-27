package hostedaccounts_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/google/uuid"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountMiningEmptyAndPrivateAuthority(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Mining customer", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Private subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	result, err := f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.Zero(t, result.Totals.AssetCount)
	require.Len(t, result.Children, 1)
	require.Equal(t, child.AccountID, result.Children[0].AccountId)
	require.NotNil(t, result.GeneratedAt)
	_, err = f.svc.GetAccount(t.Context(), f.user, f.org, child.AccountID)
	require.Error(t, err, "report breakdown does not grant private child authority")
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, client.AccountID, f.host, hostedaccounts.RoleViewer))
	result, err = f.svc.MiningSummary(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.Nil(t, result.Direct)
	require.Empty(t, result.Children)
	require.False(t, result.CanManageAssignments)
	_, err = f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID, OwnerAccountId: &child.AccountID})
	require.Error(t, err, "viewers cannot infer ownership through filters")
}

func (f fixture) miningDevice(t *testing.T, class commonpb.DeviceClass) int64 {
	t.Helper()
	identifier := uuid.NewString()
	discovered := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: identifier, DeviceClass: class, Model: "Synthetic", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.1", Port: "8080", UrlScheme: "http"}, OrgID: f.org, IsActive: true}
	_, err := sqlstores.NewSQLDiscoveredDeviceStore(f.db).Save(t.Context(), discoverymodels.DeviceOrgIdentifier{OrgID: f.org, DeviceIdentifier: identifier}, discovered)
	require.NoError(t, err)
	store := sqlstores.NewSQLDeviceStore(f.db)
	require.NoError(t, store.InsertDevice(t.Context(), &discovered.Device, f.org, identifier))
	require.NoError(t, store.UpsertDevicePairing(t.Context(), &discovered.Device, f.org, "PAIRED"))
	var id int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM device WHERE org_id=$1 AND device_identifier=$2`, f.org, identifier).Scan(&id))
	return id
}
func (f fixture) miningModule(t *testing.T) (int64, int64) {
	t.Helper()
	controller := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,now())`, controller, f.org)
	require.NoError(t, err)
	var tank, module int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, controller).Scan(&tank))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at) VALUES($1,1,now(),now(),'active',now()) RETURNING id`, tank).Scan(&module))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_latest_metrics(module_id,time,collected_at,hashrate_hs,power_w,coverage) VALUES($1,now(),now(),100e12,2000,jsonb_build_object('hashrate',jsonb_build_object('reporting',1,'expected',2,'topology_complete',true,'oldest_at',now()),'power',jsonb_build_object('reporting',1,'expected',2,'topology_complete',true,'oldest_at',now())))`, module)
	require.NoError(t, err)
	return module, controller
}

// Publish a new physical observation explicitly after the tested allocation.
func (f fixture) publishCurrentModule(t *testing.T, module int64) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `UPDATE container_module_latest_metrics SET time=clock_timestamp(),collected_at=clock_timestamp(),coverage=(SELECT jsonb_object_agg(key,value || jsonb_build_object('oldest_at',clock_timestamp(),'newest_at',clock_timestamp())) FROM jsonb_each(coverage)) WHERE module_id=$1`, module)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,site_id,hashrate_hs,power_w,efficiency_jh,temperature_c,coverage) SELECT l.module_id,l.time,l.collected_at,d.site_id,l.hashrate_hs,l.power_w,l.efficiency_jh,l.temperature_c,l.coverage FROM container_module_latest_metrics l JOIN container_module m ON m.id=l.module_id JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id WHERE l.module_id=$1`, module)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module SET state_observed_at=clock_timestamp() WHERE id=$1`, module)
	require.NoError(t, err)
}
func selected(kind pb.MiningAssetKind, id, revision int64) *pb.MiningAssetSelection {
	return &pb.MiningAssetSelection{Kind: kind, Id: id, ExpectedRevision: revision}
}
func (f fixture) allocate(t *testing.T, actor int64, parent, child string, intent pb.AssetAssignmentIntent, assets ...*pb.MiningAssetSelection) error {
	t.Helper()
	return f.svc.AssignAssets(t.Context(), actor, f.org, &pb.AssignAccountAssetsRequest{AccountId: parent, ChildAccountId: child, Intent: intent, Assets: assets})
}
func TestAccountMiningMixedLeavesAllocationCoverageAndRecall(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, _ := f.miningModule(t)
	assign, recall := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, miner, 0), selected(6, module, 0)))
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, assign, selected(6, module, 1)))
	f.publishCurrentModule(t, module)
	summary, err := f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 2, summary.Totals.AssetCount)
	require.EqualValues(t, 1, summary.Totals.MinerCount)
	require.EqualValues(t, 1, summary.Totals.ModuleCount)
	require.EqualValues(t, 1, summary.Direct.AssetCount)
	require.EqualValues(t, 1, summary.Children[0].Summary.AssetCount)
	require.Equal(t, 100e12, *summary.Totals.HashrateHs)
	require.Equal(t, 2000.0, *summary.Totals.PowerW)
	require.Nil(t, summary.Totals.EfficiencyJTh, "partial selected estate cannot report efficiency")
	require.EqualValues(t, 1, summary.Totals.HashrateReportingCount)
	require.EqualValues(t, 1, summary.Totals.PartialModuleCount)
	rootRows, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root})
	require.NoError(t, err)
	for _, asset := range rootRows.Assets {
		require.Equal(t, client.AccountID, asset.OwnerAccountId, "host sees customer owner, not subclient identity")
	}
	// A stale leaf aborts every mutation and emits no success audit.
	var before, after int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='account_assets_assigned'`).Scan(&before))
	require.Error(t, f.allocate(t, f.user, client.AccountID, child.AccountID, assign, selected(5, miner, 1), selected(6, module, 1)))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='account_assets_assigned'`).Scan(&after))
	require.Equal(t, before, after)
	summary, err = f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.Direct.AssetCount)
	// Suspension blocks child access, but never erases capacity from parent totals.
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.DefaultCapabilities, true))
	summary, err = f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 2, summary.Totals.AssetCount)
	_, err = f.svc.MiningSummary(t.Context(), f.host, f.org, child.AccountID)
	require.Error(t, err)
	// Retirement retains allocation and hides identifying physical details.
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module SET retired_at=now() WHERE id=$1`, module)
	require.NoError(t, err)
	rows, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.Len(t, rows.Assets, 2)
	retired := rows.Assets[1]
	require.False(t, retired.Available)
	require.Empty(t, retired.Name)
	require.Zero(t, retired.ContainerId)
	require.Nil(t, retired.HashrateHs)
	var replacement int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at) SELECT tank_id,position,now(),now(),'active',now() FROM container_module WHERE id=$1 RETURNING id`, module).Scan(&replacement))
	rootPage, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root, OwnerAccountId: &f.root})
	require.NoError(t, err)
	require.Len(t, rootPage.Assets, 1)
	require.Equal(t, replacement, rootPage.Assets[0].Id)
	require.Zero(t, rootPage.Assets[0].Revision, "replacement modules never inherit old allocations")
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, recall, selected(6, module, 2)))
	require.Error(t, f.allocate(t, f.user, client.AccountID, child.AccountID, assign, selected(6, module, 1)), "old revision cannot survive recall")
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, recall, selected(5, miner, 1), selected(6, module, 3)))
	require.Error(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, miner, 0)), "recall must preserve ABA witness")
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, miner, 2)))
}

func TestAccountMiningPoliciesPrivacyPagingAndBoundaries(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	sibling := f.client(t, "Sibling", f.host, hostedaccounts.AllCapabilities)
	first := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	second := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, first, 0), selected(5, second, 0)))
	require.Error(t, f.allocate(t, f.user, client.AccountID, sibling.AccountID, assign, selected(5, first, 1)))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, client.AccountID, f.host, hostedaccounts.RoleViewer))
	viewer, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, viewer.Assets, 1)
	require.Empty(t, viewer.Assets[0].OwnerAccountId)
	require.Empty(t, viewer.Assets[0].OwnerAccountName)
	require.NotEmpty(t, viewer.NextPageToken)
	page, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID, PageSize: 1, PageToken: viewer.NextPageToken})
	require.NoError(t, err)
	require.Len(t, page.Assets, 1)
	require.NotEqual(t, viewer.Assets[0].Id, page.Assets[0].Id)
	_, err = f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID, PageToken: viewer.NextPageToken, Search: "changed"})
	require.Error(t, err)
	require.Error(t, f.allocate(t, f.host, client.AccountID, child.AccountID, assign, selected(5, first, 1)), "host role must not leak into customer viewer mode")
	// Restrict the customer to no sites: retain count, redact every physical detail.
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, client.AccountID, true, nil))
	report, err := f.svc.MiningSummary(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 2, report.Totals.AssetCount)
	require.EqualValues(t, 2, report.Totals.OutsidePolicyCount)
	require.EqualValues(t, 2, report.Totals.UnknownCount)
	viewer, err = f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	for _, asset := range viewer.Assets {
		require.Empty(t, asset.Name)
		require.Zero(t, asset.SiteId)
		require.Empty(t, asset.SiteName)
		require.Nil(t, asset.ObservedAt)
	}
	require.Error(t, f.allocate(t, f.user, client.AccountID, child.AccountID, assign, selected(5, first, 1)))
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, client.AccountID, false, nil))
	// A foreign organization cannot read or select another organization's leaf.
	var foreignOrg int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization(org_id,name) VALUES($1,'Foreign') RETURNING id`, uuid.NewString()).Scan(&foreignOrg))
	other := fixture{db: f.db, org: foreignOrg}
	foreign := other.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.Error(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, foreign, 0)))
	_, err = f.svc.MiningSummary(t.Context(), f.host, other.org, client.AccountID)
	require.Error(t, err)
}

func TestAccountMiningAssignmentRechecksConcurrentModuleRetirement(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT device_id FROM container_inventory WHERE device_id=$1 FOR UPDATE`, controller)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		result <- f.svc.AssignAssets(t.Context(), f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{selected(6, module, 0)}})
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM container_inventory WHERE device_id=ANY%')`).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	_, err = tx.ExecContext(t.Context(), `UPDATE container_module SET retired_at=now() WHERE id=$1`, module)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Error(t, <-result, "retirement committed during admission must reject assignment")
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset`).Scan(&count))
	require.Zero(t, count)
}

func TestAccountMiningAssignmentRechecksConcurrentSiteMove(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	first, err := sqlc.New(f.db).CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Allowed", Slug: "allowed"})
	require.NoError(t, err)
	second, err := sqlc.New(f.db).CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Moved", Slug: "moved"})
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, first.ID, miner)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, client.AccountID, true, []int64{first.ID}))
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, miner)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		result <- f.svc.AssignAssets(t.Context(), f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{selected(5, miner, 0)}})
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT discovered_device_id FROM device WHERE id=%')`).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	_, err = tx.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, second.ID, miner)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Error(t, <-result, "site moved while admission waited must fail live destination policy")
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset`).Scan(&count))
	require.Zero(t, count)
}

func TestAccountMiningWeightedMetricsAndStaleCoverage(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	stale := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0), selected(5, stale, 0), selected(6, module, 0)))
	f.publishCurrentModule(t, module)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,power_w) SELECT now(),device_identifier,200e12,6000 FROM device WHERE id=$1 UNION ALL SELECT now()-interval '11 minutes',device_identifier,900e12,9999 FROM device WHERE id=$2`, miner, stale)
	require.NoError(t, err)
	result, err := f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 3, result.Totals.AssetCount)
	require.EqualValues(t, 2, result.Totals.HashrateReportingCount)
	require.Equal(t, 300e12, *result.Totals.HashrateHs)
	require.Equal(t, 8000.0, *result.Totals.PowerW)
	require.Nil(t, result.Totals.EfficiencyJTh, "stale selected asset makes coverage incomplete")
}
func TestAccountMiningRootRequiresPhysicalReadAuthority(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	role, err := authz.NewService(f.db, nil).CreateCustomRole(t.Context(), f.host, f.org, "Account management only", "", []string{authz.PermAccountManage})
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	rows, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root})
	require.NoError(t, err)
	require.Empty(t, rows.Assets)
	summary, err := f.svc.MiningSummary(t.Context(), f.user, f.org, f.root)
	require.NoError(t, err)
	require.Zero(t, summary.Totals.AssetCount)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, miner)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = f.svc.AssignAssets(ctx, f.user, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{selected(5, miner, 0)}})
	require.ErrorContains(t, err, "selected assets changed", "physical read authority must be checked before contended device admission")
}
func TestAccountMiningTypedPersistenceAndSubclientDirectFilter(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	// A missing typed FK must never pass SQL CHECK's three-valued logic.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,client_id) VALUES($1,5,$2,$2,$3)`, f.org, miner, client.ID)
	require.Error(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(5, miner, 1)))
	rows, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: child.AccountID, OwnerAccountId: &child.AccountID})
	require.NoError(t, err)
	require.Len(t, rows.Assets, 1)
	require.False(t, rows.CanManageAssignments)
}

func TestAccountAssetListReturnsLiveManagementMetadataWithoutMetrics(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.DefaultCapabilities, true))
	response, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.True(t, response.CanViewAssignments)
	require.True(t, response.CanManageAssignments)
	require.Len(t, response.Children, 1)
	require.Equal(t, child.AccountID, response.Children[0].AccountId)
	require.Equal(t, "Child", response.Children[0].Name)
	require.True(t, response.Children[0].Suspended)
	require.Nil(t, response.Children[0].Summary, "list metadata must not compute child metrics")
	// Removing delegation keeps named management metadata but removes mutation authority.
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.View|hostedaccounts.ManageMembers, false))
	response, err = f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.True(t, response.CanViewAssignments)
	require.False(t, response.CanManageAssignments)
	require.Len(t, response.Children, 1)
	require.Nil(t, response.Children[0].Summary)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, client.AccountID, f.host, hostedaccounts.RoleViewer))
	response, err = f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.False(t, response.CanViewAssignments)
	require.False(t, response.CanManageAssignments)
	require.Empty(t, response.Children)
}

func TestAccountAssetSelectedProjectionKeepsExactTypedPairs(t *testing.T) {
	f := setup(t)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, _ := f.miningModule(t)
	// A fresh fixture gives miner1/module1; equal numeric IDs cannot expand selection.
	require.Equal(t, miner, module)
	query := sqlc.New(f.db)
	for _, kind := range []pb.MiningAssetKind{pb.MiningAssetKind_MINING_ASSET_KIND_MINER, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE} {
		selection, err := json.Marshal([]map[string]any{{"kind": kind, "id": miner}})
		require.NoError(t, err)
		rows, err := query.ListHostedAccountMiningAssets(t.Context(), sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: f.org, IsRoot: true, SelectedAssets: selection})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, int32(kind), rows[0].Kind)
		require.Equal(t, miner, rows[0].ID)
	}
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module, 0)))
	_, err := f.db.ExecContext(t.Context(), `UPDATE container_module SET retired_at=now() WHERE id=$1`, module)
	require.NoError(t, err)
	selection, err := json.Marshal([]map[string]any{{"kind": pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, "id": module}})
	require.NoError(t, err)
	retained, err := query.ListHostedAccountMiningAssets(t.Context(), sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: f.org, IsRoot: true, SelectedAssets: selection})
	require.NoError(t, err)
	require.Len(t, retained, 1)
	require.False(t, retained[0].Available)
	require.Equal(t, module, retained[0].ID)
	rows, err := query.ListHostedAccountMiningAssets(t.Context(), sqlc.ListHostedAccountMiningAssetsParams{OrganizationID: f.org, IsRoot: true, SelectedAssets: []byte("[]")})
	require.NoError(t, err)
	require.Len(t, rows, 2, "summary's empty selection keeps full scoped estate")
}

func TestAccountMiningRootMixedSiteAuthority(t *testing.T) {
	for _, narrowed := range []bool{false, true} {
		name := "site allowlist"
		if narrowed {
			name = "org grant narrowed at site"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			q := sqlc.New(f.db)
			allowed, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Allowed", Slug: "allowed"})
			require.NoError(t, err)
			hidden, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Hidden", Slug: "hidden"})
			require.NoError(t, err)
			first := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			second := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=CASE WHEN id=$1 THEN $3::bigint ELSE $4::bigint END WHERE id IN ($1,$2)`, first, second, allowed.ID, hidden.ID)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w) SELECT now(),device_identifier,site_id,CASE WHEN id=$1 THEN 200e12 ELSE 900e12 END,6000 FROM device WHERE id IN ($1,$2)`, first, second)
			require.NoError(t, err)
			svc := authz.NewService(f.db, nil)
			manager, err := svc.CreateCustomRole(t.Context(), f.host, f.org, "Account management", "", []string{authz.PermAccountManage})
			require.NoError(t, err)
			reader, err := svc.CreateCustomRole(t.Context(), f.host, f.org, "Physical reader", "", []string{authz.PermAccountManage, authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead})
			require.NoError(t, err)
			orgRole, siteRole, siteID := manager.ID, reader.ID, allowed.ID
			if narrowed {
				orgRole, siteRole, siteID = reader.ID, manager.ID, hidden.ID
			}
			_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: orgRole, ScopeType: "org"})
			require.NoError(t, err)
			_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: siteRole, ScopeType: "site", ScopeID: sql.NullInt64{Int64: siteID, Valid: true}})
			require.NoError(t, err)
			summary, err := f.svc.MiningSummary(t.Context(), f.user, f.org, f.root)
			require.NoError(t, err)
			require.EqualValues(t, 1, summary.Totals.AssetCount)
			require.Equal(t, 200e12, *summary.Totals.HashrateHs)
			rows, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root, PageSize: 1})
			require.NoError(t, err)
			require.Len(t, rows.Assets, 1)
			require.Equal(t, first, rows.Assets[0].Id)
			require.Equal(t, 200e12, *rows.Assets[0].HashrateHs)
			require.Empty(t, rows.NextPageToken, "unauthorized leaves must not fill or extend a page")
			exact, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root, SelectedAssets: []*pb.MiningAssetReference{{Kind: 5, Id: first}, {Kind: 5, Id: second}}})
			require.NoError(t, err)
			require.Len(t, exact.Assets, 1, "exact lookup must preserve physical site authority")
			require.Equal(t, first, exact.Assets[0].Id)
			require.Error(t, f.allocate(t, f.user, f.root, client.AccountID, 1, selected(5, first, 0), selected(5, second, 0)))
			var allocations int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset`).Scan(&allocations))
			require.Zero(t, allocations, "mixed selection must fail atomically")
			require.NoError(t, f.allocate(t, f.user, f.root, client.AccountID, 1, selected(5, first, 0)))
		})
	}
}

// Root ancestor grants let host operators administer clients without a seat, so
// their host site authority must bound client reports as it bounds the host one.
func TestAccountMiningClientReportPreservesHostSiteAuthority(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", 0, hostedaccounts.AllCapabilities)
	customer := addUser(t, f.db, f.org)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, client.AccountID, customer, hostedaccounts.RoleViewer))
	q := sqlc.New(f.db)
	allowed, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Allowed", Slug: "allowed"})
	require.NoError(t, err)
	hidden, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Hidden", Slug: "hidden"})
	require.NoError(t, err)
	visible := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	private := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=CASE WHEN id=$1 THEN $3::bigint ELSE $4::bigint END WHERE id IN ($1,$2)`, visible, private, allowed.ID, hidden.ID)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, visible, 0), selected(5, private, 0)))
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-time.Hour)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w,temp_c) SELECT $1,device_identifier,site_id,CASE WHEN id=$2 THEN 100e12 ELSE 900e12 END,3000,60 FROM device WHERE id IN ($2,$3)`, start.Add(30*time.Minute), visible, private)
	require.NoError(t, err)
	svc := authz.NewService(f.db, nil)
	manager, err := svc.CreateCustomRole(t.Context(), f.host, f.org, "Account management", "", []string{authz.PermAccountManage})
	require.NoError(t, err)
	reader, err := svc.CreateCustomRole(t.Context(), f.host, f.org, "Physical reader", "", []string{authz.PermAccountManage, authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead})
	require.NoError(t, err)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: manager.ID, ScopeType: "org"})
	require.NoError(t, err)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: reader.ID, ScopeType: "site", ScopeID: sql.NullInt64{Int64: allowed.ID, Valid: true}})
	require.NoError(t, err)

	for _, reader := range []struct {
		name   string
		actor  int64
		assets []int64
	}{{"host operator", f.user, []int64{visible}}, {"customer member", customer, []int64{visible, private}}} {
		t.Run(reader.name, func(t *testing.T) {
			summary, err := f.svc.MiningSummary(t.Context(), reader.actor, f.org, client.AccountID)
			require.NoError(t, err)
			require.EqualValues(t, len(reader.assets), summary.Totals.AssetCount)
			rows, err := f.svc.ListAssets(t.Context(), reader.actor, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
			require.NoError(t, err)
			ids := make([]int64, 0, len(rows.Assets))
			for _, asset := range rows.Assets {
				ids = append(ids, asset.Id)
			}
			require.ElementsMatch(t, reader.assets, ids)
			dashboard, err := f.svc.Dashboard(t.Context(), reader.actor, f.org, dashboardRequest(client.AccountID, start, end, nil))
			require.NoError(t, err)
			require.EqualValues(t, len(reader.assets), dashboard.Totals.AssetCount)
			require.Len(t, dashboard.Readings, 1)
			history := dashboard.Readings[0].Summary
			require.EqualValues(t, len(reader.assets), history.AssetCount)
			require.EqualValues(t, len(reader.assets), history.HashrateReportingCount)
			if reader.name == "host operator" {
				require.Equal(t, 100e12, history.GetHashrateHs(), "host must not read the hidden site's measurements")
			} else {
				require.Equal(t, 1000e12, history.GetHashrateHs())
			}
			var dashboardSites []int64
			for _, site := range dashboard.Sites {
				dashboardSites = append(dashboardSites, site.SiteId)
			}
			var siteIDs []int64
			for _, site := range rows.FilterOptions.Sites {
				siteIDs = append(siteIDs, site.Id)
			}
			if reader.name == "host operator" {
				require.Equal(t, []int64{allowed.ID}, siteIDs, "host physical authority must also bound filter options")
				require.Equal(t, []int64{allowed.ID}, dashboardSites, "host physical authority must also bound dashboard sites")
			} else {
				require.ElementsMatch(t, []int64{allowed.ID, hidden.ID}, siteIDs)
				require.ElementsMatch(t, []int64{allowed.ID, hidden.ID}, dashboardSites)
			}
		})
	}
}

func TestAccountMiningRejectsUnauthorizedSelectionBeforePhysicalLocks(t *testing.T) {
	for _, kind := range []pb.MiningAssetKind{5, 6} {
		t.Run(kind.String(), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
			require.NoError(t, err)
			own := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, own, 0)))
			foreign := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			device := foreign
			if kind == 6 {
				foreign, device = f.miningModule(t)
			}
			other := f.client(t, "Other customer", f.host, hostedaccounts.AllCapabilities)
			require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, 1, selected(kind, foreign, 0)))
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, device)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- f.svc.AssignAssets(ctx, f.user, f.org, &pb.AssignAccountAssetsRequest{AccountId: client.AccountID, ChildAccountId: child.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{selected(5, own, 1), selected(kind, foreign, 1)}})
			}()
			select {
			case err := <-result:
				require.ErrorContains(t, err, "selected assets changed")
			case <-time.After(time.Second):
				t.Fatal("unauthorized selection waited for an unrelated physical-device lock")
			}
			// The unrelated device lock remains held, but the rejected batch must have
			// released account admission so other reports can proceed.
			_, err = f.svc.MiningSummary(ctx, f.user, f.org, client.AccountID)
			require.NoError(t, err)
		})
	}
}

func TestAccountMiningAssignmentBoundsPhysicalLockWait(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, miner)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)) }()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(8 * time.Second):
		t.Fatal("assignment did not bound its physical lock wait")
	}
	// The operation must roll back its organization lock before the device is free.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = f.svc.MiningSummary(ctx, f.user, f.org, client.AccountID)
	require.NoError(t, err)
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_asset`).Scan(&count))
	require.Zero(t, count)
}

func TestAccountMiningExactSelectionLookup(t *testing.T) {
	f := setup(t)
	customer := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	first := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	other := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, _ := f.miningModule(t)
	refs := []*pb.MiningAssetReference{{Kind: 5, Id: first}, {Kind: 6, Id: module}}
	root, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root, SelectedAssets: refs})
	require.NoError(t, err)
	require.Len(t, root.Assets, 2)
	require.Empty(t, root.NextPageToken)
	require.Equal(t, first, root.Assets[0].Id)
	require.Equal(t, module, root.Assets[1].Id)
	require.NotEqual(t, other, root.Assets[0].Id)
	require.NoError(t, f.allocate(t, f.host, f.root, customer.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, first, 0)))
	root, err = f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root, SelectedAssets: refs})
	require.NoError(t, err)
	require.EqualValues(t, 1, root.Assets[0].Revision)
	require.Equal(t, customer.AccountID, root.Assets[0].OwnerAccountId)
	scoped, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: customer.AccountID, SelectedAssets: refs})
	require.NoError(t, err)
	require.Len(t, scoped.Assets, 1, "exact lookup must never expand account scope")
	require.Equal(t, first, scoped.Assets[0].Id)
	for _, request := range []*pb.ListAccountAssetsRequest{
		{SelectedAssets: refs, Search: "ignored"},
		{SelectedAssets: refs, PageSize: 1},
		{SelectedAssets: refs, PageToken: "cursor"},
		{SelectedAssets: refs, OwnerAccountId: &f.root},
		{SelectedAssets: []*pb.MiningAssetReference{{Kind: 5, Id: first}, {Kind: 5, Id: first}}},
		{SelectedAssets: []*pb.MiningAssetReference{{Kind: 1, Id: first}}},
	} {
		request.AccountId = f.root
		_, err = f.svc.ListAssets(t.Context(), f.host, f.org, request)
		require.Error(t, err)
	}
}

func TestAccountMiningMaximumMixedBatchAssignRecallAndStaleRevision(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Maximum batch", f.user, hostedaccounts.AllCapabilities)
	assets := make([]*pb.MiningAssetSelection, 0, 500)
	for range 250 {
		assets = append(assets, selected(pb.MiningAssetKind_MINING_ASSET_KIND_MINER, f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG), 0))
	}
	module, _ := f.miningModule(t)
	assets = append(assets, selected(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module, 0))
	for position := 2; position <= 250; position++ {
		var id int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at) SELECT tank_id,$2,now(),now(),'active',now() FROM container_module WHERE id=$1 RETURNING id`, module, position).Scan(&id))
		assets = append(assets, selected(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, id, 0))
	}
	require.Len(t, assets, 500)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	recall := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, assign, assets...))
	assertLedger := func(clientID any, revision int64, auditCount int) {
		t.Helper()
		var count, miners, modules, audits int
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),count(*) FILTER (WHERE kind=5),count(*) FILTER (WHERE kind=6) FROM hosted_account_asset WHERE organization_id=$1 AND client_id IS NOT DISTINCT FROM $2::bigint AND subclient_id IS NULL AND revision=$3`, f.org, clientID, revision).Scan(&count, &miners, &modules))
		require.Equal(t, 500, count)
		require.Equal(t, 250, miners)
		require.Equal(t, 250, modules)
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='account_assets_assigned'`).Scan(&audits))
		require.Equal(t, auditCount, audits)
	}
	assertLedger(client.ID, 1, 1)
	for _, asset := range assets {
		asset.ExpectedRevision = 1
	}
	assets[len(assets)-1].ExpectedRevision = 0
	require.ErrorContains(t, f.allocate(t, f.host, f.root, client.AccountID, recall, assets...), "selected assets changed")
	assertLedger(client.ID, 1, 1)
	assets[len(assets)-1].ExpectedRevision = 1
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, recall, assets...))
	assertLedger(nil, 2, 2)
}

// Every rejected assignment must leave both the reporting ledger and the audit
// trail untouched, whichever guard rejects it.
func TestAccountMiningAssignmentGuardsLeaveLedgerUnchanged(t *testing.T) {
	f := setup(t)
	first := f.client(t, "First", f.user, hostedaccounts.AllCapabilities)
	second := f.client(t, "Second", f.host, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, first.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	held := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	free := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	assign, recall := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, assign, selected(5, held, 0)))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, true))
	snapshot := func() (ledger string, audits int) {
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT coalesce(json_agg(a ORDER BY kind,asset_id),'[]')::text FROM hosted_account_asset a WHERE organization_id=$1`, f.org).Scan(&ledger))
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1`, f.org).Scan(&audits))
		return ledger, audits
	}
	ledger, audits := snapshot()
	oversized := make([]*pb.MiningAssetSelection, 501)
	for i := range oversized {
		oversized[i] = selected(5, int64(i+1), 0)
	}
	for _, guard := range []struct {
		name     string
		actor    int64
		parent   string
		child    string
		intent   pb.AssetAssignmentIntent
		assets   []*pb.MiningAssetSelection
		rejected func(error) bool
	}{
		{"already held at current revision", f.host, f.root, second.AccountID, assign, []*pb.MiningAssetSelection{selected(5, held, 1)}, fleeterror.IsFailedPreconditionError},
		{"recall from the wrong child", f.host, f.root, second.AccountID, recall, []*pb.MiningAssetSelection{selected(5, held, 1)}, fleeterror.IsFailedPreconditionError},
		{"assign to suspended child", f.user, first.AccountID, child.AccountID, assign, []*pb.MiningAssetSelection{selected(5, held, 1)}, fleeterror.IsForbiddenError},
		{"child of another parent", f.host, f.root, child.AccountID, assign, []*pb.MiningAssetSelection{selected(5, free, 0)}, fleeterror.IsForbiddenError},
		{"empty selection", f.host, f.root, second.AccountID, assign, nil, fleeterror.IsInvalidArgumentError},
		{"oversized selection", f.host, f.root, second.AccountID, assign, oversized, fleeterror.IsInvalidArgumentError},
		{"unspecified intent", f.host, f.root, second.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_UNSPECIFIED, []*pb.MiningAssetSelection{selected(5, free, 0)}, fleeterror.IsInvalidArgumentError},
		{"negative revision", f.host, f.root, second.AccountID, assign, []*pb.MiningAssetSelection{selected(5, free, -1)}, fleeterror.IsInvalidArgumentError},
		{"duplicate selection", f.host, f.root, second.AccountID, assign, []*pb.MiningAssetSelection{selected(5, free, 0), selected(5, free, 0)}, fleeterror.IsInvalidArgumentError},
		{"unknown kind", f.host, f.root, second.AccountID, assign, []*pb.MiningAssetSelection{selected(pb.MiningAssetKind_MINING_ASSET_KIND_UNSPECIFIED, free, 0)}, fleeterror.IsInvalidArgumentError},
	} {
		t.Run(guard.name, func(t *testing.T) {
			err := f.allocate(t, guard.actor, guard.parent, guard.child, guard.intent, guard.assets...)
			require.True(t, guard.rejected(err), "unexpected rejection: %v", err)
			after, afterAudits := snapshot()
			require.JSONEq(t, ledger, after)
			require.Equal(t, audits, afterAudits)
		})
	}
	// Suspension stops new capacity, but parents can still reclaim what they lent.
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false))
	require.NoError(t, f.allocate(t, f.user, first.AccountID, child.AccountID, assign, selected(5, held, 1)))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, true))
	require.NoError(t, f.allocate(t, f.user, first.AccountID, child.AccountID, recall, selected(5, held, 2)))
}

func TestAccountMiningPreservesNodeOfflineState(t *testing.T) {
	for _, test := range []struct {
		name                      string
		baselineAge, heartbeatAge time.Duration
		offline                   bool
	}{
		{name: "fresh heartbeat overrides stale baseline", baselineAge: 3 * time.Minute},
		{name: "stale heartbeat overrides fresh baseline", heartbeatAge: 3 * time.Minute, offline: true},
		{name: "stale node", baselineAge: 3 * time.Minute, heartbeatAge: 3 * time.Minute, offline: true},
		{name: "fresh node"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(pb.MiningAssetKind_MINING_ASSET_KIND_MINER, miner, 0)))
			var node int64
			now := time.Now().UTC()
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status,last_seen_at) VALUES($1,'Test node',$2,$3,'CONFIRMED',$4) RETURNING id`, f.org, []byte("hosted-test-node"), make([]byte, 32), now.Add(-test.baselineAge)).Scan(&node))
			_, err := f.db.ExecContext(t.Context(), `INSERT INTO fleet_node_heartbeat(fleet_node_id,org_id,last_seen_at) VALUES($1,$2,$3)`, node, f.org, now.Add(-test.heartbeatAge))
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, node, miner, f.org)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_status(device_id,status,status_timestamp) VALUES($1,'ACTIVE',clock_timestamp())`, miner)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,site_id,hash_rate_hs,power_w) SELECT clock_timestamp(),device_identifier,site_id,100e12,3000 FROM device WHERE id=$1`, miner)
			require.NoError(t, err)
			checkAsset := func(asset *pb.AccountAsset) {
				t.Helper()
				if test.offline {
					require.Equal(t, "offline", asset.State)
					require.Nil(t, asset.HashrateHs)
					require.Nil(t, asset.PowerW)
				} else {
					require.Equal(t, "active", asset.State)
					require.NotNil(t, asset.HashrateHs)
					require.Equal(t, 100e12, *asset.HashrateHs)
					require.NotNil(t, asset.PowerW)
					require.Equal(t, 3000.0, *asset.PowerW)
				}
			}
			for _, scope := range []struct {
				actor   int64
				account string
			}{{f.host, f.root}, {f.user, client.AccountID}} {
				summary, err := f.svc.MiningSummary(t.Context(), scope.actor, f.org, scope.account)
				require.NoError(t, err)
				if test.offline {
					require.EqualValues(t, 1, summary.Totals.OfflineCount)
					require.Zero(t, summary.Totals.HashingCount)
					require.Nil(t, summary.Totals.HashrateHs)
					require.Nil(t, summary.Totals.PowerW)
				} else {
					require.Zero(t, summary.Totals.OfflineCount)
					require.EqualValues(t, 1, summary.Totals.HashingCount)
					require.Equal(t, 100e12, summary.Totals.GetHashrateHs())
					require.Equal(t, 3000.0, summary.Totals.GetPowerW())
				}
				assets, err := f.svc.ListAssets(t.Context(), scope.actor, f.org, &pb.ListAccountAssetsRequest{AccountId: scope.account})
				require.NoError(t, err)
				require.Len(t, assets.Assets, 1)
				checkAsset(assets.Assets[0])
				detail, err := f.svc.AssetDetail(t.Context(), scope.actor, f.org, &pb.GetAccountAssetDetailRequest{AccountId: scope.account, Asset: &pb.MiningAssetReference{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_MINER, Id: miner}})
				require.NoError(t, err)
				checkAsset(detail.Asset)
			}
		})
	}
}

func TestAccountMiningRepeatedModuleSourceDoesNotDuplicateAssets(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, module, 0)))
	f.publishCurrentModule(t, module)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,site_id,hashrate_hs,power_w,efficiency_jh,temperature_c,coverage) SELECT module_id,time,collected_at+interval '1 microsecond',site_id,hashrate_hs,power_w,efficiency_jh,temperature_c,coverage FROM container_module_metrics WHERE module_id=$1`, module)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module_latest_metrics SET collected_at=collected_at+interval '1 microsecond' WHERE module_id=$1`, module)
	require.NoError(t, err)
	summary, err := f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.Totals.AssetCount)
	require.Equal(t, 2000.0, summary.Totals.GetPowerW())
	assets, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.Len(t, assets.Assets, 1)
	detail, err := f.svc.AssetDetail(t.Context(), f.user, f.org, &pb.GetAccountAssetDetailRequest{AccountId: client.AccountID, Asset: &pb.MiningAssetReference{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, Id: module}})
	require.NoError(t, err)
	require.Equal(t, module, detail.Asset.Id)
}
