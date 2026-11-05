package poolconfigurations

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type reviewAuthority struct {
	binding string
	blocked bool
}

func (a *reviewAuthority) ReviewTargets(_ context.Context, _ sqlc.Querier, _ Scope, _ int64, targets []AssignmentTarget, _ []Endpoint) ([]TargetApproval, error) {
	if a.blocked {
		return nil, fleeterror.NewForbiddenError("equipment permissions changed")
	}
	approved := make([]TargetApproval, len(targets))
	for i, target := range targets {
		approved[i] = TargetApproval{Target: target, Binding: a.binding}
	}
	return approved, nil
}

func TestReviewedEditIsAtomicBoundAndReplayable(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	password := "secret"
	options := RevisionOptions{Name: "Production", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout", Password: &password}}}
	config, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) { return repo.Create(ctx, q, scope, options) })
	require.NoError(t, err)
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		return repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, []AssignmentChange{{Target: target}})
	})
	require.NoError(t, err)
	options.Slots[0].ID = config.Slots[0].ID
	options.Slots[0].URL = "stratum+tcp://backup.example"
	authority := &reviewAuthority{binding: "device-epoch-1"}
	review, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Review, error) {
		return repo.ReviewEdit(ctx, q, scope, 1, config.ID, config.Version, options, authority)
	})
	require.NoError(t, err)
	require.Len(t, review.Targets, 1)
	require.True(t, review.SettingsChanged)
	for _, mutation := range []string{
		`UPDATE pool_configuration_review SET actor_id=2 WHERE review_id=$1`,
		`UPDATE pool_configuration_review SET name='unreviewed' WHERE review_id=$1`,
		`UPDATE pool_configuration_review SET targets='[]' WHERE review_id=$1`,
		`UPDATE pool_configuration_review SET configuration_version=configuration_version+1 WHERE review_id=$1`,
	} {
		_, err := conn.ExecContext(ctx, mutation, review.ID)
		require.ErrorContains(t, err, "reviews are immutable")
	}
	expiredID := uuid.NewString()
	_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_review(review_id,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,expires_at)
 SELECT $1,kind,organization_id,configuration_id,actor_id,configuration_version,assignment_version,name,slots,settings_changed,targets,clock_timestamp()-INTERVAL '1 second' FROM pool_configuration_review WHERE review_id=$2`, expiredID, review.ID)
	require.NoError(t, err)
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ReviewedSave, error) {
		return repo.ConfirmReview(ctx, q, scope, 1, expiredID, authority, nil)
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "expired reviews require a fresh impact check")
	confirm := func(actor int64) (ReviewedSave, error) {
		return db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, scope, actor, review.ID, authority, nil)
		})
	}
	_, err = confirm(2)
	require.Error(t, err, "review approval is bound to its actor")
	authority.binding = "device-epoch-2"
	_, err = confirm(1)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "new equipment binding requires fresh review")
	authority.binding = "device-epoch-1"
	authority.blocked = true
	_, err = confirm(1)
	require.True(t, fleeterror.IsForbiddenError(err))
	var revision string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT current_revision_id FROM pool_configuration WHERE configuration_id=$1`, config.ID).Scan(&revision))
	require.Equal(t, config.RevisionID, revision, "failed confirmation cannot save a revision or partial update")
	authority.blocked = false
	saved, err := confirm(1)
	require.NoError(t, err)
	require.NotNil(t, saved.Operation)
	require.Len(t, saved.Operation.Targets, 1)
	require.NotEqual(t, config.RevisionID, saved.Configuration.RevisionID)
	var binding string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT reviewed_binding FROM pool_configuration_operation_target WHERE operation_id=$1`, saved.Operation.ID).Scan(&binding))
	require.Equal(t, authority.binding, binding, "queued work retains the exact reviewed physical and ownership binding")
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET reviewed_binding='new binding' WHERE operation_id=$1`, saved.Operation.ID)
	require.ErrorContains(t, err, "operation target identity is immutable")
	again, err := confirm(1)
	require.NoError(t, err)
	require.True(t, again.Replay)
	again.Replay = false
	require.Equal(t, saved, again, "a lost response must not create duplicate updates")
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_revision WHERE configuration_id=$1`, config.ID).Scan(&count))
	require.Equal(t, 2, count)
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_review SET saved_version=saved_version+1 WHERE review_id=$1`, review.ID)
	require.ErrorContains(t, err, "reviews are immutable", "accepted review results cannot be rewritten")
}

func TestReviewRejectsChangedAssignmentSetAndRenameSendsNoWork(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	options := RevisionOptions{Name: "Production", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}}
	config, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) { return repo.Create(ctx, q, scope, options) })
	require.NoError(t, err)
	options.Slots[0].ID = config.Slots[0].ID
	options.Slots[0].WorkerTemplate = "{miner_name}"
	authority := &reviewAuthority{binding: "current"}
	review, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Review, error) {
		return repo.ReviewEdit(ctx, q, scope, 1, config.ID, config.Version, options, authority)
	})
	require.NoError(t, err)
	require.Empty(t, review.Targets)
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		return repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, []AssignmentChange{{Target: target}})
	})
	require.NoError(t, err)
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ReviewedSave, error) {
		return repo.ConfirmReview(ctx, q, scope, 1, review.ID, authority, nil)
	})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "reviewing zero targets cannot authorize newly assigned equipment")
	options.Name = "Renamed"
	options.Slots[0].WorkerTemplate = ""
	authority.blocked = true
	review, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Review, error) {
		return repo.ReviewEdit(ctx, q, scope, 1, config.ID, config.Version, options, authority)
	})
	require.NoError(t, err, "metadata-only edits do not require hardware command permissions")
	require.Empty(t, review.Targets)
	saved, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ReviewedSave, error) {
		return repo.ConfirmReview(ctx, q, scope, 1, review.ID, authority, nil)
	})
	require.NoError(t, err)
	require.Nil(t, saved.Operation)
	require.Equal(t, config.RevisionID, saved.Configuration.RevisionID)
	require.Equal(t, "Renamed", saved.Configuration.Name)
}

func TestAssignmentReviewFreezesSelectionAndRejectsInterveningAssignment(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	config, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, RevisionOptions{Name: "Pools", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	})
	require.NoError(t, err)
	authority := &reviewAuthority{binding: "current"}
	prepare := func() Review {
		t.Helper()
		review, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Review, error) {
			return repo.ReviewAssignment(ctx, q, scope, 1, config.ID, config.Version, []AssignmentTarget{target}, authority)
		})
		require.NoError(t, err)
		return review
	}
	first, stale := prepare(), prepare()
	confirm := func(id string) (ReviewedSave, error) {
		return db.WithTransaction(ctx, conn, func(q sqlc.Querier) (ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, scope, 1, id, authority, nil)
		})
	}
	result, err := confirm(first.ID)
	require.NoError(t, err)
	require.NotNil(t, result.Operation)
	require.Equal(t, config.RevisionID, result.Operation.RevisionID)
	_, err = confirm(stale.ID)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	retry, err := confirm(first.ID)
	require.NoError(t, err)
	require.Equal(t, result.Operation.ID, retry.Operation.ID)
}
