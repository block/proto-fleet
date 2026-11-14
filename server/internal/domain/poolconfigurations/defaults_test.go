package poolconfigurations

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLibraryAlwaysHasOneDefaultUntilEmpty(t *testing.T) {
	conn, scope, repo, _ := assignmentFixture(t)
	ctx := t.Context()
	create := func(name string) Configuration {
		c, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
			return repo.Create(ctx, q, scope, RevisionOptions{Name: name, Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
		})
		require.NoError(t, err)
		return c
	}
	first, second := create("First"), create("Second")
	require.True(t, first.IsDefault)
	require.False(t, second.IsDefault)
	err := db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.SetDefault(ctx, q, scope, second.ID, second.Version)
	})
	require.NoError(t, err)
	page, err := repo.List(ctx, sqlc.New(conn), scope, "", 50, false)
	require.NoError(t, err)
	for _, c := range page.Configurations {
		require.Equal(t, c.ID == second.ID, c.IsDefault)
	}
	current, err := repo.Get(ctx, sqlc.New(conn), scope, second.ID)
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.Archive(ctx, q, scope, second.ID, current.Version)
	})
	require.NoError(t, err)
	remaining, err := repo.Get(ctx, sqlc.New(conn), scope, first.ID)
	require.NoError(t, err)
	require.True(t, remaining.IsDefault)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.Archive(ctx, q, scope, first.ID, remaining.Version)
	})
	require.NoError(t, err)
	require.True(t, create("New first").IsDefault)
}

func TestDefaultRejectsStaleArchivedAndForeignConfigurations(t *testing.T) {
	conn, scope, repo, _ := assignmentFixture(t)
	ctx := t.Context()
	c, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, RevisionOptions{Name: "Default", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	})
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.SetDefault(ctx, q, scope, c.ID, c.Version-1)
	})
	require.Error(t, err, "stale versions are rejected within the correct owner scope")
	foreign := Scope{OrganizationID: scope.OrganizationID}
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT $1,organization_id,id,1,'Other customer',15 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), scope.OrganizationID).Scan(&foreign.AccountID))
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repo.SetDefault(ctx, q, foreign, c.ID, c.Version)
	})
	require.Error(t, err, "foreign ownership is rejected even with the current version")
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error { return repo.Archive(ctx, q, scope, c.ID, c.Version) })
	require.NoError(t, err)
	err = db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error { return repo.SetDefault(ctx, q, scope, c.ID, c.Version+1) })
	require.Error(t, err)
}
