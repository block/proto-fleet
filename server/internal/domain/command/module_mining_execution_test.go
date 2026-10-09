package command

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type executionModuleController struct {
	writes   int
	received sdk.ModuleMiningRequest
	err      error
}

func (c *executionModuleController) SetModuleMining(_ context.Context, r sdk.ModuleMiningRequest) error {
	c.writes++
	c.received = r
	return c.err
}

func TestModuleMiningExecutionRejectsReplacedRetiredMovedAndStaleIdentities(t *testing.T) {
	svc, conn, deviceID, _, tank := setupTankMiningExecution(t)
	var moduleID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at,state) VALUES($1,2,'module-a',now(),now(),'hashing') RETURNING id`, tank.TankIDs[0]).Scan(&moduleID))
	payload := scopedMiningPayload{TankMiningRequest: sdk.TankMiningRequest{Mining: new(false)}, OrgID: tank.OrgID, ModuleIDs: []int64{moduleID}, Modules: []sdk.ModuleMiningTarget{{Tank: 1, Module: 2, ReportedID: "module-a"}}}
	controller := &executionModuleController{}
	require.NoError(t, svc.executeModuleMining(t.Context(), deviceID, tank.OrgID, payload, controller))
	require.Equal(t, payload.Modules, controller.received.Modules)
	require.False(t, *controller.received.Mining)
	controller.err = errors.New("connection lost after write")
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeModuleMining(t.Context(), deviceID, tank.OrgID, payload, controller)))
	require.Equal(t, 2, controller.writes)
	for _, query := range []string{
		`UPDATE container_module SET reported_id='replacement'`,
		`UPDATE container_module SET retired_at=now()`,
		`UPDATE container_module SET position=3`,
		`UPDATE container_module SET last_seen_at=now()-interval '3 minutes'`,
	} {
		_, err := conn.ExecContext(t.Context(), `UPDATE container_module SET reported_id='module-a',retired_at=NULL,position=2,last_seen_at=now()`)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), query)
		require.NoError(t, err)
		require.True(t, fleeterror.IsFailedPreconditionError(svc.executeModuleMining(t.Context(), deviceID, tank.OrgID, payload, controller)), query)
		require.Equal(t, 2, controller.writes)
	}
}
