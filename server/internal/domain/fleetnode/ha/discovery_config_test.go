package ha

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestStandbyDiscoveryCopyRequiresAcknowledgedInstallation(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "persisted", false: "failed"}[success], func(t *testing.T) {
			registry := control.NewRegistry()
			active, err := registry.RegisterAuthenticated(1, "active", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			defer active.Unregister()
			active.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1-10.0.0.4"}})
			standby, err := registry.RegisterAuthenticated(2, "standby", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
			require.NoError(t, err)
			defer standby.Unregister()
			standby.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{})
			svc := &Service{registry: registry}
			done := make(chan error, 1)
			go func() {
				done <- svc.transferDiscoveryConfig(t.Context(), sqlc.ListFleetNodeHAPairsRow{ActiveNodeID: 1, StandbyNodeID: 2})
			}()
			command := <-standby.Outgoing
			env := &gatewaypb.AgentCommand{}
			require.NoError(t, proto.Unmarshal(command.Payload, env))
			require.Equal(t, []string{"10.0.0.1-10.0.0.4"}, env.GetInstallDiscoveryConfig().Ranges)
			require.Empty(t, registry.DiscoveryConfig(2).Ranges)
			payload, err := proto.Marshal(env.GetInstallDiscoveryConfig())
			require.NoError(t, err)
			code := gatewaypb.AckCode_ACK_CODE_BAD_REQUEST
			if success {
				code = gatewaypb.AckCode_ACK_CODE_OK
			}
			standby.PublishAck(&gatewaypb.ControlAck{CommandId: command.CommandId, Succeeded: success, Code: code, Payload: payload})
			err = <-done
			if success {
				require.NoError(t, err)
				require.Equal(t, []string{"10.0.0.1-10.0.0.4"}, registry.DiscoveryConfig(2).Ranges)
			} else {
				require.Error(t, err)
				require.Empty(t, registry.DiscoveryConfig(2).Ranges)
			}
		})
	}
}

func TestStandbyDiscoveryCopyRejectsLegacyAndMissingConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name           string
		config         *gatewaypb.DiscoveryConfig
		standbyVersion gatewaypb.CommandProtocolVersion
	}{
		{name: "legacy nodes without configuration", standbyVersion: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7},
		{name: "legacy standby", config: &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}}, standbyVersion: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7},
		{name: "missing configuration", standbyVersion: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8},
		{name: "empty configuration", config: &gatewaypb.DiscoveryConfig{}, standbyVersion: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := control.NewRegistry()
			active, err := registry.RegisterAuthenticated(1, "active", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7)
			require.NoError(t, err)
			defer active.Unregister()
			active.SetDiscoveryConfig(tc.config)
			standby, err := registry.RegisterAuthenticated(2, "standby", tc.standbyVersion)
			require.NoError(t, err)
			defer standby.Unregister()
			svc := &Service{registry: registry}

			require.Error(t, svc.transferDiscoveryConfig(t.Context(), sqlc.ListFleetNodeHAPairsRow{ActiveNodeID: 1, StandbyNodeID: 2}))
			require.Empty(t, standby.Outgoing)
		})
	}
}
