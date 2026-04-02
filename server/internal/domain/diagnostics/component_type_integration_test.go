package diagnostics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins/mappers"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	sdkv1 "github.com/block/proto-fleet/server/sdk/v1"
	sdkv1errors "github.com/block/proto-fleet/server/sdk/v1/errors"
)

// TestPollErrors_WithSDKErrorsHavingComponentTypes_ShouldPreserveComponentTypeThroughPipeline verifies that ComponentType values are correctly
// preserved through the entire error ingestion pipeline from plugin SDK errors through
// to database storage.
func TestPollErrors_WithSDKErrorsHavingComponentTypes_ShouldPreserveComponentTypeThroughPipeline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	now := time.Now()
	testDeviceID := "test-miner-001"
	hashboardID := "0"
	fanID := "2"
	psuID := "1"
	eepromID := "0"

	// Simulate SDK errors as they would come from a plugin (Proto or Antminer)
	sdkErrors := []sdkv1.DeviceError{
		{
			MinerError:        sdkv1errors.HashboardOverTemperature,
			CauseSummary:      "Hashboard temperature exceeded 95°C",
			RecommendedAction: "Check cooling and airflow",
			Severity:          sdkv1errors.SeverityMajor,
			FirstSeenAt:       now.Add(-time.Hour),
			LastSeenAt:        now,
			DeviceID:          testDeviceID,
			ComponentID:       &hashboardID,
			ComponentType:     sdkv1errors.ComponentTypeHashBoard, // SDK value: 2
			Impact:            "Reduced hashrate by 30%",
			Summary:           "HB0 temperature critical",
		},
		{
			MinerError:        sdkv1errors.FanFailed,
			CauseSummary:      "Fan RPM below threshold",
			RecommendedAction: "Replace fan unit",
			Severity:          sdkv1errors.SeverityCritical,
			FirstSeenAt:       now.Add(-30 * time.Minute),
			LastSeenAt:        now,
			DeviceID:          testDeviceID,
			ComponentID:       &fanID,
			ComponentType:     sdkv1errors.ComponentTypeFan, // SDK value: 3
			Impact:            "Risk of overheating",
			Summary:           "Fan 2 failure",
		},
		{
			MinerError:        sdkv1errors.PSUNotPresent,
			CauseSummary:      "PSU not detected",
			RecommendedAction: "Check PSU connection",
			Severity:          sdkv1errors.SeverityCritical,
			FirstSeenAt:       now.Add(-15 * time.Minute),
			LastSeenAt:        now,
			DeviceID:          testDeviceID,
			ComponentID:       &psuID,
			ComponentType:     sdkv1errors.ComponentTypePSU, // SDK value: 1
			Impact:            "No power to hashboards",
			Summary:           "PSU 1 missing",
		},
		{
			MinerError:        sdkv1errors.MinerError(9999), // Some control board error
			CauseSummary:      "Control board communication error",
			RecommendedAction: "Restart control board",
			Severity:          sdkv1errors.SeverityMinor,
			FirstSeenAt:       now.Add(-5 * time.Minute),
			LastSeenAt:        now,
			DeviceID:          testDeviceID,
			ComponentID:       nil,                                   // Control board doesn't have an index
			ComponentType:     sdkv1errors.ComponentTypeControlBoard, // SDK value: 4
			Impact:            "Limited monitoring capability",
			Summary:           "Control board issue",
		},
		{
			MinerError:        sdkv1errors.MinerError(8888), // EEPROM error (no Fleet equivalent)
			CauseSummary:      "EEPROM read failure",
			RecommendedAction: "Check EEPROM chip",
			Severity:          sdkv1errors.SeverityInfo,
			FirstSeenAt:       now.Add(-2 * time.Minute),
			LastSeenAt:        now,
			DeviceID:          testDeviceID,
			ComponentID:       &eepromID,
			ComponentType:     sdkv1errors.ComponentTypeEEPROM, // SDK value: 5 (maps to Unspecified)
			Impact:            "Configuration may be lost",
			Summary:           "EEPROM error",
		},
	}

	// Convert SDK errors to Fleet errors using the mapper (simulating plugin wrapper behavior)
	fleetErrors := make([]models.ErrorMessage, len(sdkErrors))
	for i, sdkErr := range sdkErrors {
		fleetErrors[i] = mappers.SDKDeviceErrorToFleetErrorMessage(sdkErr)
	}

	// Verify the mapper correctly converted ComponentType values
	t.Run("Mapper_Conversion", func(t *testing.T) {
		// HashBoard (SDK 2) -> HashBoards (Fleet 3)
		assert.Equal(t, models.ComponentTypeHashBoards, fleetErrors[0].ComponentType,
			"HashBoard should map to HashBoards")
		require.NotNil(t, fleetErrors[0].ComponentID)
		assert.Equal(t, "0", *fleetErrors[0].ComponentID)

		// Fan (SDK 3) -> Fans (Fleet 2)
		assert.Equal(t, models.ComponentTypeFans, fleetErrors[1].ComponentType,
			"Fan should map to Fans")
		require.NotNil(t, fleetErrors[1].ComponentID)
		assert.Equal(t, "2", *fleetErrors[1].ComponentID)

		// PSU (SDK 1) -> PSU (Fleet 4)
		assert.Equal(t, models.ComponentTypePSU, fleetErrors[2].ComponentType,
			"PSU should map correctly")
		require.NotNil(t, fleetErrors[2].ComponentID)
		assert.Equal(t, "1", *fleetErrors[2].ComponentID)

		// ControlBoard (SDK 4) -> ControlBoard (Fleet 1)
		assert.Equal(t, models.ComponentTypeControlBoard, fleetErrors[3].ComponentType,
			"ControlBoard should map correctly")
		assert.Nil(t, fleetErrors[3].ComponentID, "Control board should not have component ID")

		// EEPROM (SDK 5) -> Unspecified (Fleet 0) - no Fleet equivalent
		assert.Equal(t, models.ComponentTypeUnspecified, fleetErrors[4].ComponentType,
			"EEPROM should map to Unspecified (no Fleet equivalent)")
	})

	// Mock miner that returns the converted Fleet errors
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier(testDeviceID)).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: testDeviceID,
		Errors:   fleetErrors,
	}, nil)

	// Mock error store that verifies ComponentType is preserved when storing
	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)

	// Set up expectations for each error to be upserted with correct ComponentType
	for i, expectedErr := range fleetErrors {
		capturedErr := expectedErr // Capture for closure
		mockErrorStore.EXPECT().
			UpsertError(gomock.Any(), int64(1), testDeviceID, gomock.Any()).
			DoAndReturn(func(_ context.Context, _ int64, _ string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
				// Verify ComponentType is preserved
				assert.Equal(t, capturedErr.ComponentType, errMsg.ComponentType,
					"ComponentType should be preserved for error %d", i)

				// Verify ComponentID is preserved
				if capturedErr.ComponentID != nil {
					require.NotNil(t, errMsg.ComponentID, "ComponentID should be preserved for error %d", i)
					assert.Equal(t, *capturedErr.ComponentID, *errMsg.ComponentID,
						"ComponentID value should match for error %d", i)
				} else {
					assert.Nil(t, errMsg.ComponentID, "ComponentID should be nil for error %d", i)
				}

				// Verify other critical fields
				assert.Equal(t, capturedErr.MinerError, errMsg.MinerError)
				assert.Equal(t, capturedErr.Severity, errMsg.Severity)

				// Return the error as if it was stored
				errMsg.ErrorID = "ULID-" + string(rune('A'+i)) // Simulate DB-assigned ID
				return errMsg, nil
			})
	}

	// Create service and poll errors
	svc := newTestService(ctrl, mockErrorStore)
	result := svc.PollErrors(context.Background(), mockMiner)

	// Verify the poll results
	t.Run("Poll_Results", func(t *testing.T) {
		assert.Equal(t, 1, result.MinersProcessed, "Should process one miner")
		assert.Equal(t, 0, result.MinersFailed, "Should have no failures")
		assert.Equal(t, len(sdkErrors), result.ErrorsUpserted, "Should upsert all errors")
		assert.Equal(t, 0, result.UpsertsFailed, "Should have no upsert failures")
		assert.False(t, result.Cancelled, "Should not be cancelled")
	})
}

// TestUpsertError_WithSameErrorCodeButDifferentComponentTypes_ShouldTreatAsDistinctErrors verifies that errors with the same MinerError but
// different ComponentType values are treated as distinct errors in deduplication.
func TestUpsertError_WithSameErrorCodeButDifferentComponentTypes_ShouldTreatAsDistinctErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	now := time.Now()
	testDeviceID := "test-miner-002"
	componentZero := "0"

	// Two temperature errors on different component types
	hashboardTempError := models.ErrorMessage{
		MinerError:    models.HashboardOverTemperature,
		ComponentID:   &componentZero,
		ComponentType: models.ComponentTypeHashBoards, // Fleet value: 3
		Severity:      models.SeverityMajor,
		Summary:       "Hashboard 0 overheating",
		FirstSeenAt:   now.Add(-time.Hour),
		LastSeenAt:    now,
	}

	// Same error code but for control board
	controlBoardTempError := models.ErrorMessage{
		MinerError:    models.HashboardOverTemperature, // Same error code
		ComponentID:   nil,
		ComponentType: models.ComponentTypeControlBoard, // Fleet value: 1
		Severity:      models.SeverityMajor,
		Summary:       "Control board overheating",
		FirstSeenAt:   now.Add(-30 * time.Minute),
		LastSeenAt:    now,
	}

	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockMiner.EXPECT().GetID().Return(minerModels.DeviceIdentifier(testDeviceID)).AnyTimes()
	mockMiner.EXPECT().GetOrgID().Return(int64(1)).AnyTimes()
	mockMiner.EXPECT().GetErrors(gomock.Any()).Return(models.DeviceErrors{
		DeviceID: testDeviceID,
		Errors:   []models.ErrorMessage{hashboardTempError, controlBoardTempError},
	}, nil)

	mockErrorStore := storeMocks.NewMockErrorStore(ctrl)

	// Both errors should be upserted separately due to different ComponentType
	mockErrorStore.EXPECT().
		UpsertError(gomock.Any(), int64(1), testDeviceID, gomock.Any()).
		Times(2). // Expect two separate upserts
		DoAndReturn(func(_ context.Context, _ int64, _ string, errMsg *models.ErrorMessage) (*models.ErrorMessage, error) {
			// Verify ComponentType is preserved for deduplication
			assert.Contains(t,
				[]models.ComponentType{models.ComponentTypeHashBoards, models.ComponentTypeControlBoard},
				errMsg.ComponentType,
				"ComponentType should be one of the expected values")
			return errMsg, nil
		})

	svc := newTestService(ctrl, mockErrorStore)
	result := svc.PollErrors(context.Background(), mockMiner)

	assert.Equal(t, 2, result.ErrorsUpserted, "Both errors should be upserted (different ComponentType)")
}
