package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

const (
	// Test fixture timestamps for error timestamp validation tests
	testErrorFirstSeenTimestamp = 1609459200 // 2021-01-01 00:00:00 UTC
	testErrorLastSeenTimestamp  = 1609459300 // 2021-01-01 00:01:40 UTC
)

// newTestService creates a diagnostics service for testing with the given mock error store.
// Uses context.Background() for the closer goroutine which will be cleaned up when tests complete.
func newTestService(ctrl *gomock.Controller, mockErrorStore *storeMocks.MockErrorStore) *Service {
	mockTransactor := storeMocks.NewMockTransactor(ctrl)
	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		},
	).AnyTimes()
	return NewService(context.Background(), Config{}, mockErrorStore, mockTransactor)
}

func TestPollErrors_WithNoMiners_ShouldReturnEmptyResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)

	svc := newTestService(ctrl, mockErrorStore)

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
		assert.Equal(t, models.ComponentTypeHashBoards, errMsg.ComponentType, "should default ComponentType from MinerError")
		return errMsg, nil
	})

	svc := newTestService(ctrl, mockErrorStore)
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

	svc := newTestService(ctrl, mockErrorStore)
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

	svc := newTestService(ctrl, mockErrorStore)
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

	svc := newTestService(ctrl, mockErrorStore)
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

	svc := newTestService(ctrl, mockErrorStore)
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

	svc := newTestService(ctrl, mockErrorStore)
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

	svc := newTestService(ctrl, mockErrorStore)
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

	svc := newTestService(ctrl, mockErrorStore)
	result := svc.PollErrors(ctx, mockMiner)

	assert.True(t, result.Cancelled)
	assert.Equal(t, 0, result.MinersProcessed)
	assert.Equal(t, 0, result.MinersFailed)
}

// ============================================================================
// Query Tests
// ============================================================================

func TestQuery_WithNoFilters_ShouldReturnAllOpenErrors(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR1", Severity: models.SeverityCritical, LastSeenAt: now, DeviceID: "123"},
		{ErrorID: "ERR2", Severity: models.SeverityMajor, LastSeenAt: now, DeviceID: "456"},
	}

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)
	mockErrorStore.EXPECT().CountErrors(gomock.Any(), gomock.Any()).Return(int64(2), nil)

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID: 1,
	}

	result, err := svc.Query(t.Context(), opts)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Errors, 2)
	assert.Equal(t, int64(2), result.TotalCount)
}

func TestQuery_WithORLogic_ShouldUseORQuery(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR1", Severity: models.SeverityCritical, LastSeenAt: now, DeviceID: "123"},
	}

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)
	mockErrorStore.EXPECT().CountErrors(gomock.Any(), gomock.Any()).Return(int64(1), nil)

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID: 1,
		Filter: &models.QueryFilter{
			Logic: models.FilterLogicOR,
		},
	}

	result, err := svc.Query(t.Context(), opts)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Errors, 1)
}

func TestQuery_WithComponentView_ShouldGroupByComponent(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	hb0 := "HB0"
	// Use realistic device identifier (not a numeric string)
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR1", Severity: models.SeverityCritical, LastSeenAt: now, DeviceID: "proto-123", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19"},
		{ErrorID: "ERR2", Severity: models.SeverityMajor, LastSeenAt: now, DeviceID: "proto-123", ComponentID: &hb0, ComponentType: models.ComponentTypeHashBoards, DeviceType: "S19"},
	}
	mockComponentKeys := []models.ComponentKey{
		{DeviceID: 123, DeviceIdentifier: "proto-123", ComponentType: models.ComponentTypeHashBoards, ComponentID: &hb0, WorstSeverity: models.SeverityCritical},
	}

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	// Two-query approach: first get component keys, then count, then fetch errors
	mockErrorStore.EXPECT().QueryComponentKeys(gomock.Any(), gomock.Any()).Return(mockComponentKeys, nil)
	mockErrorStore.EXPECT().CountComponents(gomock.Any(), gomock.Any()).Return(int64(1), nil)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID:      1,
		ResultView: models.ResultViewComponent,
	}

	result, err := svc.Query(t.Context(), opts)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Len(t, result.ComponentErrs, 1)
	assert.Equal(t, "HB0", result.ComponentErrs[0].ComponentID)
	assert.Equal(t, int64(1), result.TotalCount)
	// CRITICAL: Verify errors are actually populated in the component group
	assert.Len(t, result.ComponentErrs[0].Errors, 2, "errors should be populated in component group")
}

func TestQuery_WithDeviceView_ShouldGroupByDevice(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	// Use realistic device identifiers (not numeric strings)
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR1", Severity: models.SeverityCritical, LastSeenAt: now, DeviceID: "proto-123", DeviceType: "S19"},
		{ErrorID: "ERR2", Severity: models.SeverityMajor, LastSeenAt: now, DeviceID: "proto-123", DeviceType: "S19"},
		{ErrorID: "ERR3", Severity: models.SeverityMinor, LastSeenAt: now, DeviceID: "antminer-456", DeviceType: "R2"},
	}
	mockDeviceKeys := []models.DeviceKey{
		{DeviceID: 123, DeviceIdentifier: "proto-123", WorstSeverity: models.SeverityCritical},
		{DeviceID: 456, DeviceIdentifier: "antminer-456", WorstSeverity: models.SeverityMinor},
	}

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	// Two-query approach: first get device keys, then count, then fetch errors
	mockErrorStore.EXPECT().QueryDeviceKeys(gomock.Any(), gomock.Any()).Return(mockDeviceKeys, nil)
	mockErrorStore.EXPECT().CountDevices(gomock.Any(), gomock.Any()).Return(int64(2), nil)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID:      1,
		ResultView: models.ResultViewDevice,
	}

	result, err := svc.Query(t.Context(), opts)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Len(t, result.DeviceErrs, 2)
	assert.Equal(t, int64(2), result.TotalCount)
	// CRITICAL: Verify errors are actually populated in each device group
	// Find the device group for proto-123 (should have 2 errors)
	var protoDevice *models.DeviceErrorGroup
	var antminerDevice *models.DeviceErrorGroup
	for i := range result.DeviceErrs {
		if result.DeviceErrs[i].DeviceID == 123 {
			protoDevice = &result.DeviceErrs[i]
		} else if result.DeviceErrs[i].DeviceID == 456 {
			antminerDevice = &result.DeviceErrs[i]
		}
	}
	assert.NotNil(t, protoDevice, "proto-123 device group should exist")
	assert.NotNil(t, antminerDevice, "antminer-456 device group should exist")
	assert.Len(t, protoDevice.Errors, 2, "proto-123 should have 2 errors")
	assert.Len(t, antminerDevice.Errors, 1, "antminer-456 should have 1 error")
}

func TestQuery_WhenQueryFails_ShouldReturnError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(nil, errors.New("database error"))

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID: 1,
	}

	result, err := svc.Query(t.Context(), opts)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestQuery_WhenCountFails_ShouldReturnError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return([]models.ErrorMessage{}, nil)
	mockErrorStore.EXPECT().CountErrors(gomock.Any(), gomock.Any()).Return(int64(0), errors.New("count error"))

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID: 1,
	}

	result, err := svc.Query(t.Context(), opts)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestQuery_WithNilOpts_ShouldUseDefaults(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return([]models.ErrorMessage{}, nil)
	mockErrorStore.EXPECT().CountErrors(gomock.Any(), gomock.Any()).Return(int64(0), nil)

	svc := newTestService(ctrl, mockErrorStore)

	result, err := svc.Query(t.Context(), nil)

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestQuery_WithFullPage_ShouldReturnNextPageToken(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	// Create exactly DefaultPageSize errors to indicate a full page
	mockErrors := make([]models.ErrorMessage, DefaultPageSize)
	for i := range mockErrors {
		mockErrors[i] = models.ErrorMessage{
			ErrorID:    fmt.Sprintf("ERR_%d", i),
			Severity:   models.SeverityMajor,
			LastSeenAt: now.Add(time.Duration(-i) * time.Minute),
			DeviceID:   "123",
		}
	}

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)
	mockErrorStore.EXPECT().CountErrors(gomock.Any(), gomock.Any()).Return(int64(100), nil)

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID: 1,
	}

	result, err := svc.Query(t.Context(), opts)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.NextPageToken)
}

func TestQuery_WithPartialPage_ShouldNotReturnNextPageToken(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR1", Severity: models.SeverityCritical, LastSeenAt: now, DeviceID: "123"},
	}

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)
	mockErrorStore.EXPECT().CountErrors(gomock.Any(), gomock.Any()).Return(int64(1), nil)

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID: 1,
	}

	result, err := svc.Query(t.Context(), opts)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.NextPageToken)
}

// ============================================================================
// GetError Tests
// ============================================================================

func TestGetError_WithValidErrorID_ShouldReturnError(t *testing.T) {
	ctrl := gomock.NewController(t)

	now := time.Now()
	expectedError := &models.ErrorMessage{
		ErrorID:    "01HQXYZ123ABC",
		MinerError: models.PSUFaultGeneric,
		Severity:   models.SeverityCritical,
		Summary:    "PSU fault detected",
		LastSeenAt: now,
	}

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().GetErrorByErrorID(gomock.Any(), int64(1), "01HQXYZ123ABC").Return(expectedError, nil)

	svc := newTestService(ctrl, mockErrorStore)

	result, err := svc.GetError(t.Context(), 1, "01HQXYZ123ABC")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "01HQXYZ123ABC", result.ErrorID)
	assert.Equal(t, models.PSUFaultGeneric, result.MinerError)
}

func TestGetError_WithNotFoundError_ShouldReturnError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	mockErrorStore.EXPECT().GetErrorByErrorID(gomock.Any(), int64(1), "NONEXISTENT").Return(nil, errors.New("not found"))

	svc := newTestService(ctrl, mockErrorStore)

	result, err := svc.GetError(t.Context(), 1, "NONEXISTENT")

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestQuery_WithInvalidPageToken_ShouldReturnError(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	// No calls expected since we should error before querying

	svc := newTestService(ctrl, mockErrorStore)
	opts := &models.QueryOptions{
		OrgID:     1,
		PageToken: "invalid-not-base64!!!", // Invalid cursor token
	}

	result, err := svc.Query(t.Context(), opts)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid page token")
}

func TestUpsertErrors_WithZeroLastSeenAt_ShouldSetToCurrentTime(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)

	testDeviceID := "test-device-123"
	orgID := int64(1)

	// Create an error with zero LastSeenAt (for backwards compatibility if any plugin doesn't set it)
	errorWithZeroLastSeen := models.ErrorMessage{
		MinerError:  models.HashboardOverTemperature,
		Severity:    models.SeverityMajor,
		Summary:     "Error with zero LastSeenAt",
		FirstSeenAt: time.Unix(testErrorFirstSeenTimestamp, 0),
		LastSeenAt:  time.Time{}, // Zero value
	}

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier(testDeviceID)).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(orgID).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: testDeviceID,
		Errors:   []models.ErrorMessage{errorWithZeroLastSeen},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	capturedError := &models.ErrorMessage{}
	mockErrorStore.EXPECT().UpsertError(
		gomock.Any(),
		orgID,
		testDeviceID,
		gomock.Any(),
	).DoAndReturn(func(_ context.Context, _ int64, _ string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
		// Capture the error that was passed to UpsertError
		*capturedError = *errMsg
		return errMsg, nil
	})

	beforeCall := time.Now()
	svc := newTestService(ctrl, mockErrorStore)
	result := svc.PollErrors(t.Context(), mockMiner)
	afterCall := time.Now()

	// Verify the poll succeeded
	assert.Equal(t, 1, result.ErrorsUpserted)
	assert.Equal(t, 0, result.UpsertsFailed)

	// Verify FirstSeenAt was preserved
	assert.Equal(t, time.Unix(testErrorFirstSeenTimestamp, 0), capturedError.FirstSeenAt)

	// Verify LastSeenAt was set to current time (not zero)
	assert.False(t, capturedError.LastSeenAt.IsZero(), "LastSeenAt should not be zero")
	assert.True(t, capturedError.LastSeenAt.After(beforeCall) || capturedError.LastSeenAt.Equal(beforeCall),
		"LastSeenAt should be at or after the call time")
	assert.True(t, capturedError.LastSeenAt.Before(afterCall) || capturedError.LastSeenAt.Equal(afterCall),
		"LastSeenAt should be at or before the call completion")
}

func TestUpsertErrors_WithNonZeroLastSeenAt_ShouldPreserveIt(t *testing.T) {
	ctrl := gomock.NewController(t)

	testDeviceID := "test-device-123"
	orgID := int64(1)
	providedLastSeenAt := time.Unix(testErrorLastSeenTimestamp, 0)

	// Create an error with non-zero LastSeenAt (like Antminer plugin would provide)
	errorWithLastSeen := models.ErrorMessage{
		MinerError:  models.PSUNotPresent,
		Severity:    models.SeverityCritical,
		Summary:     "Antminer error with LastSeenAt",
		FirstSeenAt: time.Unix(testErrorFirstSeenTimestamp, 0),
		LastSeenAt:  providedLastSeenAt,
	}

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier(testDeviceID)).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(orgID).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: testDeviceID,
		Errors:   []models.ErrorMessage{errorWithLastSeen},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)
	capturedError := &models.ErrorMessage{}
	mockErrorStore.EXPECT().UpsertError(
		gomock.Any(),
		orgID,
		testDeviceID,
		gomock.Any(),
	).DoAndReturn(func(_ context.Context, _ int64, _ string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
		// Capture the error that was passed to UpsertError
		*capturedError = *errMsg
		return errMsg, nil
	})

	svc := newTestService(ctrl, mockErrorStore)
	result := svc.PollErrors(t.Context(), mockMiner)

	// Verify the poll succeeded
	assert.Equal(t, 1, result.ErrorsUpserted)
	assert.Equal(t, 0, result.UpsertsFailed)

	// Verify both timestamps were preserved as provided
	assert.Equal(t, time.Unix(testErrorFirstSeenTimestamp, 0), capturedError.FirstSeenAt)
	assert.Equal(t, providedLastSeenAt, capturedError.LastSeenAt,
		"LastSeenAt should be preserved when provided by plugin")
}
