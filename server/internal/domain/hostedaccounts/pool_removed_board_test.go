package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRemovedBoardCannotBreakControllerPoolStatusBeforeReconciliation(t *testing.T) {
	f := setup(t)
	module, controller := f.miningModule(t)
	board := f.moduleSelection(t, module).Boards[0].Id
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE hashboard_id=$1`, board)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_slot SET occupancy='empty',hashboard_id=NULL WHERE hashboard_id=$1`, board)
	require.NoError(t, err)
	// Model an older backend's pending transition before cleanup has run.
	// Insert as failed so the new insertion guard does not retire this fixture.
	var event uuid.UUID
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state) VALUES($1,$2,7,$3,'ownership','failed') RETURNING transition_id`, f.org, controller, board).Scan(&event))
	_, err = f.db.ExecContext(t.Context(), `UPDATE pool_lifecycle_transition SET state='pending' WHERE transition_id=$1`, event)
	require.NoError(t, err)
	authority, _, _, cipher, ctx := poolFixture(t, f)
	ctx = authn.SetInfo(ctx, &session.Info{UserID: f.host, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
	reviewer := poolapplications.NewReviewer(authority, poolCapabilities{})
	service := poolconfigurations.NewService(f.db, poolconfigurations.NewRepository(cipher), authority, reviewer)
	result, err := service.GetAssignments(ctx, "", nil, []int64{controller}, nil)
	require.NoError(t, err, "missing board display IDs must not fail the controller's entire assignment response")
	require.Len(t, result.Assignments, 1, "the controller must be visible for this regression")
	q := sqlc.New(f.db)
	visible, err := q.ListVisiblePoolLifecycleTransitions(t.Context(), sqlc.ListVisiblePoolLifecycleTransitionsParams{OrganizationID: f.org, DeviceIds: []int64{controller}})
	require.NoError(t, err)
	for _, transition := range visible {
		require.NotEqual(t, event, transition.TransitionID, "the unresolved installation has no current display target")
	}
	require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
	row, err := q.GetPoolLifecycleTransition(t.Context(), event)
	require.NoError(t, err)
	require.Equal(t, "superseded", row.State)
	require.Equal(t, "board_removed", row.ResultCode)
}
