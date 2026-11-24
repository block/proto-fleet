package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/authn"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestSavedPoolAssignmentsRequireCurrentOwnerAndReadAuthority(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleViewer))
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	authority, _, _, cipher, ctx := poolFixture(t, f)
	ctx, err := hostedaccounts.WithRequestAccountID(ctx, account.AccountID)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(cipher)
	scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: account.ID}
	config, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
		return repo.Create(ctx, q, scope, poolOptions(nil))
	})
	require.NoError(t, err)
	require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error {
		return repo.Adopt(ctx, q, scope, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}, config.ID, config.RevisionID)
	}))
	service := poolconfigurations.NewService(f.db, repo, authority, nil)
	result, err := service.GetAssignments(ctx, account.AccountID, nil, []int64{miner}, nil)
	require.NoError(t, err)
	require.Len(t, result.Assignments, 1, "a viewer can see current intent without Change pools")
	require.Equal(t, config.ID, result.Assignments[0].ConfigurationID)
	hostCtx := authn.SetInfo(t.Context(), &session.Info{UserID: f.host, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
	hostResult, err := service.GetAssignments(hostCtx, "", nil, []int64{miner}, nil)
	require.NoError(t, err)
	require.Empty(t, hostResult.Assignments, "operator scope cannot expose a customer's private configuration")
	rootCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), f.root)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureSites(rootCtx, f.host, f.org, account.AccountID, true, nil))
	result, err = service.GetAssignments(ctx, account.AccountID, nil, []int64{miner}, nil)
	require.NoError(t, err)
	require.Empty(t, result.Assignments, "site policy also applies to saved configuration names")
	require.NoError(t, f.svc.ConfigureSites(rootCtx, f.host, f.org, account.AccountID, false, nil))
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
	result, err = service.GetAssignments(ctx, account.AccountID, nil, []int64{miner}, nil)
	require.NoError(t, err)
	require.Empty(t, result.Assignments, "recalled hardware no longer displays the previous owner's assignment")
}
