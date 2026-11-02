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

func TestPoolAssignmentConfirmationRequiresPasswordAndNotLibraryManage(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer)
	config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ManagePools, false))
	review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}})
	require.NoError(t, err, "applying requires read plus Change pools, not library management")
	_, err = service.ConfirmReview(ctx, f.account.AccountID, review.ID, "incorrect")
	require.Error(t, err)
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&count))
	require.Zero(t, count)
	saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
	require.NoError(t, err)
	require.NotNil(t, saved.Operation)
	calls := f.verifier.calls
	for _, password := range []string{"", "actor-password"} {
		replay, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, password)
		require.NoError(t, err)
		require.True(t, replay.Replay)
		require.Equal(t, saved.Operation.ID, replay.Operation.ID)
		require.Equal(t, calls, f.verifier.calls, "an accepted result replay cannot dispatch again or require another password")
	}
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&count))
	require.Equal(t, 1, count, "replaying the original confirmation must not create more equipment work")
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM activity_log WHERE event_type='apply_pool_configuration'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.View|hostedaccounts.ManageMembers, false))
	operation, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
	require.NoError(t, err, "hosted readers can view results without pool-change or library-management permission")
	require.Equal(t, saved.Operation.ID, operation.ID)
}

func TestPoolConfirmationRechecksCredentialsAndAuthorityAfterVerification(t *testing.T) {
	for _, mutation := range []string{"password reset", "permission revoked", "allocation recalled"} {
		t.Run(mutation, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
			require.NoError(t, err)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true})
			service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), f.pools, reviewer)
			config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
			require.NoError(t, err)
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}})
			require.NoError(t, err)
			f.verifier.afterVerification = func() error {
				switch mutation {
				case "password reset":
					_, err := f.db.ExecContext(t.Context(), `UPDATE "user" SET password_hash='new-credential' WHERE id=$1`, f.user)
					require.NoError(t, err)
					return nil
				case "permission revoked":
					return f.svc.ConfigureChild(t.Context(), f.host, f.org, f.account.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.ChangePools, false)
				default:
					return f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1))
				}
			}
			_, err = service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
			require.Error(t, err)
			require.Equal(t, 1, f.verifier.calls)
			var count int
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&count))
			require.Zero(t, count, "a verification race must not create any equipment work")
		})
	}
}
