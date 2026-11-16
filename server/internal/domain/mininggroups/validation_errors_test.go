package mininggroups

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestSharedWorkflowWaitsForMissingInventoryAndResumes(t *testing.T) {
	for _, position := range []int{1, 2, 0} {
		t.Run(map[int]string{1: "selected board", 2: "empty sibling slot", 0: "mixed observations"}[position], func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			_, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			before := f.state(t)
			_, err := f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot s USING container_module m,container_tank tank WHERE s.module_id=m.id AND m.tank_id=tank.id AND tank.device_id=$1 AND s.position=$2`, f.target.DeviceID, position)
			if position == 0 {
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_slot s SET observed_at=s.observed_at+INTERVAL '1 millisecond' FROM container_module m,container_tank tank WHERE s.module_id=m.id AND m.tank_id=tank.id AND tank.device_id=$1 AND s.position=2`, f.target.DeviceID)
			}
			require.NoError(t, err)
			firmware.calls = nil
			f.reconcile(t, ctx)
			after := f.state(t)
			require.Equal(t, "blocked", after.Status)
			require.Equal(t, before.AttemptID, after.AttemptID)
			for _, call := range firmware.calls {
				require.True(t, call.ReadOnly(), "missing inventory must prevent mutations without ending the workflow")
			}
			require.Empty(t, firmware.receipts)

			publishSharedBoardInventory(t, ctx, f)
			_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_slot s SET hashboard_id=i.hashboard_id,occupancy='occupied' FROM hashboard_installation i WHERE i.device_id=$1 AND i.removed_at IS NULL AND s.module_id=i.module_id AND s.position=i.position`, f.target.DeviceID)
			require.NoError(t, err)
			for i := 0; i < 30 && active(f.state(t).Status); i++ {
				f.reconcile(t, ctx)
			}
			require.Equal(t, statusComplete, f.state(t).Status)
			require.NotEmpty(t, firmware.receipts, "the same workflow must resume after inventory returns")
		})
	}
}

func TestSharedWorkflowPreservesDatabaseValidationError(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
	before := f.state(t)
	_, err := f.conn.ExecContext(ctx, `ALTER TABLE pool_configuration_revision RENAME COLUMN slots TO unavailable_slots`)
	require.NoError(t, err)
	firmware.calls = nil
	err = f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
	var databaseErr *pgconn.PgError
	require.ErrorAs(t, err, &databaseErr)
	require.Equal(t, "42703", databaseErr.Code, "keep the original undefined-column error instead of treating it as an ownership change")
	after := f.state(t)
	require.Equal(t, before.Status, after.Status)
	require.Equal(t, before.AttemptID, after.AttemptID)
	require.Empty(t, firmware.calls)
}
