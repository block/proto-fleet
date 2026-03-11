package device

import (
	"fmt"
	"math"
	"time"

	"github.com/proto-at-block/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/proto-at-block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/proto-at-block/proto-fleet/server/sdk/v1/errors"
)

const (
	impactUnableToStartMining        = "Unable to start mining"
	impactReducedHashrateBoardShut   = "Reduced hashrate, board shutdowns"
	impactBayShutdown                = "Bay shutdown"
	actionRetestReplace              = "Retest, replace if consistent failure"
	actionCheckPSURetestReplace      = "Check for PSU errors, retest, replace if consistent failure"
	actionCheckFanCoolingReplace     = "Check fan and cooling, replace if consistent failure"
	actionCheckACConnectorsReplace   = "Check AC connectors, replace if consistent failure"
	actionRetestVerifyPSUFansReplace = "Retest and verify PSU fans work, replace if consistent failure"
)

type errorMapping struct {
	minerError        sdkerrors.MinerError
	severity          sdkerrors.Severity
	causeSummary      string
	recommendedAction string
	impact            string
}

// Rig error mappings keyed by error_code string
var rigErrorMappings = map[string]errorMapping{
	"LowHashRate": {
		minerError:        sdkerrors.HashrateBelowTarget,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Reported hashrate won't match hashrate reported by the pool",
		recommendedAction: "Check the network connection to the pool and restart the miner",
		impact:            "Reduced mining revenue",
	},
	"OverHeat": {
		minerError:        sdkerrors.DeviceOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "System cooling failure",
		recommendedAction: "Inspect fans for sufficient cooling",
		impact:            "System shutdown",
	},
	"InsufficientCooling": {
		minerError:        sdkerrors.FanFailed,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Not enough fans or critical cooling fan not operating correctly",
		recommendedAction: "Check fan connections and cooling, hashboard harness/connections, replace fan module if consistent failure. Check hashboard configuration (hashboards must be installed in slots (1,3,4,6,7,9))",
		impact:            "Hashboards in affected bay are unable to start mining",
	},
	"PoolConnectionFailure": {
		minerError:        sdkerrors.DeviceCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Incorrect pool URL, bad credentials, pool communication failure",
		recommendedAction: "Update mining pool. Update firmware if failure persists",
		impact:            impactUnableToStartMining,
	},
	"PoolConfigMissing": {
		minerError:        sdkerrors.FirmwareConfigInvalid,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "There is no mining pool configuration",
		recommendedAction: "Update mining pool configuration",
		impact:            impactUnableToStartMining,
	},
	"MiningStoppedDueToPhaseImbalance": {
		minerError:        sdkerrors.PSUInputPhaseImbalance,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Full bay failure when phase balancing is enabled",
		recommendedAction: "Address root cause of the bay failure (PSU, hashboards, etc)",
		impact:            "Mining Stopped, will not restart until the bay is repaired",
	},
	"PSURecoveryInProgress": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "It may indicate an issue in the LLC section of that PSU",
		recommendedAction: "Check for PSU errors, retest, replace if consistent failure. Check fan and cooling, replace if consistent failure",
		impact:            "Hashboards in affected bay are unable to start mining",
	},
	"NetworkError": {
		minerError:        sdkerrors.DeviceCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Damaged ethernet cable or tray",
		recommendedAction: "Inspect IO module, ethernet cable. Replace if consistent failure",
		impact:            "Unable to connect to mining pool",
	},
	"FirmwareUpdateFailure": {
		minerError:        sdkerrors.FirmwareImageInvalid,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Invalid/corrupt firmware image",
		recommendedAction: "Factory reset then update firmware, replace if consistent failure",
		impact:            "Unable to update firmware",
	},
}

// Fan error mappings
var fanErrorMappings = map[string]errorMapping{
	"FanHardware": {
		minerError:        sdkerrors.FanFailed,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Unseated/damaged connector, missing power",
		recommendedAction: "Replace fan, inspect fan module wiring if consistent failure",
		impact:            "Miner overheating",
	},
	"FanSlow": {
		minerError:        sdkerrors.FanSpeedDeviation,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Mechanical damage, dust, worn motor, Connector/cable issue",
		recommendedAction: "Replace fan module",
		impact:            "Miner overheating",
	},
	"SetFanSpeedFailed": {
		minerError:        sdkerrors.FanSpeedDeviation,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "Failed to set fan speed",
		recommendedAction: "Monitor fan control system",
		impact:            "Cannot adjust cooling dynamically",
	},
	"FanConnectedInImmersion": {
		minerError:        sdkerrors.VendorErrorUnmapped,
		severity:          sdkerrors.SeverityInfo,
		causeSummary:      "Remove fans for immersion cooling",
		recommendedAction: "Remove fans for immersion mode",
		impact:            "Mining Disabled",
	},
}

// Hashboard error mappings
var hashboardErrorMappings = map[string]errorMapping{
	"HbOverHeat": {
		minerError:        sdkerrors.HashboardOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Insufficient cooling",
		recommendedAction: actionCheckFanCoolingReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"AsicEnumeration": {
		minerError:        sdkerrors.HashboardMissingChips,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Damaged fuse, clock crystal, MCU, or faulty ASIC",
		recommendedAction: actionRetestReplace,
		impact:            impactUnableToStartMining,
	},
	"HbCommunication": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"CommTasksNotInitialized": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"InvalidAddWorkResponse": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"InvalidMetadataResponse": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"MissingCommChannel": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"CommandTimeout": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"TaskCommunicationError": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"CommsAlive": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"AsicEcc": {
		minerError:        sdkerrors.ASICCRCErrorExcessive,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure, software bug",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"HbUnderVoltage": {
		minerError:        sdkerrors.BoardPowerRailUndervolt,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Voltage droop",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"HbOverVoltage": {
		minerError:        sdkerrors.BoardPowerRailOvervolt,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "PSU spike",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"HbOverCurrent": {
		minerError:        sdkerrors.BoardPowerOvercurrent,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "PSU spike, ASIC malfunction",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"AsicOverHeat": {
		minerError:        sdkerrors.HashboardASICOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "TIM wearout",
		recommendedAction: actionCheckFanCoolingReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"AsicUnderHeat": {
		minerError:        sdkerrors.HashboardASICUnderTemperature,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "ASIC outside of operating temperature, ambient too cold",
		recommendedAction: actionRetestReplace,
		impact:            impactReducedHashrateBoardShut,
	},
	"AsicNotHashing": {
		minerError:        sdkerrors.HashrateBelowTarget,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "The ASIC may get stuck while SLINK remains functional; this could be due to unfavorable temperature, frequency, or voltage conditions",
		recommendedAction: "The firmware will restart mining with the hashboard; however, if the same ASIC continues to appear as not mining, the ASICs need to be inspected",
		impact:            "Reduced hashrate",
	},
	"PowerLost": {
		minerError:        sdkerrors.BoardPowerPGOODMissing,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Sudden loss of power to hashboard",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            "board shutdowns, unable to start mining",
	},
}

// PSU error mappings
var psuErrorMappings = map[string]errorMapping{
	"PsuCommLost": {
		minerError:        sdkerrors.PSUCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "I2C mux/bus contention, I2C bus signal integrity issue, command/response timeout",
		recommendedAction: "Not a concerning failure if intermittent, replace if consistent",
		impact:            "Loss of control, status messages",
	},
	"PsuOutputUnderVoltage": {
		minerError:        sdkerrors.PSUOutputVoltageFault,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Output undervoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
	},
	"PsuOutputOverVoltage": {
		minerError:        sdkerrors.PSUOutputVoltageFault,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Output overvoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
	},
	"PsuOutputFailure": {
		minerError:        sdkerrors.PSUOutputVoltageFault,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Output failure",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
	},
	"PsuOutputOverCurrent": {
		minerError:        sdkerrors.PSUOutputOvercurrent,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Loose AC connection",
		recommendedAction: actionCheckACConnectorsReplace,
		impact:            impactBayShutdown,
	},
	"PsuFans": {
		minerError:        sdkerrors.PSUFanFault,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "PSU fan failure",
		recommendedAction: actionRetestVerifyPSUFansReplace,
		impact:            "Over temperature",
	},
	"PsuOverTemperature": {
		minerError:        sdkerrors.PSUOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Fan cooling system failure, high ambient temperatures",
		recommendedAction: actionRetestVerifyPSUFansReplace,
		impact:            impactBayShutdown,
	},
	"PsuUnderTemperature": {
		minerError:        sdkerrors.PSUUnderTemperature,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "PSU operating in too cold/out of spec ambient conditions",
		recommendedAction: "N/A",
		impact:            impactBayShutdown,
	},
	"PsuInput": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Input voltage fault",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
	},
	"PsuInputUnderVoltage": {
		minerError:        sdkerrors.PSUInputVoltageLow,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Input undervoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
	},
	"PsuInputOverVoltage": {
		minerError:        sdkerrors.PSUInputVoltageHigh,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Input overvoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
	},
	"PsuInputOverCurrent": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Input overcurrent",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
	},
	"PsuNoInputVoltage": {
		minerError:        sdkerrors.PSUInputVoltageLow,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Loose power cables",
		recommendedAction: "Tighten the power cable connections, replace any faulty cables, or verify the input power supply",
		impact:            impactBayShutdown,
	},
	"PsuPowerNoGood": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Damaged PSU - It may indicate an issue in the LLC section of that PSU",
		recommendedAction: actionRetestReplace,
		impact:            "PSU damaged",
	},
}

func (d *Device) convertErrorsResponse(resp *proto.ErrorsResponse) sdk.DeviceErrors {
	if resp == nil || len(resp.Errors) == 0 {
		return sdk.DeviceErrors{
			DeviceID: d.id,
			Errors:   []sdk.DeviceError{},
		}
	}

	errors := make([]sdk.DeviceError, 0, len(resp.Errors))
	for _, notifErr := range resp.Errors {
		deviceErr := convertNotificationError(notifErr, d.id)
		errors = append(errors, deviceErr)
	}

	return sdk.DeviceErrors{
		DeviceID: d.id,
		Errors:   errors,
	}
}

func convertNotificationError(notifErr proto.NotificationError, deviceID string) sdk.DeviceError {
	var timestamp time.Time
	if notifErr.Timestamp > math.MaxInt64 {
		timestamp = time.Now()
	} else {
		timestamp = time.Unix(notifErr.Timestamp, 0)
	}

	baseError := sdk.DeviceError{
		DeviceID:    deviceID,
		FirstSeenAt: timestamp,
		LastSeenAt:  time.Now(),
	}

	// Look up the error mapping based on source and error_code
	mapping, found := lookupErrorMapping(notifErr.Source, notifErr.ErrorCode)
	if found {
		baseError.MinerError = mapping.minerError
		baseError.Severity = mapping.severity
		baseError.CauseSummary = mapping.causeSummary
		baseError.RecommendedAction = mapping.recommendedAction
		baseError.Impact = mapping.impact
		baseError.Summary = notifErr.Message
	} else {
		baseError.MinerError = sdkerrors.VendorErrorUnmapped
		baseError.Severity = sdkerrors.SeverityInfo
		baseError.CauseSummary = fmt.Sprintf("Unhandled error code: %s/%s", notifErr.Source, notifErr.ErrorCode)
		baseError.Summary = notifErr.Message
	}

	// Set component info
	switch notifErr.Source {
	case "fan":
		componentID := fmt.Sprintf("%d", notifErr.Slot)
		baseError.ComponentID = &componentID
		baseError.ComponentType = sdkerrors.ComponentTypeFan
	case "hashboard":
		componentID := fmt.Sprintf("%d", notifErr.Slot)
		baseError.ComponentID = &componentID
		baseError.ComponentType = sdkerrors.ComponentTypeHashBoard
	case "psu":
		componentID := fmt.Sprintf("%d", notifErr.Slot)
		baseError.ComponentID = &componentID
		baseError.ComponentType = sdkerrors.ComponentTypePSU
	}

	// Use message from REST response as summary if available
	if baseError.Summary == "" {
		baseError.Summary = formatDefaultSummary(notifErr)
	}

	return baseError
}

func lookupErrorMapping(source, errorCode string) (errorMapping, bool) {
	switch source {
	case "rig":
		m, ok := rigErrorMappings[errorCode]
		return m, ok
	case "fan":
		m, ok := fanErrorMappings[errorCode]
		return m, ok
	case "hashboard":
		m, ok := hashboardErrorMappings[errorCode]
		return m, ok
	case "psu":
		m, ok := psuErrorMappings[errorCode]
		return m, ok
	default:
		return errorMapping{}, false
	}
}

func formatDefaultSummary(notifErr proto.NotificationError) string {
	switch notifErr.Source {
	case "psu":
		return fmt.Sprintf("Power supply %d detected an error: %s", notifErr.Slot, notifErr.ErrorCode)
	case "fan":
		return fmt.Sprintf("Fan %d detected an error: %s", notifErr.Slot, notifErr.ErrorCode)
	case "hashboard":
		return fmt.Sprintf("Hashboard %d detected an error: %s", notifErr.Slot, notifErr.ErrorCode)
	case "rig":
		return fmt.Sprintf("Control board detected an error: %s", notifErr.ErrorCode)
	default:
		return fmt.Sprintf("Device detected an error: %s", notifErr.ErrorCode)
	}
}
