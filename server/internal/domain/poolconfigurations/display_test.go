package poolconfigurations

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestSavedAssignmentsFollowCurrentIntentAndRetirement(t *testing.T) {
	conn, scope, repo, target := assignmentFixture(t)
	ctx := t.Context()
	_, err := conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=1 WHERE org_id=$1`, scope.OrganizationID)
	require.NoError(t, err)
	config, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, RevisionOptions{Name: "Production", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	})
	require.NoError(t, err)
	require.NoError(t, db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error { return repo.Adopt(ctx, q, scope, target, config.ID, config.RevisionID) }))
	service := NewService(conn, repo, selectionAuthority{Principal{Scope: scope, ActorID: 1}}, nil)
	read := func() EquipmentAssignments {
		t.Helper()
		result, err := service.GetAssignments(ctx, "", nil, []int64{target.ID}, nil)
		require.NoError(t, err)
		require.Equal(t, int64(1), result.SelectedCount)
		return result
	}
	adopted := read()
	require.Len(t, adopted.Assignments, 1)
	require.Equal(t, "adopted", adopted.Assignments[0].State)
	require.Empty(t, adopted.Assignments[0].OperationID)
	// A fresh service instance has the same result: there is no browser/session cache dependency.
	service = NewService(conn, repo, selectionAuthority{Principal{Scope: scope, ActorID: 1}}, nil)
	require.Equal(t, adopted, read())
	result, err := service.GetAssignments(ctx, "", []string{adopted.Assignments[0].DeviceIdentifier}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, adopted, result)
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration SET name='Renamed' WHERE configuration_id=$1`, config.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", read().Assignments[0].Name)
	_, err = conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET retired_at=now(),version=version+1 WHERE organization_id=$1`, scope.OrganizationID)
	require.NoError(t, err)
	require.Len(t, read().Assignments, 1)
	require.Empty(t, read().Assignments[0].ConfigurationID)
	require.Equal(t, "unavailable", read().Assignments[0].State)
	// Another organization cannot retrieve the original device's assignment.
	service = NewService(conn, repo, selectionAuthority{Principal{Scope: Scope{OrganizationID: scope.OrganizationID + 1}, ActorID: 1}}, nil)
	require.Empty(t, read().Assignments)
}
