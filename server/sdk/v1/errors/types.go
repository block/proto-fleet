// Package errors provides the SDK error types for device error reporting.
package errors

import (
	"time"

	pb "github.com/btc-mining/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
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
type MinerError = pb.MinerError

const (
	MinerErrorUnspecified MinerError = pb.MinerError_MINER_ERROR_UNSPECIFIED

	// PSU
	PSUNotPresent          MinerError = pb.MinerError_PSU_NOT_PRESENT
	PSUModelMismatch       MinerError = pb.MinerError_PSU_MODEL_MISMATCH
	PSUCommunicationLost   MinerError = pb.MinerError_PSU_COMMUNICATION_LOST
	PSUFaultGeneric        MinerError = pb.MinerError_PSU_FAULT_GENERIC
	PSUInputVoltageLow     MinerError = pb.MinerError_PSU_INPUT_VOLTAGE_LOW
	PSUInputVoltageHigh    MinerError = pb.MinerError_PSU_INPUT_VOLTAGE_HIGH
	PSUOutputVoltageFault  MinerError = pb.MinerError_PSU_OUTPUT_VOLTAGE_FAULT
	PSUOutputOvercurrent   MinerError = pb.MinerError_PSU_OUTPUT_OVERCURRENT
	PSUFanFault            MinerError = pb.MinerError_PSU_FAN_FAULT
	PSUOverTemperature     MinerError = pb.MinerError_PSU_OVER_TEMPERATURE
	PSUInputPhaseImbalance MinerError = pb.MinerError_PSU_INPUT_PHASE_IMBALANCE
	PSUUnderTemperature    MinerError = pb.MinerError_PSU_UNDER_TEMPERATURE

	// Thermal & fans
	FanFailed              MinerError = pb.MinerError_FAN_FAILED
	FanTachSignalLost      MinerError = pb.MinerError_FAN_TACH_SIGNAL_LOST
	FanSpeedDeviation      MinerError = pb.MinerError_FAN_SPEED_DEVIATION
	InletOverTemperature   MinerError = pb.MinerError_INLET_OVER_TEMPERATURE
	DeviceOverTemperature  MinerError = pb.MinerError_DEVICE_OVER_TEMPERATURE
	DeviceUnderTemperature MinerError = pb.MinerError_DEVICE_UNDER_TEMPERATURE

	// Hashboard / ASIC chain & core digital
	HashboardNotPresent           MinerError = pb.MinerError_HASHBOARD_NOT_PRESENT
	HashboardOverTemperature      MinerError = pb.MinerError_HASHBOARD_OVER_TEMPERATURE
	HashboardMissingChips         MinerError = pb.MinerError_HASHBOARD_MISSING_CHIPS
	ASICChainCommunicationLost    MinerError = pb.MinerError_ASIC_CHAIN_COMMUNICATION_LOST
	ASICClockPLLUnlocked          MinerError = pb.MinerError_ASIC_CLOCK_PLL_UNLOCKED
	ASICCRCErrorExcessive         MinerError = pb.MinerError_ASIC_CRC_ERROR_EXCESSIVE
	HashboardASICOverTemperature  MinerError = pb.MinerError_HASHBOARD_ASIC_OVER_TEMPERATURE
	HashboardASICUnderTemperature MinerError = pb.MinerError_HASHBOARD_ASIC_UNDER_TEMPERATURE

	// Board-level power rails & protection
	BoardPowerPGOODMissing  MinerError = pb.MinerError_BOARD_POWER_PGOOD_MISSING
	BoardPowerOvercurrent   MinerError = pb.MinerError_BOARD_POWER_OVERCURRENT
	BoardPowerRailUndervolt MinerError = pb.MinerError_BOARD_POWER_RAIL_UNDERVOLT
	BoardPowerRailOvervolt  MinerError = pb.MinerError_BOARD_POWER_RAIL_OVERVOLT
	BoardPowerShortDetected MinerError = pb.MinerError_BOARD_POWER_SHORT_DETECTED

	// Sensors
	TempSensorOpenOrShort MinerError = pb.MinerError_TEMP_SENSOR_OPEN_OR_SHORT
	TempSensorFault       MinerError = pb.MinerError_TEMP_SENSOR_FAULT
	VoltageSensorFault    MinerError = pb.MinerError_VOLTAGE_SENSOR_FAULT
	CurrentSensorFault    MinerError = pb.MinerError_CURRENT_SENSOR_FAULT

	// Non-volatile storage / firmware
	EEPROMCRCMismatch     MinerError = pb.MinerError_EEPROM_CRC_MISMATCH
	EEPROMReadFailure     MinerError = pb.MinerError_EEPROM_READ_FAILURE
	FirmwareImageInvalid  MinerError = pb.MinerError_FIRMWARE_IMAGE_INVALID
	FirmwareConfigInvalid MinerError = pb.MinerError_FIRMWARE_CONFIG_INVALID

	// Control-plane & on-board comms
	ControlBoardCommunicationLost MinerError = pb.MinerError_CONTROL_BOARD_COMMUNICATION_LOST
	ControlBoardFailure           MinerError = pb.MinerError_CONTROL_BOARD_FAILURE
	DeviceInternalBusFault        MinerError = pb.MinerError_DEVICE_INTERNAL_BUS_FAULT
	DeviceCommunicationLost       MinerError = pb.MinerError_DEVICE_COMMUNICATION_LOST
	IOModuleFailure               MinerError = pb.MinerError_IO_MODULE_FAILURE

	// Performance advisories
	HashrateBelowTarget  MinerError = pb.MinerError_HASHRATE_BELOW_TARGET
	HashboardWarnCRCHigh MinerError = pb.MinerError_HASHBOARD_WARN_CRC_HIGH
	ThermalMarginLow     MinerError = pb.MinerError_THERMAL_MARGIN_LOW

	// Catch-alls
	VendorErrorUnmapped MinerError = pb.MinerError_VENDOR_ERROR_UNMAPPED
)

// Severity represents the criticality level of an error
type Severity = pb.Severity

const (
	SeverityUnspecified Severity = pb.Severity_SEVERITY_UNSPECIFIED
	SeverityCritical    Severity = pb.Severity_SEVERITY_CRITICAL // Miner stops hashing or unsafe
	SeverityMajor       Severity = pb.Severity_SEVERITY_MAJOR    // Degraded hashing / imminent trip
	SeverityMinor       Severity = pb.Severity_SEVERITY_MINOR    // Recoverable, limited effect
	SeverityInfo        Severity = pb.Severity_SEVERITY_INFO     // Informational / advisory
)

// DeviceError represents an error reported by a plugin for a device.
// This is the plugin-facing error type without fleet-managed ErrorID.
// Plugins populate this type and return it from GetErrors().
// The fleet server then constructs ErrorMessage by adding ErrorID.
type DeviceError struct {
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
	Impact            string            // Human-readable business impact (e.g., "Stops mining", "Reduces hashrate by 30%")
	Summary           string            // High level summary - typically raw message from miner
}

func (de DeviceError) ToErrorMessage(errorID string) ErrorMessage {
	return ErrorMessage{
		ErrorID:           errorID,
		MinerError:        de.MinerError,
		CauseSummary:      de.CauseSummary,
		RecommendedAction: de.RecommendedAction,
		Severity:          de.Severity,
		FirstSeenAt:       de.FirstSeenAt,
		LastSeenAt:        de.LastSeenAt,
		ClosedAt:          de.ClosedAt,
		VendorAttributes:  de.VendorAttributes,
		DeviceID:          de.DeviceID,
		ComponentID:       de.ComponentID,
		Impact:            de.Impact,
		Summary:           de.Summary,
	}
}

type ErrorMessage struct {
	ErrorID           string            // ULID (globally unique, time-sortable)
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
	Impact            string            // Human-readable business impact (e.g., "Stops mining", "Reduces hashrate by 30%")
	Summary           string            // High level summary - typically raw message from miner
}

// DeviceErrors contains all plugin-reported errors for a specific device.
// This is returned by plugin GetErrors() calls and contains DeviceError instances.
type DeviceErrors struct {
	DeviceID string
	Errors   []DeviceError
}

// ============================================================================
// Conversion Functions - SDK <-> Protobuf
// ============================================================================

// DeviceErrorFromProto converts protobuf ErrorMessage to SDK DeviceError.
// This strips the fleet-managed ErrorID field from the protobuf message.
func DeviceErrorFromProto(pb *pb.ErrorMessage) DeviceError {
	if pb == nil {
		return DeviceError{}
	}

	var firstSeenAt, lastSeenAt time.Time
	if pb.FirstSeenAt != nil {
		firstSeenAt = pb.FirstSeenAt.AsTime()
	}
	if pb.LastSeenAt != nil {
		lastSeenAt = pb.LastSeenAt.AsTime()
	}

	var closedAt *time.Time
	if pb.ClosedAt != nil {
		t := pb.ClosedAt.AsTime()
		closedAt = &t
	}

	var componentID *string
	if pb.ComponentId != nil {
		componentID = pb.ComponentId
	}

	return DeviceError{
		MinerError:        pb.MinerError,
		CauseSummary:      pb.CauseSummary,
		RecommendedAction: pb.RecommendedAction,
		Severity:          pb.Severity,
		FirstSeenAt:       firstSeenAt,
		LastSeenAt:        lastSeenAt,
		ClosedAt:          closedAt,
		VendorAttributes:  pb.VendorAttributes,
		DeviceID:          pb.DeviceId,
		ComponentID:       componentID,
		Impact:            pb.Impact,
		Summary:           pb.Summary,
	}
}

// ToProto converts SDK ErrorMessage to protobuf
func (e ErrorMessage) ToProto() *pb.ErrorMessage {
	pbErr := &pb.ErrorMessage{
		MinerError:        e.MinerError,
		CauseSummary:      e.CauseSummary,
		RecommendedAction: e.RecommendedAction,
		Severity:          e.Severity,
		FirstSeenAt:       timestamppb.New(e.FirstSeenAt),
		LastSeenAt:        timestamppb.New(e.LastSeenAt),
		VendorAttributes:  e.VendorAttributes,
		DeviceId:          e.DeviceID,
		Impact:            e.Impact,
		Summary:           e.Summary,
	}

	// Optional error ID (empty when sent from plugins, populated by fleet)
	if e.ErrorID != "" {
		pbErr.ErrorId = &e.ErrorID
	}

	if e.ClosedAt != nil {
		pbErr.ClosedAt = timestamppb.New(*e.ClosedAt)
	}

	if e.ComponentID != nil {
		pbErr.ComponentId = e.ComponentID
	}

	return pbErr
}

// FromProto converts protobuf ErrorMessage to SDK type
func FromProto(pb *pb.ErrorMessage) ErrorMessage {
	if pb == nil {
		return ErrorMessage{}
	}

	// Required timestamp fields
	var firstSeenAt, lastSeenAt time.Time
	if pb.FirstSeenAt != nil {
		firstSeenAt = pb.FirstSeenAt.AsTime()
	}
	if pb.LastSeenAt != nil {
		lastSeenAt = pb.LastSeenAt.AsTime()
	}

	// Optional timestamp field
	var closedAt *time.Time
	if pb.ClosedAt != nil {
		t := pb.ClosedAt.AsTime()
		closedAt = &t
	}

	// Optional component ID
	var componentID *string
	if pb.ComponentId != nil {
		componentID = pb.ComponentId
	}

	// Optional error ID (may be empty from plugins, populated by fleet)
	var errorID string
	if pb.ErrorId != nil {
		errorID = *pb.ErrorId
	}

	return ErrorMessage{
		ErrorID:           errorID,
		MinerError:        pb.MinerError,
		CauseSummary:      pb.CauseSummary,
		RecommendedAction: pb.RecommendedAction,
		Severity:          pb.Severity,
		FirstSeenAt:       firstSeenAt,
		LastSeenAt:        lastSeenAt,
		ClosedAt:          closedAt,
		VendorAttributes:  pb.VendorAttributes,
		DeviceID:          pb.DeviceId,
		ComponentID:       componentID,
		Impact:            pb.Impact,
		Summary:           pb.Summary,
	}
}

// DeviceErrorsFromProto converts protobuf DeviceErrors to SDK DeviceErrors.
// This strips fleet-managed fields (ErrorID, DeviceID) from each error.
func DeviceErrorsFromProto(pb *pb.DeviceErrors) DeviceErrors {
	if pb == nil {
		return DeviceErrors{}
	}

	errors := make([]DeviceError, len(pb.Errors))
	for i, pbErr := range pb.Errors {
		errors[i] = DeviceErrorFromProto(pbErr)
	}

	return DeviceErrors{
		DeviceID: pb.DeviceId,
		Errors:   errors,
	}
}
