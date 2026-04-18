package mappers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	sdkv1 "github.com/block/proto-fleet/server/sdk/v1"
	sdkv1errors "github.com/block/proto-fleet/server/sdk/v1/errors"
)

func TestSDKDeviceErrorsToFleetDeviceErrors(t *testing.T) {
	now := time.Now()
	componentID := "hashboard-0"
	fanComponentID := "2"

	sdkErrors := sdkv1.DeviceErrors{
		DeviceID: "device-123",
		Errors: []sdkv1.DeviceError{
			{
				MinerError:        3001, // HashboardOverTemperature
				CauseSummary:      "Hashboard temperature exceeded threshold",
				RecommendedAction: "Check cooling system",
				Severity:          2, // Major
				FirstSeenAt:       now.Add(-time.Hour),
				LastSeenAt:        now,
				VendorAttributes: map[string]string{
					"vendor_code": "E001",
					"firmware":    "v1.2.3",
				},
				DeviceID:      "device-123",
				ComponentID:   &componentID,
				ComponentType: sdkv1errors.ComponentTypeHashBoard, // SDK value 2
				Impact:        "Reduced hashrate",
				Summary:       "Temperature warning on hashboard 0",
			},
			{
				MinerError:        2000, // FanFailed
				CauseSummary:      "Fan stopped spinning",
				RecommendedAction: "Replace fan",
				Severity:          1, // Critical
				FirstSeenAt:       now.Add(-2 * time.Hour),
				LastSeenAt:        now,
				VendorAttributes:  map[string]string{},
				DeviceID:          "device-123",
				ComponentID:       &fanComponentID,
				ComponentType:     sdkv1errors.ComponentTypeFan, // SDK value 3
				Impact:            "Device may overheat",
				Summary:           "Fan failure detected",
			},
			{
				MinerError:        1000, // PSU issue
				CauseSummary:      "PSU not responding",
				RecommendedAction: "Check PSU connection",
				Severity:          1, // Critical
				FirstSeenAt:       now.Add(-30 * time.Minute),
				LastSeenAt:        now,
				VendorAttributes:  map[string]string{},
				DeviceID:          "device-123",
				ComponentID:       nil,
				ComponentType:     sdkv1errors.ComponentTypePSU, // SDK value 1
				Impact:            "No power",
				Summary:           "PSU failure",
			},
		},
	}

	result := SDKDeviceErrorsToFleetDeviceErrors(sdkErrors)

	assert.Equal(t, "device-123", result.DeviceID)
	require.Len(t, result.Errors, 3)

	// First error - hashboard over temp
	err0 := result.Errors[0]
	assert.Equal(t, models.MinerError(3001), err0.MinerError)
	assert.Equal(t, models.Severity(2), err0.Severity)
	assert.Equal(t, "E001", err0.VendorCode)
	assert.Equal(t, "v1.2.3", err0.Firmware)
	assert.Empty(t, err0.ErrorID)
	require.NotNil(t, err0.ComponentID)
	assert.Equal(t, "hashboard-0", *err0.ComponentID)
	assert.Equal(t, models.ComponentTypeHashBoards, err0.ComponentType, "HashBoard type should map to HashBoards")

	// Second error - fan failed
	err1 := result.Errors[1]
	assert.Equal(t, models.MinerError(2000), err1.MinerError)
	assert.Equal(t, models.Severity(1), err1.Severity)
	assert.Empty(t, err1.VendorCode)
	assert.Empty(t, err1.Firmware)
	require.NotNil(t, err1.ComponentID)
	assert.Equal(t, "2", *err1.ComponentID)
	assert.Equal(t, models.ComponentTypeFans, err1.ComponentType, "Fan type should map to Fans")

	// Third error - PSU issue
	err2 := result.Errors[2]
	assert.Equal(t, models.MinerError(1000), err2.MinerError)
	assert.Equal(t, models.Severity(1), err2.Severity)
	assert.Nil(t, err2.ComponentID)
	assert.Equal(t, models.ComponentTypePSU, err2.ComponentType, "PSU type should map correctly")
}

// TestSDKDeviceErrorToFleetErrorMessage_NormalizesUnspecifiedSeverity verifies that
// SeverityUnspecified (0) is normalized to the error code's DefaultSeverity.
// Related: #1607.
func TestSDKDeviceErrorToFleetErrorMessage_NormalizesUnspecifiedSeverity(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name             string
		sdkMinerError    sdkv1errors.MinerError
		sdkSeverity      sdkv1errors.Severity
		expectedSeverity models.Severity
	}{
		{
			name:             "unspecified severity with known error code uses DefaultSeverity",
			sdkMinerError:    2000, // FanFailed — DefaultSeverity = Critical
			sdkSeverity:      0,    // UNSPECIFIED
			expectedSeverity: models.SeverityCritical,
		},
		{
			name:             "unspecified severity with hashboard error uses DefaultSeverity",
			sdkMinerError:    3001, // HashboardOverTemperature — DefaultSeverity = Critical
			sdkSeverity:      0,    // UNSPECIFIED
			expectedSeverity: models.SeverityCritical,
		},
		{
			name:             "unspecified severity with unknown error code falls back to Info",
			sdkMinerError:    0, // MinerErrorUnspecified
			sdkSeverity:      0, // UNSPECIFIED
			expectedSeverity: models.SeverityInfo,
		},
		{
			name:             "negative severity (invalid) with known code uses DefaultSeverity",
			sdkMinerError:    2000, // FanFailed
			sdkSeverity:      -1,
			expectedSeverity: models.SeverityCritical,
		},
		{
			name:             "explicit critical severity passes through unchanged",
			sdkMinerError:    2000, // FanFailed
			sdkSeverity:      1,    // Critical
			expectedSeverity: models.SeverityCritical,
		},
		{
			name:             "explicit info severity passes through unchanged",
			sdkMinerError:    2000, // FanFailed — DefaultSeverity = Critical, but explicit Info wins
			sdkSeverity:      4,    // Info
			expectedSeverity: models.SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdkError := sdkv1.DeviceError{
				DeviceID:    "device-123",
				MinerError:  tt.sdkMinerError,
				Severity:    tt.sdkSeverity,
				FirstSeenAt: now,
				LastSeenAt:  now,
			}

			result := SDKDeviceErrorToFleetErrorMessage(sdkError)

			assert.Equal(t, tt.expectedSeverity, result.Severity)
		})
	}
}

func TestSDKMinerErrorToFleetMinerError(t *testing.T) {
	tests := []struct {
		name     string
		input    sdkv1errors.MinerError
		expected models.MinerError
	}{
		{"unspecified", 0, models.MinerErrorUnspecified},
		{"psu_not_present", 1000, models.PSUNotPresent},
		{"hashboard_over_temp", 3001, models.HashboardOverTemperature},
		{"negative_returns_unspecified", -1, models.MinerErrorUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SDKMinerErrorToFleetMinerError(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSDKSeverityToFleetSeverity(t *testing.T) {
	tests := []struct {
		name     string
		input    sdkv1errors.Severity
		expected models.Severity
	}{
		{"unspecified", 0, models.SeverityUnspecified},
		{"critical", 1, models.SeverityCritical},
		{"major", 2, models.SeverityMajor},
		{"negative_returns_unspecified", -1, models.SeverityUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SDKSeverityToFleetSeverity(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSDKComponentTypeToFleetComponentType(t *testing.T) {
	tests := []struct {
		name     string
		input    sdkv1errors.ComponentType
		expected models.ComponentType
	}{
		{
			name:     "unspecified_maps_to_unspecified",
			input:    sdkv1errors.ComponentTypeUnspecified,
			expected: models.ComponentTypeUnspecified,
		},
		{
			name:     "psu_maps_correctly",
			input:    sdkv1errors.ComponentTypePSU,
			expected: models.ComponentTypePSU,
		},
		{
			name:     "hashboard_maps_to_hashboards",
			input:    sdkv1errors.ComponentTypeHashBoard,
			expected: models.ComponentTypeHashBoards,
		},
		{
			name:     "fan_maps_to_fans",
			input:    sdkv1errors.ComponentTypeFan,
			expected: models.ComponentTypeFans,
		},
		{
			name:     "control_board_maps_correctly",
			input:    sdkv1errors.ComponentTypeControlBoard,
			expected: models.ComponentTypeControlBoard,
		},
		{
			name:     "eeprom_maps_to_unspecified",
			input:    sdkv1errors.ComponentTypeEEPROM,
			expected: models.ComponentTypeUnspecified,
		},
		{
			name:     "io_module_maps_to_unspecified",
			input:    sdkv1errors.ComponentTypeIOModule,
			expected: models.ComponentTypeUnspecified,
		},
		{
			name:     "negative_returns_unspecified",
			input:    -1,
			expected: models.ComponentTypeUnspecified,
		},
		{
			name:     "unknown_high_value_returns_unspecified",
			input:    999,
			expected: models.ComponentTypeUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SDKComponentTypeToFleetComponentType(tt.input)
			assert.Equal(t, tt.expected, result, "ComponentType mapping failed for %v", tt.input)
		})
	}
}
