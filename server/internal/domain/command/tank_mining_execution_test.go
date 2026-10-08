package command

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type executionTankController struct {
	writes   int
	received sdk.TankMiningRequest
	err      error
}

func (c *executionTankController) SetTankMining(_ context.Context, r sdk.TankMiningRequest) error {
	c.writes++
	c.received = r
	return c.err
}
func TestTankMiningExecutionRechecksPresenceSiteAndScope(t *testing.T) {
	svc, conn, _, deviceID, _ := setupPasswordCommandDevice(t, "proto", false)
	const orgID int64 = 1
	var err error
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, deviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,observed_at) VALUES($1,now())`, deviceID)
	require.NoError(t, err)
	var tank int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, deviceID).Scan(&tank))
	controller := &executionTankController{}
	payload := tankMiningPayload{TankMiningRequest: sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)}, TankIDs: []int64{tank}, OrgID: orgID}
	require.NoError(t, svc.executeTankMining(t.Context(), deviceID, orgID, 0, payload, controller))
	require.Equal(t, 1, controller.writes)
	require.False(t, *controller.received.Mining)
	controller.err = errors.New("connection lost")
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, 0, payload, controller)))
	require.Equal(t, 2, controller.writes)
	for _, sql := range []string{`UPDATE container_tank SET present=false`, `UPDATE container_tank SET last_seen_at=now()-interval '3 minutes'`, `UPDATE container_tank SET position=2`} {
		_, err = conn.ExecContext(t.Context(), `UPDATE container_tank SET present=true,position=1,last_seen_at=now()`)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), sql)
		require.NoError(t, err)
		require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, 0, payload, controller)))
		require.Equal(t, 2, controller.writes)
	}
	_, err = conn.ExecContext(t.Context(), `UPDATE container_tank SET present=true,position=1,last_seen_at=now()`)
	require.NoError(t, err)
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, 55, payload, controller)))
	require.Equal(t, 2, controller.writes)
}
