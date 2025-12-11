package models

import (
	"testing"
)

func getAllDefinedErrorCodes() []MinerError {
	return []MinerError{
		// PSU errors (1000-1999)
		PSUNotPresent,
		PSUModelMismatch,
		PSUCommunicationLost,
		PSUFaultGeneric,
		PSUInputVoltageLow,
		PSUInputVoltageHigh,
		PSUOutputVoltageFault,
		PSUOutputOvercurrent,
		PSUFanFault,
		PSUOverTemperature,
		PSUInputPhaseImbalance,
		PSUUnderTemperature,

		// Thermal & Fan errors (2000-2999)
		FanFailed,
		FanTachSignalLost,
		FanSpeedDeviation,
		InletOverTemperature,
		DeviceOverTemperature,
		DeviceUnderTemperature,

		// Hashboard / ASIC errors (3000-3999)
		HashboardNotPresent,
		HashboardOverTemperature,
		HashboardMissingChips,
		ASICChainCommunicationLost,
		ASICClockPLLUnlocked,
		ASICCRCErrorExcessive,
		HashboardASICOverTemperature,
		HashboardASICUnderTemperature,

		// Board-level power errors (3500-3999)
		BoardPowerPGOODMissing,
		BoardPowerOvercurrent,
		BoardPowerRailUndervolt,
		BoardPowerRailOvervolt,
		BoardPowerShortDetected,

		// Sensor errors (4000-4999)
		TempSensorOpenOrShort,
		TempSensorFault,
		VoltageSensorFault,
		CurrentSensorFault,

		// Storage / Firmware errors (5000-5999)
		EEPROMCRCMismatch,
		EEPROMReadFailure,
		FirmwareImageInvalid,
		FirmwareConfigInvalid,

		// Control-plane errors (6000-6999)
		ControlBoardCommunicationLost,
		ControlBoardFailure,
		DeviceInternalBusFault,
		DeviceCommunicationLost,
		IOModuleFailure,

		// Performance advisories (8000-8999)
		HashrateBelowTarget,
		HashboardWarnCRCHigh,
		ThermalMarginLow,

		// Catch-all (9000-9999)
		VendorErrorUnmapped,
	}
}

func TestGetMinerErrorInfo(t *testing.T) {
	metadata := GetMinerErrorInfo()
	expectedCodes := getAllDefinedErrorCodes()

	// Verify all expected codes are present
	for _, code := range expectedCodes {
		info, ok := metadata[code]
		if !ok {
			t.Errorf("Expected error code %d to be present in metadata", code)
			continue
		}

		// Verify all required fields are non-empty
		if info.Name == "" {
			t.Errorf("Error code %d has empty Name", code)
		}
		if info.DefaultSummary == "" {
			t.Errorf("Error code %d has empty DefaultSummary", code)
		}
		if info.DefaultAction == "" {
			t.Errorf("Error code %d has empty DefaultAction", code)
		}
		if info.DefaultImpact == "" {
			t.Errorf("Error code %d has empty DefaultImpact", code)
		}
		if info.DefaultSeverity == SeverityUnspecified {
			t.Errorf("Error code %d has unspecified severity", code)
		}
	}

	// Verify count matches expected
	expectedCount := len(expectedCodes)
	actualCount := len(metadata)
	if actualCount != expectedCount {
		t.Errorf("Expected %d error codes, got %d", expectedCount, actualCount)

		// Find which codes are missing or extra
		expectedSet := make(map[MinerError]bool)
		for _, code := range expectedCodes {
			expectedSet[code] = true
		}

		for code := range metadata {
			if !expectedSet[code] {
				t.Errorf("Unexpected error code in metadata: %d", code)
			}
		}
	}
}

func TestMinerErrorInfoCompleteness(t *testing.T) {
	metadata := GetMinerErrorInfo()

	// Verify no MinerErrorUnspecified in metadata
	if _, ok := metadata[MinerErrorUnspecified]; ok {
		t.Error("MinerErrorUnspecified should not be included in metadata")
	}

	// Verify all entries have valid severity levels
	validSeverities := map[Severity]bool{
		SeverityCritical: true,
		SeverityMajor:    true,
		SeverityMinor:    true,
		SeverityInfo:     true,
	}

	for code, info := range metadata {
		if !validSeverities[info.DefaultSeverity] {
			t.Errorf("Error code %d has invalid severity: %d", code, info.DefaultSeverity)
		}
	}
}
