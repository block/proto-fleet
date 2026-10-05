package command

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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

type equipmentMiner struct {
	interfaces.Miner
	got sdk.ContainerControlRequest
	err error
}

func (m *equipmentMiner) SetContainerControl(_ context.Context, req sdk.ContainerControlRequest) error {
	m.got = req
	return m.err
}
func TestEquipmentExecutionMakesUncertainOutcomesTerminal(t *testing.T) {
	for _, outcome := range []error{nil, context.DeadlineExceeded, errors.New("node disconnected"), errors.New("PLC rejected")} {
		ctrl := gomock.NewController(t)
		base := minerIfaceMocks.NewMockMiner(ctrl)
		base.EXPECT().GetOrgID().Return(int64(12))
		base.EXPECT().GetSiteID().Return(int64(34))
		miner := &equipmentMiner{Miner: base, err: outcome}
		getter := minerMocks.NewMockCachedMinerGetter(ctrl)
		getter.EXPECT().GetMiner(gomock.Any(), int64(54)).Return(miner, nil)
		svc := NewExecutionService(&Config{MaxWorkers: 1}, nil, nil, nil, nil, getter, nil, nil, nil)
		payload, err := json.Marshal(sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(false)})
		require.NoError(t, err)
		_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.SetContainerControl, queue.Message{DeviceID: 54, Payload: payload})
		if outcome == nil {
			require.NoError(t, err)
		} else {
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
		}
		require.NotNil(t, miner.got.Enabled)
		require.False(t, *miner.got.Enabled)
	}
}
