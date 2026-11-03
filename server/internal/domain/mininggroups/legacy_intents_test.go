package mininggroups

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type intentQueryCounter struct{ calls int }

func (c *intentQueryCounter) RetryQuery(_ context.Context, _ string, call func() error) error {
	c.calls++
	return call()
}

func TestHistoricalIntentQueryCountDoesNotGrowPerBoard(t *testing.T) {
	ctx, f, _, config, boards := newTransferFailureFixture(t)
	row := f.state(t)
	row.OperationID = uuid.NullUUID{} // Historical boards outside the current selection.
	counter := &intentQueryCounter{}
	q := sqlc.NewRetryingQuerier(f.service.queries, counter)
	desired, err := f.service.liveIntent(ctx, q, row, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"02:00:00:00:00:01": config}, desired)
	singleBoardQueries := counter.calls
	addHistoricalPoolBoard(t, ctx, f, boards[0])
	counter.calls = 0
	desired, err = f.service.liveIntent(ctx, q, row, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"02:00:00:00:00:01": config, historicalBoardMAC: config}, desired)
	require.Equal(t, singleBoardQueries, counter.calls, "loading historical intent should not require an extra query for every board")
}

func TestLegacyIntentSurvivesAdoptionButNotManagedOrRetiredAssignments(t *testing.T) {
	ctx, f, _, legacyConfig, _ := newTransferFailureFixture(t)
	row := f.state(t)
	row.OperationID = uuid.NullUUID{}
	q := f.service.queries
	intents, err := q.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: intents[0].InstallationID, DeviceID: row.DeviceID}
	scope := poolconfigurations.Scope{OrganizationID: row.OrganizationID}
	repo := poolconfigurations.NewRepository(f.service.secrets)
	config, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
		config, err := repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: "Imported", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
		if err != nil {
			return config, err
		}
		return config, repo.Adopt(ctx, q, scope, target, config.ID, config.RevisionID)
	})
	require.NoError(t, err)
	desired, err := f.service.liveIntent(ctx, q, row, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{intents[0].MacAddress: legacyConfig}, desired, "adoption must preserve existing controller intent")
	_, err = db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
		return repo.Assign(ctx, q, scope, row.ActorID.Int64, config.ID, config.RevisionID, []poolconfigurations.AssignmentChange{{Target: target, ExpectedVersion: 1}})
	})
	require.NoError(t, err)
	desired, err = f.service.liveIntent(ctx, q, row, nil)
	require.NoError(t, err)
	require.Empty(t, desired, "new managed intent supersedes the legacy source")
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		return repo.RetireAssignment(ctx, q, scope, target, 2)
	}))
	desired, err = f.service.liveIntent(ctx, q, row, nil)
	require.NoError(t, err)
	require.Empty(t, desired, "retirement must not resurrect legacy intent")
	retained, err := q.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: row.OrganizationID, DeviceID: row.DeviceID})
	require.NoError(t, err)
	require.Equal(t, intents, retained, "recovery and cleanup still see the complete legacy records")
}
