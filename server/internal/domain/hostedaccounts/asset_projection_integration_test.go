package hostedaccounts_test

import (
	"encoding/json"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountAssetPageScopesModuleOwnershipBeforeAggregation(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Small customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))

	// Add unrelated modules to the same controller. The customer still owns one.
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at)
	 SELECT tank_id,p,first_seen_at,last_seen_at FROM container_module CROSS JOIN generate_series(2,3000) p WHERE id=$1`, module)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `CREATE TEMP TABLE extra_boards ON COMMIT DROP AS
	 SELECT m.id AS module_id,p AS position,nextval(pg_get_serial_sequence('hashboard','id')) AS id,m.last_seen_at
	 FROM container_module m CROSS JOIN generate_series(1,3) p WHERE m.id<>$1`, module)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO hashboard(id,org_id,first_seen_at,last_seen_at)
	 SELECT id,$1,last_seen_at,last_seen_at FROM extra_boards`, f.org)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO hashboard_observation(hashboard_id,device_id,last_seen_at)
	 SELECT id,$1,last_seen_at FROM extra_boards`, controller)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at)
	 SELECT id,module_id,position,last_seen_at,last_seen_at FROM extra_boards`)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO hashboard_slot(module_id,position,hashboard_id,occupancy,observed_at)
	 SELECT module_id,position,id,'occupied',last_seen_at FROM extra_boards`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	_, err = f.db.ExecContext(t.Context(), `ANALYZE`)
	require.NoError(t, err)
	var membershipCount int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_module_membership`).Scan(&membershipCount))
	require.Equal(t, 3000, membershipCount)

	capture := &miningProjectionDB{DB: f.db}
	rows, err := sqlc.New(capture).ListHostedAccountMiningAssetPage(t.Context(), sqlc.ListHostedAccountMiningAssetPageParams{
		OrganizationID: f.org, AccountInternalID: client.ID, HostAuthority: json.RawMessage("[]"), PolicySites: []int64{}, PageLimit: 51,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, module, rows[0].ID)
	require.EqualValues(t, pb.MiningAssetKind_MINING_ASSET_KIND_MODULE, rows[0].Kind)
	var plans []struct {
		Plan          miningProjectionPlan
		ExecutionTime float64 `json:"Execution Time"`
	}
	require.NoError(t, json.Unmarshal(capture.plan, &plans))
	require.Len(t, plans, 1)
	t.Logf("one customer module in a 3,000-module fleet: %.3f ms", plans[0].ExecutionTime)
	assertScopedModuleAggregation(t, plans[0].Plan)
	options, err := sqlc.New(capture).ListHostedAccountAssetFilterOptions(t.Context(), sqlc.ListHostedAccountAssetFilterOptionsParams{
		OrganizationID: f.org, AccountInternalID: client.ID, HostAuthority: json.RawMessage("[]"), PolicySites: []int64{},
	})
	require.NoError(t, err)
	require.Len(t, options, 1)
	require.Equal(t, controller, options[0].ContainerID)
	require.NoError(t, json.Unmarshal(capture.plan, &plans))
	assertScopedModuleAggregation(t, plans[0].Plan)
}

func assertScopedModuleAggregation(t *testing.T, plan miningProjectionPlan) {
	t.Helper()
	if plan.NodeType == "Aggregate" && len(plan.GroupKey) > 0 && plan.ActualLoops > 0 {
		require.LessOrEqual(t, plan.ActualRows+plan.RowsRemovedByFilter, float64(4),
			"a one-module customer must not aggregate unrelated modules: %v", plan.GroupKey)
	}
	for _, child := range plan.Plans {
		assertScopedModuleAggregation(t, child)
	}
}
