package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	minerMocks "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	minerModels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	storeMocks "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

func TestPollErrors_WithNoMiners_ShouldReturnEmptyResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)

	svc := NewService(mockErrorStore)

	result := svc.PollErrors(t.Context())

	assert.Equal(t, PollResult{}, result)
}

func TestPollErrors_WithSingleMiner_ShouldUpsertErrors(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	testDeviceID := "test-device-123"

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier(testDeviceID)).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: testDeviceID,
		Errors: []models.ErrorMessage{
			{
				MinerError:  models.HashboardOverTemperature,
				Severity:    models.SeverityMajor,
				Summary:     "Test error",
				FirstSeenAt: now,
				LastSeenAt:  now,
			},
		},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().UpsertError(
		gomock.Any(),
		int64(1),
		testDeviceID,
		gomock.Any(),
	).DoAndReturn(func(_ context.Context, _ int64, _ string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
		assert.Equal(t, models.HashboardOverTemperature, errMsg.MinerError)
		assert.Equal(t, models.SeverityMajor, errMsg.Severity)
		assert.Equal(t, "Test error", errMsg.Summary)
		return errMsg, nil
	})

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(t.Context(), mockMiner)

	assert.Equal(t, 1, result.MinersProcessed)
	assert.Equal(t, 0, result.MinersFailed)
	assert.Equal(t, 1, result.ErrorsUpserted)
	assert.Equal(t, 0, result.UpsertsFailed)
	assert.False(t, result.Cancelled)
}

func TestPollErrors_WithMultipleMiners_ShouldProcessAll(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()

	mockMiner1 := minerMocks.NewMockMiner(ctrl)
	mockMiner1.EXPECT().GetID().Return(minerModels.DeviceIdentifier("device-1")).AnyTimes()
	mockMiner1.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner1.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "device-1",
		Errors: []models.ErrorMessage{
			{MinerError: models.PSUNotPresent, Severity: models.SeverityCritical, FirstSeenAt: now, LastSeenAt: now},
		},
	}, nil)

	mockMiner2 := minerMocks.NewMockMiner(ctrl)
	mockMiner2.EXPECT().GetID().Return(minerModels.DeviceIdentifier("device-2")).AnyTimes()
	mockMiner2.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner2.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "device-2",
		Errors: []models.ErrorMessage{
			{MinerError: models.FanFailed, Severity: models.SeverityMajor, FirstSeenAt: now, LastSeenAt: now},
		},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "device-1", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ int64, _ string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
			assert.Equal(t, models.PSUNotPresent, errMsg.MinerError)
			assert.Equal(t, models.SeverityCritical, errMsg.Severity)
			return errMsg, nil
		})
	mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "device-2", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ int64, _ string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
			assert.Equal(t, models.FanFailed, errMsg.MinerError)
			assert.Equal(t, models.SeverityMajor, errMsg.Severity)
			return errMsg, nil
		})

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(t.Context(), mockMiner1, mockMiner2)

	assert.Equal(t, 2, result.MinersProcessed)
	assert.Equal(t, 2, result.ErrorsUpserted)
	assert.False(t, result.Cancelled)
}

func TestPollErrors_WhenMinerGetErrorsFails_ShouldContinueToNextMiner(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()

	failingMiner := minerMocks.NewMockMiner(ctrl)
	failingMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier("failing-device")).AnyTimes()
	failingMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	failingMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{}, errors.New("connection error"))

	successMiner := minerMocks.NewMockMiner(ctrl)
	successMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier("success-device")).AnyTimes()
	successMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	successMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "success-device",
		Errors: []models.ErrorMessage{
			{MinerError: models.HashboardOverTemperature, Severity: models.SeverityMinor, FirstSeenAt: now, LastSeenAt: now},
		},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "success-device", gomock.Any()).Return(&models.ErrorMessage{}, nil)

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(t.Context(), failingMiner, successMiner)

	assert.Equal(t, 1, result.MinersProcessed)
	assert.Equal(t, 1, result.MinersFailed)
	assert.Equal(t, 1, result.ErrorsUpserted)
	assert.False(t, result.Cancelled)
}

func TestPollErrors_WhenUpsertFails_ShouldContinueToNextError(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier("test-device")).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "test-device",
		Errors: []models.ErrorMessage{
			{MinerError: models.PSUFaultGeneric, Severity: models.SeverityCritical, FirstSeenAt: now, LastSeenAt: now},
			{MinerError: models.FanFailed, Severity: models.SeverityMajor, FirstSeenAt: now, LastSeenAt: now},
			{MinerError: models.HashboardOverTemperature, Severity: models.SeverityMinor, FirstSeenAt: now, LastSeenAt: now},
		},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	gomock.InOrder(
		mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "test-device", gomock.Any()).Return(nil, errors.New("db error")),
		mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "test-device", gomock.Any()).Return(&models.ErrorMessage{}, nil),
		mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "test-device", gomock.Any()).Return(&models.ErrorMessage{}, nil),
	)

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(t.Context(), mockMiner)

	assert.Equal(t, 1, result.MinersProcessed)
	assert.Equal(t, 2, result.ErrorsUpserted)
	assert.Equal(t, 1, result.UpsertsFailed)
	assert.False(t, result.Cancelled)
}

func TestPollErrors_WithMinerReturningNoErrors_ShouldSkipUpsert(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier("no-errors-device")).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "no-errors-device",
		Errors:   []models.ErrorMessage{},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(t.Context(), mockMiner)

	assert.Equal(t, 1, result.MinersProcessed)
	assert.Equal(t, 0, result.ErrorsUpserted)
	assert.False(t, result.Cancelled)
}

func TestPollErrors_WithMultipleErrorsFromSingleMiner_ShouldUpsertAll(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier("multi-error-device")).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "multi-error-device",
		Errors: []models.ErrorMessage{
			{MinerError: models.PSUFaultGeneric, Severity: models.SeverityCritical, FirstSeenAt: now, LastSeenAt: now},
			{MinerError: models.FanFailed, Severity: models.SeverityMajor, FirstSeenAt: now, LastSeenAt: now},
			{MinerError: models.HashboardOverTemperature, Severity: models.SeverityMinor, FirstSeenAt: now, LastSeenAt: now},
		},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "multi-error-device", gomock.Any()).Times(3).Return(&models.ErrorMessage{}, nil)

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(t.Context(), mockMiner)

	assert.Equal(t, 1, result.MinersProcessed)
	assert.Equal(t, 3, result.ErrorsUpserted)
	assert.False(t, result.Cancelled)
}

func TestPollErrors_WithMixedMinerResults_ShouldHandleGracefully(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()

	miner1 := minerMocks.NewMockMiner(ctrl)
	miner1.EXPECT().GetID().Return(minerModels.DeviceIdentifier("device-1")).AnyTimes()
	miner1.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	miner1.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "device-1",
		Errors:   []models.ErrorMessage{{MinerError: models.PSUNotPresent, Severity: models.SeverityCritical, FirstSeenAt: now, LastSeenAt: now}},
	}, nil)

	miner2 := minerMocks.NewMockMiner(ctrl)
	miner2.EXPECT().GetID().Return(minerModels.DeviceIdentifier("device-2")).AnyTimes()
	miner2.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	miner2.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{}, errors.New("network timeout"))

	miner3 := minerMocks.NewMockMiner(ctrl)
	miner3.EXPECT().GetID().Return(minerModels.DeviceIdentifier("device-3")).AnyTimes()
	miner3.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	miner3.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{DeviceID: "device-3", Errors: []models.ErrorMessage{}}, nil)

	miner4 := minerMocks.NewMockMiner(ctrl)
	miner4.EXPECT().GetID().Return(minerModels.DeviceIdentifier("device-4")).AnyTimes()
	miner4.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	miner4.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: "device-4",
		Errors:   []models.ErrorMessage{{MinerError: models.FanFailed, Severity: models.SeverityMajor, FirstSeenAt: now, LastSeenAt: now}},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "device-1", gomock.Any()).Return(&models.ErrorMessage{}, nil)
	mockErrorStore.EXPECT().UpsertError(gomock.Any(), int64(1), "device-4", gomock.Any()).Return(&models.ErrorMessage{}, nil)

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(t.Context(), miner1, miner2, miner3, miner4)

	assert.Equal(t, 3, result.MinersProcessed)
	assert.Equal(t, 1, result.MinersFailed)
	assert.Equal(t, 2, result.ErrorsUpserted)
	assert.False(t, result.Cancelled)
}

func TestPollErrors_WithCancelledContext_ShouldSetCancelledFlag(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := NewService(mockErrorStore)
	result := svc.PollErrors(ctx, mockMiner)

	assert.True(t, result.Cancelled)
	assert.Equal(t, 0, result.MinersProcessed)
	assert.Equal(t, 0, result.MinersFailed)
}
