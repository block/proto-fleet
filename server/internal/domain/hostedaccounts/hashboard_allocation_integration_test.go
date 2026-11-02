package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func (f fixture) miningBoard(t *testing.T, module, controller int64, position int) int64 {
	t.Helper()
	var board int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.org).Scan(&board))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hashboard_observation(hashboard_id,device_id,last_seen_at) VALUES($1,$2,now());`, board, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,$3,now(),now())`, board, module, position)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT $1,$2,$3,'occupied',last_seen_at FROM container_module WHERE id=$1`, module, board, position)
	require.NoError(t, err)
	return board
}

// Simulate the fresh inventory needed after a controller's site changes.
func (f fixture) observeModuleAtCurrentSite(t *testing.T, module, controller int64) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `UPDATE container_module SET last_seen_at=(SELECT observed_at FROM container_inventory WHERE device_id=$2) WHERE id=$1`, module, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET observed_at=(SELECT last_seen_at FROM container_module WHERE id=$1) WHERE module_id=$1`, module)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_observation SET site_id=(SELECT site_id FROM device WHERE id=$2) WHERE hashboard_id IN (SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1)`, module, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET site_id=(SELECT site_id FROM device WHERE id=$2) WHERE module_id=$1`, module, controller)
	require.NoError(t, err)
}

// These tests exercise module history written before migration 1049. Replace
// their synthetic board periods with closed legacy periods before backdating
// the fixture. Production never reconstructs old ownership from current boards.
func (f fixture) legacyModuleHistory(t *testing.T) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_asset_interval
	 (organization_id,kind,asset_id,device_id,site_id,device_identifier,client_id,subclient_id,started_at,ended_at)
	 SELECT a.organization_id,6,p.module_id,a.device_id,a.site_id,a.device_identifier,a.client_id,a.subclient_id,a.started_at,COALESCE(a.ended_at,now())
	 FROM hosted_account_asset_interval a JOIN hashboard_installation p ON p.hashboard_id=a.asset_id
	 WHERE a.kind=7 AND a.organization_id=$1 AND p.removed_at IS NULL
	 GROUP BY a.organization_id,p.module_id,a.device_id,a.site_id,a.device_identifier,a.client_id,a.subclient_id,a.started_at,a.ended_at`, f.org)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hosted_account_asset_interval WHERE kind=7 AND organization_id=$1`, f.org)
	require.NoError(t, err)
}

func (f fixture) moduleSelection(t *testing.T, module int64) *pb.MiningAssetSelection {
	t.Helper()
	response, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root, SelectedAssets: []*pb.MiningAssetReference{{Kind: 6, Id: module}}})
	require.NoError(t, err)
	require.Len(t, response.Assets, 1)
	asset := response.Assets[0]
	return &pb.MiningAssetSelection{Kind: 6, Id: module, ExpectedRevision: asset.Revision, Boards: asset.Boards}
}

func TestHashboardAllocationModuleSnapshotAndDiscoveryGap(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	snapshot := f.moduleSelection(t, module)
	require.Len(t, snapshot.Boards, 3)
	require.NoError(t, f.svc.AssignAssets(t.Context(), f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{snapshot}}))
	var count, modules int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*),count(*) FILTER(WHERE kind=6) FROM hosted_account_asset WHERE client_id=$1`, client.ID).Scan(&count, &modules))
	require.Equal(t, 3, count)
	require.Zero(t, modules)
	list, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.Len(t, list.Assets, 1)
	require.Equal(t, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, list.Assets[0].Kind)
	list, err = f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID, HashboardsOnly: true})
	require.NoError(t, err)
	require.Len(t, list.Assets, 3)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET occupancy='unknown' WHERE module_id=$1 AND position=3`, module)
	require.NoError(t, err)
	list, err = f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.Len(t, list.Assets, 3)
	for _, asset := range list.Assets {
		require.Equal(t, pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, asset.Kind)
	}
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset WHERE client_id=$1`, client.ID).Scan(&count))
	require.Equal(t, 3, count)
	// Unknown slots are not empty slots, even though every known board is owned.
	_, err = f.svc.AssetDetail(t.Context(), f.user, f.org, &pb.GetAccountAssetDetailRequest{AccountId: client.AccountID, Asset: &pb.MiningAssetReference{Kind: 6, Id: module}})
	require.Error(t, err)
}

func TestHashboardAllocationRejectsChangedMembershipAndOverlap(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	snapshot := f.moduleSelection(t, module)
	old := snapshot.Boards[0].Id
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=now() WHERE hashboard_id=$1;`, old)
	require.NoError(t, err)
	var position int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `DELETE FROM hashboard_slot WHERE hashboard_id=$1 RETURNING position`, old).Scan(&position))
	f.miningBoard(t, module, controller, position)
	require.Error(t, f.svc.AssignAssets(t.Context(), f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{snapshot}}))
	snapshot = f.moduleSelection(t, module)
	require.Error(t, f.svc.AssignAssets(t.Context(), f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{snapshot, selected(7, snapshot.Boards[0].Id, 0)}}))
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset`).Scan(&count))
	require.Zero(t, count)
}

func TestHashboardAllocationSearchKeepsMatchingBoards(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET custom_name='search-module' WHERE id=$1`, controller)
	require.NoError(t, err)
	for i, serial := range []string{"search-board-001", "search-board-002", "other-board"} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) VALUES($1,'serial',$2,$3)`, f.org, serial, boards[i].Id)
		require.NoError(t, err)
	}
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	for _, scope := range []struct {
		name, account string
		actor         int64
	}{{"host", f.root, f.host}, {"client", client.AccountID, f.user}} {
		for _, test := range []struct {
			name, search string
			boardsOnly   bool
			kind         pb.MiningAssetKind
			ids          []int64
		}{
			{"unfiltered module", "", false, 6, []int64{module}},
			{"board serial", "search-board-001", false, 7, []int64{boards[0].Id}},
			{"board pagination", "search-board", false, 7, []int64{boards[0].Id, boards[1].Id}},
			{"module name", "search-module", false, 6, []int64{module}},
			{"matching module groups matching boards", "search", false, 6, []int64{module}},
			{"boards only", "search", true, 7, []int64{boards[0].Id, boards[1].Id}},
		} {
			t.Run(scope.name+"/"+test.name, func(t *testing.T) {
				request := &pb.ListAccountAssetsRequest{AccountId: scope.account, Search: test.search, HashboardsOnly: test.boardsOnly, PageSize: 1, ContainerId: &controller, OwnerAccountId: &client.AccountID}
				var ids []int64
				for page := 0; ; page++ {
					require.Less(t, page, 4, "pagination must terminate")
					response, err := f.svc.ListAssets(t.Context(), scope.actor, f.org, request)
					require.NoError(t, err)
					for _, asset := range response.Assets {
						require.Equal(t, test.kind, asset.Kind)
						ids = append(ids, asset.Id)
					}
					if response.NextPageToken == "" {
						break
					}
					request.PageToken = response.NextPageToken
				}
				require.Equal(t, test.ids, ids)
			})
		}
	}
}

func TestHashboardAllocationManualReplacementPreservesChainAndHistory(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	module, _ := f.miningModule(t)
	other, _ := f.miningModule(t)
	old := f.moduleSelection(t, module).Boards[0].Id
	replacement := f.moduleSelection(t, other).Boards[0].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, old, 0)))
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(7, old, 1)))
	request := &pb.ReplaceAccountHashboardRequest{AccountId: f.root, OldBoard: &pb.HashboardAllocationSelection{Id: old, ExpectedRevision: 2}, ReplacementBoard: &pb.HashboardAllocationSelection{Id: replacement}}
	// Customers cannot execute an operator replacement through their own workspace.
	require.Error(t, f.svc.ReplaceHashboard(t.Context(), f.user, f.org, &pb.ReplaceAccountHashboardRequest{AccountId: client.AccountID, OldBoard: request.OldBoard, ReplacementBoard: request.ReplacementBoard}))
	require.NoError(t, f.svc.ReplaceHashboard(t.Context(), f.host, f.org, request))
	var owner, subowner int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT client_id,subclient_id FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, replacement).Scan(&owner, &subowner))
	require.Equal(t, client.ID, owner)
	require.Equal(t, child.ID, subowner)
	var ended, started time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT ended_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND subclient_id=$2 ORDER BY started_at DESC LIMIT 1`, old, child.ID).Scan(&ended))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1 AND ended_at IS NULL`, replacement).Scan(&started))
	require.Equal(t, ended, started)
	require.Error(t, f.svc.ReplaceHashboard(t.Context(), f.host, f.org, request), "a stale review must not repeat the transfer")
	var metadata string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT metadata FROM activity_log WHERE event_type='account_hashboard_replaced'`).Scan(&metadata))
	require.True(t, json.Valid([]byte(metadata)))
	// Moving the replacement to another controller retains both allocation levels.
	_, destination := f.miningModule(t)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_observation SET device_id=$1 WHERE hashboard_id=$2`, destination, replacement)
	require.NoError(t, err)
	var device int64
	require.NoError(t, f.db.QueryRowContext(context.Background(), `SELECT device_id,client_id,subclient_id FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, replacement).Scan(&device, &owner, &subowner))
	require.Equal(t, destination, device)
	require.Equal(t, client.ID, owner)
	require.Equal(t, child.ID, subowner)
}

func TestHashboardAllocationMixedOwnersAndSubclients(t *testing.T) {
	f := setup(t)
	first := f.client(t, "First", f.user, hostedaccounts.AllCapabilities)
	second := f.client(t, "Second", f.host, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, first.AccountID, "Subclient", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	module, _ := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 1, selected(7, boards[0].Id, 0), selected(7, boards[1].Id, 0)))
	require.NoError(t, f.allocate(t, f.host, f.root, second.AccountID, 1, selected(7, boards[2].Id, 0)))
	f.publishCurrentModule(t, module)
	root, err := f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root})
	require.NoError(t, err)
	require.Len(t, root.Assets, 1)
	require.Empty(t, root.Assets[0].OwnerAccountId)
	require.Equal(t, "Mixed allocations", root.Assets[0].OwnerAccountName)
	list, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: first.AccountID})
	require.NoError(t, err)
	require.Len(t, list.Assets, 2)
	for _, board := range list.Assets {
		require.Equal(t, pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, board.Kind)
		require.Nil(t, board.PowerW, "partial owners must not receive shared PSU power")
		require.Empty(t, board.Boards, "a board cannot disclose sibling membership")
	}
	_, err = f.svc.AssetDetail(t.Context(), f.user, f.org, &pb.GetAccountAssetDetailRequest{AccountId: first.AccountID, Asset: &pb.MiningAssetReference{Kind: 6, Id: module}})
	require.Error(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, second.AccountID, 2, selected(7, boards[2].Id, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, first.AccountID, 1, selected(7, boards[2].Id, 2)))
	require.NoError(t, f.allocate(t, f.user, first.AccountID, child.AccountID, 1, selected(7, boards[0].Id, 1)))
	f.publishCurrentModule(t, module)
	list, err = f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: first.AccountID})
	require.NoError(t, err)
	require.Len(t, list.Assets, 1)
	require.Equal(t, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, list.Assets[0].Kind)
	require.Empty(t, list.Assets[0].OwnerAccountId)
	require.Equal(t, "Mixed allocations", list.Assets[0].OwnerAccountName)
	root, err = f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: f.root, OwnerAccountId: &first.AccountID})
	require.NoError(t, err)
	require.Len(t, root.Assets, 1)
	require.Equal(t, first.AccountID, root.Assets[0].OwnerAccountId)
	require.Equal(t, first.Name, root.Assets[0].OwnerAccountName)
	report, err := f.svc.MiningSummary(t.Context(), f.user, f.org, first.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 3, report.Totals.AssetCount)
	require.EqualValues(t, 3, report.Totals.HashboardCount)
	require.Nil(t, report.Totals.PowerW, "shared PSU power does not fill missing board readings")
	require.EqualValues(t, 2, report.Direct.HashboardCount)
	require.EqualValues(t, 1, report.Children[0].Summary.HashboardCount)
	require.Nil(t, report.Children[0].Summary.PowerW)
	list, err = f.svc.ListAssets(t.Context(), f.host, f.org, &pb.ListAccountAssetsRequest{AccountId: child.AccountID})
	require.NoError(t, err)
	require.Len(t, list.Assets, 1)
	require.Equal(t, boards[0].Id, list.Assets[0].Id)
	require.Equal(t, pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, list.Assets[0].Kind)
}

func TestHashboardAllocationConfirmedEmptySlotsAndConflicts(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, boards[0].Id, 0), selected(7, boards[1].Id, 0)))
	f.publishCurrentModule(t, module)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=now() WHERE hashboard_id=$1`, boards[2].Id)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET occupancy='empty',hashboard_id=NULL WHERE hashboard_id=$1`, boards[2].Id)
	require.NoError(t, err)
	list := func() []*pb.AccountAsset {
		response, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
		require.NoError(t, err)
		return response.Assets
	}
	assets := list()
	require.Len(t, assets, 1)
	require.Equal(t, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, assets[0].Kind)
	require.Len(t, assets[0].Boards, 2)
	require.Nil(t, assets[0].HashrateHs, "cached aggregate still includes the departed unowned board")
	require.Nil(t, assets[0].PowerW)
	f.publishCurrentModule(t, module)
	require.NotNil(t, list()[0].HashrateHs, "fresh aggregate is authorized after confirmed membership changes")
	// A fault concerns operation, not the established hardware membership.
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET state='error',state_observed_at=now() WHERE module_id=$1`, module)
	require.NoError(t, err)
	require.Len(t, list(), 1)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET unavailable_reason='identity_conflict' WHERE hashboard_id=$1`, boards[0].Id)
	require.NoError(t, err)
	require.Len(t, list(), 2, "conflicting membership exposes only retained boards")
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET occupancy='empty',hashboard_id=NULL,unavailable_reason='' WHERE module_id=$1`, module)
	require.NoError(t, err)
	var qualifies bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM hosted_module_membership WHERE module_id=$1)`, module).Scan(&qualifies))
	require.False(t, qualifies, "an empty module cannot pass an empty ownership check")
	require.Len(t, list(), 2, "discovery changes never erase allocations")
}

func TestHashboardAllocationReplacementHistoryUsesBoardPeriods(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	old, replacement := boards[0].Id, boards[1].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, old, 0)))
	require.NoError(t, f.svc.ReplaceHashboard(t.Context(), f.host, f.org, &pb.ReplaceAccountHashboardRequest{AccountId: f.root, OldBoard: &pb.HashboardAllocationSelection{Id: old, ExpectedRevision: 1}, ReplacementBoard: &pb.HashboardAllocationSelection{Id: replacement}}))
	var boundary time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=7 AND asset_id=$1`, replacement).Scan(&boundary))
	start, end := boundary.Add(-time.Minute), boundary.Add(time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE kind=7 AND asset_id=$2`, start, old)
	require.NoError(t, err)
	for _, sample := range []struct {
		board int64
		at    time.Time
		hash  float64
	}{
		{old, boundary.Add(-time.Second), 100e12}, {old, boundary, 900e12},
		{replacement, boundary.Add(-time.Second), 900e12}, {replacement, boundary, 200e12},
	} {
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs,power_w,temperature_c) SELECT $1,id,$2,$2,'active','', $3,200,55 FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, sample.board, sample.at, sample.hash)
		require.NoError(t, err)
	}
	// Changing today's controller identifier must not erase stable-board history.
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET device_identifier=device_identifier||'-changed' WHERE id=$1`, controller)
	require.NoError(t, err)
	report, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.NoError(t, err)
	var hashrates []float64
	for _, reading := range report.Readings {
		require.EqualValues(t, 1, reading.Summary.HashboardCount, "sequential boards must not appear as concurrent equipment")
		require.Zero(t, reading.Summary.ModuleCount)
		if reading.Summary.HashrateHs != nil {
			hashrates = append(hashrates, reading.Summary.GetHashrateHs())
			require.Equal(t, 200.0, reading.Summary.GetPowerW())
		}
	}
	require.Equal(t, []float64{100e12, 200e12}, hashrates)
}

func TestHashboardAllocationMigrationPreservesLegacyBoundaries(t *testing.T) {
	for _, scenario := range []string{"established", "unresolved", "moved"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.AllCapabilities)
			require.NoError(t, err)
			module, controller := f.miningModule(t)
			// Start from main's schema, without any later replacement/order triggers.
			for _, name := range []string{
				"001052_internal_hashboard_allocation_consumers",
				"001051_internal_hashboard_allocation_order",
				"001050_internal_automatic_hashboard_allocation",
				"001049_internal_hashboard_allocations",
			} {
				down, err := migrations.Current.ReadFile("current/" + name + ".down.sql")
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), string(down))
				require.NoError(t, err)
			}
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,module_id,client_id,subclient_id,revision) VALUES($1,6,$2,$3,$2,$4,$5,8)`, f.org, module, controller, client.ID, child.ID)
			require.NoError(t, err)
			if scenario == "moved" {
				other, destination := f.miningModule(t)
				var site int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Destination','destination') RETURNING id`, f.org).Scan(&site))
				_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, destination)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE module_id=$1 AND removed_at IS NULL`, module)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `UPDATE container_module SET tank_id=(SELECT tank_id FROM container_module WHERE id=$2),position=2,
				 last_seen_at=(SELECT observed_at FROM container_inventory WHERE device_id=$3) WHERE id=$1`, module, other, destination)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_observation SET device_id=$2,site_id=$3
				 WHERE hashboard_id IN (SELECT hashboard_id FROM hashboard_slot WHERE module_id=$1)`, module, destination, site)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,site_id)
				 SELECT hashboard_id,module_id,position,clock_timestamp(),clock_timestamp(),$2 FROM hashboard_slot WHERE module_id=$1`, module, site)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET observed_at=(SELECT last_seen_at FROM container_module WHERE id=$1) WHERE module_id=$1`, module)
				require.NoError(t, err)
				var allocatedController int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_id FROM hosted_account_asset WHERE kind=6 AND asset_id=$1`, module).Scan(&allocatedController))
				require.Equal(t, controller, allocatedController, "legacy allocations retain the controller from before the move")
				controller = destination
			}
			var originalStart time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at FROM hosted_account_asset_interval WHERE kind=6 AND asset_id=$1`, module).Scan(&originalStart))
			up, err := migrations.Current.ReadFile("current/001049_internal_hashboard_allocations.up.sql")
			require.NoError(t, err)
			if scenario == "unresolved" {
				_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET occupancy='unknown' WHERE module_id=$1 AND position=3`, module)
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), string(up))
				require.ErrorContains(t, err, "unresolved board membership")
				var count int
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset WHERE kind=6 AND client_id=$1 AND subclient_id=$2`, client.ID, child.ID).Scan(&count))
				require.Equal(t, 1, count, "failed migration leaves existing ownership intact")
				_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET occupancy='occupied' WHERE module_id=$1 AND position=3`, module)
				require.NoError(t, err)
			}
			_, err = f.db.ExecContext(t.Context(), string(up))
			require.NoError(t, err)
			for _, name := range []string{
				"001050_internal_automatic_hashboard_allocation",
				"001051_internal_hashboard_allocation_order",
				"001052_internal_hashboard_allocation_consumers",
			} {
				migration, err := migrations.Current.ReadFile("current/" + name + ".up.sql")
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), string(migration))
				require.NoError(t, err)
			}
			var start, boundary time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT started_at,ended_at FROM hosted_account_asset_interval WHERE kind=6 AND asset_id=$1`, module).Scan(&start, &boundary))
			require.Equal(t, originalStart, start, "migration must not backdate legacy history")
			var boards, periods int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset WHERE kind=7 AND client_id=$1 AND subclient_id=$2 AND revision=8 AND device_id=$3`, client.ID, child.ID, controller).Scan(&boards))
			require.Equal(t, 3, boards)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset_interval i JOIN device d ON d.id=i.device_id
			 WHERE i.kind=7 AND i.client_id=$1 AND i.subclient_id=$2 AND i.started_at=$3 AND i.ended_at IS NULL
			 AND i.device_id=$4 AND i.site_id IS NOT DISTINCT FROM d.site_id AND i.device_identifier=d.device_identifier`, client.ID, child.ID, boundary, controller).Scan(&periods))
			require.Equal(t, 3, periods, "board ownership begins only at the migration boundary")
			// A single requested bucket crosses the real migration. Keep the old
			// module aggregate and the new boards in separate time slices.
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO container_module_metrics(module_id,time,collected_at,hashrate_hs,power_w,coverage)
			 VALUES($1,$2,$2,300e12,600,jsonb_build_object('hashrate',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz),'power',jsonb_build_object('oldest_at',$2::timestamptz,'newest_at',$2::timestamptz)))`, module, originalStart)
			require.NoError(t, err)
			for _, sample := range []struct {
				at   time.Time
				hash float64
			}{{originalStart, 900e12}, {boundary, 100e12}} {
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_metrics(hashboard_id,installation_id,time,collected_at,state,mining_state,hashrate_hs,power_w)
				 SELECT hashboard_id,id,$2,$2,'active','',$3,200 FROM hashboard_installation WHERE module_id=$1 AND removed_at IS NULL`, module, sample.at, sample.hash)
				require.NoError(t, err)
			}
			report, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, originalStart.Add(-time.Second), boundary.Add(time.Minute), nil))
			require.NoError(t, err)
			require.Len(t, report.Readings, 2)
			require.EqualValues(t, 1, report.Readings[0].Summary.ModuleCount)
			require.Zero(t, report.Readings[0].Summary.HashboardCount)
			require.EqualValues(t, 3, report.Readings[1].Summary.HashboardCount)
			require.Zero(t, report.Readings[1].Summary.ModuleCount)
			for _, reading := range report.Readings {
				require.Equal(t, 300e12, reading.Summary.GetHashrateHs(), "do not double-count the module or infer earlier board ownership")
				require.Equal(t, 600.0, reading.Summary.GetPowerW())
			}
			down, err := migrations.Current.ReadFile("current/001049_internal_hashboard_allocations.down.sql")
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), string(down))
			require.ErrorContains(t, err, "retained history")
		})
	}
}

func TestHashboardAllocationReplacementRejectsInvalidDestinations(t *testing.T) {
	for _, reason := range []string{"allocated", "stale old", "stale replacement", "suspended subclient", "site policy", "unavailable"} {
		t.Run(reason, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
			require.NoError(t, err)
			module, _ := f.miningModule(t)
			other, _ := f.miningModule(t)
			old, replacement := f.moduleSelection(t, module).Boards[0].Id, f.moduleSelection(t, other).Boards[0].Id
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, old, 0)))
			require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(7, old, 1)))
			request := &pb.ReplaceAccountHashboardRequest{AccountId: f.root, OldBoard: &pb.HashboardAllocationSelection{Id: old, ExpectedRevision: 2}, ReplacementBoard: &pb.HashboardAllocationSelection{Id: replacement}}
			switch reason {
			case "allocated":
				sibling := f.client(t, "Sibling", f.host, hostedaccounts.AllCapabilities)
				require.NoError(t, f.allocate(t, f.host, f.root, sibling.AccountID, 1, selected(7, replacement, 0)))
				request.ReplacementBoard.ExpectedRevision = 1
			case "stale old":
				request.OldBoard.ExpectedRevision = 1
			case "stale replacement":
				request.ReplacementBoard.ExpectedRevision = 1
			case "suspended subclient":
				require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, true))
			case "site policy":
				require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, client.AccountID, true, nil))
			case "unavailable":
				_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_observation SET released_at=now() WHERE hashboard_id=$1`, replacement)
				require.NoError(t, err)
			}
			var before, after string
			query := `SELECT jsonb_agg(to_jsonb(a) ORDER BY kind,asset_id)::text FROM hosted_account_asset a`
			require.NoError(t, f.db.QueryRowContext(t.Context(), query).Scan(&before))
			require.Error(t, f.svc.ReplaceHashboard(t.Context(), f.host, f.org, request))
			require.NoError(t, f.db.QueryRowContext(t.Context(), query).Scan(&after))
			require.Equal(t, before, after, "rejected transfer must be atomic")
			var audits int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='account_hashboard_replaced'`).Scan(&audits))
			require.Zero(t, audits)
		})
	}
}

func TestHashboardReplacementRejectsConcurrentControllerMove(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	_, otherController := f.miningModule(t)
	boards := f.moduleSelection(t, module).Boards
	old, replacement := boards[0].Id, boards[1].Id
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, old, 0)))
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `SELECT device_id FROM container_inventory WHERE device_id=$1 FOR UPDATE`, controller)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		result <- f.svc.ReplaceHashboard(t.Context(), f.host, f.org, &pb.ReplaceAccountHashboardRequest{AccountId: f.root, OldBoard: &pb.HashboardAllocationSelection{Id: old, ExpectedRevision: 1}, ReplacementBoard: &pb.HashboardAllocationSelection{Id: replacement}})
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM container_inventory WHERE device_id=ANY%')`).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	_, err = tx.ExecContext(t.Context(), `UPDATE hashboard_observation SET device_id=$1 WHERE hashboard_id=$2`, otherController, replacement)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Error(t, <-result, "a moved unallocated board must not escape the physical locks")
	var assigned int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT asset_id FROM hosted_account_asset WHERE client_id=$1`, client.ID).Scan(&assigned))
	require.Equal(t, old, assigned)
}
