package poolconfigurations

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestBoardSelectionExplainsUnavailableModuleInventory(t *testing.T) {
	conn, scope, _, device := assignmentFixture(t)
	ctx := t.Context()
	var identifier string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT device_identifier FROM device WHERE id=$1`, device.ID).Scan(&identifier))
	_, err := conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, identifier)
	require.NoError(t, err)
	q := sqlc.New(conn)
	_, err = q.AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: identifier, ObservedAt: time.Now()})
	require.NoError(t, err)
	var tank, module, board int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, device.ID).Scan(&tank))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,identity_confirmed,first_seen_at,last_seen_at) VALUES($1,1,'T1-M01',true,now(),now()) RETURNING id`, tank).Scan(&module))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, scope.OrganizationID).Scan(&board))
	_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,1,now(),now(),'{"macAddress":"02:00:00:00:00:01"}')`, board, module)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,1,'occupied',now());`, module, board)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,position,occupancy,observed_at) VALUES($1,2,'empty',now()),($1,3,'empty',now())`, module)
	require.NoError(t, err)
	selection := BoardSelection{ModuleIDs: []int64{module}}
	principal := Principal{Scope: scope}
	rows, err := resolveBoards(ctx, q, principal, selection, true)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// Polling continues, but the controller can no longer locate this module.
	// Retained slot records must not turn the problem into a request to refresh.
	_, err = conn.ExecContext(ctx, `UPDATE container_module SET identity_confirmed=false WHERE id=$1`, module)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE hashboard_slot SET observed_at=now()-interval '10 minutes' WHERE module_id=$1`, module)
	require.NoError(t, err)
	_, err = resolveBoards(ctx, q, principal, selection, true)
	require.ErrorContains(t, err, "module mapping")
	require.NotContains(t, err.Error(), "refresh")
	_, err = resolveBoards(ctx, q, principal, selection, false)
	require.NoError(t, err, "assignment displays remain readable while inventory is unavailable")

	// With a confirmed module identity, an old observation really is stale.
	_, err = conn.ExecContext(ctx, `UPDATE container_module SET identity_confirmed=true WHERE id=$1`, module)
	require.NoError(t, err)
	_, err = resolveBoards(ctx, q, principal, selection, true)
	require.ErrorContains(t, err, "inventory")
	require.NotContains(t, err.Error(), "module mapping")
}
