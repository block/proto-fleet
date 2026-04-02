package device

import (
	"fmt"
	"math"
	"time"

	"github.com/block/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
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

	sourceHashboard = "hashboard"
)

type errorMapping struct {
	minerError        sdkerrors.MinerError
	severity          sdkerrors.Severity
	causeSummary      string
	recommendedAction string
	impact            string
	formatSummary     func(proto.NotificationError) string
}

// Rig error mappings keyed by error_code string
var rigErrorMappings = map[string]errorMapping{
	"LowHashRate": {
		minerError:        sdkerrors.HashrateBelowTarget,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Reported hashrate won't match hashrate reported by the pool",
		recommendedAction: "Check the network connection to the pool and restart the miner",
		impact:            "Reduced mining revenue",
		formatSummary: func(_ proto.NotificationError) string {
			return "Control board detected low hashrate"
		},
	},
	"OverHeat": {
		minerError:        sdkerrors.DeviceOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "System cooling failure",
		recommendedAction: "Inspect fans for sufficient cooling",
		impact:            "System shutdown",
		formatSummary: func(_ proto.NotificationError) string {
			return "Control board is overheating"
		},
	},
	"InsufficientCooling": {
		minerError:        sdkerrors.FanFailed,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Not enough fans or critical cooling fan not operating correctly",
		recommendedAction: "Check fan connections and cooling, hashboard harness/connections, replace fan module if consistent failure. Check hashboard configuration (hashboards must be installed in slots (1,3,4,6,7,9))",
		impact:            "Hashboards in affected bay are unable to start mining",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Bay %d has insufficient cooling", "Bay has insufficient cooling")
		},
	},
	"PoolConnectionFailure": {
		minerError:        sdkerrors.DeviceCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Incorrect pool URL, bad credentials, pool communication failure",
		recommendedAction: "Update mining pool. Update firmware if failure persists",
		impact:            impactUnableToStartMining,
		formatSummary: func(_ proto.NotificationError) string {
			return "Control board is unable to connect to pool"
		},
	},
	"PoolConfigMissing": {
		minerError:        sdkerrors.FirmwareConfigInvalid,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "There is no mining pool configuration",
		recommendedAction: "Update mining pool configuration",
		impact:            impactUnableToStartMining,
		formatSummary: func(_ proto.NotificationError) string {
			return "Control board pool configuration missing"
		},
	},
	"MiningStoppedDueToPhaseImbalance": {
		minerError:        sdkerrors.PSUInputPhaseImbalance,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Full bay failure when phase balancing is enabled",
		recommendedAction: "Address root cause of the bay failure (PSU, hashboards, etc)",
		impact:            "Mining Stopped, will not restart until the bay is repaired",
		formatSummary: func(_ proto.NotificationError) string {
			return "Mining stopped due to phase imbalance"
		},
	},
	"PSURecoveryInProgress": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "It may indicate an issue in the LLC section of that PSU",
		recommendedAction: "Check for PSU errors, retest, replace if consistent failure. Check fan and cooling, replace if consistent failure",
		impact:            "Hashboards in affected bay are unable to start mining",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "PSU recovery in progress for bay %d", "PSU recovery in progress for bay")
		},
	},
	"NetworkError": {
		minerError:        sdkerrors.DeviceCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Damaged ethernet cable or tray",
		recommendedAction: "Inspect IO module, ethernet cable. Replace if consistent failure",
		impact:            "Unable to connect to mining pool",
		formatSummary: func(_ proto.NotificationError) string {
			return "Control board is unable to connect to the network"
		},
	},
	"FirmwareUpdateFailure": {
		minerError:        sdkerrors.FirmwareImageInvalid,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Invalid/corrupt firmware image",
		recommendedAction: "Factory reset then update firmware, replace if consistent failure",
		impact:            "Unable to update firmware",
		formatSummary: func(_ proto.NotificationError) string {
			return "Control board encountered a firmware update error"
		},
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
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Fan %d hardware error", "Fan hardware error")
		},
	},
	"FanSlow": {
		minerError:        sdkerrors.FanSpeedDeviation,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Mechanical damage, dust, worn motor, Connector/cable issue",
		recommendedAction: "Replace fan module",
		impact:            "Miner overheating",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Fan %d has stalled", "Fan has stalled")
		},
	},
	"SetFanSpeedFailed": {
		minerError:        sdkerrors.FanSpeedDeviation,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "Failed to set fan speed",
		recommendedAction: "Monitor fan control system",
		impact:            "Cannot adjust cooling dynamically",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Fan %d failed to set fan speed", "Fan failed to set fan speed")
		},
	},
	"FanConnectedInImmersion": {
		minerError:        sdkerrors.VendorErrorUnmapped,
		severity:          sdkerrors.SeverityInfo,
		causeSummary:      "Remove fans for immersion cooling",
		recommendedAction: "Remove fans for immersion mode",
		impact:            "Mining Disabled",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Fan %d connected in immersion mode", "Fan connected in immersion mode")
		},
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
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d overheating", "Hashboard overheating")
		},
	},
	"AsicEnumeration": {
		minerError:        sdkerrors.HashboardMissingChips,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Damaged fuse, clock crystal, MCU, or faulty ASIC",
		recommendedAction: actionRetestReplace,
		impact:            impactUnableToStartMining,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d unable to start mining", "Hashboard unable to start mining")
		},
	},
	"HbCommunication": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"CommTasksNotInitialized": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"InvalidAddWorkResponse": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"InvalidMetadataResponse": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"MissingCommChannel": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"CommandTimeout": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"TaskCommunicationError": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"CommsAlive": {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary:     hashboardCommunicationSummary,
	},
	"AsicEcc": {
		minerError:        sdkerrors.ASICCRCErrorExcessive,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure, software bug",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d excessive ECC errors detected", "Hashboard excessive ECC errors detected")
		},
	},
	"HbUnderVoltage": {
		minerError:        sdkerrors.BoardPowerRailUndervolt,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Voltage droop",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d undervoltage detected", "Hashboard undervoltage detected")
		},
	},
	"HbOverVoltage": {
		minerError:        sdkerrors.BoardPowerRailOvervolt,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "PSU spike",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d overvoltage detected", "Hashboard overvoltage detected")
		},
	},
	"HbOverCurrent": {
		minerError:        sdkerrors.BoardPowerOvercurrent,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "PSU spike, ASIC malfunction",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d overcurrent detected", "Hashboard overcurrent detected")
		},
	},
	"AsicOverHeat": {
		minerError:        sdkerrors.HashboardASICOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "TIM wearout",
		recommendedAction: actionCheckFanCoolingReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d ASIC is overheating", "Hashboard ASIC is overheating")
		},
	},
	"AsicUnderHeat": {
		minerError:        sdkerrors.HashboardASICUnderTemperature,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "ASIC outside of operating temperature, ambient too cold",
		recommendedAction: actionRetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d ASIC temperature is too low", "Hashboard ASIC temperature is too low")
		},
	},
	"AsicNotHashing": {
		minerError:        sdkerrors.HashrateBelowTarget,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "The ASIC may get stuck while SLINK remains functional; this could be due to unfavorable temperature, frequency, or voltage conditions",
		recommendedAction: "The firmware will restart mining with the hashboard; however, if the same ASIC continues to appear as not mining, the ASICs need to be inspected",
		impact:            "Reduced hashrate",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d ASIC is not hashing", "Hashboard ASIC is not hashing")
		},
	},
	"PowerLost": {
		minerError:        sdkerrors.BoardPowerPGOODMissing,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Sudden loss of power to hashboard",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            "board shutdowns, unable to start mining",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d has lost power", "Hashboard has lost power")
		},
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
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d communication error", "Power supply communication error")
		},
	},
	"PsuOutputUnderVoltage": {
		minerError:        sdkerrors.PSUOutputVoltageFault,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Output undervoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d output voltage is too low", "Power supply output voltage is too low")
		},
	},
	"PsuOutputOverVoltage": {
		minerError:        sdkerrors.PSUOutputVoltageFault,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Output overvoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d output voltage is too high", "Power supply output voltage is too high")
		},
	},
	"PsuOutputFailure": {
		minerError:        sdkerrors.PSUOutputVoltageFault,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Output failure",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d output fault", "Power supply output fault")
		},
	},
	"PsuOutputOverCurrent": {
		minerError:        sdkerrors.PSUOutputOvercurrent,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Loose AC connection",
		recommendedAction: actionCheckACConnectorsReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d load is drawing too much current", "Power supply load is drawing too much current")
		},
	},
	"PsuFans": {
		minerError:        sdkerrors.PSUFanFault,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "PSU fan failure",
		recommendedAction: actionRetestVerifyPSUFansReplace,
		impact:            "Over temperature",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d fan failure", "Power supply fan failure")
		},
	},
	"PsuOverTemperature": {
		minerError:        sdkerrors.PSUOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Fan cooling system failure, high ambient temperatures",
		recommendedAction: actionRetestVerifyPSUFansReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d overheating", "Power supply overheating")
		},
	},
	"PsuUnderTemperature": {
		minerError:        sdkerrors.PSUUnderTemperature,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "PSU operating in too cold/out of spec ambient conditions",
		recommendedAction: "N/A",
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d temperature is too low", "Power supply temperature is too low")
		},
	},
	"PsuInput": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Input voltage fault",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d is detecting an input voltage fault", "Power supply is detecting an input voltage fault")
		},
	},
	"PsuInputUnderVoltage": {
		minerError:        sdkerrors.PSUInputVoltageLow,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Input undervoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d input voltage is too low", "Power supply input voltage is too low")
		},
	},
	"PsuInputOverVoltage": {
		minerError:        sdkerrors.PSUInputVoltageHigh,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Input overvoltage",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d input voltage is too high", "Power supply input voltage is too high")
		},
	},
	"PsuInputOverCurrent": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Input overcurrent",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d input current is too high", "Power supply input current is too high")
		},
	},
	"PsuNoInputVoltage": {
		minerError:        sdkerrors.PSUInputVoltageLow,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Loose power cables",
		recommendedAction: "Tighten the power cable connections, replace any faulty cables, or verify the input power supply",
		impact:            impactBayShutdown,
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d is not detecting input voltage", "Power supply is not detecting input voltage")
		},
	},
	"PsuPowerNoGood": {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Damaged PSU - It may indicate an issue in the LLC section of that PSU",
		recommendedAction: actionRetestReplace,
		impact:            "PSU damaged",
		formatSummary: func(notifErr proto.NotificationError) string {
			return formatSummaryWithOptionalSlot(notifErr.Slot, "Power supply %d detected a power fault", "Power supply detected a power fault")
		},
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
		if baseError.Summary == "" && mapping.formatSummary != nil {
			baseError.Summary = mapping.formatSummary(notifErr)
		}
	} else {
		baseError.MinerError = sdkerrors.VendorErrorUnmapped
		baseError.Severity = sdkerrors.SeverityInfo
		baseError.CauseSummary = fmt.Sprintf("Unhandled error code: %s/%s", notifErr.Source, notifErr.ErrorCode)
		baseError.Summary = notifErr.Message
	}

	// Set component info based on error source.
	// Rig errors with a non-zero slot target a specific bay and should be
	// reported as component-level errors so they don't collapse with
	// device-level errors in diagnostics grouping.
	switch notifErr.Source {
	case "rig":
		if notifErr.Slot > 0 {
			componentID := fmt.Sprintf("%d", notifErr.Slot)
			baseError.ComponentID = &componentID
			baseError.ComponentType = sdkerrors.ComponentTypeUnspecified
		}
	case "fan":
		if notifErr.Slot > 0 {
			componentID := fmt.Sprintf("%d", notifErr.Slot)
			baseError.ComponentID = &componentID
			baseError.ComponentType = sdkerrors.ComponentTypeFan
		}
	case sourceHashboard:
		if notifErr.Slot > 0 {
			componentID := fmt.Sprintf("%d", notifErr.Slot)
			baseError.ComponentID = &componentID
			baseError.ComponentType = sdkerrors.ComponentTypeHashBoard
		}
	case "psu":
		if notifErr.Slot > 0 {
			componentID := fmt.Sprintf("%d", notifErr.Slot)
			baseError.ComponentID = &componentID
			baseError.ComponentType = sdkerrors.ComponentTypePSU
		}
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
	case sourceHashboard:
		m, ok := hashboardErrorMappings[errorCode]
		return m, ok
	case "psu":
		m, ok := psuErrorMappings[errorCode]
		return m, ok
	default:
		return errorMapping{}, false
	}
}

func hashboardCommunicationSummary(notifErr proto.NotificationError) string {
	return formatSummaryWithOptionalSlot(notifErr.Slot, "Hashboard %d communication error", "Hashboard communication error")
}

func formatSummaryWithOptionalSlot(slot int, withSlotFormat string, withoutSlot string) string {
	if slot > 0 {
		return fmt.Sprintf(withSlotFormat, slot)
	}
	return withoutSlot
}

func formatDefaultSummary(notifErr proto.NotificationError) string {
	switch notifErr.Source {
	case "psu":
		return fmt.Sprintf("Power supply %d detected an error: %s", notifErr.Slot, notifErr.ErrorCode)
	case "fan":
		return fmt.Sprintf("Fan %d detected an error: %s", notifErr.Slot, notifErr.ErrorCode)
	case sourceHashboard:
		return fmt.Sprintf("Hashboard %d detected an error: %s", notifErr.Slot, notifErr.ErrorCode)
	case "rig":
		return fmt.Sprintf("Control board detected an error: %s", notifErr.ErrorCode)
	default:
		return fmt.Sprintf("Device detected an error: %s", notifErr.ErrorCode)
	}
}
