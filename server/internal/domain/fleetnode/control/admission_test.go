package control

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

func TestCommandAdmissionPolicy(t *testing.T) {
	unknown := &gatewaypb.AgentCommand{}
	unknown.ProtoReflect().SetUnknown(protowire.AppendTag(nil, 99, protowire.BytesType))

	tests := []struct {
		name      string
		command   *gatewaypb.AgentCommand
		wantClass CommandAdmissionClass
		wantKind  string
	}{
		{
			name:      "telemetry",
			command:   &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_Telemetry{Telemetry: nil}},
			wantClass: CommandAdmissionDeferrableRead,
			wantKind:  "telemetry",
		},
		{
			name: "cooling mode read",
			command: minerAgentCommand(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetCoolingMode{
				GetCoolingMode: &gatewaypb.GetCoolingModeAction{},
			}}),
			wantClass: CommandAdmissionDeferrableRead,
			wantKind:  "get_cooling_mode",
		},
		{
			name: "error read",
			command: minerAgentCommand(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetErrors{
				GetErrors: &gatewaypb.GetErrorsAction{},
			}}),
			wantClass: CommandAdmissionDeferrableRead,
			wantKind:  "get_errors",
		},
		{
			name: "pool read",
			command: minerAgentCommand(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetMiningPools{
				GetMiningPools: &gatewaypb.GetMiningPoolsAction{},
			}}),
			wantClass: CommandAdmissionGeneral,
			wantKind:  "get_mining_pools",
		},
		{
			name: "firmware status read",
			command: minerAgentCommand(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetFirmwareUpdateStatus{
				GetFirmwareUpdateStatus: &gatewaypb.GetFirmwareUpdateStatusAction{},
			}}),
			wantClass: CommandAdmissionGeneral,
			wantKind:  "get_firmware_update_status",
		},
		{
			name: "mutation",
			command: minerAgentCommand(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_Reboot{
				Reboot: &gatewaypb.RebootAction{},
			}}),
			wantClass: CommandAdmissionGeneral,
			wantKind:  "reboot",
		},
		{
			name:      "empty action",
			command:   minerAgentCommand(&gatewaypb.MinerCommand{}),
			wantClass: CommandAdmissionGeneral,
			wantKind:  "miner_command_empty",
		},
		{
			name:      "empty envelope",
			command:   &gatewaypb.AgentCommand{},
			wantClass: CommandAdmissionGeneral,
			wantKind:  "empty",
		},
		{
			name:      "unknown envelope",
			command:   unknown,
			wantClass: CommandAdmissionGeneral,
			wantKind:  "unknown",
		},
		{
			name:      "malformed envelope",
			command:   nil,
			wantClass: CommandAdmissionGeneral,
			wantKind:  "malformed",
		},
		{
			name: "discovery",
			command: &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_Discover{
				Discover: &pairingpb.DiscoverRequest{},
			}},
			wantClass: CommandAdmissionExclusive,
			wantKind:  "discovery",
		},
		{
			name: "pairing",
			command: &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_Pair{
				Pair: &pairingpb.FleetNodePairRequest{},
			}},
			wantClass: CommandAdmissionExclusive,
			wantKind:  "pairing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, kind := AdmissionForCommand(tt.command)
			assert.Equal(t, tt.wantClass, class)
			assert.Equal(t, tt.wantKind, kind)
		})
	}
}

func TestOptimisticCommandCapacityLeavesOperatorReservation(t *testing.T) {
	require.Equal(t, 512, MaxConcurrentCommandsPerFleetNode)
	require.Equal(t, 8, ReservedGeneralCommandSlotsPerFleetNode)
	require.Equal(t, 504, MaxConcurrentDeferrableReadsPerFleetNode)
	require.Equal(t,
		MaxConcurrentCommandsPerFleetNode,
		MaxConcurrentDeferrableReadsPerFleetNode+ReservedGeneralCommandSlotsPerFleetNode,
	)
	require.GreaterOrEqual(t, outgoingBuffer, MaxConcurrentCommandsPerFleetNode)
}

func minerAgentCommand(command *gatewaypb.MinerCommand) *gatewaypb.AgentCommand {
	return &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_MinerCommand{
		MinerCommand: command,
	}}
}
