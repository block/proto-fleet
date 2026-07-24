package command

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/generated/sqlc"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/sv2/translator"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type releaseRecordingTranslatorManager struct {
	assignments []translator.Assignment
	profiles    []*translator.Profile
	endpoint    translator.Endpoint
	onApply     func()
	changed     bool
	err         error
}

func (*releaseRecordingTranslatorManager) PreviewAssignment(
	context.Context,
	*translator.Profile,
	translator.Assignment,
) (translator.Endpoint, error) {
	return "", nil
}

func (m *releaseRecordingTranslatorManager) ApplyAssignment(
	_ context.Context,
	profile *translator.Profile,
	assignment translator.Assignment,
) (translator.Endpoint, bool, error) {
	if m.onApply != nil {
		m.onApply()
	}
	m.profiles = append(m.profiles, profile)
	m.assignments = append(m.assignments, assignment)
	return m.endpoint, m.changed, m.err
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

func TestExecuteCommandOnDevice_UpdateMiningPools_AppliesTranslationAtDispatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	miner := minerIfaceMocks.NewMockMiner(ctrl)
	manager := &releaseRecordingTranslatorManager{
		endpoint: "stratum+tcp://10.0.0.9:34255",
	}
	profile := translator.Profile{Upstreams: []translator.Upstream{{
		URL:      translationTestSV2URL,
		Username: "account",
	}}}
	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			URL:      "stratum+tcp://planned-endpoint:34255",
			Username: "account",
		},
		SV2Translation: &dto.SV2TranslationInstruction{
			Profile:               profile,
			TranslatedPoolIndexes: []int{0},
		},
	})
	require.NoError(t, err)

	mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(41)).Return(miner, nil)
	miner.EXPECT().GetOrgID().Return(int64(7))
	miner.EXPECT().GetSiteID().Return(int64(0))
	miner.EXPECT().GetID().Return(models.DeviceIdentifier("miner-a")).AnyTimes()
	miner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(_ context.Context, payload dto.UpdateMiningPoolsPayload) error {
			require.Len(t, manager.assignments, 1, "translator must be ready before miner dispatch")
			assert.Equal(t, manager.endpoint.String(), payload.DefaultPool.URL)
			assert.Nil(t, payload.SV2Translation)
			assert.False(t, payload.ReleaseSV2Translation)
			return nil
		})

	service := &ExecutionService{
		minerService:      mockMinerGetter,
		translatorManager: manager,
	}
	_, _, err = service.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, queue.Message{
		DeviceID: 41,
		Payload:  payloadBytes,
	})

	require.NoError(t, err)
	require.Len(t, manager.profiles, 1)
	require.NotNil(t, manager.profiles[0])
	assert.True(t, translator.ProfilesEqual(profile, *manager.profiles[0]))
	require.Len(t, manager.assignments, 1)
	assert.Equal(t, []string{"miner-a"}, manager.assignments[0].SelectedDeviceIdentifiers)
	assert.Equal(t, []string{"miner-a"}, manager.assignments[0].TranslatedDeviceIdentifiers)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReconcilesTranslationAfterMinerFailure(t *testing.T) {
	const oldPoolURL = "stratum+tcp://old-pool.example.com:3333"
	endpoint := translator.Endpoint("stratum+tcp://10.0.0.9:34255")
	tests := []struct {
		name              string
		assignmentChanged bool
		currentPoolURL    string
		wantRollback      bool
	}{
		{
			name:              "new assignment is rolled back when miner kept old pool",
			assignmentChanged: true,
			currentPoolURL:    oldPoolURL,
			wantRollback:      true,
		},
		{
			name:              "new assignment is preserved when miner applied update before error",
			assignmentChanged: true,
			currentPoolURL:    endpoint.String(),
		},
		{
			name:           "existing assignment is preserved when miner still uses translator",
			currentPoolURL: endpoint.String(),
		},
		{
			name:           "unconfirmed assignment is rolled back on retry",
			currentPoolURL: oldPoolURL,
			wantRollback:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
			miner := minerIfaceMocks.NewMockMiner(ctrl)
			manager := &releaseRecordingTranslatorManager{
				endpoint: endpoint,
				changed:  test.assignmentChanged,
			}
			profile := translator.Profile{Upstreams: []translator.Upstream{{
				URL:      translationTestSV2URL,
				Username: "account",
			}}}
			payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
				DefaultPool: dto.MiningPool{
					URL:      "stratum+tcp://planned-endpoint:34255",
					Username: "account",
				},
				SV2Translation: &dto.SV2TranslationInstruction{
					Profile:               profile,
					TranslatedPoolIndexes: []int{0},
				},
			})
			require.NoError(t, err)

			mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(41)).Return(miner, nil)
			miner.EXPECT().GetOrgID().Return(int64(7))
			miner.EXPECT().GetSiteID().Return(int64(0))
			miner.EXPECT().GetID().Return(models.DeviceIdentifier("miner-a")).AnyTimes()
			miner.EXPECT().
				UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
				Return(errors.New("miner rejected pool update"))
			miner.EXPECT().GetMiningPools(gomock.Any()).Return([]interfaces.MinerConfiguredPool{{
				URL: test.currentPoolURL,
			}}, nil)

			service := &ExecutionService{
				minerService:      mockMinerGetter,
				translatorManager: manager,
			}
			_, _, err = service.executeCommandOnDevice(
				t.Context(),
				commandtype.UpdateMiningPools,
				queue.Message{DeviceID: 41, Payload: payloadBytes},
			)

			require.Error(t, err)
			if test.wantRollback {
				require.Len(t, manager.assignments, 2)
				assert.Equal(t, []string{"miner-a"}, manager.assignments[1].SelectedDeviceIdentifiers)
				assert.Empty(t, manager.assignments[1].TranslatedDeviceIdentifiers)
				assert.Nil(t, manager.profiles[1])
				return
			}
			require.Len(t, manager.assignments, 1)
			assert.Equal(t, []string{"miner-a"}, manager.assignments[0].TranslatedDeviceIdentifiers)
		})
	}
}

func TestHandleUnpairPostProcessingByIdentifier_ReleasesTranslationFirst(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)
	pairingStatusUpdated := false
	manager := &releaseRecordingTranslatorManager{
		onApply: func() {
			assert.False(t, pairingStatusUpdated, "translator release must precede Fleet cleanup")
		},
	}

	mockDeviceStore.EXPECT().
		UpdateDevicePairingStatusByIdentifier(
			gomock.Any(),
			"miner-a",
			string(sqlc.PairingStatusEnumUNPAIRED),
		).
		DoAndReturn(func(context.Context, string, string) error {
			pairingStatusUpdated = true
			return nil
		})
	mockMinerGetter.EXPECT().InvalidateMiner(models.DeviceIdentifier("miner-a"))

	service := &ExecutionService{
		minerService:      mockMinerGetter,
		deviceStore:       mockDeviceStore,
		translatorManager: manager,
	}

	err := service.handleUnpairPostProcessingByIdentifier(t.Context(), "miner-a")

	require.NoError(t, err)
	require.Len(t, manager.assignments, 1)
	assert.Nil(t, manager.profiles[0])
	assert.Equal(t, []string{"miner-a"}, manager.assignments[0].SelectedDeviceIdentifiers)
	assert.Empty(t, manager.assignments[0].TranslatedDeviceIdentifiers)
}

func TestHandleUnpairPostProcessingByIdentifier_ReturnsTranslatorReleaseFailure(t *testing.T) {
	manager := &releaseRecordingTranslatorManager{
		err: errors.New("persist translator state"),
	}
	service := &ExecutionService{translatorManager: manager}

	err := service.handleUnpairPostProcessingByIdentifier(t.Context(), "miner-a")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to release device from Stratum V2 translator after unpair")
	require.Len(t, manager.assignments, 1)
	assert.Equal(t, []string{"miner-a"}, manager.assignments[0].SelectedDeviceIdentifiers)
}
