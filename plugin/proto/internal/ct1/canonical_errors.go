package ct1

import (
	"strings"

	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
)

// Firmware spelling stays in Cause; these classifications are shared with all
// Fleet drivers. Enumeration/ECC/temperature deltas are not missing chips/CRC/
// absolute overtemperature, respectively.
var canonicalCauses = map[string]sdkerrors.MinerError{
	"BOARD_OVER_CURRENT":                   sdkerrors.BoardPowerOvercurrent,
	"BOARD_OVER_VOLTAGE":                   sdkerrors.BoardPowerRailOvervolt,
	"BOARD_UNDER_VOLTAGE":                  sdkerrors.BoardPowerRailUndervolt,
	"BOARD_OVER_TEMPERATURE":               sdkerrors.HashboardOverTemperature,
	"BOARD_POWER_SUPPLY":                   sdkerrors.PSUFaultGeneric,
	"BOARD_POWER_SUPPLY_OVER_TEMPERATURE":  sdkerrors.PSUOverTemperature,
	"PSU_OVER_TEMPERATURE":                 sdkerrors.PSUOverTemperature,
	"BOARD_EXCESSIVE_BUSBAR_VOLTAGE_DROP":  sdkerrors.BoardBusbarVoltageDrop,
	"BLACKBOX_STORAGE_FAULT":               sdkerrors.DiagnosticStorageFault,
	"ASIC_OVER_VOLTAGE":                    sdkerrors.ASICOvervoltage,
	"ASIC_UNDER_VOLTAGE":                   sdkerrors.ASICUndervoltage,
	"ASIC_OVER_TEMPERATURE":                sdkerrors.HashboardASICOverTemperature,
	"ASIC_UNDER_TEMPERATURE":               sdkerrors.HashboardASICUnderTemperature,
	"ASIC_ENUMERATION":                     sdkerrors.ASICEnumerationFailed,
	"ASIC_ECC":                             sdkerrors.ASICECCError,
	"ASIC_CORNER_TEMPERATURE_DELTA":        sdkerrors.ASICTemperatureImbalance,
	"ASIC_LEADING_EDGE_TEMPERATURE_DELTA":  sdkerrors.ASICTemperatureImbalance,
	"ASIC_TRAILING_EDGE_TEMPERATURE_DELTA": sdkerrors.ASICTemperatureImbalance,
	"ASIC_HOTSPOT_TEMPERATURE_DELTA":       sdkerrors.ASICTemperatureImbalance,
	"SOFTWARE_ERROR":                       sdkerrors.FirmwareRuntimeError,
	"USB_ERROR":                            sdkerrors.DeviceInternalBusFault,
	"MC3_STARTUP_TIMEOUT":                  sdkerrors.HashboardStartupFailed,
	"MC3_STARTUP_FAILED":                   sdkerrors.HashboardStartupFailed,
	"MC3_NO_STACK_RUNNING":                 sdkerrors.HashboardNoStackRunning,
	"MC3_RUNTIME_COMMS_BUSY_TIMEOUT":       sdkerrors.DeviceInternalBusFault,
	"MC3_KNOWN_GOOD_STARTUP_DROP":          sdkerrors.HashboardStartupFailed,
	"ASIC_THERMAL_THROTTLING":              sdkerrors.ASICThermalThrottling,
	"STACK_ENUMERATION":                    sdkerrors.HashboardStackEnumerationFailed,
	"STACK_DELTA_THERMAL_THROTTLE":         sdkerrors.ASICThermalThrottling,
	"HASHRAIL_NOT_OK":                      sdkerrors.BoardPowerRailFault,
	"REFERENCE_CLOCKS_NOT_OK":              sdkerrors.ReferenceClockFault,
}

func canonicalIssue(cause string, warning bool) (sdkerrors.MinerError, sdkerrors.Severity) {
	code, ok := canonicalCauses[strings.TrimPrefix(cause, "HASHBOARD_ERROR_CAUSE_")]
	if !ok {
		code = sdkerrors.VendorErrorUnmapped
	}
	severity := sdkerrors.SeverityMajor
	if warning {
		severity = sdkerrors.SeverityMinor
		switch code {
		case sdkerrors.ASICThermalThrottling, sdkerrors.PSUOverTemperature, sdkerrors.HashboardASICOverTemperature, sdkerrors.HashboardOverTemperature:
			severity = sdkerrors.SeverityMajor
		}
		return code, severity
	}
	switch code {
	case sdkerrors.BoardPowerOvercurrent, sdkerrors.BoardPowerRailOvervolt, sdkerrors.BoardPowerRailUndervolt,
		sdkerrors.HashboardOverTemperature, sdkerrors.PSUOverTemperature, sdkerrors.HashboardASICOverTemperature,
		sdkerrors.ASICOvervoltage, sdkerrors.ASICUndervoltage, sdkerrors.BoardPowerRailFault, sdkerrors.ReferenceClockFault:
		severity = sdkerrors.SeverityCritical
	}
	return code, severity
}
