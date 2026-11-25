package hostedaccounts_test

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func removalBatchFixture(t *testing.T, count int) (poolStepUpFixture, *poolconfigurations.Service, context.Context, *poolDevice, []string, *poolapplications.Worker) {
	t.Helper()
	f, service, ctx, assignments := workerBatchFixture(t, count)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	device := &poolDevice{pools: []dto.MiningPool{{URL: "stratum+tcp://existing.example", Username: "existing"}}}
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
	checker := poolapplications.NewChecker(f.db, reviewer, poolMinerGetter{device}, cipher, f.pools)
	service.WithChecker(checker)
	worker := poolapplications.NewWorker(f.db, poolconfigurations.NewRepository(cipher), reviewer, poolMinerGetter{device}, cipher).WithRemovals(checker)
	ids := make([]string, count)
	for i, assignment := range assignments {
		review, err := service.ReviewRemoval(ctx, "", assignment.Targets[0])
		require.NoError(t, err)
		ids[i] = review.ID
	}
	return f, service, ctx, device, ids, worker
}

func TestPoolRemovalBatchUsesOnePasswordCheckBeyondTenDevices(t *testing.T) {
	f, service, ctx, device, ids, worker := removalBatchFixture(t, 12)
	require.NoError(t, f.verifier.VerifySessionCredentials(ctx, f.info.Username, "actor-password"))
	require.NoError(t, service.AuthorizeReviews(ctx, "", ids, "actor-password"))
	for _, id := range ids {
		result, err := confirmAndRunRemoval(t, service, worker, ctx, "", id, false, "")
		require.NoError(t, err)
		require.Equal(t, "completed", result.State)
		replayed, err := confirmAndRunRemoval(t, service, worker, ctx, "", id, false, "")
		require.NoError(t, err)
		require.Equal(t, "completed", replayed.State)
	}
	require.Equal(t, 12, device.writes, "accepted retries never repeat the clear")
	require.Empty(t, device.pools)
	require.Equal(t, 2, f.verifier.calls, "opening authentication plus one batch approval")
}

func TestPoolRemovalUsesSharedReviewAndQueuedOperation(t *testing.T) {
	f, service, ctx, device, ids, _ := removalBatchFixture(t, 1)
	var kind string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT kind FROM pool_configuration_review WHERE review_id=$1`, ids[0]).Scan(&kind))
	require.Equal(t, "remove", kind)
	result, err := service.ConfirmReview(ctx, "", ids[0], poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	require.NotNil(t, result.Operation)
	require.Zero(t, device.writes, "confirmation queues work rather than writing hardware")
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT kind FROM pool_configuration_operation WHERE operation_id=$1`, result.Operation.ID).Scan(&kind))
	require.Equal(t, "clear", kind)
	var assigned int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE organization_id=$1 AND retired_at IS NULL`, f.org).Scan(&assigned))
	require.Equal(t, 1, assigned, "the original assignment remains until verified clearing")
}

func TestPoolRemovalBatchApprovalIsLimitedToExactReviews(t *testing.T) {
	f, service, ctx, device, ids, worker := removalBatchFixture(t, 2)
	require.NoError(t, service.AuthorizeReviews(ctx, "", ids[:1], "actor-password"))
	_, err := confirmAndRunRemoval(t, service, worker, ctx, "", ids[1], true, "")
	require.Error(t, err)
	result, err := confirmAndRunRemoval(t, service, worker, ctx, "", ids[0], true, "")
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
	require.Zero(t, device.writes)
	require.Len(t, device.pools, 1)
	require.Equal(t, 1, f.verifier.calls)
}

func TestPoolRemovalBatchApprovalRechecksSessionAndCredentials(t *testing.T) {
	for _, change := range []string{"session", "password", "expired", "permission"} {
		t.Run(change, func(t *testing.T) {
			f, service, ctx, device, ids, worker := removalBatchFixture(t, 1)
			require.NoError(t, service.AuthorizeReviews(ctx, "", ids, "actor-password"))
			var err error
			switch change {
			case "session":
				info := f.info
				info.SessionID = uuid.NewString()
				ctx = authn.SetInfo(t.Context(), &info)
			case "password":
				_, err = f.db.ExecContext(ctx, `UPDATE "user" SET password_hash='changed' WHERE id=$1`, f.user)
			case "expired":
				_, err = f.db.ExecContext(ctx, `ALTER TABLE pool_configuration_review DISABLE TRIGGER pool_configuration_review_immutable`)
				require.NoError(t, err)
				_, err = f.db.ExecContext(ctx, `UPDATE pool_configuration_review SET expires_at=clock_timestamp()-interval '1 minute' WHERE review_id=$1`, ids[0])
			case "permission":
				_, err = f.db.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, f.user, f.org)
			}
			require.NoError(t, err)
			_, err = confirmAndRunRemoval(t, service, worker, ctx, "", ids[0], false, "")
			require.Error(t, err)
			require.Zero(t, device.writes)
			require.Equal(t, 1, f.verifier.calls)
		})
	}
}

func TestPoolRemovalBatchRejectsInvalidSelectionBeforePasswordCheck(t *testing.T) {
	f, service, ctx, device, ids, _ := removalBatchFixture(t, 1)
	for _, batch := range [][]string{nil, {"invalid"}, {ids[0], ids[0]}, {ids[0], uuid.NewString()}} {
		require.Error(t, service.AuthorizeReviews(ctx, "", batch, "actor-password"))
	}
	require.Zero(t, f.verifier.calls)
	require.Zero(t, device.writes)
}

func TestQueuedPoolRemovalReplaysWithoutRedispatch(t *testing.T) {
	f, service, ctx, device, ids, worker := removalBatchFixture(t, 1)
	options := poolconfigurations.Confirmation{Password: "actor-password"}
	accepted, err := service.ConfirmReview(ctx, "", ids[0], options)
	require.NoError(t, err)
	replay, err := service.ConfirmReview(ctx, "", ids[0], poolconfigurations.Confirmation{})
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, accepted.Operation.ID, replay.Operation.ID)
	require.Zero(t, device.writes)
	r, err := service.GetRemoval(ctx, "", ids[0])
	require.NoError(t, err)
	require.Equal(t, "pending", r.State)
	reopened, err := service.ReviewRemoval(ctx, "", r.Target)
	require.NoError(t, err)
	require.Equal(t, ids[0], reopened.ID)
	require.NoError(t, worker.Execute(ctx, f.org, ids[0], r.Target))
	require.NoError(t, worker.Execute(ctx, f.org, ids[0], r.Target))
	require.Equal(t, 1, device.writes)
	result, err := service.GetOperation(ctx, "", ids[0])
	require.NoError(t, err)
	require.Equal(t, "clear", result.Kind)
	require.Equal(t, "completed", result.Targets[0].State)
}

func TestQueuedPoolRemovalRechecksAuthorityBeforeDispatch(t *testing.T) {
	f, service, ctx, device, ids, worker := removalBatchFixture(t, 1)
	_, err := service.ConfirmReview(ctx, "", ids[0], poolconfigurations.Confirmation{Password: "actor-password"})
	require.NoError(t, err)
	r, err := service.GetRemoval(ctx, "", ids[0])
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, f.user, f.org)
	require.NoError(t, err)
	require.Error(t, worker.Execute(ctx, f.org, ids[0], r.Target))
	require.Zero(t, device.writes)
	var state string
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT state FROM pool_configuration_operation_target WHERE operation_id=$1`, ids[0]).Scan(&state))
	require.Equal(t, "failed", state)
	var assigned bool
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT retired_at IS NULL FROM pool_configuration_assignment WHERE organization_id=$1 AND target_kind=$2 AND target_id=$3`, f.org, r.Target.Kind, r.Target.ID).Scan(&assigned))
	require.True(t, assigned)
}
