package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountAssetPoolActionRequiresUserPermissionAndDirectOwnership(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, account.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	delegated := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0), selected(5, delegated, 0)))
	require.NoError(t, f.allocate(t, f.user, account.AccountID, child.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, delegated, 1)))
	member := addUser(t, f.db, f.org)
	check := func(allowed bool) {
		t.Helper()
		result, err := f.svc.ListAssets(t.Context(), member, f.org, &pb.ListAccountAssetsRequest{AccountId: account.AccountID})
		require.NoError(t, err)
		require.Len(t, result.Assets, 2)
		for _, asset := range result.Assets {
			require.Empty(t, asset.OwnerAccountId, "pool authority does not grant private owner labels")
			require.Equal(t, allowed && asset.Id == miner, asset.CanChangePools, "only directly owned equipment accepts this account's settings")
		}
	}
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, account.AccountID, member, hostedaccounts.RoleViewer))
	check(false)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, account.AccountID, member, hostedaccounts.RolePoolOperator))
	check(true)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ManagePools, false))
	check(true)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, account.AccountID, member, hostedaccounts.RoleViewer))
	check(false)
}
