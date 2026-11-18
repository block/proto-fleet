package control

import (
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// MinerCommandReadOnly reports whether a miner command only reads or drives an
// indicator. Every other action, an undecodable request and any action added
// later count as a physical write until they are classified here. The write
// gate and command cancel both follow this one rule.
func MinerCommandReadOnly(command *gatewaypb.MinerCommand) bool {
	switch action := command.GetAction().(type) {
	case *gatewaypb.MinerCommand_ContainerMining:
		r, err := sdk.DecodeContainerMiningRequest(action.ContainerMining.GetRequest())
		return err == nil && r.ReadOnly()
	case *gatewaypb.MinerCommand_ContainerPowerFeed:
		r, err := sdk.DecodeContainerPowerFeedRequest(action.ContainerPowerFeed.GetRequest())
		return err == nil && r.ReadOnly()
	case *gatewaypb.MinerCommand_MiningGroups:
		r, err := sdk.DecodeMiningGroupRequest(action.MiningGroups.GetRequest())
		return err == nil && r.ReadOnly()
	case *gatewaypb.MinerCommand_VerifyPoolsCleared, *gatewaypb.MinerCommand_GetCoolingMode, *gatewaypb.MinerCommand_GetErrors, *gatewaypb.MinerCommand_GetMiningPools, *gatewaypb.MinerCommand_GetFirmwareUpdateStatus, *gatewaypb.MinerCommand_DownloadLogs, *gatewaypb.MinerCommand_BlinkLed, *gatewaypb.MinerCommand_SetBoardIdentify, *gatewaypb.MinerCommand_ControllerRecords, *gatewaypb.MinerCommand_StreamBoardAsicReadings:
		return true
	default:
		return false
	}
}

// CommandCancellable reports whether a node stops command when the server
// sends ControlCancel for it. Only work that reads is cancellable: telemetry,
// discovery, and a read-only miner command other than an indicator. A cancel
// for anything else is ignored, so a physical write, an indicator change,
// pairing or a credential key change is never interrupted part way and its ack
// always reports what it did.
func CommandCancellable(command *gatewaypb.AgentCommand) bool {
	switch typed := command.GetCommand().(type) {
	case *gatewaypb.AgentCommand_Telemetry, *gatewaypb.AgentCommand_Discover:
		return true
	case *gatewaypb.AgentCommand_MinerCommand:
		return MinerCommandReadOnly(typed.MinerCommand) && !minerCommandDrivesIndicator(typed.MinerCommand)
	default:
		return false
	}
}

// minerCommandDrivesIndicator reports whether a miner command sets an LED or
// board identify indicator. These skip the write gate like reads, but a
// cancel part way could leave the indicator in an unknown state.
func minerCommandDrivesIndicator(command *gatewaypb.MinerCommand) bool {
	switch command.GetAction().(type) {
	case *gatewaypb.MinerCommand_BlinkLed, *gatewaypb.MinerCommand_SetBoardIdentify:
		return true
	default:
		return false
	}
}
