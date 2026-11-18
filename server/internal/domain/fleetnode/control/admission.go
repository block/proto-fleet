package control

import (
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Fixed optimistic ceilings for one Fleet Node. At the default 15-second
// telemetry cadence, 504 deferrable slots leave substantial latency headroom
// for thousands of miners while reserving eight slots for operator work.
const (
	MaxConcurrentCommandsPerFleetNode        = 512
	ReservedGeneralCommandSlotsPerFleetNode  = 8
	MaxConcurrentDeferrableReadsPerFleetNode = MaxConcurrentCommandsPerFleetNode - ReservedGeneralCommandSlotsPerFleetNode
)

// CommandAdmissionClass names the capacity a decoded control command consumes.
type CommandAdmissionClass string

const (
	CommandAdmissionGeneral        CommandAdmissionClass = "general"
	CommandAdmissionDeferrableRead CommandAdmissionClass = "deferrable_read"
	CommandAdmissionExclusive      CommandAdmissionClass = "exclusive"
)

// AdmissionForCommand is the discoverable source of truth for ControlStream
// admission. It returns both the admission class and a bounded command kind for
// observability. Malformed, empty, unknown, and future commands default to the
// general lane so new work cannot accidentally consume the read reservation.
//
// Telemetry is observation-only and retryable, including when used to confirm
// curtailment. GetErrors runs alongside scheduled telemetry collection, and
// GetCoolingMode only prefills the settings UI. GetMiningPools remains general
// because it is also a prerequisite for operator pool changes, while
// GetFirmwareUpdateStatus advances an operator-initiated firmware workflow.
// Container power-feed inspection is an operator-opened observation that never
// changes equipment, so it must not take the slots reserved for operator writes;
// any other or undecodable power-feed request stays general.
func AdmissionForCommand(command *gatewaypb.AgentCommand) (CommandAdmissionClass, string) {
	if command == nil {
		return CommandAdmissionGeneral, "malformed"
	}
	switch typedCommand := command.GetCommand().(type) {
	case *gatewaypb.AgentCommand_Discover:
		return CommandAdmissionExclusive, "discovery"
	case *gatewaypb.AgentCommand_Pair:
		return CommandAdmissionExclusive, "pairing"
	case *gatewaypb.AgentCommand_RecoverMinerEndpoints:
		return CommandAdmissionExclusive, "endpoint_recovery"
	case *gatewaypb.AgentCommand_Telemetry:
		return CommandAdmissionDeferrableRead, "telemetry"
	case *gatewaypb.AgentCommand_ExportCredentialKey:
		return CommandAdmissionGeneral, "credential_key_export"
	case *gatewaypb.AgentCommand_InstallCredentialKey:
		return CommandAdmissionGeneral, "credential_key_install"
	case *gatewaypb.AgentCommand_MinerWeb:
		return CommandAdmissionGeneral, "miner_web"
	case *gatewaypb.AgentCommand_MinerCommand:
		return AdmissionForMinerCommand(typedCommand.MinerCommand)
	default:
		if len(command.ProtoReflect().GetUnknown()) > 0 {
			return CommandAdmissionGeneral, "unknown"
		}
		return CommandAdmissionGeneral, "empty"
	}
}

// AdmissionForMinerCommand applies the same policy before the server sends the
// command, allowing deferrable work to queue at Fleet instead of producing a
// burst of BUSY acknowledgements at the node.
func AdmissionForMinerCommand(command *gatewaypb.MinerCommand) (CommandAdmissionClass, string) {
	if command == nil || command.GetAction() == nil {
		return CommandAdmissionGeneral, "miner_command_empty"
	}
	switch command.GetAction().(type) {
	case *gatewaypb.MinerCommand_GetCoolingMode:
		return CommandAdmissionDeferrableRead, "get_cooling_mode"
	case *gatewaypb.MinerCommand_GetErrors:
		return CommandAdmissionDeferrableRead, "get_errors"
	case *gatewaypb.MinerCommand_ContainerMining:
		return CommandAdmissionGeneral, "container_mining"
	case *gatewaypb.MinerCommand_ContainerPowerFeed:
		if r, err := sdk.DecodeContainerPowerFeedRequest(command.GetContainerPowerFeed().GetRequest()); err == nil && r.ReadOnly() {
			return CommandAdmissionDeferrableRead, "container_power_feed_inspect"
		}
		return CommandAdmissionGeneral, "container_power_feed"
	case *gatewaypb.MinerCommand_MiningGroups:
		return CommandAdmissionGeneral, "mining_groups"
	case *gatewaypb.MinerCommand_SetModuleMining:
		return CommandAdmissionGeneral, "set_module_mining"
	case *gatewaypb.MinerCommand_StreamBoardAsicReadings:
		// A viewer-opened observation, bounded per node by
		// MaxConcurrentCommandStreamsPerNode, so it stays out of the slots
		// reserved for operator writes.
		return CommandAdmissionDeferrableRead, "stream_board_asic_readings"
	case *gatewaypb.MinerCommand_ControllerRecords:
		return CommandAdmissionGeneral, "controller_records"
	case *gatewaypb.MinerCommand_SetBoardIdentify:
		return CommandAdmissionGeneral, "set_board_identify"
	case *gatewaypb.MinerCommand_SetTankMining:
		return CommandAdmissionGeneral, "set_tank_mining"
	case *gatewaypb.MinerCommand_SetContainerControl:
		return CommandAdmissionGeneral, "set_container_control"
	case *gatewaypb.MinerCommand_RunContainerAlarmAction:
		return CommandAdmissionGeneral, "run_container_alarm_action"
	case *gatewaypb.MinerCommand_Reboot:
		return CommandAdmissionGeneral, "reboot"
	case *gatewaypb.MinerCommand_StartMining:
		return CommandAdmissionGeneral, "start_mining"
	case *gatewaypb.MinerCommand_StopMining:
		return CommandAdmissionGeneral, "stop_mining"
	case *gatewaypb.MinerCommand_BlinkLed:
		return CommandAdmissionGeneral, "blink_led"
	case *gatewaypb.MinerCommand_Curtail:
		return CommandAdmissionGeneral, "curtail"
	case *gatewaypb.MinerCommand_Uncurtail:
		return CommandAdmissionGeneral, "uncurtail"
	case *gatewaypb.MinerCommand_SetCoolingMode:
		return CommandAdmissionGeneral, "set_cooling_mode"
	case *gatewaypb.MinerCommand_SetPowerTarget:
		return CommandAdmissionGeneral, "set_power_target"
	case *gatewaypb.MinerCommand_UpdateMiningPools:
		return CommandAdmissionGeneral, "update_mining_pools"
	case *gatewaypb.MinerCommand_VerifyPoolsCleared:
		return CommandAdmissionGeneral, "verify_pools_cleared"
	case *gatewaypb.MinerCommand_GetMiningPools:
		return CommandAdmissionGeneral, "get_mining_pools"
	case *gatewaypb.MinerCommand_UpdateMinerPassword:
		return CommandAdmissionGeneral, "update_miner_password"
	case *gatewaypb.MinerCommand_DownloadLogs:
		return CommandAdmissionGeneral, "download_logs"
	case *gatewaypb.MinerCommand_FirmwareUpdate:
		return CommandAdmissionGeneral, "firmware_update"
	case *gatewaypb.MinerCommand_GetFirmwareUpdateStatus:
		return CommandAdmissionGeneral, "get_firmware_update_status"
	case *gatewaypb.MinerCommand_ApplyCurtailmentConfig:
		return CommandAdmissionGeneral, "apply_curtailment_config"
	default:
		return CommandAdmissionGeneral, "miner_command_unknown"
	}
}
