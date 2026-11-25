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
			name: "board ASIC stream",
			command: minerAgentCommand(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_StreamBoardAsicReadings{
				StreamBoardAsicReadings: &gatewaypb.StreamBoardASICReadingsAction{},
			}}),
			wantClass: CommandAdmissionDeferrableRead,
			wantKind:  "stream_board_asic_readings",
		},
		{
			name:      "power feed inspection",
			command:   minerAgentCommand(powerFeedCommand(`{"action":"inspect","module":{"tank":1,"module":1}}`)),
			wantClass: CommandAdmissionDeferrableRead,
			wantKind:  "container_power_feed_inspect",
		},
		{
			name:      "power feed action other than inspection",
			command:   minerAgentCommand(powerFeedCommand(`{"action":"turn_off","feed_id":"pdu-1-outlet-01","module":{"tank":1,"module":1}}`)),
			wantClass: CommandAdmissionGeneral,
			wantKind:  "container_power_feed",
		},
		{
			name:      "undecodable power feed request",
			command:   minerAgentCommand(powerFeedCommand(`{"action":"inspect","module":{"tank":1,"module":1},"outlet":"x"}`)),
			wantClass: CommandAdmissionGeneral,
			wantKind:  "container_power_feed",
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
			name: "pool clearing observation",
			command: minerAgentCommand(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_VerifyPoolsCleared{
				VerifyPoolsCleared: &gatewaypb.VerifyPoolsClearedAction{},
			}}),
			wantClass: CommandAdmissionGeneral,
			wantKind:  "verify_pools_cleared",
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
		{
			name: "endpoint recovery",
			command: &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_RecoverMinerEndpoints{
				RecoverMinerEndpoints: &gatewaypb.RecoverMinerEndpointsRequest{},
			}},
			wantClass: CommandAdmissionExclusive,
			wantKind:  "endpoint_recovery",
		},
		{
			name: "credential key export",
			command: &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_ExportCredentialKey{
				ExportCredentialKey: &gatewaypb.ExportCredentialKey{},
			}},
			wantClass: CommandAdmissionGeneral,
			wantKind:  "credential_key_export",
		},
		{
			name: "credential key install",
			command: &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_InstallCredentialKey{
				InstallCredentialKey: &gatewaypb.InstallCredentialKey{},
			}},
			wantClass: CommandAdmissionGeneral,
			wantKind:  "credential_key_install",
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

func powerFeedCommand(request string) *gatewaypb.MinerCommand {
	return &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_ContainerPowerFeed{
		ContainerPowerFeed: &gatewaypb.ContainerPowerFeedAction{Request: []byte(request)},
	}}
}
