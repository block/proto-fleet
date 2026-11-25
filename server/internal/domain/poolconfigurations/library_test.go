package poolconfigurations

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestLibraryDuplicateAndArchiveRespectAssignments(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	password := "private-secret"
	original, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, RevisionOptions{Name: "Original", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout", Password: &password}}})
	})
	require.NoError(t, err)
	duplicate, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Duplicate(ctx, q, scope, original.ID, "Variation")
	})
	require.NoError(t, err)
	require.NotEqual(t, original.ID, duplicate.ID)
	require.NotEqual(t, original.Slots[0].ID, duplicate.Slots[0].ID)
	require.True(t, duplicate.Slots[0].HasPassword)
	op, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		return repo.Assign(ctx, q, scope, 1, original.ID, original.RevisionID, []AssignmentChange{{Target: target}})
	})
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error { return repo.Archive(ctx, q, scope, original.ID, original.Version) })
	require.True(t, fleeterror.IsFailedPreconditionError(err), "an in-use configuration cannot be archived")
	_, err = db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		return repo.Assign(ctx, q, scope, 1, duplicate.ID, duplicate.RevisionID, []AssignmentChange{{Target: target, ExpectedVersion: op.Targets[0].AssignmentVersion}})
	})
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error { return repo.Archive(ctx, q, scope, original.ID, original.Version) })
	require.NoError(t, err)
	page, err := repo.List(ctx, sqlc.New(conn), scope, "", 50, false)
	require.NoError(t, err)
	require.Len(t, page.Configurations, 1)
	require.Equal(t, duplicate.ID, page.Configurations[0].ID)
	page, err = repo.List(ctx, sqlc.New(conn), scope, "", 50, true)
	require.NoError(t, err)
	require.Len(t, page.Configurations, 2)
	archived, err := repo.Get(ctx, sqlc.New(conn), scope, original.ID)
	require.NoError(t, err)
	require.True(t, archived.Archived)
}
