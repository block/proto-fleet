package poolconfigurations

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestAdoptionCreatesIntentWithoutWorkAndNeverOverwritesLaterAssignments(t *testing.T) {
	conn, scope, repository, target := assignmentFixture(t)
	ctx := t.Context()
	_, err := conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=1 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, target.DeviceID)
	require.NoError(t, err)
	configuration, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repository.Create(ctx, q, scope, RevisionOptions{Name: "Imported", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	})
	require.NoError(t, err)
	adopt := func() error {
		return db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
			return repository.Adopt(ctx, q, scope, target, configuration.ID, configuration.RevisionID)
		})
	}
	require.NoError(t, adopt())
	require.NoError(t, adopt())
	q := sqlc.New(conn)
	var assignmentVersion int64
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT assignment_version FROM pool_configuration WHERE configuration_id=$1`, configuration.ID).Scan(&assignmentVersion))
	require.EqualValues(t, 1, assignmentVersion, "import replays must not invalidate an outstanding review")
	assignments, err := q.ListPoolConfigurationAssignments(ctx, sqlc.ListPoolConfigurationAssignmentsParams{OrganizationID: scope.OrganizationID, ConfigurationID: configuration.ID})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.EqualValues(t, 1, assignments[0].Version)
	require.True(t, assignments[0].AdoptedAt.Valid)
	require.False(t, assignments[0].OperationID.Valid)
	work, err := q.ListPendingPoolConfigurationWork(ctx)
	require.NoError(t, err)
	require.Empty(t, work)
	var operations int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&operations))
	require.Zero(t, operations)
	down, err := migrations.Current.ReadFile("current/001053_shared_pool_equipment_state.down.sql")
	require.NoError(t, err)
	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(down))
	require.ErrorContains(t, err, "cannot roll back pool adoption")
	require.NoError(t, tx.Rollback())

	operation, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Operation, error) {
		return repository.Assign(ctx, q, scope, 1, configuration.ID, configuration.RevisionID, []AssignmentChange{{Target: target, ExpectedVersion: 1}})
	})
	require.NoError(t, err, "adopted intent participates in ordinary reviewed updates")
	require.Len(t, operation.Targets, 1)
	require.Error(t, adopt(), "import cannot replace a newer approved operation")
	require.NoError(t, db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		return repository.RetireAssignment(ctx, q, scope, target, 2)
	}))
	require.Error(t, adopt(), "import cannot revive retired intent")
	assignments, err = q.ListPoolConfigurationAssignments(ctx, sqlc.ListPoolConfigurationAssignmentsParams{OrganizationID: scope.OrganizationID, ConfigurationID: configuration.ID})
	require.NoError(t, err)
	require.Empty(t, assignments)
}
