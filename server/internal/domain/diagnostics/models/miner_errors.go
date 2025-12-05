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

// ComponentType represents the type of hardware component associated with an error
type ComponentType = uint

const (
	ComponentTypeUnspecified  ComponentType = 0
	ComponentTypeControlBoard ComponentType = 1
	ComponentTypeFans         ComponentType = 2
	ComponentTypeHashBoards   ComponentType = 3
	ComponentTypePSU          ComponentType = 4
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
	DeviceID          string            // Device this error belongs to
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
