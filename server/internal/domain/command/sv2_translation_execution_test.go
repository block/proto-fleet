package command

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/sv2/translator"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type releaseRecordingTranslatorManager struct {
	assignments []translator.Assignment
	onApply     func()
}

func (m *releaseRecordingTranslatorManager) ApplyAssignment(
	_ context.Context,
	_ *translator.Profile,
	assignment translator.Assignment,
) (translator.Endpoint, error) {
	if m.onApply != nil {
		m.onApply()
	}
	m.assignments = append(m.assignments, assignment)
	return "", nil
}

func (*releaseRecordingTranslatorManager) Resume(context.Context) error {
	return nil
}

func (*releaseRecordingTranslatorManager) ActiveProfile() (translator.Profile, translator.Endpoint, bool) {
	return translator.Profile{}, "", false
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReleasesTranslationOnlyAfterSuccessfulUpdates(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	successfulMiner := minerIfaceMocks.NewMockMiner(ctrl)
	failedMiner := minerIfaceMocks.NewMockMiner(ctrl)
	manager := &releaseRecordingTranslatorManager{}
	successfulUpdateFinished := false
	manager.onApply = func() {
		assert.True(t, successfulUpdateFinished, "translator release must follow the miner pool update")
	}

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			URL: "stratum+tcp://pool.example.com:3333",
		},
		ReleaseSV2Translation: true,
	})
	require.NoError(t, err)

	mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(41)).Return(successfulMiner, nil)
	successfulMiner.EXPECT().GetOrgID().Return(int64(7))
	successfulMiner.EXPECT().GetSiteID().Return(int64(0))
	successfulMiner.EXPECT().GetID().Return(models.DeviceIdentifier("miner-success")).AnyTimes()
	successfulMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(context.Context, dto.UpdateMiningPoolsPayload) error {
			successfulUpdateFinished = true
			return nil
		})

	mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(42)).Return(failedMiner, nil)
	failedMiner.EXPECT().GetOrgID().Return(int64(7))
	failedMiner.EXPECT().GetSiteID().Return(int64(0))
	failedMiner.EXPECT().GetID().Return(models.DeviceIdentifier("miner-failed")).AnyTimes()
	failedMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		Return(errors.New("miner rejected pool update"))

	service := &ExecutionService{
		minerService:      mockMinerGetter,
		translatorManager: manager,
	}

	_, _, err = service.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, queue.Message{
		DeviceID: 41,
		Payload:  payloadBytes,
	})
	require.NoError(t, err)

	_, _, err = service.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, queue.Message{
		DeviceID: 42,
		Payload:  payloadBytes,
	})
	require.Error(t, err)

	require.Len(t, manager.assignments, 1)
	assert.Equal(t, []string{"miner-success"}, manager.assignments[0].SelectedDeviceIdentifiers)
	assert.Empty(t, manager.assignments[0].TranslatedDeviceIdentifiers)
}
