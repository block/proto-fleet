package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestClientManagementSummaryAllocationAndLoginCounts(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	empty := f.client(t, "Empty", f.host, hostedaccounts.AllCapabilities)
	subclient, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, _ := f.miningModule(t)
	var sameContainerModule int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at) SELECT tank_id,2,now(),now(),'active',now() FROM container_module WHERE id=$1 RETURNING id`, module).Scan(&sameContainerModule))
	otherContainerModule, _ := f.miningModule(t)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, assign, selected(5, miner, 0), selected(6, module, 0), selected(6, sameContainerModule, 0), selected(6, otherContainerModule, 0)))
	require.NoError(t, f.allocate(t, f.user, client.AccountID, subclient.AccountID, assign, selected(6, module, 1)))

	for _, state := range []string{"active", "revoked", "deleted", "unaffiliated"} {
		user := addUser(t, f.db, f.org)
		require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, client.AccountID, user, hostedaccounts.RoleViewer))
		switch state {
		case "revoked":
			require.NoError(t, f.svc.RevokeMembership(t.Context(), f.user, f.org, client.AccountID, user))
		case "deleted":
			_, err = f.db.ExecContext(t.Context(), `UPDATE "user" SET deleted_at=now() WHERE id=$1`, user)
			require.NoError(t, err)
		case "unaffiliated":
			_, err = f.db.ExecContext(t.Context(), `UPDATE user_organization SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, user, f.org)
			require.NoError(t, err)
		}
	}
	_, err = hostedaccounts.NewInvitationService(f.svc, nil).Create(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.RoleViewer, 0)
	require.NoError(t, err)
	check := func() {
		t.Helper()
		rows, err := f.svc.ListChildren(t.Context(), f.host, f.org, f.root)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.Equal(t, client.AccountID, rows[0].AccountID)
		require.Equal(t, &hostedaccounts.ClientManagementSummary{MinerCount: 1, ModuleCount: 3, ContainerCount: 2, LoginCount: 2}, rows[0].ManagementSummary)
		require.Equal(t, empty.AccountID, rows[1].AccountID)
		require.Equal(t, &hostedaccounts.ClientManagementSummary{LoginCount: 1}, rows[1].ManagementSummary)
		require.Zero(t, rows[0].EffectiveCapabilities, "aggregate management does not grant private client access")
	}
	check()
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_module SET retired_at=now() WHERE id=$1`, module)
	require.NoError(t, err)
	check()
	rows, err := f.svc.ListChildren(t.Context(), f.user, f.org, client.AccountID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].ManagementSummary, "parent delegation must not expose subclient membership counts")
	directory, err := f.svc.ListAccounts(t.Context(), f.user, f.org)
	require.NoError(t, err)
	for _, account := range directory {
		require.Nil(t, account.ManagementSummary, "membership directory must not gain management details")
	}
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.AllCapabilities, true))
	check()
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.AllCapabilities, false))
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1), selected(6, module, 2), selected(6, sameContainerModule, 1), selected(6, otherContainerModule, 1)))
	rows, err = f.svc.ListChildren(t.Context(), f.host, f.org, f.root)
	require.NoError(t, err)
	require.Equal(t, &hostedaccounts.ClientManagementSummary{LoginCount: 2}, rows[0].ManagementSummary)
	_, err = f.svc.ListChildren(t.Context(), f.user, f.org, f.root)
	require.Error(t, err, "client administration cannot enumerate host clients or management counts")
}
