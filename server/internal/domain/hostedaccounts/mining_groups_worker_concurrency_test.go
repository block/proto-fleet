package hostedaccounts_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/mininggroups"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type heldDeactivationMiner struct {
	interfaces.Miner
	started chan struct{}
	release <-chan struct{}
}

func (m heldDeactivationMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m heldDeactivationMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action == "deactivate" {
		close(m.started)
		select {
		case <-m.release:
			return sdk.MiningGroupResult{Deactivated: true}, nil
		case <-ctx.Done():
			return sdk.MiningGroupResult{}, fmt.Errorf("held controller call: %w", ctx.Err())
		}
	}
	return sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 289,
		Groups: []sdk.MiningGroup{{ID: "default", Revision: 1}}, ObservedAt: time.Now()}, nil
}

func TestMiningGroupDispatchDoesNotHoldOrganizationAdmission(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	otherModule, _ := f.miningModule(t)
	busyBoard := f.moduleSelection(t, module).Boards[0].Id
	otherBoards := f.moduleSelection(t, otherModule).Boards
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(7, otherBoards[0].Id, 0)))
	// Use independent operators and roles to isolate organization admission
	// from the existing permission locks protecting users and role edits.
	role, err := authz.NewService(f.db, nil).CreateCustomRole(t.Context(), f.host, f.org, "Mining operator", "", []string{
		authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerUpdatePools,
		authz.PermMinerDeactivateMiningGroups, authz.PermMinerStartMining, authz.PermMinerStopMining,
	})
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `DELETE FROM user_organization_role WHERE user_id=$1 AND organization_id=$2`, f.user, f.org)
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	pools, _, _, cipher, operatorCtx := poolFixture(t, f)
	started, release := make(chan struct{}), make(chan struct{})
	firmware := heldDeactivationMiner{started: started, release: release}
	service := mininggroups.NewService(f.db, firmware, pools, cipher)
	var identifier string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, controller).Scan(&identifier))
	_, err = f.db.ExecContext(t.Context(), `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1`, controller)
	require.NoError(t, err)
	_, err = service.GetState(operatorCtx, identifier)
	require.NoError(t, err)
	_, err = service.Deactivate(operatorCtx, identifier, uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, service.Reconcile(operatorCtx, f.org, controller, identifier))
	done := make(chan error, 1)
	go func() { done <- service.Reconcile(operatorCtx, f.org, controller, identifier) }()
	defer func() {
		close(release)
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("deactivation dispatch did not finish")
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("deactivation did not reach controller dispatch")
	}
	assign := func(intent pb.AssetAssignmentIntent, asset *pb.MiningAssetSelection) error {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		return f.svc.AssignAssets(ctx, f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: intent, Assets: []*pb.MiningAssetSelection{asset}})
	}
	err = assign(pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(7, busyBoard, 0))
	require.True(t, fleeterror.IsUnavailableError(err), "the dispatching container must keep its binding lock: %v", err)
	require.NoError(t, assign(pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(7, otherBoards[0].Id, 1)))
	require.NoError(t, assign(pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(7, otherBoards[1].Id, 0)))
	var recalled, assigned, busyAllocated bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT client_id IS NULL FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, otherBoards[0].Id).Scan(&recalled))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT client_id IS NOT NULL FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, otherBoards[1].Id).Scan(&assigned))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM hosted_account_asset WHERE kind=7 AND asset_id=$1 AND client_id IS NOT NULL)`, busyBoard).Scan(&busyAllocated))
	require.True(t, recalled)
	require.True(t, assigned)
	require.False(t, busyAllocated)
}
