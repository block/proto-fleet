package poolconfigurations

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Run an intervening event after the admission transaction has released its
// locks, but before final confirmation starts. All review state uses real SQL.
type confirmationBoundaryAuthority struct {
	Authority
	calls       int
	beforeFinal func()
}

func (a *confirmationBoundaryAuthority) AuthorizePoolConfiguration(ctx context.Context, q sqlc.Querier, accountID string, access Access) (Principal, error) {
	a.calls++
	if a.calls == 2 {
		a.beforeFinal()
	}
	return a.Authority.AuthorizePoolConfiguration(ctx, q, accountID, access)
}

func (f workerNameFixture) sessionContext(t *testing.T) context.Context {
	t.Helper()
	return authn.SetInfo(t.Context(), &session.Info{UserID: f.actorID, OrganizationID: f.scope.OrganizationID, AuthMethod: session.AuthMethodSession, SessionID: uuid.NewString()})
}

func requireConfirmationCode(t *testing.T, err error, code pb.ConfirmReviewErrorCode) {
	t.Helper()
	var detail fleeterror.FleetError
	require.ErrorAs(t, err, &detail)
	require.Equal(t, fleeterror.ErrorCodeTypeEndpoint, detail.FleetErrorCodeType)
	require.EqualValues(t, code, detail.FleetErrorCode)
}

func TestConfirmationMissingApprovalHasDedicatedGenericError(t *testing.T) {
	for _, kind := range []string{"worker name", "pool assignment"} {
		t.Run(kind, func(t *testing.T) {
			f := newWorkerNameFixture(t)
			ctx := f.sessionContext(t)
			var review Review
			if kind == "worker name" {
				reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
				require.NoError(t, err)
				review = reviews[0]
			} else {
				var err error
				review, err = f.service.ReviewAssignment(ctx, "", f.config.ID, f.config.Version, []AssignmentTarget{f.target})
				require.NoError(t, err)
			}
			before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
			_, err := f.service.ConfirmReview(ctx, "", review.ID, Confirmation{Password: ""})
			requireConfirmationCode(t, err, pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_AUTHORIZATION_REQUIRED)
			require.ErrorContains(t, err, "Verify your password to confirm these changes.")
			require.NotContains(t, err.Error(), "worker-name")
			require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
		})
	}
}

func TestConfirmationExpiryAfterAdmissionPrecedesAuthorization(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := f.sessionContext(t)
	reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	expiring := uuid.NewString()
	_, err = f.conn.ExecContext(ctx, `INSERT INTO pool_configuration_review(review_id,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,expires_at)
 SELECT $1,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,clock_timestamp()+interval '3 seconds' FROM pool_configuration_review WHERE review_id=$2`, expiring, reviews[0].ID)
	require.NoError(t, err)
	require.NoError(t, f.service.AuthorizeWorkerNameReviews(ctx, []string{expiring}, "actor-password"))
	boundary := &confirmationBoundaryAuthority{Authority: f.service.authority, beforeFinal: func() {
		// Expire the immutable deadline between transactions, without rewriting it.
		_, err := f.conn.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.01) FROM pool_configuration_review WHERE review_id=$1`, expiring)
		require.NoError(t, err)
	}}
	f.service.authority = boundary
	before := poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	_, err = f.service.ConfirmReview(ctx, "", expiring, Confirmation{Password: ""})
	require.Equal(t, 2, boundary.calls, "the initial admission must succeed before expiry")
	requireConfirmationCode(t, err, pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_EXPIRED_UNACCEPTED)
	require.Equal(t, before, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
}

func TestConfirmationAcceptedAfterAdmissionReplaysWithoutApproval(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := f.sessionContext(t)
	reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	other := NewService(f.conn, f.repo, f.service.authority, f.equipment)
	var accepted ReviewedSave
	var afterAccepted map[string]string
	f.service.authority = &confirmationBoundaryAuthority{Authority: f.service.authority, beforeFinal: func() {
		var err error
		accepted, err = other.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: "actor-password"})
		require.NoError(t, err)
		afterAccepted = poolMaintenanceState(t, f.conn, f.scope.OrganizationID)
	}}
	replay, err := f.service.ConfirmReview(ctx, "", reviews[0].ID, Confirmation{Password: ""})
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, accepted.Operation.ID, replay.Operation.ID)
	require.Equal(t, afterAccepted, poolMaintenanceState(t, f.conn, f.scope.OrganizationID))
}

func TestConfirmationReviewExpiryIsEvaluatedAfterLockWait(t *testing.T) {
	f := newWorkerNameFixture(t)
	ctx := t.Context()
	reviews, err := f.service.ReviewWorkerNames(ctx, map[string]string{f.identifier: "new-worker"})
	require.NoError(t, err)
	expiring := uuid.NewString()
	_, err = f.conn.ExecContext(ctx, `INSERT INTO pool_configuration_review(review_id,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,expires_at)
 SELECT $1,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,clock_timestamp()+interval '3 seconds' FROM pool_configuration_review WHERE review_id=$2`, expiring, reviews[0].ID)
	require.NoError(t, err)
	blocker, err := f.conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.ExecContext(ctx, `SELECT review_id FROM pool_configuration_review WHERE review_id=$1 FOR UPDATE`, expiring)
	require.NoError(t, err)
	waiting, err := f.conn.Conn(ctx)
	require.NoError(t, err)
	defer func() {
		_ = blocker.Rollback()
		_ = waiting.Close()
	}()
	var pid int
	require.NoError(t, waiting.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	type result struct {
		valid bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		review, err := sqlc.New(waiting).LockPoolConfigurationReview(ctx, sqlc.LockPoolConfigurationReviewParams{OrganizationID: f.scope.OrganizationID, ActorID: f.actorID, ReviewID: expiring})
		done <- result{review.Valid, err}
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.conn.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, pid).Scan(&blocked)
		return err == nil && blocked
	}, time.Second, 10*time.Millisecond)
	_, err = f.conn.ExecContext(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.01) FROM pool_configuration_review WHERE review_id=$1`, expiring)
	require.NoError(t, err)
	require.NoError(t, blocker.Rollback())
	got := <-done
	require.NoError(t, got.err)
	require.False(t, got.valid, "expiry must be evaluated after acquiring the lock")
}
