package ha

import (
	"errors"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

var haTestPair = sqlc.ListFleetNodeHAPairsRow{ActiveNodeID: 1, StandbyNodeID: 2}

func registerDiscoveryPair(t *testing.T, registry *control.Registry, activeRanges ...string) (active, standby *control.Stream) {
	t.Helper()
	active, err := registry.RegisterAuthenticated(1, "active", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	t.Cleanup(active.Unregister)
	active.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: activeRanges})
	standby, err = registry.RegisterAuthenticated(2, "standby", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	t.Cleanup(standby.Unregister)
	standby.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{})
	return active, standby
}

// transferWithAck runs a transfer and answers the install command with reply.
func transferWithAck(t *testing.T, svc *Service, standby *control.Stream, reply func(command *gatewaypb.ControlCommand, installed *gatewaypb.DiscoveryConfig) *gatewaypb.ControlAck) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- svc.transferDiscoveryConfig(t.Context(), haTestPair) }()
	command := <-standby.Outgoing
	env := &gatewaypb.AgentCommand{}
	require.NoError(t, proto.Unmarshal(command.Payload, env))
	ack := reply(command, env.GetInstallDiscoveryConfig())
	ack.CommandId = command.CommandId
	standby.PublishAck(ack)
	return <-done
}

func echoAck(t *testing.T, installed *gatewaypb.DiscoveryConfig) *gatewaypb.ControlAck {
	t.Helper()
	payload, err := proto.Marshal(installed)
	require.NoError(t, err)
	return &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK, Payload: payload}
}

func TestStandbyDiscoveryCopyRequiresAcknowledgedInstallation(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "persisted", false: "failed"}[success], func(t *testing.T) {
			// Arrange
			registry := control.NewRegistry()
			_, standby := registerDiscoveryPair(t, registry, "10.0.0.1-10.0.0.4")
			svc := &Service{registry: registry}
			var requested []string

			// Act
			err := transferWithAck(t, svc, standby, func(_ *gatewaypb.ControlCommand, installed *gatewaypb.DiscoveryConfig) *gatewaypb.ControlAck {
				requested = installed.GetRanges()
				require.Empty(t, registry.DiscoveryConfig(2).Ranges, "standby metadata changes only after acknowledgement")
				ack := echoAck(t, installed)
				if !success {
					ack.Succeeded, ack.Code = false, gatewaypb.AckCode_ACK_CODE_BAD_REQUEST
				}
				return ack
			})

			// Assert
			require.Equal(t, []string{"10.0.0.1-10.0.0.4"}, requested)
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

func TestStandbyDiscoveryCopyRejectsInvalidAcknowledgement(t *testing.T) {
	tests := []struct {
		name     string
		payload  func(t *testing.T) []byte
		contains string
	}{
		{
			name: "different ranges",
			payload: func(t *testing.T) []byte {
				payload, err := proto.Marshal(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.9.9.9"}})
				require.NoError(t, err)
				return payload
			},
			contains: "different discovery ranges",
		},
		{name: "undecodable payload", payload: func(*testing.T) []byte { return []byte{0xff} }, contains: "decode installed discovery configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			registry := control.NewRegistry()
			_, standby := registerDiscoveryPair(t, registry, "10.0.0.1-10.0.0.4")
			svc := &Service{registry: registry}

			// Act
			err := transferWithAck(t, svc, standby, func(*gatewaypb.ControlCommand, *gatewaypb.DiscoveryConfig) *gatewaypb.ControlAck {
				return &gatewaypb.ControlAck{Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK, Payload: tt.payload(t)}
			})

			// Assert
			require.ErrorContains(t, err, tt.contains)
			require.Empty(t, registry.DiscoveryConfig(2).Ranges)
		})
	}
}

func TestStandbyDiscoveryCopyReportsRangeConflict(t *testing.T) {
	tests := []struct {
		name         string
		message      string
		wantConflict bool
	}{
		{name: "standby local ranges conflict", message: control.StandbyDiscoveryConflictMessage, wantConflict: true},
		{name: "other bad request", message: "active node discovery ranges are not configured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			registry := control.NewRegistry()
			_, standby := registerDiscoveryPair(t, registry, "10.0.0.1-10.0.0.4")
			svc := &Service{registry: registry}

			// Act
			err := transferWithAck(t, svc, standby, func(*gatewaypb.ControlCommand, *gatewaypb.DiscoveryConfig) *gatewaypb.ControlAck {
				return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BAD_REQUEST, ErrorMessage: tt.message}
			})

			// Assert
			require.Error(t, err)
			require.Equal(t, tt.wantConflict, errors.Is(err, errStandbyDiscoveryConflict))
			require.Empty(t, registry.DiscoveryConfig(2).Ranges)
		})
	}
}

func TestStandbyDiscoveryCopyFailsWhenStandbyDisconnects(t *testing.T) {
	t.Run("before dispatch", func(t *testing.T) {
		// Arrange
		registry := control.NewRegistry()
		_, standby := registerDiscoveryPair(t, registry, "10.0.0.1-10.0.0.4")
		standby.Unregister()
		svc := &Service{registry: registry}

		// Act
		err := svc.transferDiscoveryConfig(t.Context(), haTestPair)

		// Assert
		require.ErrorContains(t, err, "standby is disconnected")
	})

	t.Run("before acknowledgement", func(t *testing.T) {
		// Arrange
		registry := control.NewRegistry()
		_, standby := registerDiscoveryPair(t, registry, "10.0.0.1-10.0.0.4")
		svc := &Service{registry: registry}
		done := make(chan error, 1)
		go func() { done <- svc.transferDiscoveryConfig(t.Context(), haTestPair) }()
		<-standby.Outgoing

		// Act
		standby.Unregister()
		err := <-done

		// Assert
		require.ErrorIs(t, err, control.ErrNoActiveStream)
	})
}

func TestRecordInstalledDiscoveryConfigRejectsReconnectedStandby(t *testing.T) {
	// Arrange
	registry := control.NewRegistry()
	_, standby := registerDiscoveryPair(t, registry, "10.0.0.1-10.0.0.4")
	connectedAt, ok := registry.ConnectedSince(2)
	require.True(t, ok)
	standby.Unregister()
	reconnected, err := registry.RegisterAuthenticated(2, "standby", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	defer reconnected.Unregister()
	config := &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1-10.0.0.4"}}
	svc := &Service{registry: registry}

	// Act
	err = svc.recordInstalledDiscoveryConfig(2, connectedAt, config, echoAck(t, config))

	// Assert
	require.ErrorContains(t, err, "standby reconnected during discovery configuration installation")
	require.Empty(t, registry.DiscoveryConfig(2).GetRanges(), "the new connection never acknowledged the ranges")
}

func TestStandbyDiscoveryCopyRejectsLegacyAndMissingConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name           string
		config         *gatewaypb.DiscoveryConfig
		standbyVersion gatewaypb.CommandProtocolVersion
	}{
		{name: "legacy standby", config: &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}}, standbyVersion: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7},
		{name: "empty configuration", config: &gatewaypb.DiscoveryConfig{}, standbyVersion: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			registry := control.NewRegistry()
			active, err := registry.RegisterAuthenticated(1, "active", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7)
			require.NoError(t, err)
			defer active.Unregister()
			active.SetDiscoveryConfig(tc.config)
			standby, err := registry.RegisterAuthenticated(2, "standby", tc.standbyVersion)
			require.NoError(t, err)
			defer standby.Unregister()
			svc := &Service{registry: registry}

			// Act
			err = svc.transferDiscoveryConfig(t.Context(), haTestPair)

			// Assert
			require.Error(t, err)
			require.Empty(t, standby.Outgoing)
		})
	}
}
