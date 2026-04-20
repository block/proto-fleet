package command

import (
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// commandTypeCapabilityMap maps proto CommandType to required SDK capability constants.
// Commands may require one or more capabilities - if ANY of the capabilities is supported,
// the command is considered supported (OR relationship for cooling modes).
var commandTypeCapabilityMap = map[pb.CommandType][]string{
	pb.CommandType_COMMAND_TYPE_REBOOT:                {sdk.CapabilityReboot},
	pb.CommandType_COMMAND_TYPE_START_MINING:          {sdk.CapabilityMiningStart},
	pb.CommandType_COMMAND_TYPE_STOP_MINING:           {sdk.CapabilityMiningStop},
	pb.CommandType_COMMAND_TYPE_BLINK_LED:             {sdk.CapabilityLEDBlink},
	pb.CommandType_COMMAND_TYPE_SET_COOLING_MODE:      {sdk.CapabilityCoolingModeAir, sdk.CapabilityCoolingModeImmerse},
	pb.CommandType_COMMAND_TYPE_UPDATE_MINING_POOLS:   {sdk.CapabilityPoolConfig},
	pb.CommandType_COMMAND_TYPE_DOWNLOAD_LOGS:         {sdk.CapabilityLogsDownload},
	pb.CommandType_COMMAND_TYPE_FIRMWARE_UPDATE:       {sdk.CapabilityManualUpload},
	pb.CommandType_COMMAND_TYPE_SET_POWER_TARGET:      {sdk.CapabilityPowerModeEfficiency},
	pb.CommandType_COMMAND_TYPE_UPDATE_MINER_PASSWORD: {sdk.CapabilityUpdateMinerPassword},
}

// GetRequiredCapabilities returns the SDK capability constants required for a command type.
// Returns nil for command types that don't require capability checking.
func GetRequiredCapabilities(cmdType pb.CommandType) []string {
	return commandTypeCapabilityMap[cmdType]
}

// RequiresCapabilityCheck returns true if the command type requires capability checking.
func RequiresCapabilityCheck(cmdType pb.CommandType) bool {
	_, exists := commandTypeCapabilityMap[cmdType]
	return exists
}
