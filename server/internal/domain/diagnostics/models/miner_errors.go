package models

import (
	"time"
)

// ============================================================================
// SDK Error Types
// ============================================================================

// MinerError represents the standardized classification of device errors.
// These codes are identical between SDK and internal server representations.
// Miner-agnostic naming:
// - PSU & facility power at PSU terminals
// - Thermal & fans
// - Board/ASIC chain & hash performance
// - Board-level power rails & protection (distinct from PSU)
// - Sensors
// - Non-volatile storage / firmware
// - Control-plane & on-board comms
// - Performance advisories (non-fatal)
// - Catch-alls / vendor-unknown
type MinerError uint

const (
	MinerErrorUnspecified MinerError = 0

	// PSU
	PSUNotPresent          MinerError = 1000
	PSUModelMismatch       MinerError = 1001
	PSUCommunicationLost   MinerError = 1002
	PSUFaultGeneric        MinerError = 1003
	PSUInputVoltageLow     MinerError = 1004
	PSUInputVoltageHigh    MinerError = 1005
	PSUOutputVoltageFault  MinerError = 1006
	PSUOutputOvercurrent   MinerError = 1007
	PSUFanFault            MinerError = 1008
	PSUOverTemperature     MinerError = 1009
	PSUInputPhaseImbalance MinerError = 1010
	PSUUnderTemperature    MinerError = 1011

	// Thermal & fans
	FanFailed              MinerError = 2000
	FanTachSignalLost      MinerError = 2001
	FanSpeedDeviation      MinerError = 2002
	InletOverTemperature   MinerError = 2010
	DeviceOverTemperature  MinerError = 2011
	DeviceUnderTemperature MinerError = 2012

	// Hashboard / ASIC chain & core digital
	HashboardNotPresent           MinerError = 3000
	HashboardOverTemperature      MinerError = 3001
	HashboardMissingChips         MinerError = 3002
	ASICChainCommunicationLost    MinerError = 3003
	ASICClockPLLUnlocked          MinerError = 3004
	ASICCRCErrorExcessive         MinerError = 3005
	HashboardASICOverTemperature  MinerError = 3006
	HashboardASICUnderTemperature MinerError = 3007

	// Board-level power rails & protection
	BoardPowerPGOODMissing  MinerError = 3500
	BoardPowerOvercurrent   MinerError = 3501
	BoardPowerRailUndervolt MinerError = 3502
	BoardPowerRailOvervolt  MinerError = 3503
	BoardPowerShortDetected MinerError = 3504

	// Sensors
	TempSensorOpenOrShort MinerError = 4000
	TempSensorFault       MinerError = 4001
	VoltageSensorFault    MinerError = 4002
	CurrentSensorFault    MinerError = 4003

	// Non-volatile storage / firmware
	EEPROMCRCMismatch     MinerError = 5000
	EEPROMReadFailure     MinerError = 5001
	FirmwareImageInvalid  MinerError = 5002
	FirmwareConfigInvalid MinerError = 5003

	// Control-plane & on-board comms
	ControlBoardCommunicationLost MinerError = 6000
	ControlBoardFailure           MinerError = 6001
	DeviceInternalBusFault        MinerError = 6002
	DeviceCommunicationLost       MinerError = 6003
	IOModuleFailure               MinerError = 6010

	// Performance advisories
	HashrateBelowTarget  MinerError = 8000
	HashboardWarnCRCHigh MinerError = 8001
	ThermalMarginLow     MinerError = 8002

	// Catch-alls
	VendorErrorUnmapped MinerError = 9000
)

// Severity represents the criticality level of an error
type Severity = uint

const (
	SeverityUnspecified Severity = 0
	SeverityCritical    Severity = 1 // Miner stops hashing or unsafe
	SeverityMajor       Severity = 2 // Degraded hashing / imminent trip
	SeverityMinor       Severity = 3 // Recoverable, limited effect
	SeverityInfo        Severity = 4 // Informational / advisory
)

// ComponentType represents the type of hardware component associated with an error.
// This is kept as a domain type separate from proto to control which component types
// we support in the domain logic.
type ComponentType uint

// Component type constants - matching proto enum values for supported types only
const (
	ComponentTypeUnspecified  ComponentType = 0
	ComponentTypePSU          ComponentType = 1
	ComponentTypeHashBoards   ComponentType = 2
	ComponentTypeFans         ComponentType = 3
	ComponentTypeControlBoard ComponentType = 4
	// Note: EEPROM (5) and IO_MODULE (6) are not yet supported in the domain
)

// ErrorMessage represents a fleet-tracked miner error.
// This type includes fleet-managed fields (ErrorID) that are assigned
// when errors are persisted to the database.
type ErrorMessage struct {
	ErrorID           string            // ULID (time-sortable, assigned by Store on insert)
	MinerError        MinerError        // REQUIRED
	CauseSummary      string            // Human-readable short cause
	RecommendedAction string            // Next best action
	Severity          Severity          // Technical severity classification
	FirstSeenAt       time.Time         // When error was first observed
	LastSeenAt        time.Time         // When error was last observed
	ClosedAt          *time.Time        // Optional closed/expired error
	VendorAttributes  map[string]string // e.g., firmware, code, serials
	DeviceID          string            // Device identifier this error belongs to
	DeviceType        string            // Model name (e.g., "S19", "R2") - populated from discovered_device
	ComponentID       *string           // Optional component identifier
	ComponentType     ComponentType     // Type of hardware component (hashboard, fan, PSU, etc.)
	Impact            string            // Human-readable business impact (e.g., "Stops mining", "Reduces hashrate by 30%")
	Summary           string            // High level summary - typically raw message from miner
	VendorCode        string            // Vendor-specific error code (extracted from VendorAttributes)
	Firmware          string            // Firmware version when error occurred (extracted from VendorAttributes)
}

// DeviceErrors contains all plugin-reported errors for a specific device.
// This is returned by plugin GetErrors() calls and contains DeviceError instances.
type DeviceErrors struct {
	DeviceID string
	Errors   []ErrorMessage
}

// MinerErrorInfo provides default metadata for a canonical miner error code.
type MinerErrorInfo struct {
	Name            string
	DefaultSummary  string
	DefaultSeverity Severity
	DefaultAction   string
	DefaultImpact   string
}

var minerErrorInfo = map[MinerError]MinerErrorInfo{
	// PSU errors (1000-1999)
	PSUNotPresent: {
		Name:            "PSU Not Present",
		DefaultSummary:  "Power supply unit is not detected",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check PSU installation and connections",
		DefaultImpact:   "Miner cannot operate without power supply",
	},
	PSUModelMismatch: {
		Name:            "PSU Model Mismatch",
		DefaultSummary:  "PSU model does not match expected configuration",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Verify PSU model matches device requirements",
		DefaultImpact:   "May cause power delivery issues or reduced performance",
	},
	PSUCommunicationLost: {
		Name:            "PSU Communication Lost",
		DefaultSummary:  "Communication with PSU has been lost",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Check PSU connection cables and restart device",
		DefaultImpact:   "Cannot monitor PSU status or adjust power settings",
	},
	PSUFaultGeneric: {
		Name:            "PSU Fault",
		DefaultSummary:  "General PSU fault detected",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Inspect PSU for damage or overheating",
		DefaultImpact:   "Power delivery may be compromised",
	},
	PSUInputVoltageLow: {
		Name:            "PSU Input Voltage Low",
		DefaultSummary:  "Input voltage to PSU is below acceptable range",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Check facility power supply and voltage levels",
		DefaultImpact:   "May cause power instability or shutdown",
	},
	PSUInputVoltageHigh: {
		Name:            "PSU Input Voltage High",
		DefaultSummary:  "Input voltage to PSU is above acceptable range",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Immediately check facility power; may damage equipment",
		DefaultImpact:   "Risk of equipment damage",
	},
	PSUOutputVoltageFault: {
		Name:            "PSU Output Voltage Fault",
		DefaultSummary:  "PSU output voltage is outside acceptable range",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Replace PSU immediately",
		DefaultImpact:   "Miner may shut down or sustain damage",
	},
	PSUOutputOvercurrent: {
		Name:            "PSU Output Overcurrent",
		DefaultSummary:  "PSU output current exceeds safe limits",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check for short circuits; reduce load or replace PSU",
		DefaultImpact:   "Stops mining to prevent equipment damage",
	},
	PSUFanFault: {
		Name:            "PSU Fan Failed",
		DefaultSummary:  "Cooling fan in PSU has failed",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Replace PSU; risk of overheating",
		DefaultImpact:   "PSU may overheat and shut down",
	},
	PSUOverTemperature: {
		Name:            "PSU Over Temperature",
		DefaultSummary:  "PSU temperature exceeds safe operating range",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Improve cooling; check PSU fan and ambient temperature",
		DefaultImpact:   "May cause thermal shutdown",
	},
	PSUInputPhaseImbalance: {
		Name:            "PSU Input Phase Imbalance",
		DefaultSummary:  "Three-phase power input is imbalanced",
		DefaultSeverity: SeverityMinor,
		DefaultAction:   "Check facility power distribution",
		DefaultImpact:   "Reduced efficiency; potential for long-term damage",
	},
	PSUUnderTemperature: {
		Name:            "PSU Under Temperature",
		DefaultSummary:  "PSU temperature is below operating range",
		DefaultSeverity: SeverityInfo,
		DefaultAction:   "Allow warmup time; check ambient conditions",
		DefaultImpact:   "May affect startup reliability",
	},

	// Thermal & Fan errors (2000-2999)
	FanFailed: {
		Name:            "Fan Failed",
		DefaultSummary:  "Cooling fan has stopped working",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Replace failed fan immediately",
		DefaultImpact:   "Miner will thermal throttle or shut down",
	},
	FanTachSignalLost: {
		Name:            "Fan Tach Signal Lost",
		DefaultSummary:  "Fan speed sensor signal not detected",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Check fan connection; may need replacement",
		DefaultImpact:   "Cannot verify fan operation",
	},
	FanSpeedDeviation: {
		Name:            "Fan Speed Deviation",
		DefaultSummary:  "Fan speed differs significantly from target",
		DefaultSeverity: SeverityMinor,
		DefaultAction:   "Monitor fan; may indicate wear or obstruction",
		DefaultImpact:   "Reduced cooling efficiency",
	},
	InletOverTemperature: {
		Name:            "Inlet Over Temperature",
		DefaultSummary:  "Ambient air temperature at inlet is too high",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Improve facility cooling; check HVAC system",
		DefaultImpact:   "Reduces cooling capacity; may cause throttling",
	},
	DeviceOverTemperature: {
		Name:            "Device Over Temperature",
		DefaultSummary:  "Device internal temperature exceeds safe limits",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Immediately reduce load or improve cooling",
		DefaultImpact:   "Stops mining to prevent damage",
	},
	DeviceUnderTemperature: {
		Name:            "Device Under Temperature",
		DefaultSummary:  "Device temperature is below operating range",
		DefaultSeverity: SeverityInfo,
		DefaultAction:   "Allow warmup; check ambient conditions",
		DefaultImpact:   "May affect reliability during startup",
	},

	// Hashboard / ASIC errors (3000-3999)
	HashboardNotPresent: {
		Name:            "Hashboard Not Present",
		DefaultSummary:  "Hashboard is not detected",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check hashboard connection and seating",
		DefaultImpact:   "Reduces mining capacity by one board",
	},
	HashboardOverTemperature: {
		Name:            "Hashboard Over Temperature",
		DefaultSummary:  "Hashboard temperature exceeds safe limits",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Improve cooling; check thermal paste and heatsinks",
		DefaultImpact:   "Board will throttle or shut down",
	},
	HashboardMissingChips: {
		Name:            "Hashboard Missing Chips",
		DefaultSummary:  "Some ASIC chips on board are not responding",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Inspect board; may need repair or replacement",
		DefaultImpact:   "Reduced hashrate from affected board",
	},
	ASICChainCommunicationLost: {
		Name:            "ASIC Chain Communication Lost",
		DefaultSummary:  "Cannot communicate with ASIC chain",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Restart miner; check board connections",
		DefaultImpact:   "Affected hashboard is offline",
	},
	ASICClockPLLUnlocked: {
		Name:            "ASIC Clock PLL Unlocked",
		DefaultSummary:  "ASIC clock phase-locked loop is not locked",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Restart miner; may indicate chip failure",
		DefaultImpact:   "Affected chips cannot hash correctly",
	},
	ASICCRCErrorExcessive: {
		Name:            "ASIC CRC Error Excessive",
		DefaultSummary:  "High rate of CRC errors from ASIC chips",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Check board connections; may need repair",
		DefaultImpact:   "Reduced effective hashrate",
	},
	HashboardASICOverTemperature: {
		Name:            "Hashboard ASIC Over Temperature",
		DefaultSummary:  "ASIC chip temperature exceeds safe limits",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Improve cooling immediately",
		DefaultImpact:   "Chip will throttle or shut down",
	},
	HashboardASICUnderTemperature: {
		Name:            "Hashboard ASIC Under Temperature",
		DefaultSummary:  "ASIC chip temperature is below operating range",
		DefaultSeverity: SeverityInfo,
		DefaultAction:   "Allow warmup time",
		DefaultImpact:   "May affect reliability during startup",
	},

	// Board-level power errors (3500-3999)
	BoardPowerPGOODMissing: {
		Name:            "Board Power Good Missing",
		DefaultSummary:  "Power good signal not received from board",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check board power connections",
		DefaultImpact:   "Board cannot operate",
	},
	BoardPowerOvercurrent: {
		Name:            "Board Power Overcurrent Trip",
		DefaultSummary:  "Board power protection triggered due to overcurrent",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check for shorts; board may need repair",
		DefaultImpact:   "Board is disabled for protection",
	},
	BoardPowerRailUndervolt: {
		Name:            "Board Power Rail Undervolt",
		DefaultSummary:  "Board power rail voltage is too low",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Check power connections and PSU capacity",
		DefaultImpact:   "Board performance may be degraded",
	},
	BoardPowerRailOvervolt: {
		Name:            "Board Power Rail Overvolt",
		DefaultSummary:  "Board power rail voltage is too high",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check PSU output; risk of damage",
		DefaultImpact:   "Board may be damaged",
	},
	BoardPowerShortDetected: {
		Name:            "Board Power Short Detected",
		DefaultSummary:  "Short circuit detected on board power",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Inspect board immediately; needs repair",
		DefaultImpact:   "Board is disabled; risk of damage",
	},

	// Sensor errors (4000-4999)
	TempSensorOpenOrShort: {
		Name:            "Temperature Sensor Open or Short",
		DefaultSummary:  "Temperature sensor circuit is open or shorted",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Replace sensor",
		DefaultImpact:   "Cannot monitor temperature accurately",
	},
	TempSensorFault: {
		Name:            "Temperature Sensor Fault",
		DefaultSummary:  "Temperature sensor is reporting invalid readings",
		DefaultSeverity: SeverityMinor,
		DefaultAction:   "Check sensor connection; may need replacement",
		DefaultImpact:   "Temperature readings may be inaccurate",
	},
	VoltageSensorFault: {
		Name:            "Voltage Sensor Fault",
		DefaultSummary:  "Voltage sensor is reporting invalid readings",
		DefaultSeverity: SeverityMinor,
		DefaultAction:   "Check sensor; may need calibration or replacement",
		DefaultImpact:   "Voltage monitoring may be inaccurate",
	},
	CurrentSensorFault: {
		Name:            "Current Sensor Fault",
		DefaultSummary:  "Current sensor is reporting invalid readings",
		DefaultSeverity: SeverityMinor,
		DefaultAction:   "Check sensor; may need calibration or replacement",
		DefaultImpact:   "Current monitoring may be inaccurate",
	},

	// Storage / Firmware errors (5000-5999)
	EEPROMCRCMismatch: {
		Name:            "EEPROM CRC Mismatch",
		DefaultSummary:  "EEPROM data checksum verification failed",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Re-flash configuration; EEPROM may be failing",
		DefaultImpact:   "Device configuration may be corrupted",
	},
	EEPROMReadFailure: {
		Name:            "EEPROM Read Failure",
		DefaultSummary:  "Unable to read from EEPROM",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check EEPROM chip; may need replacement",
		DefaultImpact:   "Cannot load device configuration",
	},
	FirmwareImageInvalid: {
		Name:            "Firmware Image Invalid",
		DefaultSummary:  "Firmware image verification failed",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Re-flash firmware from known good source",
		DefaultImpact:   "Device may not operate correctly",
	},
	FirmwareConfigInvalid: {
		Name:            "Firmware Config Invalid",
		DefaultSummary:  "Firmware configuration is invalid or corrupted",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Reset to factory defaults or re-configure",
		DefaultImpact:   "Device may not operate as expected",
	},

	// Control-plane errors (6000-6999)
	ControlBoardCommunicationLost: {
		Name:            "Control Board Communication Lost",
		DefaultSummary:  "Communication with control board has been lost",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Restart device; check control board connections",
		DefaultImpact:   "Device cannot be monitored or controlled",
	},
	ControlBoardFailure: {
		Name:            "Control Board Failure",
		DefaultSummary:  "Control board has failed",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Replace control board",
		DefaultImpact:   "Device is non-operational",
	},
	DeviceInternalBusFault: {
		Name:            "Device Internal Bus Fault",
		DefaultSummary:  "Internal communication bus has faulted",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Restart device; may need repair",
		DefaultImpact:   "Components cannot communicate",
	},
	DeviceCommunicationLost: {
		Name:            "Device Communication Lost",
		DefaultSummary:  "Network communication with device has been lost",
		DefaultSeverity: SeverityCritical,
		DefaultAction:   "Check network connection and device power",
		DefaultImpact:   "Device cannot be monitored remotely",
	},
	IOModuleFailure: {
		Name:            "IO Module Failure",
		DefaultSummary:  "I/O module has failed",
		DefaultSeverity: SeverityMajor,
		DefaultAction:   "Check I/O connections; module may need replacement",
		DefaultImpact:   "Some device I/O functions unavailable",
	},

	// Performance advisories (8000-8999)
	HashrateBelowTarget: {
		Name:            "Hashrate Below Target",
		DefaultSummary:  "Current hashrate is below expected target",
		DefaultSeverity: SeverityInfo,
		DefaultAction:   "Check for throttling, chip failures, or pool issues",
		DefaultImpact:   "Reduced mining revenue",
	},
	HashboardWarnCRCHigh: {
		Name:            "Hashboard CRC Warning",
		DefaultSummary:  "CRC error rate is elevated but within tolerance",
		DefaultSeverity: SeverityInfo,
		DefaultAction:   "Monitor for increase; may indicate developing issue",
		DefaultImpact:   "Slight reduction in effective hashrate",
	},
	ThermalMarginLow: {
		Name:            "Thermal Margin Low",
		DefaultSummary:  "Temperature is approaching thermal limits",
		DefaultSeverity: SeverityInfo,
		DefaultAction:   "Consider improving cooling",
		DefaultImpact:   "Risk of throttling if temperature rises",
	},

	// Catch-all (9000-9999)
	VendorErrorUnmapped: {
		Name:            "Vendor Error Unmapped",
		DefaultSummary:  "Vendor-specific error not yet mapped to canonical code",
		DefaultSeverity: SeverityInfo,
		DefaultAction:   "Check vendor documentation for error details",
		DefaultImpact:   "Unknown; depends on vendor error",
	},
}

// GetMinerErrorInfo returns default metadata for all miner error codes.
// This provides a comprehensive reference for all canonical error codes
// supported by the fleet system.
func GetMinerErrorInfo() map[MinerError]MinerErrorInfo {
	return minerErrorInfo
}

// MinerError code range boundaries for component type mapping.
// Max values use range boundaries (e.g., 1999 instead of 1011) so new error codes
// within a category are automatically mapped without requiring constant updates.
const (
	psuErrorMin          MinerError = 1000
	psuErrorMax          MinerError = 1999
	fanThermalErrorMin   MinerError = 2000
	fanThermalErrorMax   MinerError = 2999
	hashboardErrorMin    MinerError = 3000
	hashboardErrorMax    MinerError = 3499
	boardPowerErrorMin   MinerError = 3500
	boardPowerErrorMax   MinerError = 3999
	controlPlaneErrorMin MinerError = 6000
	controlPlaneErrorMax MinerError = 6999
)

// DefaultComponentTypeForMinerError returns the default ComponentType for a MinerError.
// Used when ingesting errors that don't specify a component type.
func DefaultComponentTypeForMinerError(err MinerError) ComponentType {
	switch {
	case err >= psuErrorMin && err <= psuErrorMax:
		return ComponentTypePSU
	case err >= fanThermalErrorMin && err <= fanThermalErrorMax:
		return ComponentTypeFans
	case err >= hashboardErrorMin && err <= hashboardErrorMax:
		return ComponentTypeHashBoards
	case err >= boardPowerErrorMin && err <= boardPowerErrorMax:
		return ComponentTypeHashBoards
	case err >= controlPlaneErrorMin && err <= controlPlaneErrorMax:
		return ComponentTypeControlBoard
	default:
		return ComponentTypeUnspecified
	}
}
