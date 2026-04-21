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

func TestDefaultComponentTypeForMinerError_PSUErrors(t *testing.T) {
	psuErrors := []MinerError{
		PSUNotPresent, PSUModelMismatch, PSUCommunicationLost, PSUFaultGeneric,
		PSUInputVoltageLow, PSUInputVoltageHigh, PSUOutputVoltageFault,
		PSUOutputOvercurrent, PSUFanFault, PSUOverTemperature,
		PSUInputPhaseImbalance, PSUUnderTemperature,
	}

	for _, err := range psuErrors {
		result := DefaultComponentTypeForMinerError(err)
		if result != ComponentTypePSU {
			t.Errorf("PSU error %d: expected ComponentTypePSU, got %d", err, result)
		}
	}
}

func TestDefaultComponentTypeForMinerError_FanThermalErrors(t *testing.T) {
	fanThermalErrors := []MinerError{
		FanFailed, FanTachSignalLost, FanSpeedDeviation,
		InletOverTemperature, DeviceOverTemperature, DeviceUnderTemperature,
	}

	for _, err := range fanThermalErrors {
		result := DefaultComponentTypeForMinerError(err)
		if result != ComponentTypeFans {
			t.Errorf("Fan/thermal error %d: expected ComponentTypeFans, got %d", err, result)
		}
	}
}

func TestDefaultComponentTypeForMinerError_HashboardErrors(t *testing.T) {
	hashboardErrors := []MinerError{
		HashboardNotPresent, HashboardOverTemperature, HashboardMissingChips,
		ASICChainCommunicationLost, ASICClockPLLUnlocked, ASICCRCErrorExcessive,
		HashboardASICOverTemperature, HashboardASICUnderTemperature,
	}

	for _, err := range hashboardErrors {
		result := DefaultComponentTypeForMinerError(err)
		if result != ComponentTypeHashBoards {
			t.Errorf("Hashboard error %d: expected ComponentTypeHashBoards, got %d", err, result)
		}
	}
}

func TestDefaultComponentTypeForMinerError_BoardPowerErrors(t *testing.T) {
	boardPowerErrors := []MinerError{
		BoardPowerPGOODMissing, BoardPowerOvercurrent,
		BoardPowerRailUndervolt, BoardPowerRailOvervolt, BoardPowerShortDetected,
	}

	for _, err := range boardPowerErrors {
		result := DefaultComponentTypeForMinerError(err)
		if result != ComponentTypeHashBoards {
			t.Errorf("Board power error %d: expected ComponentTypeHashBoards, got %d", err, result)
		}
	}
}

func TestDefaultComponentTypeForMinerError_ControlPlaneErrors(t *testing.T) {
	controlPlaneErrors := []MinerError{
		ControlBoardCommunicationLost, ControlBoardFailure,
		DeviceInternalBusFault, DeviceCommunicationLost, IOModuleFailure,
	}

	for _, err := range controlPlaneErrors {
		result := DefaultComponentTypeForMinerError(err)
		if result != ComponentTypeControlBoard {
			t.Errorf("Control plane error %d: expected ComponentTypeControlBoard, got %d", err, result)
		}
	}
}

func TestDefaultComponentTypeForMinerError_UnmappedErrors(t *testing.T) {
	unmappedErrors := []MinerError{
		MinerErrorUnspecified,
		TempSensorOpenOrShort, TempSensorFault, VoltageSensorFault, CurrentSensorFault,
		EEPROMCRCMismatch, EEPROMReadFailure, FirmwareImageInvalid, FirmwareConfigInvalid,
		HashrateBelowTarget, HashboardWarnCRCHigh, ThermalMarginLow,
		VendorErrorUnmapped,
	}

	for _, err := range unmappedErrors {
		result := DefaultComponentTypeForMinerError(err)
		if result != ComponentTypeUnspecified {
			t.Errorf("Unmapped error %d: expected ComponentTypeUnspecified, got %d", err, result)
		}
	}
}

func TestDefaultComponentTypeForMinerError_BoundaryValues(t *testing.T) {
	tests := []struct {
		name     string
		code     MinerError
		expected ComponentType
	}{
		// PSU range (1000-1999)
		{"Below PSU range", 999, ComponentTypeUnspecified},
		{"PSU min boundary", 1000, ComponentTypePSU},
		{"PSU max boundary", 1999, ComponentTypePSU},
		// Fan/thermal range (2000-2999)
		{"Fan min boundary", 2000, ComponentTypeFans},
		{"Fan max boundary", 2999, ComponentTypeFans},
		// Hashboard range (3000-3499)
		{"Hashboard min boundary", 3000, ComponentTypeHashBoards},
		{"Hashboard max boundary", 3499, ComponentTypeHashBoards},
		// Board power range (3500-3999)
		{"Board power min boundary", 3500, ComponentTypeHashBoards},
		{"Board power max boundary", 3999, ComponentTypeHashBoards},
		// Gap between board power and control plane (4000-5999)
		{"Sensor range (unmapped)", 4000, ComponentTypeUnspecified},
		{"Storage range (unmapped)", 5000, ComponentTypeUnspecified},
		{"Below control plane range", 5999, ComponentTypeUnspecified},
		// Control plane range (6000-6999)
		{"Control plane min boundary", 6000, ComponentTypeControlBoard},
		{"Control plane max boundary", 6999, ComponentTypeControlBoard},
		// Above control plane
		{"Above control plane range", 7000, ComponentTypeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultComponentTypeForMinerError(tt.code)
			if result != tt.expected {
				t.Errorf("MinerError %d: expected %d, got %d", tt.code, tt.expected, result)
			}
		})
	}
}
