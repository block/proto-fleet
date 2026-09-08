package main

import pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"

// controlCommandAdmissionClass names the process-wide capacity a decoded
// command consumes. Keep the complete classification here so a new command's
// admission behavior is visible at the point where it is introduced.
type controlCommandAdmissionClass string

const (
	controlCommandAdmissionGeneral        controlCommandAdmissionClass = "general"
	controlCommandAdmissionDeferrableRead controlCommandAdmissionClass = "deferrable_read"
	controlCommandAdmissionExclusive      controlCommandAdmissionClass = "exclusive"
)

// classifyControlCommand applies the ControlStream admission policy:
//
//   - deferrable reads are safe to reject and retry without delaying an
//     explicit operator workflow;
//   - exclusive discovery and pairing use their separate single-flight slot;
//   - everything else is general, including malformed, unknown, and future
//     commands, so new work cannot accidentally consume reserved capacity.
//
// Telemetry is observation-only and retryable, including when used to confirm
// curtailment. GetErrors runs alongside scheduled telemetry collection, and
// GetCoolingMode only prefills the settings UI. GetMiningPools remains general
// because it is also a prerequisite for operator pool changes, while
// GetFirmwareUpdateStatus advances the operator-initiated firmware workflow.
func classifyControlCommand(command *pb.AgentCommand) controlCommandAdmissionClass {
	switch typedCommand := command.GetCommand().(type) {
	case *pb.AgentCommand_Discover, *pb.AgentCommand_Pair:
		return controlCommandAdmissionExclusive
	case *pb.AgentCommand_Telemetry:
		return controlCommandAdmissionDeferrableRead
	case *pb.AgentCommand_MinerCommand:
		return classifyMinerCommandAdmission(typedCommand.MinerCommand)
	default:
		return controlCommandAdmissionGeneral
	}
}

func classifyMinerCommandAdmission(command *pb.MinerCommand) controlCommandAdmissionClass {
	switch command.GetAction().(type) {
	case *pb.MinerCommand_GetCoolingMode, *pb.MinerCommand_GetErrors:
		return controlCommandAdmissionDeferrableRead
	case *pb.MinerCommand_Reboot,
		*pb.MinerCommand_StartMining,
		*pb.MinerCommand_StopMining,
		*pb.MinerCommand_BlinkLed,
		*pb.MinerCommand_Curtail,
		*pb.MinerCommand_Uncurtail,
		*pb.MinerCommand_SetCoolingMode,
		*pb.MinerCommand_SetPowerTarget,
		*pb.MinerCommand_UpdateMiningPools,
		*pb.MinerCommand_GetMiningPools,
		*pb.MinerCommand_UpdateMinerPassword,
		*pb.MinerCommand_DownloadLogs,
		*pb.MinerCommand_FirmwareUpdate,
		*pb.MinerCommand_GetFirmwareUpdateStatus,
		*pb.MinerCommand_ApplyCurtailmentConfig:
		return controlCommandAdmissionGeneral
	default:
		return controlCommandAdmissionGeneral
	}
}
