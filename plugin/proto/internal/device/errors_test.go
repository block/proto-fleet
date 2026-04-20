package device

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/plugin/proto/pkg/proto"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorValidationType int

const (
	validateSummaries errorValidationType = iota
	validateFullError
)

const (
	testTimestamp1 = 1234567890 // 2009-02-13 23:31:30 UTC
	testTimestamp2 = 1234567891
	testTimestamp3 = 1234567892

	historicalErrorTimestamp = 1609459200 // 2021-01-01 00:00:00 UTC
)

func TestDevice_ConvertErrorsResponse(t *testing.T) {
	tests := []struct {
		name              string
		response          *proto.ErrorsResponse
		expectedCount     int
		validationType    errorValidationType
		expectedSummaries []string
		expectedFullError map[string]any
	}{
		{
			name: "PSU output overvoltage includes slot number",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      2,
						ErrorCode: "PsuOutputOverVoltage",
						Timestamp: testTimestamp1,
						Message:   "Power supply 2 output voltage is too high",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Power supply 2 output voltage is too high"},
		},
		{
			name: "fan slow spin with message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "fan",
						Slot:      5,
						ErrorCode: "FanSlow",
						Timestamp: testTimestamp1,
						Message:   "Fan 5 has stalled. Target RPM: 5000, Actual RPM: 1200",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Fan 5 has stalled. Target RPM: 5000, Actual RPM: 1200"},
		},
		{
			name: "fan slow spin without message falls back to default summary",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "fan",
						Slot:      3,
						ErrorCode: "FanSlow",
						Timestamp: testTimestamp1,
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Fan 3 has stalled"},
		},
		{
			name: "rig pool connection failure with URL in message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "rig",
						ErrorCode: "PoolConnectionFailure",
						Timestamp: testTimestamp1,
						Message:   "Control board is unable to connect to pool stratum+tcp://pool.example.com:3333",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Control board is unable to connect to pool stratum+tcp://pool.example.com:3333"},
		},
		{
			name: "rig insufficient cooling with bay index in message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "rig",
						ErrorCode: "InsufficientCooling",
						Timestamp: testTimestamp1,
						Message:   "Bay 2 has insufficient cooling",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Bay 2 has insufficient cooling"},
		},
		{
			name: "rig insufficient cooling without message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "rig",
						ErrorCode: "InsufficientCooling",
						Timestamp: testTimestamp1,
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Bay has insufficient cooling"},
		},
		{
			name: "rig insufficient cooling without message uses slot as bay context",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "rig",
						Slot:      2,
						ErrorCode: "InsufficientCooling",
						Timestamp: testTimestamp1,
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Bay 2 has insufficient cooling"},
		},
		{
			name: "rig pool connection failure without message restores computed summary",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "rig",
						ErrorCode: "PoolConnectionFailure",
						Timestamp: testTimestamp1,
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Control board is unable to connect to pool"},
		},
		{
			name: "hashboard ASIC overheat with temperature in message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      4,
						ErrorCode: "AsicOverHeat",
						Timestamp: testTimestamp1,
						Message:   "Hashboard 4 ASIC is overheating: 95.3 °C, first detected at ASIC 13",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 4 ASIC is overheating: 95.3 °C, first detected at ASIC 13"},
		},
		{
			name: "hashboard board overheat with temperature in message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      2,
						ErrorCode: "HbOverHeat",
						Timestamp: testTimestamp1,
						Message:   "Hashboard 2 overheating: 88.5 °C",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 2 overheating: 88.5 °C"},
		},
		{
			name: "hashboard overcurrent with amperage in message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      1,
						ErrorCode: "HbOverCurrent",
						Timestamp: testTimestamp1,
						Message:   "Hashboard 1 overcurrent detected: 42.50 A",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 1 overcurrent detected: 42.50 A"},
		},
		{
			name: "hashboard overcurrent without message restores computed summary",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      1,
						ErrorCode: "HbOverCurrent",
						Timestamp: testTimestamp1,
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 1 overcurrent detected"},
		},
		{
			name: "hashboard communication errors map to same miner error",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      2,
						ErrorCode: "HbCommunication",
						Timestamp: testTimestamp1,
						Message:   "Hashboard 2 communication error",
					},
					{
						Source:    "hashboard",
						Slot:      3,
						ErrorCode: "CommandTimeout",
						Timestamp: testTimestamp2,
						Message:   "Hashboard 3 communication error",
					},
				},
			},
			expectedCount:  2,
			validationType: validateSummaries,
			expectedSummaries: []string{
				"Hashboard 2 communication error",
				"Hashboard 3 communication error",
			},
		},
		{
			name: "PSU no input voltage without message restores computed summary",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      2,
						ErrorCode: "PsuNoInputVoltage",
						Timestamp: testTimestamp1,
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Power supply 2 is not detecting input voltage"},
		},
		{
			name:          "nil response returns empty errors",
			response:      nil,
			expectedCount: 0,
		},
		{
			name: "empty errors array returns empty errors",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{},
			},
			expectedCount: 0,
		},
		{
			name: "multiple errors of different types",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      1,
						ErrorCode: "PsuOutputOverVoltage",
						Timestamp: testTimestamp1,
						Message:   "Power supply 1 output voltage is too high",
					},
					{
						Source:    "fan",
						Slot:      2,
						ErrorCode: "FanHardware",
						Timestamp: testTimestamp2,
						Message:   "Fan 2 hardware error",
					},
					{
						Source:    "hashboard",
						Slot:      3,
						ErrorCode: "PowerLost",
						Timestamp: testTimestamp3,
						Message:   "Hashboard 3 has lost power",
					},
				},
			},
			expectedCount:  3,
			validationType: validateSummaries,
			expectedSummaries: []string{
				"Power supply 1 output voltage is too high",
				"Fan 2 hardware error",
				"Hashboard 3 has lost power",
			},
		},
		{
			name: "PSU no input voltage maps to correct severity and cause",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      2,
						ErrorCode: "PsuNoInputVoltage",
						Timestamp: testTimestamp1,
						Message:   "Power supply 2 is not detecting input voltage",
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 2 is not detecting input voltage",
				"minerError":   sdkerrors.PSUInputVoltageLow,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Loose power cables",
			},
		},
		{
			name: "PSU communication error",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      1,
						ErrorCode: "PsuCommLost",
						Timestamp: testTimestamp1,
						Message:   "Power supply 1 communication error",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Power supply 1 communication error"},
		},
		{
			name: "PSU overtemperature",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      3,
						ErrorCode: "PsuOverTemperature",
						Timestamp: testTimestamp1,
						Message:   "Power supply 3 overheating",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Power supply 3 overheating"},
		},
		{
			name: "PSU input undervoltage",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      1,
						ErrorCode: "PsuInputUnderVoltage",
						Timestamp: testTimestamp1,
						Message:   "Power supply 1 input voltage is too low",
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 1 input voltage is too low",
				"minerError":   sdkerrors.PSUInputVoltageLow,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Input undervoltage",
			},
		},
		{
			name: "PSU input overvoltage",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      2,
						ErrorCode: "PsuInputOverVoltage",
						Timestamp: testTimestamp1,
						Message:   "Power supply 2 input voltage is too high",
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 2 input voltage is too high",
				"minerError":   sdkerrors.PSUInputVoltageHigh,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Input overvoltage",
			},
		},
		{
			name: "PSU input overcurrent",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "psu",
						Slot:      3,
						ErrorCode: "PsuInputOverCurrent",
						Timestamp: testTimestamp1,
						Message:   "Power supply 3 input current is too high",
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 3 input current is too high",
				"minerError":   sdkerrors.PSUFaultGeneric,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Input overcurrent",
			},
		},
		{
			name: "hashboard undervoltage with voltage in message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      1,
						ErrorCode: "HbUnderVoltage",
						Timestamp: testTimestamp1,
						Message:   "Hashboard 1 undervoltage detected: 10.50 V",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 1 undervoltage detected: 10.50 V"},
		},
		{
			name: "hashboard overvoltage with voltage in message",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      2,
						ErrorCode: "HbOverVoltage",
						Timestamp: testTimestamp1,
						Message:   "Hashboard 2 overvoltage detected at 14.80 V",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 2 overvoltage detected at 14.80 V"},
		},
		{
			name: "hashboard ASIC not hashing",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "hashboard",
						Slot:      3,
						ErrorCode: "AsicNotHashing",
						Timestamp: testTimestamp1,
						Message:   "Hashboard 3 ASIC is not hashing, first detected at ASIC 9",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 3 ASIC is not hashing, first detected at ASIC 9"},
		},
		{
			name: "rig network error",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "rig",
						ErrorCode: "NetworkError",
						Timestamp: testTimestamp1,
						Message:   "Control board is unable to connect to the network",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Control board is unable to connect to the network"},
		},
		{
			name: "fan hardware error",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "fan",
						Slot:      4,
						ErrorCode: "FanHardware",
						Timestamp: testTimestamp1,
						Message:   "Fan 4 hardware error",
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Fan 4 hardware error"},
		},
		{
			name: "unknown error code maps to VendorErrorUnmapped",
			response: &proto.ErrorsResponse{
				Errors: []proto.NotificationError{
					{
						Source:    "rig",
						ErrorCode: "SomeUnknownError",
						Timestamp: testTimestamp1,
						Message:   "Something unexpected happened",
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Something unexpected happened",
				"minerError":   sdkerrors.VendorErrorUnmapped,
				"severity":     sdkerrors.SeverityInfo,
				"causeSummary": "Unhandled error code: rig/SomeUnknownError",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			device := &Device{
				id: "test-device-123",
			}

			// Act
			result := device.convertErrorsResponse(tt.response)

			// Assert
			assert.Equal(t, "test-device-123", result.DeviceID)
			assert.Len(t, result.Errors, tt.expectedCount)

			// Validate based on type
			switch tt.validationType {
			case validateSummaries:
				require.Len(t, result.Errors, len(tt.expectedSummaries))
				for i, expectedSummary := range tt.expectedSummaries {
					assert.Equal(t, expectedSummary, result.Errors[i].Summary)
				}

			case validateFullError:
				require.Len(t, result.Errors, 1)
				assert.Equal(t, tt.expectedFullError["summary"], result.Errors[0].Summary)
				assert.Equal(t, tt.expectedFullError["minerError"], result.Errors[0].MinerError)
				assert.Equal(t, tt.expectedFullError["severity"], result.Errors[0].Severity)
				assert.Equal(t, tt.expectedFullError["causeSummary"], result.Errors[0].CauseSummary)
			}
		})
	}
}

func TestConvertErrorsResponse_LastSeenAtIsCurrentTime(t *testing.T) {
	response := &proto.ErrorsResponse{
		Errors: []proto.NotificationError{
			{
				Source:    "rig",
				ErrorCode: "LowHashRate",
				Timestamp: historicalErrorTimestamp, // 2021-01-01 00:00:00 UTC
				Message:   "Hashrate is below target",
			},
		},
	}

	device := &Device{
		id: "test-device-123",
	}

	// Act
	beforeCall := time.Now()
	result := device.convertErrorsResponse(response)
	afterCall := time.Now()

	// Assert
	require.Len(t, result.Errors, 1)
	err := result.Errors[0]

	// Verify FirstSeenAt is set to the miner's timestamp
	expectedFirstSeenAt := time.Unix(historicalErrorTimestamp, 0)
	assert.Equal(t, expectedFirstSeenAt, err.FirstSeenAt, "FirstSeenAt should be the miner's timestamp")

	// Verify LastSeenAt is set to current time
	assert.False(t, err.LastSeenAt.IsZero(), "LastSeenAt should not be zero")
	assert.True(t, err.LastSeenAt.After(beforeCall) || err.LastSeenAt.Equal(beforeCall),
		"LastSeenAt should be at or after the call time")
	assert.True(t, err.LastSeenAt.Before(afterCall) || err.LastSeenAt.Equal(afterCall),
		"LastSeenAt should be at or before the call completion")
}

func TestConvertNotificationError_ComponentInfo(t *testing.T) {
	tests := []struct {
		name             string
		notifErr         proto.NotificationError
		expectedCompType sdkerrors.ComponentType
		expectedCompID   string
		hasComponentID   bool
	}{
		{
			name: "fan sets component type and ID",
			notifErr: proto.NotificationError{
				Source:    "fan",
				Slot:      3,
				ErrorCode: "FanHardware",
				Message:   "Fan 3 hardware error",
			},
			expectedCompType: sdkerrors.ComponentTypeFan,
			expectedCompID:   "3",
			hasComponentID:   true,
		},
		{
			name: "hashboard sets component type and ID",
			notifErr: proto.NotificationError{
				Source:    "hashboard",
				Slot:      5,
				ErrorCode: "HbOverHeat",
				Message:   "Hashboard 5 overheating",
			},
			expectedCompType: sdkerrors.ComponentTypeHashBoard,
			expectedCompID:   "5",
			hasComponentID:   true,
		},
		{
			name: "psu sets component type and ID",
			notifErr: proto.NotificationError{
				Source:    "psu",
				Slot:      1,
				ErrorCode: "PsuFans",
				Message:   "Power supply 1 fan failure",
			},
			expectedCompType: sdkerrors.ComponentTypePSU,
			expectedCompID:   "1",
			hasComponentID:   true,
		},
		{
			name: "rig has no component ID",
			notifErr: proto.NotificationError{
				Source:    "rig",
				ErrorCode: "LowHashRate",
				Message:   "Low hashrate detected",
			},
			hasComponentID: false,
		},
		{
			name: "fan_slot_zero_has_no_component_id",
			notifErr: proto.NotificationError{
				Source:    "fan",
				Slot:      0,
				ErrorCode: "FanHardware",
				Message:   "Fan fault",
			},
			hasComponentID: false,
		},
		{
			name: "hashboard_slot_zero_has_no_component_id",
			notifErr: proto.NotificationError{
				Source:    "hashboard",
				Slot:      0,
				ErrorCode: "HbOverHeat",
				Message:   "Hashboard thermal",
			},
			hasComponentID: false,
		},
		{
			name: "psu_slot_zero_has_no_component_id",
			notifErr: proto.NotificationError{
				Source:    "psu",
				Slot:      0,
				ErrorCode: "PsuFans",
				Message:   "PSU fan",
			},
			hasComponentID: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertNotificationError(tt.notifErr, "test-device")

			if tt.hasComponentID {
				require.NotNil(t, result.ComponentID)
				assert.Equal(t, tt.expectedCompID, *result.ComponentID)
				assert.Equal(t, tt.expectedCompType, result.ComponentType)
			} else {
				assert.Nil(t, result.ComponentID)
			}
		})
	}
}

func TestLookupErrorMapping_AllSources(t *testing.T) {
	// Verify rig mappings
	m, ok := lookupErrorMapping("rig", "PoolConnectionFailure")
	assert.True(t, ok)
	assert.Equal(t, sdkerrors.DeviceCommunicationLost, m.minerError)
	assert.Equal(t, sdkerrors.SeverityMajor, m.severity)

	// Verify fan mappings
	m, ok = lookupErrorMapping("fan", "FanSlow")
	assert.True(t, ok)
	assert.Equal(t, sdkerrors.FanSpeedDeviation, m.minerError)
	assert.Equal(t, sdkerrors.SeverityMajor, m.severity)

	// Verify hashboard mappings
	m, ok = lookupErrorMapping("hashboard", "AsicOverHeat")
	assert.True(t, ok)
	assert.Equal(t, sdkerrors.HashboardASICOverTemperature, m.minerError)
	assert.Equal(t, sdkerrors.SeverityCritical, m.severity)

	// Verify PSU mappings
	m, ok = lookupErrorMapping("psu", "PsuOutputOverVoltage")
	assert.True(t, ok)
	assert.Equal(t, sdkerrors.PSUOutputVoltageFault, m.minerError)
	assert.Equal(t, sdkerrors.SeverityCritical, m.severity)

	// Verify unknown source returns not found
	_, ok = lookupErrorMapping("unknown", "SomeError")
	assert.False(t, ok)

	// Verify unknown error code returns not found
	_, ok = lookupErrorMapping("rig", "NonExistentError")
	assert.False(t, ok)
}
