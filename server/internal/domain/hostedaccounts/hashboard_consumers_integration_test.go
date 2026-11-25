package hostedaccounts_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	maintenancepb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestHashboardAllocationScopesTicketAssets(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, _ := f.miningModule(t)
	var board int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1`, module).Scan(&board))
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(7, board, 0)))
	assets, err := f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, &pb.ListAccountTicketAssetsRequest{AccountId: account.AccountID})
	require.NoError(t, err)
	require.Len(t, assets.Assets, 1, "partial owners can target their board but not the module or its other boards")
	require.Equal(t, maintenancepb.TicketCategory_TICKET_CATEGORY_HASHBOARD, assets.Assets[0].Kind)
	require.Equal(t, board, assets.Assets[0].Id)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(7, board, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 2)))
	assets, err = f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, &pb.ListAccountTicketAssetsRequest{AccountId: account.AccountID})
	require.NoError(t, err)
	require.Len(t, assets.Assets, 4, "full owners can target the module and each installed board")
}

func TestHashboardOwnershipChangeRetiresControllerPoolIntent(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	module, controller := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	_, err = f.db.ExecContext(ctx, `UPDATE container_mining_group_state SET observed='{"supported":true,"enabled":false}',observed_at=now() WHERE device_id=$1 AND organization_id=$2`, controller, f.org)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer)
	config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Customer pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	require.NoError(t, err)
	review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}})
	require.NoError(t, err)
	saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	// Inventory replacement writes the ledger directly. It must retire intent
	// without relying on the manual assignment service's cleanup call.
	_, err = f.db.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=NULL,revision=revision+1 WHERE kind=7 AND asset_id=(SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL)`, module)
	require.NoError(t, err)
	operation, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "superseded", operation.Targets[0].State)
	summary, err := service.Get(ctx, f.account.AccountID, config.ID)
	require.NoError(t, err)
	require.Zero(t, summary.AssignedCount)
}
