package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/authn"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestPoolEquipmentAllocationDoesNotGrantChangePermission(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleViewer))
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	pools, _, _, _, _ := poolFixture(t, f)
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: f.user, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}
	target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
	check := func() (string, error) {
		return db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (string, error) {
			return pools.AuthorizePoolEquipment(ctx, q, scope, f.user, target)
		})
	}
	_, err = check()
	require.True(t, fleeterror.IsForbiddenError(err), "allocation must not grant the viewer pool-change authority")
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RolePoolOperator))
	first, err := check()
	require.NoError(t, err)
	require.NotEmpty(t, first)
	// Manage and Change are independent: applying an existing configuration
	// remains possible when the account cannot edit its library.
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ManagePools, false))
	_, err = check()
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false))
	_, err = check()
	require.True(t, fleeterror.IsForbiddenError(err), "live permission revocation invalidates approved work")
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.AllCapabilities, false))
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
	_, err = check()
	require.True(t, fleeterror.IsForbiddenError(err), "former owners cannot update recalled equipment")
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 2)))
	second, err := check()
	require.NoError(t, err)
	require.NotEqual(t, first, second, "reallocation to the same account still invalidates an old review")
}

func TestPoolWholeContainerRequiresEveryModuleOwnedBySelectedAccount(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	var otherModule int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at) SELECT tank_id,2,now(),now() FROM container_module WHERE id=$1 RETURNING id`, module).Scan(&otherModule))
	for position := 1; position <= 3; position++ {
		f.miningBoard(t, otherModule, controller, position)
	}
	f.observeModuleAtCurrentSite(t, otherModule, controller)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	pools, _, _, _, _ := poolFixture(t, f)
	check := func(actor int64, scope poolconfigurations.Scope, selectedAccount string) (string, error) {
		ctx := authn.SetInfo(t.Context(), &session.Info{UserID: actor, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
		if selectedAccount != "" {
			var err error
			ctx, err = hostedaccounts.WithRequestAccountID(ctx, selectedAccount)
			require.NoError(t, err)
		}
		return db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (string, error) {
			return pools.AuthorizePoolEquipment(ctx, q, scope, actor, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller})
		})
	}
	scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}
	_, err := check(f.user, scope, account.AccountID)
	require.True(t, fleeterror.IsForbiddenError(err), "one allocated module cannot authorize a write to the entire container")
	_, err = check(f.host, poolconfigurations.Scope{OrganizationID: f.org}, "")
	require.True(t, fleeterror.IsForbiddenError(err), "operator configurations cannot overwrite customer equipment")
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, otherModule, 0)))
	_, err = check(f.user, scope, account.AccountID)
	require.NoError(t, err)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, account.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.user, account.AccountID, child.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, otherModule, 1)))
	_, err = check(f.user, scope, account.AccountID)
	require.True(t, fleeterror.IsForbiddenError(err), "parent reporting access does not allow writes to subclient-owned equipment")
}

func TestPoolEquipmentRejectsMachineCredentials(t *testing.T) {
	f := newPoolStepUpFixture(t)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	ctx, _, _ := f.keyContext(t, hostedaccounts.View|hostedaccounts.ChangePools)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, f.account.AccountID)
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
		_, err := f.pools.AuthorizePoolEquipment(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}, f.user, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner})
		return err
	})
	require.True(t, fleeterror.IsForbiddenError(err))
}

func TestPoolEquipmentOrdinaryMinersDoNotRequireStartStopAuthority(t *testing.T) {
	f := setup(t)
	pools, _, _, _, _ := poolFixture(t, f)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: f.user, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
	permissions := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerUpdatePools}
	role, err := authz.NewService(f.db, nil).CreateCustomRole(t.Context(), f.host, f.org, "Change pools only", "", permissions)
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
		_, err := pools.AuthorizePoolEquipment(ctx, q, poolconfigurations.Scope{OrganizationID: f.org}, f.user, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner})
		return err
	})
	require.NoError(t, err)
}

func TestPoolEquipmentBoardOwnershipAndReplacement(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	var board, installation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id,id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, module).Scan(&board, &installation))
	pools, _, _, _, ctx := poolFixture(t, f)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	check := func() error {
		return db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
			_, err := pools.AuthorizePoolEquipment(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}, f.user, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: installation, DeviceID: controller})
			return err
		})
	}
	require.NoError(t, check())
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=now() WHERE id=$1`, installation)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,now(),now())`, board, module)
	require.NoError(t, err)
	require.True(t, fleeterror.IsForbiddenError(check()), "even reinstalling the same board invalidates its old installation target")
}

func TestPoolEquipmentAccountSitePolicyIsRechecked(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	pools, _, _, _, ctx := poolFixture(t, f)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	check := func() error {
		return db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
			_, err := pools.AuthorizePoolEquipment(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}, f.user, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner})
			return err
		})
	}
	require.NoError(t, check())
	rootCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), f.root)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureSites(rootCtx, f.host, f.org, account.AccountID, true, nil))
	require.True(t, fleeterror.IsForbiddenError(check()), "allocation cannot bypass a narrowed account site policy")
}
