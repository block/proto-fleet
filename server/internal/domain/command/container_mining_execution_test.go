package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	interfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerMiningExecutionClassifiesAndSanitizesErrors(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			f := setupContainerMiningSiteTest(t)
			outcome := errors.New("private controller transport detail")
			if upgrade {
				outcome = devicewrite.NotDispatched(fmt.Errorf("private routing detail: %w", control.ErrCommandProtocolUpgradeRequired))
			}
			miner := &containerMiningMiner{Miner: f.miner, err: outcome}
			err := f.execution.executeContainerMining(t.Context(), f.deviceID, f.orgID, containerMiningPayload{
				OrgID: f.orgID, Request: sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)},
			}, miner)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			require.NotContains(t, err.Error(), "private")
			if upgrade {
				require.True(t, devicewrite.IsNotDispatched(err))
				require.ErrorContains(t, err, "upgraded Fleet Node")
				require.NotContains(t, err.Error(), "unconfirmed")
			} else {
				require.ErrorContains(t, err, "outcome unconfirmed")
			}
		})
	}
}

func TestContainerMiningExecutionPreservesPreDispatchErrorClassification(t *testing.T) {
	f := setupContainerMiningSiteTest(t)
	miner := &containerMiningMiner{Miner: f.miner, err: devicewrite.NotDispatched(errors.New("private routing detail"))}
	err := f.execution.executeContainerMining(t.Context(), f.deviceID, f.orgID, containerMiningPayload{
		OrgID: f.orgID, Request: sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)},
	}, miner)
	require.True(t, devicewrite.IsNotDispatched(err))
	require.ErrorContains(t, err, "preparation failed")
	require.NotContains(t, err.Error(), "private")
	require.NotContains(t, err.Error(), "unconfirmed")
}

type containerMiningMiner struct {
	interfaces.Miner
	got sdk.ContainerMiningRequest
	err error
}

func (m *containerMiningMiner) ContainerMining(_ context.Context, req sdk.ContainerMiningRequest) (sdk.ContainerMiningResult, error) {
	m.got = req
	return sdk.ContainerMiningResult{}, m.err
}
func TestContainerMiningExecutionMakesUncertainOutcomesTerminal(t *testing.T) {
	for _, outcome := range []error{nil, context.DeadlineExceeded, errors.New("node disconnected"), errors.New("PLC rejected")} {
		f := setupContainerMiningSiteTest(t)
		f.move(t, f.siteA)
		miner := &containerMiningMiner{Miner: f.miner, err: outcome}
		f.execution.minerService = containerMiningSiteGetter{miner: miner}
		message := f.enqueue(t, sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}, f.siteA)
		_, _, err := f.execution.executeQueuedCommand(t.Context(), message)
		if outcome == nil {
			require.NoError(t, err)
		} else {
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
		}
		require.NotNil(t, miner.got.Mining)
		require.False(t, *miner.got.Mining)
	}
}

func TestContainerMiningExecutionRejectsInvalidQueuedScope(t *testing.T) {
	for _, name := range []string{"missing_scope", "foreign_organization", "read_operation", "missing_mining_state"} {
		t.Run(name, func(t *testing.T) {
			f := setupContainerMiningSiteTest(t)
			f.move(t, f.siteA)
			request := sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}
			message := f.enqueue(t, request, f.siteA)
			payload := containerMiningPayload{Request: request, OrgID: f.orgID, SiteID: f.siteA}
			switch name {
			case "missing_scope":
				payload.OrgID = 0
			case "foreign_organization":
				payload.OrgID++
			case "read_operation":
				payload.Request = sdk.ContainerMiningRequest{Action: "status"}
			case "missing_mining_state":
				payload.Request.Mining = nil
			}
			var err error
			message.Payload, err = json.Marshal(payload)
			require.NoError(t, err)
			_, _, err = f.execution.executeQueuedCommand(t.Context(), message)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			require.Zero(t, f.miner.calls)
		})
	}
}

func TestPendingActivationContainerStop(t *testing.T) {
	for _, mining := range []bool{false, true} {
		t.Run(fmt.Sprint(mining), func(t *testing.T) {
			f := setupContainerMiningSiteTest(t)
			_, err := f.conn.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'activate','complete')`, f.deviceID, f.orgID)
			require.NoError(t, err)
			_, err = f.conn.ExecContext(t.Context(), `WITH request AS (INSERT INTO mining_group_request(organization_id,request_id,device_id,fingerprint) VALUES($1,gen_random_uuid(),$2,'test') RETURNING *) INSERT INTO mining_group_pool_handoff(organization_id,operation_id,device_id,captured,placement_pending) SELECT organization_id,request_id,device_id,'{}',true FROM request`, f.orgID, f.deviceID)
			require.NoError(t, err)
			message := f.enqueue(t, sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(mining)}, 0)
			_, _, err = f.execution.executeQueuedCommand(t.Context(), message)
			if mining {
				require.ErrorContains(t, err, devicewrite.ActivationPoolPlacementPending)
				require.Zero(t, f.miner.calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, f.miner.calls)
				require.False(t, *f.miner.request.Mining)
			}
		})
	}
}
