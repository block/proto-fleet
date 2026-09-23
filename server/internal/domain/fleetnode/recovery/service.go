package recovery

import (
	"context"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stableidentity"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	maxTargetsPerCommand  = 512
	maxEncodedRequest     = 900 * 1024
	recoveryOutcomePrefix = "MINER_ENDPOINT_RECOVERY_OUTCOME_"
)

var commandTimeout = 12 * time.Minute

type Store interface {
	GetOfflineFleetNodeDevices(ctx context.Context) ([]stores.FleetNodeRecoveryTarget, error)
	ApplyFleetNodeRecoveredEndpoint(ctx context.Context, target stores.FleetNodeRecoveryTarget, ipAddress, port, urlScheme string) (bool, error)
	ApplyFleetNodeRecoveryAuthenticationNeeded(ctx context.Context, target stores.FleetNodeRecoveryTarget, ipAddress, port, urlScheme string) (bool, error)
}

type Sender interface {
	SendCommand(ctx context.Context, nodeID int64, version gatewaypb.CommandProtocolVersion, command *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error)
}

type MinerInvalidator interface {
	InvalidateMiner(identifier minermodels.DeviceIdentifier)
}

type MetricsEmitter interface {
	EmitCommand(ctx context.Context, labels metrics.CommandLabels)
}

type Service struct {
	store       Store
	sender      Sender
	invalidator MinerInvalidator
	metrics     MetricsEmitter
	logger      *slog.Logger
	// RunCycle is owned by one serial recovery loop. Recovery writes are guarded
	// and idempotent, so restarting from the oldest target is safe.
	nextTarget map[int64]string
}

func NewService(store Store, sender Sender, invalidator MinerInvalidator, emitter MetricsEmitter, logger *slog.Logger) *Service {
	return &Service{
		store:       store,
		sender:      sender,
		invalidator: invalidator,
		metrics:     emitter,
		logger:      logger.With("component", "fleet_node_ip_recovery"),
		nextTarget:  make(map[int64]string),
	}
}

func (s *Service) RunCycle(ctx context.Context) {
	targets, err := s.store.GetOfflineFleetNodeDevices(ctx)
	if err != nil {
		s.logger.Error("listing offline Fleet Node miners", "error", err)
		return
	}
	byNode := make(map[int64][]stores.FleetNodeRecoveryTarget)
	for _, target := range targets {
		byNode[target.FleetNodeID] = append(byNode[target.FleetNodeID], target)
	}
	nodeIDs := slices.Sorted(maps.Keys(byNode))
	for _, nodeID := range nodeIDs {
		selected, payload := s.selectTargets(nodeID, byNode[nodeID])
		if len(selected) == 0 {
			s.logger.Warn("Fleet Node recovery targets exceed command limits", "fleet_node_id", nodeID, "available", len(byNode[nodeID]))
			continue
		}
		s.runNode(ctx, nodeID, selected, payload)
	}
}

func (s *Service) selectTargets(nodeID int64, targets []stores.FleetNodeRecoveryTarget) ([]stores.FleetNodeRecoveryTarget, []byte) {
	start := slices.IndexFunc(targets, func(target stores.FleetNodeRecoveryTarget) bool {
		return target.DeviceIdentifier == s.nextTarget[nodeID]
	})
	if start > 0 {
		rotated := make([]stores.FleetNodeRecoveryTarget, 0, len(targets))
		rotated = append(rotated, targets[start:]...)
		targets = append(rotated, targets[:start]...)
	}
	selected, payload, next := selectTargets(targets)
	// Advance before network I/O so a timeout cannot monopolize the next cycle.
	if next == "" {
		delete(s.nextTarget, nodeID)
	} else {
		s.nextTarget[nodeID] = next
	}
	return selected, payload
}

func selectTargets(targets []stores.FleetNodeRecoveryTarget) ([]stores.FleetNodeRecoveryTarget, []byte, string) {
	selected := make([]stores.FleetNodeRecoveryTarget, 0, min(len(targets), maxTargetsPerCommand))
	descriptors := make([]*gatewaypb.MinerConnectionDescriptor, 0, cap(selected))
	scanPorts := make([]string, 0, discoverylimits.MaxPortsPerIP)
	seenPorts := make(map[string]struct{}, discoverylimits.MaxPortsPerIP)
	var nextTarget string
	command := &gatewaypb.AgentCommand{Command: &gatewaypb.AgentCommand_RecoverMinerEndpoints{
		RecoverMinerEndpoints: &gatewaypb.RecoverMinerEndpointsRequest{Targets: descriptors, ScanPorts: scanPorts},
	}}
	for _, target := range targets {
		if len(selected) == maxTargetsPerCommand {
			if nextTarget == "" {
				nextTarget = target.DeviceIdentifier
			}
			break
		}
		_, seenPort := seenPorts[target.LastKnownPort]
		if !seenPort && len(scanPorts) == discoverylimits.MaxPortsPerIP {
			if nextTarget == "" {
				nextTarget = target.DeviceIdentifier
			}
			continue
		}
		descriptor := descriptorFromTarget(target)
		descriptors = append(descriptors, descriptor)
		if !seenPort {
			seenPorts[target.LastKnownPort] = struct{}{}
			scanPorts = append(scanPorts, target.LastKnownPort)
		}
		command.GetRecoverMinerEndpoints().Targets = descriptors
		command.GetRecoverMinerEndpoints().ScanPorts = scanPorts
		if proto.Size(command) > maxEncodedRequest {
			descriptors = descriptors[:len(descriptors)-1]
			if !seenPort {
				delete(seenPorts, target.LastKnownPort)
				scanPorts = scanPorts[:len(scanPorts)-1]
			}
			command.GetRecoverMinerEndpoints().Targets = descriptors
			command.GetRecoverMinerEndpoints().ScanPorts = scanPorts
			if nextTarget == "" {
				nextTarget = target.DeviceIdentifier
			}
			break
		}
		selected = append(selected, target)
	}
	if len(selected) == 0 {
		return nil, nil, nextTarget
	}
	payload, err := proto.Marshal(command)
	if err != nil {
		return nil, nil, nextTarget
	}
	return selected, payload, nextTarget
}

func descriptorFromTarget(target stores.FleetNodeRecoveryTarget) *gatewaypb.MinerConnectionDescriptor {
	return &gatewaypb.MinerConnectionDescriptor{
		DeviceIdentifier:   target.DeviceIdentifier,
		DriverName:         target.DriverName,
		IpAddress:          target.LastKnownIP,
		Port:               target.LastKnownPort,
		UrlScheme:          target.LastKnownScheme,
		SerialNumber:       target.SerialNumber,
		MacAddress:         target.MacAddress,
		CredentialUsername: target.CredentialUsername,
		CredentialPassword: target.CredentialPassword,
	}
}

func (s *Service) runNode(ctx context.Context, nodeID int64, targets []stores.FleetNodeRecoveryTarget, payload []byte) {
	cmd := &gatewaypb.ControlCommand{CommandId: id.GenerateID(), Payload: payload}
	if err := protovalidate.Validate(cmd); err != nil {
		s.logger.Error("Fleet Node recovery command failed validation", "fleet_node_id", nodeID, "targets", len(targets), "payload_bytes", len(payload), "error", err)
		return
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	ack, err := s.sender.SendCommand(commandCtx, nodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, cmd)
	if err != nil {
		s.logger.Warn("Fleet Node recovery command failed", "fleet_node_id", nodeID, "targets", len(targets), "error", err)
		return
	}
	if ack == nil {
		s.logger.Warn("Fleet Node recovery command returned no acknowledgement", "fleet_node_id", nodeID)
		return
	}
	if ack.GetCode() == gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED {
		s.logger.Warn("Fleet Node recovery command is unsupported; upgrade the node before server activation", "fleet_node_id", nodeID)
		return
	}
	if ack.GetCode() != gatewaypb.AckCode_ACK_CODE_OK && ack.GetCode() != gatewaypb.AckCode_ACK_CODE_PARTIAL {
		s.logger.Warn("Fleet Node recovery command was rejected", "fleet_node_id", nodeID, "ack_code", ack.GetCode().String())
		return
	}
	if ack.GetCode() == gatewaypb.AckCode_ACK_CODE_OK && !ack.GetSucceeded() {
		s.logger.Warn("Fleet Node recovery returned an inconsistent acknowledgement", "fleet_node_id", nodeID)
		return
	}

	response := &gatewaypb.RecoverMinerEndpointsResult{}
	if err := proto.Unmarshal(ack.GetPayload(), response); err != nil || protovalidate.Validate(response) != nil {
		s.logger.Warn("Fleet Node recovery returned an invalid payload", "fleet_node_id", nodeID)
		return
	}
	requested := make(map[string]stores.FleetNodeRecoveryTarget, len(targets))
	for _, target := range targets {
		requested[target.DeviceIdentifier] = target
	}
	seen := make(map[string]struct{}, len(response.GetResults()))
	for _, result := range response.GetResults() {
		identifier := result.GetDeviceIdentifier()
		if _, duplicate := seen[identifier]; duplicate {
			s.logger.Warn("Fleet Node recovery returned duplicate device results", "fleet_node_id", nodeID)
			return
		}
		if _, known := requested[identifier]; !known {
			s.logger.Warn("Fleet Node recovery returned an unrequested device", "fleet_node_id", nodeID)
			return
		}
		seen[identifier] = struct{}{}
	}

	counts := make(map[gatewaypb.MinerEndpointRecoveryOutcome]int)
	for _, result := range response.GetResults() {
		target := requested[result.GetDeviceIdentifier()]
		applied := s.applyResult(ctx, target, result)
		counts[result.GetOutcome()]++
		s.emitResult(ctx, target, result.GetOutcome(), applied)
	}
	attrs := []any{"fleet_node_id", nodeID, "requested", len(targets), "reported", len(response.GetResults()), "partial", ack.GetCode() == gatewaypb.AckCode_ACK_CODE_PARTIAL}
	for outcome, count := range counts {
		attrs = append(attrs, recoveryOutcomeLabel(outcome), count)
	}
	s.logger.Info("Fleet Node recovery cycle completed", attrs...)
}

func (s *Service) applyResult(ctx context.Context, target stores.FleetNodeRecoveryTarget, result *gatewaypb.MinerEndpointRecoveryResult) bool {
	want := stableidentity.New(target.SerialNumber, target.MacAddress)
	got := stableidentity.New(result.GetSerialNumber(), result.GetMacAddress())
	if !want.Matches(got) {
		return false
	}
	switch result.GetOutcome() {
	case gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND:
		if !validPrivateEndpoint(result.GetIpAddress(), result.GetPort(), result.GetUrlScheme()) {
			return false
		}
		applied, err := s.store.ApplyFleetNodeRecoveredEndpoint(ctx, target, result.GetIpAddress(), result.GetPort(), result.GetUrlScheme())
		if err != nil {
			s.logger.Error("persisting recovered Fleet Node miner endpoint", "fleet_node_id", target.FleetNodeID, "error", err)
			return false
		}
		if applied && s.invalidator != nil {
			s.invalidator.InvalidateMiner(minermodels.DeviceIdentifier(target.DeviceIdentifier))
		}
		return applied
	case gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED:
		if !validPrivateEndpoint(result.GetIpAddress(), result.GetPort(), result.GetUrlScheme()) {
			return false
		}
		applied, err := s.store.ApplyFleetNodeRecoveryAuthenticationNeeded(ctx, target, result.GetIpAddress(), result.GetPort(), result.GetUrlScheme())
		if err != nil {
			s.logger.Error("persisting Fleet Node miner authentication state", "fleet_node_id", target.FleetNodeID, "error", err)
			return false
		}
		if applied && s.invalidator != nil {
			s.invalidator.InvalidateMiner(minermodels.DeviceIdentifier(target.DeviceIdentifier))
		}
		return applied
	case gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_UNSPECIFIED,
		gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_NOT_FOUND,
		gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_UNRECOVERABLE_IDENTITY,
		gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AMBIGUOUS,
		gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR:
		return false
	}
	return false
}

func validPrivateEndpoint(ipAddress, port, scheme string) bool {
	addr, err := netip.ParseAddr(ipAddress)
	if err != nil || !addr.IsPrivate() {
		return false
	}
	portNumber, err := sdk.ParsePort(port)
	if err != nil || portNumber == 0 {
		return false
	}
	return networking.IsValidURLScheme(scheme)
}

func (s *Service) emitResult(ctx context.Context, target stores.FleetNodeRecoveryTarget, outcome gatewaypb.MinerEndpointRecoveryOutcome, applied bool) {
	if s.metrics == nil {
		return
	}
	result := metrics.ResultFailure
	if applied {
		result = metrics.ResultSuccess
	}
	s.metrics.EmitCommand(ctx, metrics.CommandLabels{
		OrganizationID: metrics.OrgIDToLabel(target.OrgID),
		Kind:           "fleet_node_ip_recovery_" + recoveryOutcomeLabel(outcome),
		Result:         result,
	})
}

func recoveryOutcomeLabel(outcome gatewaypb.MinerEndpointRecoveryOutcome) string {
	return strings.ToLower(strings.TrimPrefix(outcome.String(), recoveryOutcomePrefix))
}
