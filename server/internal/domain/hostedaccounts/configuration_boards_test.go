package hostedaccounts_test

import (
	"testing"

	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestSharedBoardConfigurationsRequireAccountPermissionAndCurrentOwner(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	var board, installation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id,id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, module).Scan(&board, &installation))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0' || position) WHERE module_id=$1`, module)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	authority, _, _, cipher, ctx := poolFixture(t, f)
	ctx, err = hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(cipher)
	reviewer := poolapplications.NewReviewer(authority, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true})
	service := poolconfigurations.NewService(f.db, repo, authority, reviewer)
	options := poolOptions(nil)
	options.Slots[1].URL = "stratum+tcp://backup.example:3333"
	config, err := service.Create(ctx, account.AccountID, options)
	require.NoError(t, err)
	whole, err := service.GetAssignments(ctx, account.AccountID, nil, []int64{controller}, nil)
	require.NoError(t, err)
	require.Len(t, whole.Assignments, 1, "all boards have the same owner")
	sources, err := sqlc.New(f.db).ListPoolDeviceImportSources(ctx, f.org)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, account.ID, sources[0].AccountID)
	require.EqualValues(t, 1, sources[0].OwnerCount)
	selection := poolconfigurations.BoardSelection{ModuleIDs: []int64{module}}
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, account.AccountID, f.host, hostedaccounts.RoleAdministrator))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleViewer))
	_, err = service.ReviewBoards(ctx, account.AccountID, config.ID, config.Version, selection)
	require.Error(t, err, "allocation does not grant pool changes")
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RolePoolOperator))
	review, err := service.ReviewBoards(ctx, account.AccountID, config.ID, config.Version, selection)
	require.NoError(t, err)
	require.Len(t, review.Targets, 3)
	scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}
	require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
		return repo.Adopt(ctx, q, scope, review.Targets[0], config.ID, config.RevisionID)
	}))
	got, err := service.GetBoardAssignments(ctx, account.AccountID, selection)
	require.NoError(t, err)
	require.Equal(t, config.ID, got.Assignments[0].ConfigurationID)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(6, module, 1)))
	got, err = service.GetBoardAssignments(ctx, account.AccountID, selection)
	require.NoError(t, err)
	require.Empty(t, got.Assignments, "recall must hide the previous owner's configuration and board identities")
	_, err = service.ReviewBoards(ctx, account.AccountID, config.ID, config.Version, selection)
	require.Error(t, err)
}

func TestSharedBoardConfigurationsRespectSplitModuleOwnership(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	var board, installation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id,id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, module).Scan(&board, &installation))
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0' || position) WHERE module_id=$1`, module)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(7, board, 0)))
	authority, _, _, cipher, ctx := poolFixture(t, f)
	ctx, err = hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(cipher)
	reviewer := poolapplications.NewReviewer(authority, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true})
	service := poolconfigurations.NewService(f.db, repo, authority, reviewer)
	config, err := service.Create(ctx, account.AccountID, poolconfigurations.RevisionOptions{Name: "Customer board", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}})
	require.NoError(t, err)
	review, err := service.ReviewBoards(ctx, account.AccountID, config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: []int64{board}})
	require.NoError(t, err, "an individually owned board is selectable in a mixed module")
	require.Len(t, review.Targets, 1)
	_, err = service.ReviewBoards(ctx, account.AccountID, config.ID, config.Version, poolconfigurations.BoardSelection{ModuleIDs: []int64{module}})
	require.Error(t, err, "selecting the entire mixed module cannot include another owner's boards")
	whole, err := service.GetAssignments(ctx, account.AccountID, nil, []int64{controller}, nil)
	require.NoError(t, err)
	require.Empty(t, whole.Assignments, "a partly owned controller is not a whole-device assignment target")
	sources, err := sqlc.New(f.db).ListPoolDeviceImportSources(ctx, f.org)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.EqualValues(t, 2, sources[0].OwnerCount, "offline import detects host/customer board ownership instead of assuming an unallocated module")
	scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}
	require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
		return repo.Adopt(ctx, q, scope, review.Targets[0], config.ID, config.RevisionID)
	}))
	got, err := service.GetBoardAssignments(ctx, account.AccountID, poolconfigurations.BoardSelection{ModuleIDs: []int64{module}})
	require.NoError(t, err)
	require.Len(t, got.Assignments, 1)
	require.Equal(t, installation, got.Assignments[0].InstallationID)
	require.Equal(t, config.ID, got.Assignments[0].ConfigurationID)
	var otherInstallation int64
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE module_id=$1 AND position=2 AND removed_at IS NULL`, module).Scan(&otherInstallation))
	err = db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
		return repo.Adopt(ctx, q, scope, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: otherInstallation, DeviceID: controller}, config.ID, config.RevisionID)
	})
	require.Error(t, err, "adoption cannot attach the customer's configuration to another owner's board")
}
