package ha

import (
	"context"
	"errors"
	"fmt"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"google.golang.org/protobuf/proto"
)

func (s *Service) transferDiscoveryConfig(ctx context.Context, pair sqlc.ListFleetNodeHAPairsRow) error {
	config := s.registry.DiscoveryConfig(pair.ActiveNodeID)
	if len(config.GetRanges()) == 0 {
		return errors.New(reasonDiscoveryConfigRequired)
	}
	if s.registry.CommandProtocolUpgradeRequiredForVersion(pair.StandbyNodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8) {
		return errors.New(reasonStandbyUpgradeRequired)
	}
	connectedAt, ok := s.registry.ConnectedSince(pair.StandbyNodeID)
	if !ok {
		return fmt.Errorf("standby is disconnected")
	}
	payload, err := proto.Marshal(&gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_InstallDiscoveryConfig{InstallDiscoveryConfig: config}})
	if err != nil {
		return fmt.Errorf("marshal discovery configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	ack, err := s.registry.SendCommand(ctx, pair.StandbyNodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8, &gatewaypb.ControlCommand{CommandId: id.GenerateID(), Payload: payload})
	if err != nil {
		return err
	}
	if !ack.GetSucceeded() || ack.GetCode() != gatewaypb.AckCode_ACK_CODE_OK {
		return control.AckFailure(ack, "discovery configuration")
	}
	installed := &gatewaypb.DiscoveryConfig{}
	if err := proto.Unmarshal(ack.GetPayload(), installed); err != nil {
		return fmt.Errorf("decode installed discovery configuration: %w", err)
	}
	if !proto.Equal(installed, config) {
		return fmt.Errorf("standby acknowledged different discovery ranges")
	}
	if !s.registry.UpdateDiscoveryConfig(pair.StandbyNodeID, connectedAt, installed) {
		return fmt.Errorf("standby reconnected during discovery configuration installation; retry setup")
	}
	return nil
}
