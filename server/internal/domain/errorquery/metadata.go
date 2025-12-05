package errorquery

import (
	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
)

// MinerErrorMetadata holds metadata for a miner error code.
type MinerErrorMetadata struct {
	Code            errorsv1.MinerError
	Name            string
	DefaultSummary  string
	DefaultSeverity errorsv1.Severity
	DefaultAction   string
	DefaultImpact   string
	ComponentType   errorsv1.ComponentType
}

// BuildMinerErrorMetadata returns metadata for all miner error codes.
func BuildMinerErrorMetadata() map[errorsv1.MinerError]*MinerErrorMetadata {
	return map[errorsv1.MinerError]*MinerErrorMetadata{
		// PSU errors (100-149)
		errorsv1.MinerError_MINER_ERROR_PSU_NOT_PRESENT: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_NOT_PRESENT,
			Name:            "PSU Not Present",
			DefaultSummary:  "Power supply unit is not detected",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check PSU installation and connections",
			DefaultImpact:   "Miner cannot operate without power supply",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_MODEL_MISMATCH: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_MODEL_MISMATCH,
			Name:            "PSU Model Mismatch",
			DefaultSummary:  "PSU model does not match expected configuration",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Verify PSU model matches device requirements",
			DefaultImpact:   "May cause power delivery issues or reduced performance",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_COMMUNICATION_LOST: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_COMMUNICATION_LOST,
			Name:            "PSU Communication Lost",
			DefaultSummary:  "Communication with PSU has been lost",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Check PSU connection cables and restart device",
			DefaultImpact:   "Cannot monitor PSU status or adjust power settings",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_FAULT_GENERIC: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_FAULT_GENERIC,
			Name:            "PSU Fault",
			DefaultSummary:  "General PSU fault detected",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Inspect PSU for damage or overheating",
			DefaultImpact:   "Power delivery may be compromised",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_INPUT_VOLTAGE_LOW: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_INPUT_VOLTAGE_LOW,
			Name:            "PSU Input Voltage Low",
			DefaultSummary:  "Input voltage to PSU is below acceptable range",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Check facility power supply and voltage levels",
			DefaultImpact:   "May cause power instability or shutdown",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_INPUT_VOLTAGE_HIGH: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_INPUT_VOLTAGE_HIGH,
			Name:            "PSU Input Voltage High",
			DefaultSummary:  "Input voltage to PSU is above acceptable range",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Immediately check facility power; may damage equipment",
			DefaultImpact:   "Risk of equipment damage",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_OUTPUT_VOLTAGE_FAULT: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_OUTPUT_VOLTAGE_FAULT,
			Name:            "PSU Output Voltage Fault",
			DefaultSummary:  "PSU output voltage is outside acceptable range",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Replace PSU immediately",
			DefaultImpact:   "Miner may shut down or sustain damage",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_OUTPUT_OVERCURRENT: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_OUTPUT_OVERCURRENT,
			Name:            "PSU Output Overcurrent",
			DefaultSummary:  "PSU output current exceeds safe limits",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check for short circuits; reduce load or replace PSU",
			DefaultImpact:   "Stops mining to prevent equipment damage",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_FAN_FAILED: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_FAN_FAILED,
			Name:            "PSU Fan Failed",
			DefaultSummary:  "Cooling fan in PSU has failed",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Replace PSU; risk of overheating",
			DefaultImpact:   "PSU may overheat and shut down",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_OVER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_OVER_TEMPERATURE,
			Name:            "PSU Over Temperature",
			DefaultSummary:  "PSU temperature exceeds safe operating range",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Improve cooling; check PSU fan and ambient temperature",
			DefaultImpact:   "May cause thermal shutdown",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_INPUT_PHASE_IMBALANCE: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_INPUT_PHASE_IMBALANCE,
			Name:            "PSU Input Phase Imbalance",
			DefaultSummary:  "Three-phase power input is imbalanced",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MINOR,
			DefaultAction:   "Check facility power distribution",
			DefaultImpact:   "Reduced efficiency; potential for long-term damage",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},
		errorsv1.MinerError_MINER_ERROR_PSU_UNDER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_PSU_UNDER_TEMPERATURE,
			Name:            "PSU Under Temperature",
			DefaultSummary:  "PSU temperature is below operating range",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Allow warmup time; check ambient conditions",
			DefaultImpact:   "May affect startup reliability",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_PSU,
		},

		// Thermal & Fan errors (200-229)
		errorsv1.MinerError_MINER_ERROR_FAN_FAILED: {
			Code:            errorsv1.MinerError_MINER_ERROR_FAN_FAILED,
			Name:            "Fan Failed",
			DefaultSummary:  "Cooling fan has stopped working",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Replace failed fan immediately",
			DefaultImpact:   "Miner will thermal throttle or shut down",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		},
		errorsv1.MinerError_MINER_ERROR_FAN_TACH_SIGNAL_LOST: {
			Code:            errorsv1.MinerError_MINER_ERROR_FAN_TACH_SIGNAL_LOST,
			Name:            "Fan Tach Signal Lost",
			DefaultSummary:  "Fan speed sensor signal not detected",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Check fan connection; may need replacement",
			DefaultImpact:   "Cannot verify fan operation",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		},
		errorsv1.MinerError_MINER_ERROR_FAN_SPEED_DEVIATION: {
			Code:            errorsv1.MinerError_MINER_ERROR_FAN_SPEED_DEVIATION,
			Name:            "Fan Speed Deviation",
			DefaultSummary:  "Fan speed differs significantly from target",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MINOR,
			DefaultAction:   "Monitor fan; may indicate wear or obstruction",
			DefaultImpact:   "Reduced cooling efficiency",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		},
		errorsv1.MinerError_MINER_ERROR_INLET_OVER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_INLET_OVER_TEMPERATURE,
			Name:            "Inlet Over Temperature",
			DefaultSummary:  "Ambient air temperature at inlet is too high",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Improve facility cooling; check HVAC system",
			DefaultImpact:   "Reduces cooling capacity; may cause throttling",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		},
		errorsv1.MinerError_MINER_ERROR_DEVICE_OVER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_DEVICE_OVER_TEMPERATURE,
			Name:            "Device Over Temperature",
			DefaultSummary:  "Device internal temperature exceeds safe limits",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Immediately reduce load or improve cooling",
			DefaultImpact:   "Stops mining to prevent damage",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		},
		errorsv1.MinerError_MINER_ERROR_DEVICE_UNDER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_DEVICE_UNDER_TEMPERATURE,
			Name:            "Device Under Temperature",
			DefaultSummary:  "Device temperature is below operating range",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Allow warmup; check ambient conditions",
			DefaultImpact:   "May affect reliability during startup",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		},

		// Hashboard / ASIC errors (300-349)
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_NOT_PRESENT: {
			Code:            errorsv1.MinerError_MINER_ERROR_HASHBOARD_NOT_PRESENT,
			Name:            "Hashboard Not Present",
			DefaultSummary:  "Hashboard is not detected",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check hashboard connection and seating",
			DefaultImpact:   "Reduces mining capacity by one board",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_OVER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_HASHBOARD_OVER_TEMPERATURE,
			Name:            "Hashboard Over Temperature",
			DefaultSummary:  "Hashboard temperature exceeds safe limits",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Improve cooling; check thermal paste and heatsinks",
			DefaultImpact:   "Board will throttle or shut down",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_MISSING_CHIPS: {
			Code:            errorsv1.MinerError_MINER_ERROR_HASHBOARD_MISSING_CHIPS,
			Name:            "Hashboard Missing Chips",
			DefaultSummary:  "Some ASIC chips on board are not responding",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Board may need repair or replacement",
			DefaultImpact:   "Reduced hashrate from affected board",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_ASIC_CHAIN_COMMUNICATION_LOST: {
			Code:            errorsv1.MinerError_MINER_ERROR_ASIC_CHAIN_COMMUNICATION_LOST,
			Name:            "ASIC Chain Communication Lost",
			DefaultSummary:  "Cannot communicate with ASIC chain",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Restart miner; check board connections",
			DefaultImpact:   "Affected hashboard is offline",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_ASIC_CLOCK_PLL_UNLOCKED: {
			Code:            errorsv1.MinerError_MINER_ERROR_ASIC_CLOCK_PLL_UNLOCKED,
			Name:            "ASIC Clock PLL Unlocked",
			DefaultSummary:  "ASIC clock phase-locked loop is not locked",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Restart miner; may indicate chip failure",
			DefaultImpact:   "Affected chips cannot hash correctly",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_ASIC_CRC_ERROR_EXCESSIVE: {
			Code:            errorsv1.MinerError_MINER_ERROR_ASIC_CRC_ERROR_EXCESSIVE,
			Name:            "ASIC CRC Error Excessive",
			DefaultSummary:  "High rate of CRC errors from ASIC chips",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Check board connections; may need repair",
			DefaultImpact:   "Reduced effective hashrate",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_ASIC_OVER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_HASHBOARD_ASIC_OVER_TEMPERATURE,
			Name:            "Hashboard ASIC Over Temperature",
			DefaultSummary:  "ASIC chip temperature exceeds safe limits",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Improve cooling immediately",
			DefaultImpact:   "Chip will throttle or shut down",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_ASIC_UNDER_TEMPERATURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_HASHBOARD_ASIC_UNDER_TEMPERATURE,
			Name:            "Hashboard ASIC Under Temperature",
			DefaultSummary:  "ASIC chip temperature is below operating range",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Allow warmup time",
			DefaultImpact:   "May affect reliability during startup",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},

		// Board-level power errors (350-369)
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_PGOOD_MISSING: {
			Code:            errorsv1.MinerError_MINER_ERROR_BOARD_POWER_PGOOD_MISSING,
			Name:            "Board Power Good Missing",
			DefaultSummary:  "Power good signal not received from board",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check board power connections",
			DefaultImpact:   "Board cannot operate",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_OVERCURRENT_TRIP: {
			Code:            errorsv1.MinerError_MINER_ERROR_BOARD_POWER_OVERCURRENT_TRIP,
			Name:            "Board Power Overcurrent Trip",
			DefaultSummary:  "Board power protection triggered due to overcurrent",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check for shorts; board may need repair",
			DefaultImpact:   "Board is disabled for protection",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_RAIL_UNDERVOLT: {
			Code:            errorsv1.MinerError_MINER_ERROR_BOARD_POWER_RAIL_UNDERVOLT,
			Name:            "Board Power Rail Undervolt",
			DefaultSummary:  "Board power rail voltage is too low",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Check power connections and PSU capacity",
			DefaultImpact:   "Board performance may be degraded",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_RAIL_OVERVOLT: {
			Code:            errorsv1.MinerError_MINER_ERROR_BOARD_POWER_RAIL_OVERVOLT,
			Name:            "Board Power Rail Overvolt",
			DefaultSummary:  "Board power rail voltage is too high",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check PSU output; risk of damage",
			DefaultImpact:   "Board may be damaged",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_BOARD_POWER_SHORT_DETECTED: {
			Code:            errorsv1.MinerError_MINER_ERROR_BOARD_POWER_SHORT_DETECTED,
			Name:            "Board Power Short Detected",
			DefaultSummary:  "Short circuit detected on board power",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Board needs immediate inspection and repair",
			DefaultImpact:   "Board is disabled; risk of damage",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},

		// Sensor errors (400-429)
		errorsv1.MinerError_MINER_ERROR_TEMP_SENSOR_OPEN_OR_SHORT: {
			Code:            errorsv1.MinerError_MINER_ERROR_TEMP_SENSOR_OPEN_OR_SHORT,
			Name:            "Temperature Sensor Open or Short",
			DefaultSummary:  "Temperature sensor circuit is open or shorted",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Sensor needs replacement",
			DefaultImpact:   "Cannot monitor temperature accurately",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_TEMP_SENSOR_FAULT: {
			Code:            errorsv1.MinerError_MINER_ERROR_TEMP_SENSOR_FAULT,
			Name:            "Temperature Sensor Fault",
			DefaultSummary:  "Temperature sensor is reporting invalid readings",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MINOR,
			DefaultAction:   "Check sensor connection; may need replacement",
			DefaultImpact:   "Temperature readings may be inaccurate",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_VOLTAGE_SENSOR_FAULT: {
			Code:            errorsv1.MinerError_MINER_ERROR_VOLTAGE_SENSOR_FAULT,
			Name:            "Voltage Sensor Fault",
			DefaultSummary:  "Voltage sensor is reporting invalid readings",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MINOR,
			DefaultAction:   "Check sensor; may need calibration or replacement",
			DefaultImpact:   "Voltage monitoring may be inaccurate",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_CURRENT_SENSOR_FAULT: {
			Code:            errorsv1.MinerError_MINER_ERROR_CURRENT_SENSOR_FAULT,
			Name:            "Current Sensor Fault",
			DefaultSummary:  "Current sensor is reporting invalid readings",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MINOR,
			DefaultAction:   "Check sensor; may need calibration or replacement",
			DefaultImpact:   "Current monitoring may be inaccurate",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},

		// Storage / Firmware errors (500-549)
		errorsv1.MinerError_MINER_ERROR_EEPROM_CRC_MISMATCH: {
			Code:            errorsv1.MinerError_MINER_ERROR_EEPROM_CRC_MISMATCH,
			Name:            "EEPROM CRC Mismatch",
			DefaultSummary:  "EEPROM data checksum verification failed",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Re-flash configuration; EEPROM may be failing",
			DefaultImpact:   "Device configuration may be corrupted",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_EEPROM,
		},
		errorsv1.MinerError_MINER_ERROR_EEPROM_READ_FAILURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_EEPROM_READ_FAILURE,
			Name:            "EEPROM Read Failure",
			DefaultSummary:  "Unable to read from EEPROM",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check EEPROM chip; may need replacement",
			DefaultImpact:   "Cannot load device configuration",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_EEPROM,
		},
		errorsv1.MinerError_MINER_ERROR_FIRMWARE_IMAGE_INVALID: {
			Code:            errorsv1.MinerError_MINER_ERROR_FIRMWARE_IMAGE_INVALID,
			Name:            "Firmware Image Invalid",
			DefaultSummary:  "Firmware image verification failed",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Re-flash firmware from known good source",
			DefaultImpact:   "Device may not operate correctly",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_FIRMWARE_CONFIG_INVALID: {
			Code:            errorsv1.MinerError_MINER_ERROR_FIRMWARE_CONFIG_INVALID,
			Name:            "Firmware Config Invalid",
			DefaultSummary:  "Firmware configuration is invalid or corrupted",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Reset to factory defaults or re-configure",
			DefaultImpact:   "Device may not operate as expected",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},

		// Control-plane errors (600-649)
		errorsv1.MinerError_MINER_ERROR_CONTROL_BOARD_COMMUNICATION_LOST: {
			Code:            errorsv1.MinerError_MINER_ERROR_CONTROL_BOARD_COMMUNICATION_LOST,
			Name:            "Control Board Communication Lost",
			DefaultSummary:  "Communication with control board has been lost",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Restart device; check control board connections",
			DefaultImpact:   "Device cannot be monitored or controlled",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_CONTROL_BOARD_FAILURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_CONTROL_BOARD_FAILURE,
			Name:            "Control Board Failure",
			DefaultSummary:  "Control board has failed",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Control board needs replacement",
			DefaultImpact:   "Device is non-operational",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_DEVICE_INTERNAL_BUS_FAULT: {
			Code:            errorsv1.MinerError_MINER_ERROR_DEVICE_INTERNAL_BUS_FAULT,
			Name:            "Device Internal Bus Fault",
			DefaultSummary:  "Internal communication bus has faulted",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Restart device; may need repair",
			DefaultImpact:   "Components cannot communicate",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_DEVICE_COMMUNICATION_LOST: {
			Code:            errorsv1.MinerError_MINER_ERROR_DEVICE_COMMUNICATION_LOST,
			Name:            "Device Communication Lost",
			DefaultSummary:  "Network communication with device has been lost",
			DefaultSeverity: errorsv1.Severity_SEVERITY_CRITICAL,
			DefaultAction:   "Check network connection and device power",
			DefaultImpact:   "Device cannot be monitored remotely",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_IO_MODULE_FAILURE: {
			Code:            errorsv1.MinerError_MINER_ERROR_IO_MODULE_FAILURE,
			Name:            "IO Module Failure",
			DefaultSummary:  "I/O module has failed",
			DefaultSeverity: errorsv1.Severity_SEVERITY_MAJOR,
			DefaultAction:   "Check I/O connections; module may need replacement",
			DefaultImpact:   "Some device I/O functions unavailable",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_IO_MODULE,
		},

		// Performance advisories (800-829)
		errorsv1.MinerError_MINER_ERROR_HASHRATE_BELOW_TARGET: {
			Code:            errorsv1.MinerError_MINER_ERROR_HASHRATE_BELOW_TARGET,
			Name:            "Hashrate Below Target",
			DefaultSummary:  "Current hashrate is below expected target",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Check for throttling, chip failures, or pool issues",
			DefaultImpact:   "Reduced mining revenue",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_HASHBOARD_WARN_CRC_HIGH: {
			Code:            errorsv1.MinerError_MINER_ERROR_HASHBOARD_WARN_CRC_HIGH,
			Name:            "Hashboard CRC Warning",
			DefaultSummary:  "CRC error rate is elevated but within tolerance",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Monitor for increase; may indicate developing issue",
			DefaultImpact:   "Slight reduction in effective hashrate",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD,
		},
		errorsv1.MinerError_MINER_ERROR_THERMAL_MARGIN_LOW: {
			Code:            errorsv1.MinerError_MINER_ERROR_THERMAL_MARGIN_LOW,
			Name:            "Thermal Margin Low",
			DefaultSummary:  "Temperature is approaching thermal limits",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Consider improving cooling",
			DefaultImpact:   "Risk of throttling if temperature rises",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_FAN,
		},

		// Catch-all (900-909)
		errorsv1.MinerError_MINER_ERROR_VENDOR_ERROR_UNMAPPED: {
			Code:            errorsv1.MinerError_MINER_ERROR_VENDOR_ERROR_UNMAPPED,
			Name:            "Vendor Error Unmapped",
			DefaultSummary:  "Vendor-specific error not yet mapped to canonical code",
			DefaultSeverity: errorsv1.Severity_SEVERITY_INFO,
			DefaultAction:   "Check vendor documentation for error details",
			DefaultImpact:   "Unknown; depends on vendor error",
			ComponentType:   errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED,
		},
	}
}

// GetComponentTypeForError returns the component type associated with a miner error.
func GetComponentTypeForError(code errorsv1.MinerError) errorsv1.ComponentType {
	metadata := BuildMinerErrorMetadata()
	if m, ok := metadata[code]; ok {
		return m.ComponentType
	}
	return errorsv1.ComponentType_COMPONENT_TYPE_UNSPECIFIED
}
