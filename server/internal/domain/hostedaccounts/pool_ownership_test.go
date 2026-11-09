package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestReallocationEndsPreviousPoolAssignmentAndRequiresExplicitNewAssignment(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer)
	options := poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}}
	config, err := service.Create(ctx, f.account.AccountID, options)
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	targets := []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}}
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, targets)
	require.NoError(t, err)
	saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
	previous, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "superseded", previous.Targets[0].State)
	summary, err := service.Get(ctx, f.account.AccountID, config.ID)
	require.NoError(t, err)
	require.Zero(t, summary.AssignedCount)
	_, err = service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, targets)
	require.Error(t, err, "the old account cannot use a retired assignment as authority")
	other := f.client(t, "Next owner", f.user, hostedaccounts.AllCapabilities)
	nextCtx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), other.AccountID)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 2)))
	var active int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE retired_at IS NULL`).Scan(&active))
	require.Zero(t, active, "allocating equipment does not apply or inherit any configuration")
	next, err := service.Create(nextCtx, other.AccountID, options)
	require.NoError(t, err)
	review, err = service.ReviewAssignment(nextCtx, other.AccountID, next.ID, next.Version, targets)
	require.NoError(t, err)
	accepted, err := service.ConfirmReview(nextCtx, other.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.Greater(t, accepted.Operation.Targets[0].AssignmentVersion, saved.Operation.Targets[0].AssignmentVersion)
	_, err = service.Get(nextCtx, other.AccountID, config.ID)
	require.Error(t, err, "the new owner never gains access to the previous owner's private configuration")
}
