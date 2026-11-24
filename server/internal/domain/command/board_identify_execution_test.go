package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	interfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type executionBoardController struct {
	writes   int
	received sdk.BoardIdentifyRequest
	err      error
}

func (c *executionBoardController) SetBoardIdentify(_ context.Context, r sdk.BoardIdentifyRequest) error {
	c.writes++
	c.received = r
	return c.err
}

// setupBoardIdentifyExecution installs one confirmed board in tank 1, module 2,
// slot 2 and returns the queued payload that targets it.
func setupBoardIdentifyExecution(t *testing.T) (*ExecutionService, *sql.DB, int64, int64, int64, boardIdentifyPayload) {
	t.Helper()
	svc, conn, deviceID, _, tank := setupTankMiningExecution(t)
	var moduleID, hashboardID, installationID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,reported_id,identity_confirmed,first_seen_at,last_seen_at,state) VALUES($1,2,'module-a',true,now(),now(),'active') RETURNING id`, tank.TankIDs[0]).Scan(&moduleID))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, tank.OrgID).Scan(&hashboardID))
	_, err := conn.ExecContext(t.Context(), `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) VALUES($1,'mac','aa:bb:cc:dd:ee:01',$2)`, tank.OrgID, hashboardID)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,2,now(),now()) RETURNING id`, hashboardID, moduleID).Scan(&installationID))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,2,'occupied',now())`, moduleID, hashboardID)
	require.NoError(t, err)

	rows, err := resolveBoardIdentifyTargets(t.Context(), sqlc.New(conn), tank.OrgID, nil, []int64{hashboardID})
	require.NoError(t, err)
	payload := boardIdentifyPayload{
		BoardIdentifyRequest: boardIdentifyRequest(rows[0], sdk.BoardIdentifyBlink),
		OrgID:                tank.OrgID, SiteID: rows[0].SiteID.Int64, HashboardID: hashboardID, InstallationID: installationID,
	}
	require.Equal(t, sdk.BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 2, ModuleReportedID: "module-a", MACAddress: "aa:bb:cc:dd:ee:01"}, payload.Board)

	return svc, conn, deviceID, moduleID, hashboardID, payload
}

func TestBoardIdentifyExecutionRevalidatesBoardBeforeWriting(t *testing.T) {
	svc, conn, deviceID, moduleID, hashboardID, payload := setupBoardIdentifyExecution(t)
	controller := &executionBoardController{}
	require.NoError(t, svc.executeBoardIdentify(t.Context(), deviceID, payload.OrgID, payload, controller))
	require.Equal(t, 1, controller.writes)
	require.Equal(t, payload.BoardIdentifyRequest, controller.received)

	controller.err = errors.New("controller updated 0 boards; outcome unconfirmed")
	require.Error(t, svc.executeBoardIdentify(t.Context(), deviceID, payload.OrgID, payload, controller), "a controller failure is reported, never success")
	require.Equal(t, 2, controller.writes)
	controller.err = nil

	foreign := payload
	foreign.OrgID++
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeBoardIdentify(t.Context(), deviceID, payload.OrgID, foreign, controller)))
	require.Equal(t, 2, controller.writes)

	reset := `UPDATE hashboard_slot SET hashboard_id=$1,occupancy='occupied',unavailable_reason='',observed_at=now() WHERE module_id=$2`
	for _, query := range []string{
		`UPDATE hashboard_slot SET occupancy='empty'`,
		`UPDATE hashboard_slot SET unavailable_reason='controller offline'`,
		`UPDATE hashboard_slot SET observed_at=now()-interval '3 minutes'`,
		`UPDATE container_module SET reported_id='replacement'`,
		`UPDATE container_module SET identity_confirmed=false`,
		`UPDATE hashboard_identity SET value='aa:bb:cc:dd:ee:99'`,
		`UPDATE hashboard_installation SET removed_at=now()`,
	} {
		_, err := conn.ExecContext(t.Context(), reset, hashboardID, moduleID)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), `UPDATE container_module SET reported_id='module-a',identity_confirmed=true,last_seen_at=now()`)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), query)
		require.NoError(t, err)
		require.True(t, fleeterror.IsFailedPreconditionError(svc.executeBoardIdentify(t.Context(), deviceID, payload.OrgID, payload, controller)), query)
		require.Equal(t, 2, controller.writes, query)
		_, err = conn.ExecContext(t.Context(), `UPDATE hashboard_identity SET value='aa:bb:cc:dd:ee:01'`)
		require.NoError(t, err)
	}
}

type boardIdentifyMiner struct {
	interfaces.Miner
	*executionBoardController
}

// The queue stores identify commands as JSON; this pins that a dequeued message
// decodes and reaches the controller only through the revalidating path.
func TestBoardIdentifyQueuedMessageDispatchesToController(t *testing.T) {
	svc, _, deviceID, _, _, payload := setupBoardIdentifyExecution(t)
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	dispatch := func(miner interfaces.Miner, body []byte) (int64, error) {
		ctrl := gomock.NewController(t)
		getter := minerMocks.NewMockCachedMinerGetter(ctrl)
		getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(miner, nil)
		svc.minerService = getter
		_, siteID, err := svc.executeCommandOnDevice(t.Context(), commandtype.SetBoardIdentify, queue.Message{DeviceID: deviceID, OrgID: payload.OrgID, Payload: body})
		return siteID, err
	}
	base := minerIfaceMocks.NewMockMiner(gomock.NewController(t))
	base.EXPECT().GetOrgID().Return(payload.OrgID).AnyTimes()
	base.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

	controller := &executionBoardController{}
	siteID, err := dispatch(&boardIdentifyMiner{Miner: base, executionBoardController: controller}, encoded)
	require.NoError(t, err)
	require.Equal(t, payload.SiteID, siteID)
	require.Equal(t, 1, controller.writes)
	require.Equal(t, payload.BoardIdentifyRequest, controller.received)

	_, err = dispatch(&boardIdentifyMiner{Miner: base, executionBoardController: controller}, []byte("{"))
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	_, err = dispatch(base, encoded)
	require.True(t, fleeterror.IsUnimplementedError(err), "%v", err)
	require.Equal(t, 1, controller.writes)
}
