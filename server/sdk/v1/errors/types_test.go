package errors

import (
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testErrorTime = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
)

// ============================================================================
// Test Helpers
// ============================================================================

func createFullDeviceError() DeviceError {
	closedAt := testErrorTime.Add(10 * time.Minute)
	componentID := "0"

	return DeviceError{
		MinerError:        PSUOutputOvercurrent,
		CauseSummary:      "PSU exceeded safe current threshold",
		RecommendedAction: "Check power consumption and PSU capacity",
		Severity:          SeverityCritical,
		FirstSeenAt:       testErrorTime,
		LastSeenAt:        testErrorTime.Add(5 * time.Minute),
		ClosedAt:          &closedAt,
		VendorAttributes: map[string]string{
			"current_amps":   "15.5",
			"threshold_amps": "14.0",
			"rail":           "12V",
			"firmware":       "v2.1.3",
		},
		DeviceID:      "device-123",
		ComponentID:   &componentID,
		ComponentType: ComponentTypePSU,
		Impact:        "Stops mining immediately",
		Summary:       "PSU Overcurrent Fault",
	}
}

func createMinimalDeviceError() DeviceError {
	return DeviceError{
		MinerError:    FanFailed,
		Severity:      SeverityMajor,
		DeviceID:      "device-456",
		FirstSeenAt:   testErrorTime,
		LastSeenAt:    testErrorTime,
		ComponentType: ComponentTypeUnspecified,
	}
}

func TestDeviceErrorRoundTrip_Full(t *testing.T) {
	// Arrange
	original := createFullDeviceError()

	// Act
	pbError := original.ToProto()
	require.NotNil(t, pbError)
	converted := DeviceErrorFromProto(pbError)

	// Assert
	assert.Equal(t, original.MinerError, converted.MinerError)
	assert.Equal(t, original.CauseSummary, converted.CauseSummary)
	assert.Equal(t, original.RecommendedAction, converted.RecommendedAction)
	assert.Equal(t, original.Severity, converted.Severity)
	assert.Equal(t, original.FirstSeenAt.Unix(), converted.FirstSeenAt.Unix())
	assert.Equal(t, original.LastSeenAt.Unix(), converted.LastSeenAt.Unix())
	require.NotNil(t, converted.ClosedAt)
	assert.Equal(t, original.ClosedAt.Unix(), converted.ClosedAt.Unix())
	assert.Equal(t, original.VendorAttributes, converted.VendorAttributes)
	assert.Equal(t, original.DeviceID, converted.DeviceID)
	require.NotNil(t, converted.ComponentID)
	assert.Equal(t, *original.ComponentID, *converted.ComponentID)
	assert.Equal(t, original.ComponentType, converted.ComponentType)
	assert.Equal(t, original.Impact, converted.Impact)
	assert.Equal(t, original.Summary, converted.Summary)
}

func TestDeviceErrorRoundTrip_Minimal(t *testing.T) {
	// Arrange
	original := createMinimalDeviceError()

	// Act
	pbError := original.ToProto()
	converted := DeviceErrorFromProto(pbError)

	// Assert
	assert.Equal(t, original.MinerError, converted.MinerError)
	assert.Equal(t, original.Severity, converted.Severity)
	assert.Equal(t, original.DeviceID, converted.DeviceID)
	assert.Equal(t, "", converted.CauseSummary)
	assert.Equal(t, "", converted.RecommendedAction)
	assert.Nil(t, converted.ClosedAt)
	assert.Nil(t, converted.ComponentID)
	assert.Equal(t, original.FirstSeenAt.Unix(), converted.FirstSeenAt.Unix())
}

func TestDeviceErrorRoundTrip_AllMinerErrors(t *testing.T) {
	// Arrange
	minerErrors := []MinerError{
		// PSU errors
		PSUNotPresent, PSUModelMismatch, PSUCommunicationLost, PSUFaultGeneric,
		PSUInputVoltageLow, PSUInputVoltageHigh, PSUOutputVoltageFault,
		PSUOutputOvercurrent, PSUFanFault, PSUOverTemperature,
		PSUInputPhaseImbalance, PSUUnderTemperature,
		// Thermal & fans
		FanFailed, FanTachSignalLost, FanSpeedDeviation,
		InletOverTemperature, DeviceOverTemperature, DeviceUnderTemperature,
		// Hashboard / ASIC
		HashboardNotPresent, HashboardOverTemperature, HashboardMissingChips,
		ASICChainCommunicationLost, ASICClockPLLUnlocked, ASICCRCErrorExcessive,
		HashboardASICOverTemperature, HashboardASICUnderTemperature,
		// Board power
		BoardPowerPGOODMissing, BoardPowerOvercurrent, BoardPowerRailUndervolt,
		BoardPowerRailOvervolt, BoardPowerShortDetected,
		// Sensors
		TempSensorOpenOrShort, TempSensorFault, VoltageSensorFault, CurrentSensorFault,
		// Firmware
		EEPROMCRCMismatch, EEPROMReadFailure, FirmwareImageInvalid, FirmwareConfigInvalid,
		// Control & comms
		ControlBoardCommunicationLost, ControlBoardFailure, DeviceInternalBusFault,
		DeviceCommunicationLost, IOModuleFailure,
		// Performance advisories
		HashrateBelowTarget, HashboardWarnCRCHigh, ThermalMarginLow,
		// Catch-all
		VendorErrorUnmapped,
	}

	for _, errCode := range minerErrors {
		t.Run(errCode.String(), func(t *testing.T) {
			// Arrange
			original := DeviceError{
				MinerError:  errCode,
				Severity:    SeverityMajor,
				DeviceID:    "test-device",
				FirstSeenAt: testErrorTime,
				LastSeenAt:  testErrorTime,
			}

			// Act
			pbError := original.ToProto()
			converted := DeviceErrorFromProto(pbError)

			// Assert
			assert.Equal(t, original.MinerError, converted.MinerError)
		})
	}
}

func TestDeviceErrorRoundTrip_AllSeverities(t *testing.T) {
	// Arrange
	severities := []Severity{
		SeverityCritical,
		SeverityMajor,
		SeverityMinor,
		SeverityInfo,
	}

	for _, sev := range severities {
		t.Run(sev.String(), func(t *testing.T) {
			// Arrange
			original := DeviceError{
				MinerError:  FanFailed,
				Severity:    sev,
				DeviceID:    "test-device",
				FirstSeenAt: testErrorTime,
				LastSeenAt:  testErrorTime,
			}

			// Act
			pbError := original.ToProto()
			converted := DeviceErrorFromProto(pbError)

			// Assert
			assert.Equal(t, original.Severity, converted.Severity)
		})
	}
}

func TestDeviceErrorFromProto(t *testing.T) {
	// Arrange - Create protobuf DeviceError
	closedAt := testErrorTime.Add(10 * time.Minute)
	componentID := "0"

	deviceError := createFullDeviceError()
	pbError := deviceError.ToProto()

	// Act - Convert to DeviceError
	converted := DeviceErrorFromProto(pbError)

	// Assert - All fields preserved
	assert.Equal(t, deviceError.MinerError, converted.MinerError)
	assert.Equal(t, deviceError.CauseSummary, converted.CauseSummary)
	assert.Equal(t, deviceError.RecommendedAction, converted.RecommendedAction)
	assert.Equal(t, deviceError.Severity, converted.Severity)
	assert.Equal(t, deviceError.FirstSeenAt.Unix(), converted.FirstSeenAt.Unix())
	assert.Equal(t, deviceError.LastSeenAt.Unix(), converted.LastSeenAt.Unix())
	require.NotNil(t, converted.ClosedAt)
	assert.Equal(t, closedAt.Unix(), converted.ClosedAt.Unix())
	assert.Equal(t, deviceError.VendorAttributes, converted.VendorAttributes)
	assert.Equal(t, deviceError.DeviceID, converted.DeviceID)
	require.NotNil(t, converted.ComponentID)
	assert.Equal(t, componentID, *converted.ComponentID)
	assert.Equal(t, deviceError.ComponentType, converted.ComponentType)
	assert.Equal(t, deviceError.Impact, converted.Impact)
	assert.Equal(t, deviceError.Summary, converted.Summary)
}

func TestDeviceErrorsFromProto_MultipleErrors(t *testing.T) {
	// Arrange - Create protobuf DeviceErrors
	pbErrors := &pb.DeviceErrors{
		DeviceId: "device-789",
		Errors: []*pb.DeviceError{
			createFullDeviceError().ToProto(),
			createMinimalDeviceError().ToProto(),
			DeviceError{
				MinerError:  HashboardOverTemperature,
				Severity:    SeverityMajor,
				DeviceID:    "device-789",
				FirstSeenAt: testErrorTime,
				LastSeenAt:  testErrorTime,
			}.ToProto(),
		},
	}

	// Act
	converted := DeviceErrorsFromProto(pbErrors)

	// Assert
	assert.Equal(t, "device-789", converted.DeviceID)
	assert.Len(t, converted.Errors, 3)

	// Verify errors
	assert.Equal(t, PSUOutputOvercurrent, converted.Errors[0].MinerError)
	assert.Equal(t, FanFailed, converted.Errors[1].MinerError)
	assert.Equal(t, HashboardOverTemperature, converted.Errors[2].MinerError)
}

func TestDeviceErrorsFromProto_EmptyErrors(t *testing.T) {
	// Arrange
	pbErrors := &pb.DeviceErrors{
		DeviceId: "device-empty",
		Errors:   []*pb.DeviceError{},
	}

	// Act
	converted := DeviceErrorsFromProto(pbErrors)

	// Assert
	assert.Equal(t, "device-empty", converted.DeviceID)
	assert.Empty(t, converted.Errors)
}

func TestDeviceErrorsFromProto_NilProtobuf(t *testing.T) {
	// Arrange - no setup needed, testing nil input

	// Act
	converted := DeviceErrorsFromProto(nil)

	// Assert
	assert.Equal(t, "", converted.DeviceID)
	assert.Nil(t, converted.Errors)
}

func TestDeviceError_OptionalFields(t *testing.T) {
	// Arrange
	tests := []struct {
		name        string
		closedAt    *time.Time
		componentID *string
	}{
		{
			name:        "both_nil",
			closedAt:    nil,
			componentID: nil,
		},
		{
			name:        "closed_at_only",
			closedAt:    func() *time.Time { t := testErrorTime.Add(time.Hour); return &t }(),
			componentID: nil,
		},
		{
			name:        "component_id_only",
			closedAt:    nil,
			componentID: func() *string { s := "component-123"; return &s }(),
		},
		{
			name:        "both_set",
			closedAt:    func() *time.Time { t := testErrorTime.Add(time.Hour); return &t }(),
			componentID: func() *string { s := "component-123"; return &s }(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			original := DeviceError{
				MinerError:  FanFailed,
				Severity:    SeverityMajor,
				DeviceID:    "test",
				FirstSeenAt: testErrorTime,
				LastSeenAt:  testErrorTime,
				ClosedAt:    tt.closedAt,
				ComponentID: tt.componentID,
			}

			// Act
			pbError := original.ToProto()
			converted := DeviceErrorFromProto(pbError)

			// Assert
			if tt.closedAt == nil {
				assert.Nil(t, converted.ClosedAt)
			} else {
				require.NotNil(t, converted.ClosedAt)
				assert.Equal(t, tt.closedAt.Unix(), converted.ClosedAt.Unix())
			}

			if tt.componentID == nil {
				assert.Nil(t, converted.ComponentID)
			} else {
				require.NotNil(t, converted.ComponentID)
				assert.Equal(t, *tt.componentID, *converted.ComponentID)
			}
		})
	}
}

func TestDeviceError_VendorAttributes_Various(t *testing.T) {
	// Arrange
	tests := []struct {
		name       string
		attributes map[string]string
	}{
		{
			name:       "nil_attributes",
			attributes: nil,
		},
		{
			name:       "empty_attributes",
			attributes: map[string]string{},
		},
		{
			name: "single_attribute",
			attributes: map[string]string{
				"firmware_version": "v2.1.3",
			},
		},
		{
			name: "multiple_attributes",
			attributes: map[string]string{
				"firmware_version":  "v2.1.3",
				"vendor_error_code": "0xABCD",
				"serial_number":     "SN12345",
				"batch_id":          "BATCH-2024-01",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			original := DeviceError{
				MinerError:       PSUFaultGeneric,
				Severity:         SeverityMajor,
				DeviceID:         "test",
				FirstSeenAt:      testErrorTime,
				LastSeenAt:       testErrorTime,
				VendorAttributes: tt.attributes,
			}

			// Act
			pbError := original.ToProto()
			converted := DeviceErrorFromProto(pbError)

			// Assert
			if tt.attributes == nil {
				assert.Nil(t, converted.VendorAttributes)
			} else {
				assert.Equal(t, tt.attributes, converted.VendorAttributes)
			}
		})
	}
}
