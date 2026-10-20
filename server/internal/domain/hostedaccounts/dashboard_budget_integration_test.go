package hostedaccounts_test

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountDashboardBoundsRawTelemetryWork(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	start := end.Add(-24 * time.Hour)
	assets := []*pb.MiningAssetSelection{}
	for range 38 {
		id, _ := f.miningModule(t)
		assets = append(assets, selected(6, id, 0))
	}
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, assets...))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, client.ID)
	require.NoError(t, err)
	_, err = f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, start, end, nil))
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	require.ErrorContains(t, err, "choose a shorter time range")
	result, err := f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, end.Add(-time.Hour), end, nil))
	require.NoError(t, err)
	require.EqualValues(t, 38, result.Totals.AssetCount)
	_, err = f.svc.Dashboard(t.Context(), f.user, f.org, dashboardRequest(client.AccountID, end.Add(-7*24*time.Hour), end, nil))
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	require.ErrorContains(t, err, "at most 24 hours")
}
