package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
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
	_, err = service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "incorrect"})
	require.Error(t, err)
	var count int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&count))
	require.Zero(t, count)
	saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.NotNil(t, saved.Operation)
	calls := f.verifier.calls
	for _, password := range []string{"", "actor-password"} {
		replay, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: password})
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
			_, err = service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
			require.Error(t, err)
			require.Equal(t, 1, f.verifier.calls)
			var count int
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&count))
			require.Zero(t, count, "a verification race must not create any equipment work")
		})
	}
}

func TestExpiredPoolConfirmationDistinguishesNeverAcceptedFromReplay(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	repo := poolconfigurations.NewRepository(cipher)
	service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer)
	config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}})
	require.NoError(t, err)
	// Reviews are immutable. Insert expired fixture snapshots rather than changing
	// an existing review's deadline or using wall-clock sleeps.
	expiredCopy := func() string {
		id := uuid.NewString()
		_, err := f.db.ExecContext(ctx, `INSERT INTO pool_configuration_review(review_id,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,expires_at,saved_version,saved_revision_id,operation_id)
   SELECT $1,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,clock_timestamp()-interval '1 second',saved_version,saved_revision_id,operation_id FROM pool_configuration_review WHERE review_id=$2`, id, review.ID)
		require.NoError(t, err)
		return id
	}
	expired := expiredCopy()
	_, err = service.ConfirmReview(ctx, f.account.AccountID, expired, poolconfigurations.Confirmation{Password: "actor-password"})
	var terminal fleeterror.FleetError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, fleeterror.ErrorCodeTypeEndpoint, terminal.FleetErrorCodeType)
	require.EqualValues(t, pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_EXPIRED_UNACCEPTED, terminal.FleetErrorCode)
	require.Zero(t, f.verifier.calls, "expiry is checked before password verification")
	f.requireNoPoolOperations(t)
	// Repository confirmation repeats the expiry check after credential verification.
	_, err = db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
		return repo.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}, f.user, expired, reviewer, nil)
	})
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, fleeterror.ErrorCodeTypeEndpoint, terminal.FleetErrorCodeType)
	require.EqualValues(t, pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_EXPIRED_UNACCEPTED, terminal.FleetErrorCode)
	saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.NotNil(t, saved.Operation)
	calls := f.verifier.calls
	replay, err := service.ConfirmReview(ctx, f.account.AccountID, expiredCopy(), poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, saved.Operation.ID, replay.Operation.ID)
	require.Equal(t, calls, f.verifier.calls)
	var operations int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&operations))
	require.Equal(t, 1, operations)
}
