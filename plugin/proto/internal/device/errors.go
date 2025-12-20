package device

import (
	"fmt"
	"math"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_error_code"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_fan_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_hb_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_psu_api"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	sdkerrors "github.com/btc-mining/proto-fleet/server/sdk/v1/errors"
)

const (
	minValidBayIndex = 0

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
	// Type assertions in formatSummary functions are safe because applyErrorMapping
	// only calls formatSummary with the correct error type from the corresponding mapping.
	formatSummary func(any) string
}

//nolint:forcetypeassert // Type assertions in formatSummary are safe by design - see comment on errorMapping.formatSummary
var rigErrorMappings = map[miner_error_code.RigErrorCode]errorMapping{
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_LOW_HASH_RATE: {
		minerError:        sdkerrors.HashrateBelowTarget,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Reported hashrate won't match hashrate reported by the pool",
		recommendedAction: "Check the network connection to the pool and restart the miner",
		impact:            "Reduced mining revenue",
		formatSummary: func(_ any) string {
			return "Control board detected low hashrate"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_OVER_HEAT: {
		minerError:        sdkerrors.DeviceOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "System cooling failure",
		recommendedAction: "Inspect fans for sufficient cooling",
		impact:            "System shutdown",
		formatSummary: func(_ any) string {
			return "Control board is overheating"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_INSUFFICIENT_COOLING: {
		minerError:        sdkerrors.FanFailed,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Not enough fans or critical cooling fan not operating correctly",
		recommendedAction: "Check fan connections and cooling, hashboard harness/connections, replace fan module if consistent failure. Check hashboard configuration (hashboards must be installed in slots (1,3,4,6,7,9))",
		impact:            "Hashboards in affected bay are unable to start mining",
		formatSummary: func(err any) string {
			rigErr := err.(*miner_error_code.RigError)
			if bayIndex := rigErr.GetBayIndex(); bayIndex != nil && bayIndex.BayIndex > minValidBayIndex {
				return fmt.Sprintf("Bay %d has insufficient cooling", bayIndex.BayIndex)
			}
			return "Bay has insufficient cooling"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_POOL_CONNECTION_FAILURE: {
		minerError:        sdkerrors.DeviceCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Incorrect pool URL, bad credentials, pool communication failure",
		recommendedAction: "Update mining pool. Update firmware if failure persists",
		impact:            impactUnableToStartMining,
		formatSummary: func(err any) string {
			rigErr := err.(*miner_error_code.RigError)
			if detail, ok := rigErr.Detail.(*miner_error_code.RigError_PoolInfo_); ok && detail.PoolInfo != nil {
				return fmt.Sprintf("Control board is unable to connect to pool %s", detail.PoolInfo.Url)
			}
			return "Control board is unable to connect to pool"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_POOL_CONFIG_MISSING: {
		minerError:        sdkerrors.FirmwareConfigInvalid,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "There is no mining pool configuration",
		recommendedAction: "Update mining pool configuration",
		impact:            impactUnableToStartMining,
		formatSummary: func(_ any) string {
			return "Control board pool configuration missing"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_MINING_STOPPED_DUE_TO_PHASE_IMBALANCE: {
		minerError:        sdkerrors.PSUInputPhaseImbalance,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Full bay failure when phase balancing is enabled",
		recommendedAction: "Address root cause of the bay failure (PSU, hashboards, etc)",
		impact:            "Mining Stopped, will not restart until the bay is repaired",
		formatSummary: func(_ any) string {
			return "Mining stopped due to phase imbalance"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_PSU_RECOVERY_IN_PROGRESS: {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "It may indicate an issue in the LLC section of that PSU",
		recommendedAction: "Check for PSU errors, retest, replace if consistent failure. Check fan and cooling, replace if consistent failure",
		impact:            "Hashboards in affected bay are unable to start mining",
		formatSummary: func(err any) string {
			rigErr := err.(*miner_error_code.RigError)
			if bayIndex := rigErr.GetBayIndex(); bayIndex != nil && bayIndex.BayIndex > minValidBayIndex {
				return fmt.Sprintf("PSU recovery in progress for bay %d", bayIndex.BayIndex)
			}
			return "PSU recovery in progress"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_NETWORK_ERROR: {
		minerError:        sdkerrors.DeviceCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Damaged ethernet cable or tray",
		recommendedAction: "Inspect IO module, ethernet cable. Replace if consistent failure",
		impact:            "Unable to connect to mining pool",
		formatSummary: func(_ any) string {
			return "Control board is unable to connect to the network"
		},
	},
	miner_error_code.RigErrorCode_RIG_ERROR_CODE_FIRMWARE_UPDATE_FAILURE: {
		minerError:        sdkerrors.FirmwareImageInvalid,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Invalid/corrupt firmware image",
		recommendedAction: "Factory reset then update firmware, replace if consistent failure",
		impact:            "Unable to update firmware",
		formatSummary: func(_ any) string {
			return "Control board encountered a firmware update error"
		},
	},
}

//nolint:forcetypeassert // Type assertions in formatSummary are safe by design - see comment on errorMapping.formatSummary
var fanErrorMappings = map[miner_fan_api.FanErrorCode]errorMapping{
	miner_fan_api.FanErrorCode_FAN_ERROR_CODE_HARDWARE: {
		minerError:        sdkerrors.FanFailed,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Unseated/damaged connector, missing power",
		recommendedAction: "Replace fan, inspect fan module wiring if consistent failure",
		impact:            "Miner overheating",
		formatSummary: func(err any) string {
			fanErr := err.(*miner_fan_api.FanError)
			return fmt.Sprintf("Fan %d hardware error", fanErr.Index)
		},
	},
	miner_fan_api.FanErrorCode_FAN_ERROR_CODE_SLOW_SPIN: {
		minerError:        sdkerrors.FanSpeedDeviation,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Mechanical damage, dust, worn motor, Connector/cable issue",
		recommendedAction: "Replace fan module",
		impact:            "Miner overheating",
		formatSummary: func(err any) string {
			fanErr := err.(*miner_fan_api.FanError)
			if detail, ok := fanErr.Detail.(*miner_fan_api.FanError_FanSpeed_); ok && detail.FanSpeed != nil {
				return fmt.Sprintf("Fan %d has stalled. Target RPM: %d, Actual RPM: %d",
					fanErr.Index, detail.FanSpeed.FanPwmTargetPct, detail.FanSpeed.FanRpmTach)
			}
			return fmt.Sprintf("Fan %d has stalled", fanErr.Index)
		},
	},
	miner_fan_api.FanErrorCode_FAN_ERROR_CODE_SET_FAN_SPEED_FAILED: {
		minerError:        sdkerrors.FanSpeedDeviation,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "Failed to set fan speed",
		recommendedAction: "Monitor fan control system",
		impact:            "Cannot adjust cooling dynamically",
		formatSummary: func(err any) string {
			fanErr := err.(*miner_fan_api.FanError)
			return fmt.Sprintf("Fan %d failed to set fan speed", fanErr.Index)
		},
	},
	miner_fan_api.FanErrorCode_FAN_ERROR_CODE_FAN_CONNECTED_IN_IMMERSION: {
		minerError:        sdkerrors.VendorErrorUnmapped,
		severity:          sdkerrors.SeverityInfo,
		causeSummary:      "Remove fans for immersion cooling",
		recommendedAction: "Remove fans for immersion mode",
		impact:            "Mining Disabled",
		formatSummary: func(err any) string {
			fanErr := err.(*miner_fan_api.FanError)
			return fmt.Sprintf("Fan %d connected in immersion mode", fanErr.Index)
		},
	},
}

//nolint:forcetypeassert // Type assertions in formatSummary are safe by design - see comment on errorMapping.formatSummary
var hashboardErrorMappings = map[miner_hb_api.HbErrorCode]errorMapping{
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_OVER_HEAT: {
		minerError:        sdkerrors.HashboardOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Insufficient cooling",
		recommendedAction: actionCheckFanCoolingReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			if temp, ok := hbErr.Detail.(*miner_hb_api.HbError_Temperature); ok {
				return fmt.Sprintf("Hashboard %d overheating: %.1f °C", hbErr.Index, temp.Temperature)
			}
			return fmt.Sprintf("Hashboard %d overheating", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_ASIC_ENUMERATION: {
		minerError:        sdkerrors.HashboardMissingChips,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Damaged fuse, clock crystal, MCU, or faulty ASIC",
		recommendedAction: actionRetestReplace,
		impact:            impactUnableToStartMining,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d unable to start mining", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_COMMUNICATION: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_COMM_TASKS_NOT_INITIALIZED: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_INVALID_ADD_WORK_RESPONSE: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_INVALID_METADATA_RESPONSE: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_MISSING_COMM_CHANNEL: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_COMMAND_TIMEOUT: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_TASK_COMMUNICATION_ERROR: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_COMMS_ALIVE: {
		minerError:        sdkerrors.ASICChainCommunicationLost,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d communication error", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_ASIC_ECC: {
		minerError:        sdkerrors.ASICCRCErrorExcessive,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "Communication channel (e.g. SLINK) electrical failure, software bug",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d excessive ECC errors detected", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_UNDER_VOLTAGE: {
		minerError:        sdkerrors.BoardPowerRailUndervolt,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Voltage droop",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			if volt, ok := hbErr.Detail.(*miner_hb_api.HbError_Voltage); ok {
				return fmt.Sprintf("Hashboard %d undervoltage detected: %.2f V", hbErr.Index, volt.Voltage)
			}
			return fmt.Sprintf("Hashboard %d undervoltage detected", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_OVER_VOLTAGE: {
		minerError:        sdkerrors.BoardPowerRailOvervolt,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "PSU spike",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			if volt, ok := hbErr.Detail.(*miner_hb_api.HbError_Voltage); ok {
				return fmt.Sprintf("Hashboard %d overvoltage detected at %.2f V", hbErr.Index, volt.Voltage)
			}
			return fmt.Sprintf("Hashboard %d overvoltage detected", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_OVER_CURRENT: {
		minerError:        sdkerrors.BoardPowerOvercurrent,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "PSU spike, ASIC malfunction",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			if curr, ok := hbErr.Detail.(*miner_hb_api.HbError_Current); ok {
				return fmt.Sprintf("Hashboard %d overcurrent detected: %.2f A", hbErr.Index, curr.Current)
			}
			return fmt.Sprintf("Hashboard %d overcurrent detected", hbErr.Index)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_ASIC_OVER_HEAT: {
		minerError:        sdkerrors.HashboardASICOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "TIM wearout",
		recommendedAction: actionCheckFanCoolingReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			asicID := hbErr.GetAsicIndex()
			if temp, ok := hbErr.Detail.(*miner_hb_api.HbError_Temperature); ok {
				return fmt.Sprintf("Hashboard %d ASIC is overheating: %.1f °C, first detected at ASIC %d",
					hbErr.Index, temp.Temperature, asicID+1)
			}
			return fmt.Sprintf("Hashboard %d ASIC is overheating, first detected at ASIC %d", hbErr.Index, asicID+1)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_ASIC_UNDER_HEAT: {
		minerError:        sdkerrors.HashboardASICUnderTemperature,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "ASIC outside of operating temperature, ambient too cold",
		recommendedAction: actionRetestReplace,
		impact:            impactReducedHashrateBoardShut,
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			asicID := hbErr.GetAsicIndex()
			if temp, ok := hbErr.Detail.(*miner_hb_api.HbError_Temperature); ok {
				return fmt.Sprintf("Hashboard %d ASIC temperature is too low: %.1f °C, first detected at ASIC %d",
					hbErr.Index, temp.Temperature, asicID+1)
			}
			return fmt.Sprintf("Hashboard %d ASIC temperature is too low, first detected at ASIC %d", hbErr.Index, asicID+1)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_ASIC_NOT_HASHING: {
		minerError:        sdkerrors.HashrateBelowTarget,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "The ASIC may get stuck while SLINK remains functional; this could be due to unfavorable temperature, frequency, or voltage conditions",
		recommendedAction: "The firmware will restart mining with the hashboard; however, if the same ASIC continues to appear as not mining, the ASICs need to be inspected",
		impact:            "Reduced hashrate",
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			asicID := hbErr.GetAsicIndex()
			return fmt.Sprintf("Hashboard %d ASIC is not hashing, first detected at ASIC %d", hbErr.Index, asicID+1)
		},
	},
	miner_hb_api.HbErrorCode_HB_ERROR_CODE_POWER_LOST: {
		minerError:        sdkerrors.BoardPowerPGOODMissing,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Sudden loss of power to hashboard",
		recommendedAction: actionCheckPSURetestReplace,
		impact:            "board shutdowns, unable to start mining",
		formatSummary: func(err any) string {
			hbErr := err.(*miner_hb_api.HbError)
			return fmt.Sprintf("Hashboard %d has lost power", hbErr.Index)
		},
	},
}

//nolint:forcetypeassert // Type assertions in formatSummary are safe by design - see comment on errorMapping.formatSummary
var psuErrorMappings = map[miner_psu_api.PsuErrorCode]errorMapping{
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_COMM_LOST: {
		minerError:        sdkerrors.PSUCommunicationLost,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "I2C mux/bus contention, I2C bus signal integrity issue, command/response timeout",
		recommendedAction: "Not a concerning failure if intermittent, replace if consistent",
		impact:            "Loss of control, status messages",
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			// PSU indices are already 1-based from firmware
			return fmt.Sprintf("Power supply %d communication error", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_UNDER_VOLTAGE: {
		minerError:        sdkerrors.PSUInputVoltageLow,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Overload",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Low input voltage detected on power supply %d", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_OVER_VOLTAGE: {
		minerError:        sdkerrors.PSUInputVoltageHigh,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Internal fault",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d overvoltage detected", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_OUTPUT_FAILURE: {
		minerError:        sdkerrors.PSUOutputVoltageFault,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Output failure",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d output fault", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_OVER_CURRENT: {
		minerError:        sdkerrors.PSUOutputOvercurrent,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Loose AC connection",
		recommendedAction: actionCheckACConnectorsReplace,
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d load is drawing too much current", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_FANS: {
		minerError:        sdkerrors.PSUFanFault,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "PSU fan failure",
		recommendedAction: actionRetestVerifyPSUFansReplace,
		impact:            "Over temperature",
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d fan failure", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_OVER_TEMPERATURE: {
		minerError:        sdkerrors.PSUOverTemperature,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Fan cooling system failure, high ambient temperatures",
		recommendedAction: actionRetestVerifyPSUFansReplace,
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d overheating", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_UNDER_TEMPERATURE: {
		minerError:        sdkerrors.PSUUnderTemperature,
		severity:          sdkerrors.SeverityMinor,
		causeSummary:      "PSU operating in too cold/out of spec ambient conditions",
		recommendedAction: "N/A",
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d temperature is too low", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_INPUT: {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityMajor,
		causeSummary:      "Input voltage fault",
		recommendedAction: actionRetestReplace,
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d is detecting an input voltage fault", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_NO_INPUT_VOLTAGE: {
		minerError:        sdkerrors.PSUInputVoltageLow,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Loose power cables",
		recommendedAction: "Tighten the power cable connections, replace any faulty cables, or verify the input power supply",
		impact:            impactBayShutdown,
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d is not detecting input voltage", psuErr.Index)
		},
	},
	miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_POWER_NO_GOOD: {
		minerError:        sdkerrors.PSUFaultGeneric,
		severity:          sdkerrors.SeverityCritical,
		causeSummary:      "Damaged PSU - It may indicate an issue in the LLC section of that PSU",
		recommendedAction: actionRetestReplace,
		impact:            "PSU damaged",
		formatSummary: func(err any) string {
			psuErr := err.(*miner_psu_api.PsuError)
			return fmt.Sprintf("Power supply %d detected a power fault", psuErr.Index)
		},
	},
}

func (d *Device) convertErrorsResponse(resp *miner_data_api.ErrorsResponse) sdk.DeviceErrors {
	if resp == nil || len(resp.Errors) == 0 {
		return sdk.DeviceErrors{
			DeviceID: d.id,
			Errors:   []sdk.DeviceError{},
		}
	}

	errors := make([]sdk.DeviceError, 0, len(resp.Errors))
	for _, errFromDb := range resp.Errors {
		if errFromDb.Error == nil {
			continue
		}

		deviceErr := convertMinerError(errFromDb, d.id)
		errors = append(errors, deviceErr)
	}

	return sdk.DeviceErrors{
		DeviceID: d.id,
		Errors:   errors,
	}
}

func convertMinerError(errFromDb *miner_data_api.ErrorFromDb, deviceID string) sdk.DeviceError {
	var timestamp time.Time
	if errFromDb.Timestamp > math.MaxInt64 {
		timestamp = time.Now()
	} else {
		timestamp = time.Unix(int64(errFromDb.Timestamp), 0)
	}

	// Proto miners only provide the initial error timestamp (when first detected),
	// not when the error was last observed. Set LastSeenAt to current time
	// to indicate when the plugin observed this error during polling.
	baseError := sdk.DeviceError{
		DeviceID:    deviceID,
		FirstSeenAt: timestamp,
		LastSeenAt:  time.Now(), // Current observation time
	}

	switch err := errFromDb.Error.Err.(type) {
	case *miner_error_code.Error_RigError:
		return convertRigError(err.RigError, baseError)
	case *miner_error_code.Error_FanError:
		return convertFanError(err.FanError, baseError)
	case *miner_error_code.Error_HbError:
		return convertHashboardError(err.HbError, baseError)
	case *miner_error_code.Error_PsuError:
		return convertPSUError(err.PsuError, baseError)
	default:
		baseError.MinerError = sdkerrors.VendorErrorUnmapped
		baseError.Severity = sdkerrors.SeverityInfo
		baseError.Summary = "Unknown error type"
		return baseError
	}
}

func applyErrorMapping(base sdk.DeviceError, err any) sdk.DeviceError {
	var mapping errorMapping
	var found bool
	var errorCodeString string

	switch e := err.(type) {
	case *miner_psu_api.PsuError:
		mapping, found = psuErrorMappings[e.Code]
		errorCodeString = e.Code.String()
	case *miner_fan_api.FanError:
		mapping, found = fanErrorMappings[e.Code]
		errorCodeString = e.Code.String()
	case *miner_hb_api.HbError:
		mapping, found = hashboardErrorMappings[e.Code]
		errorCodeString = e.Code.String()
	case *miner_error_code.RigError:
		mapping, found = rigErrorMappings[e.Code]
		errorCodeString = e.Code.String()
	default:
		base.MinerError = sdkerrors.VendorErrorUnmapped
		base.Severity = sdkerrors.SeverityInfo
		base.Summary = "Unknown error type"
		return base
	}

	if found {
		base.MinerError = mapping.minerError
		base.Severity = mapping.severity
		base.CauseSummary = mapping.causeSummary
		base.RecommendedAction = mapping.recommendedAction
		base.Impact = mapping.impact
		if mapping.formatSummary != nil {
			base.Summary = mapping.formatSummary(err)
		}
	} else {
		base.MinerError = sdkerrors.VendorErrorUnmapped
		base.Severity = sdkerrors.SeverityInfo
		base.CauseSummary = fmt.Sprintf("Unhandled error code: %s", errorCodeString)
		base.Summary = formatUnmappedError(err, errorCodeString)
	}
	return base
}

func formatUnmappedError(err any, errorCodeString string) string {
	switch e := err.(type) {
	case *miner_psu_api.PsuError:
		return fmt.Sprintf("Power supply %d detected an error: %s", e.Index, errorCodeString)
	case *miner_fan_api.FanError:
		return fmt.Sprintf("Fan %d detected an error: %s", e.Index, errorCodeString)
	case *miner_hb_api.HbError:
		return fmt.Sprintf("Hashboard %d detected an error: %s", e.Index, errorCodeString)
	case *miner_error_code.RigError:
		return fmt.Sprintf("Control board detected an error: %s", errorCodeString)
	default:
		return fmt.Sprintf("Device detected an error: %s", errorCodeString)
	}
}

func convertRigError(rigErr *miner_error_code.RigError, base sdk.DeviceError) sdk.DeviceError {
	base = applyErrorMapping(base, rigErr)

	if rigErr.Code == miner_error_code.RigErrorCode_RIG_ERROR_CODE_INSUFFICIENT_COOLING {
		if detail, ok := rigErr.Detail.(*miner_error_code.RigError_InsufficientCooling_); ok {
			base.ComponentType = sdkerrors.ComponentTypeFan
			base.VendorAttributes = map[string]string{
				"operational_fans": fmt.Sprintf("%d", detail.InsufficientCooling.NumOperationalFans),
				"expected_fans":    fmt.Sprintf("%d", detail.InsufficientCooling.NumExpectedFans),
			}
		}
	}

	return base
}

func convertFanError(fanErr *miner_fan_api.FanError, base sdk.DeviceError) sdk.DeviceError {
	fanID := fmt.Sprintf("%d", fanErr.Index)
	base.ComponentID = &fanID
	base.ComponentType = sdkerrors.ComponentTypeFan

	return applyErrorMapping(base, fanErr)
}

func convertHashboardError(hbErr *miner_hb_api.HbError, base sdk.DeviceError) sdk.DeviceError {
	hbID := fmt.Sprintf("%d", hbErr.Index)
	base.ComponentID = &hbID
	base.ComponentType = sdkerrors.ComponentTypeHashBoard

	return applyErrorMapping(base, hbErr)
}

func convertPSUError(psuErr *miner_psu_api.PsuError, base sdk.DeviceError) sdk.DeviceError {
	psuID := fmt.Sprintf("%d", psuErr.Index)
	base.ComponentID = &psuID
	base.ComponentType = sdkerrors.ComponentTypePSU

	return applyErrorMapping(base, psuErr)
}
