package hostedaccounts_test

import (
	"encoding/json"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHashboardCurrentSummariesMatchHistory(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	module, controller := f.miningModule(t)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Site','site') RETURNING id`, f.org).Scan(&site))
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, controller)
	require.NoError(t, err)
	f.observeModuleAtCurrentSite(t, module, controller)
	boards := f.moduleSelection(t, module).Boards
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(7, boards[0].Id, 1)))
	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET last_seen_at=$1 WHERE module_id=$2`, at, module)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET state='active',state_observed_at=$1 WHERE module_id=$2`, at, module)
	require.NoError(t, err)
	var readings []map[string]any
	for index, board := range boards {
		readings = append(readings, map[string]any{"board_id": board.Id, "module_id": module, "position": index + 1, "time": at, "state": "active", "state_observed_at": at, "hashrate_hs": 100e12, "power_w": 1000, "temperature_c": 50})
	}
	encoded, err := json.Marshal(readings)
	require.NoError(t, err)
	q := sqlc.New(f.db)
	require.NoError(t, q.InsertHashboardMetrics(t.Context(), sqlc.InsertHashboardMetricsParams{CollectedAt: at, Boards: encoded}))
	require.NoError(t, q.InsertHashboardDiagnosticObservations(t.Context(), sqlc.InsertHashboardDiagnosticObservationsParams{CollectedAt: at, Boards: encoded}))
	// The site move cleared the old module readings. Publish different module
	// power so using its aggregate instead of the boards cannot pass.
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module_latest_metrics SET hashrate_hs=100e12,power_w=2000,coverage=jsonb_build_object('hashrate',jsonb_build_object('reporting',3,'expected',3,'topology_complete',true),'power',jsonb_build_object('reporting',1,'expected',1,'topology_complete',true)) WHERE module_id=$1`, module)
	require.NoError(t, err)
	f.publishCurrentModule(t, module)
	check := func(summary *pb.MiningSummary, count int64) {
		t.Helper()
		require.Equal(t, count, summary.AssetCount)
		require.Equal(t, count, summary.HashboardCount)
		require.Zero(t, summary.ModuleCount)
		require.Equal(t, count, summary.HashrateReportingCount)
		require.Equal(t, count, summary.PowerReportingCount)
		require.Equal(t, float64(count)*100e12, summary.GetHashrateHs())
		require.Equal(t, float64(count)*1000, summary.GetPowerW())
		require.Equal(t, 10.0, summary.GetEfficiencyJTh())
	}
	summary, err := f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	check(summary.Totals, 3)
	check(summary.Direct, 2)
	check(summary.Children[0].Summary, 1)
	for _, scope := range []struct {
		owner *string
		count int64
	}{{nil, 3}, {&client.AccountID, 2}, {&child.AccountID, 1}} {
		report, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, at, at.Add(time.Minute), scope.owner))
		require.NoError(t, err)
		check(report.Totals, scope.count)
		require.Len(t, report.Sites, 1)
		check(report.Sites[0].Summary, scope.count)
		require.Len(t, report.Readings, 1)
		check(report.Readings[0].Summary, scope.count)
		require.EqualValues(t, scope.count, report.Readings[0].UptimeStatus.HashingCount)
	}
	inventory, err := f.svc.ListAssets(t.Context(), f.user, f.org, &pb.ListAccountAssetsRequest{AccountId: client.AccountID})
	require.NoError(t, err)
	require.Len(t, inventory.Assets, 1)
	require.Equal(t, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, inventory.Assets[0].Kind)
	require.Equal(t, 2000.0, inventory.Assets[0].GetPowerW())
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM hashboard_latest_metrics WHERE hashboard_id=$1`, boards[1].Id)
	require.NoError(t, err)
	summary, err = f.svc.MiningSummary(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 3, summary.Totals.AssetCount)
	require.EqualValues(t, 2, summary.Totals.PowerReportingCount)
	require.Equal(t, 2000.0, summary.Totals.GetPowerW(), "missing board power cannot fall back to the module")
	require.Nil(t, summary.Totals.EfficiencyJTh)
}
