// Package remotenode provides the remote-node Miner adapter: an interfaces.Miner
// whose control commands are marshaled and dispatched to a fleet node over the
// ControlStream rather than dialed directly. The fleet node reconstructs a local
// device and executes the command against the LAN miner.
package remotenode

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"unicode/utf8"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// CommandSender dispatches a ControlCommand to a fleet node and blocks for its
// terminal ack. *control.Registry satisfies it.
type CommandSender interface {
	SendCommand(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error)
}

// Config carries everything the adapter needs to address a fleet-node-paired miner.
type Config struct {
	Sender      CommandSender
	FleetNodeID int64
	OrgID       int64
	SiteID      int64
	// Gate, if set, bounds concurrent commands the server has in flight to this
	// fleet node so a large batch paces rather than oversubscribing the node.
	Gate Gate

	DeviceIdentifier string
	DriverName       string
	IPAddress        string
	Port             string
	URLScheme        string
	SerialNumber     string
	MacAddress       string
	// CredentialUsername and CredentialPassword are the miner credentials encrypted
	// separately by the fleet node and decrypted just-in-time there. Empty for
	// no-secret drivers.
	CredentialUsername []byte
	CredentialPassword []byte
}

// Miner routes interfaces.Miner control commands to a fleet node. It is a pure
// value (no live connection), so caching the handle is safe; stream liveness is
// resolved per command by the registry.
type Miner struct {
	sender      CommandSender
	gate        Gate
	fleetNodeID int64
	orgID       int64
	siteID      int64
	desc        *gatewaypb.MinerConnectionDescriptor
	connInfo    networking.ConnectionInfo
}

var _ interfaces.Miner = (*Miner)(nil)

// New builds a remote-node miner. It returns an error only if the connection
// coordinates are malformed (bad port/scheme), matching the direct PluginMiner.
func New(cfg Config) (*Miner, error) {
	scheme, err := networking.ProtocolFromString(cfg.URLScheme)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("remote-node miner: parse scheme: %v", err)
	}
	connInfo, err := networking.NewConnectionInfo(cfg.IPAddress, cfg.Port, scheme)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("remote-node miner: connection info: %v", err)
	}
	return &Miner{
		sender:      cfg.Sender,
		gate:        cfg.Gate,
		fleetNodeID: cfg.FleetNodeID,
		orgID:       cfg.OrgID,
		siteID:      cfg.SiteID,
		desc: &gatewaypb.MinerConnectionDescriptor{
			DeviceIdentifier:   cfg.DeviceIdentifier,
			DriverName:         cfg.DriverName,
			IpAddress:          cfg.IPAddress,
			Port:               cfg.Port,
			UrlScheme:          cfg.URLScheme,
			SerialNumber:       cfg.SerialNumber,
			MacAddress:         cfg.MacAddress,
			CredentialUsername: cfg.CredentialUsername,
			CredentialPassword: cfg.CredentialPassword,
		},
		connInfo: *connInfo,
	}, nil
}

func (m *Miner) GetDriverName() string { return m.desc.GetDriverName() }
func (m *Miner) GetID() minermodels.DeviceIdentifier {
	return minermodels.DeviceIdentifier(m.desc.GetDeviceIdentifier())
}
func (m *Miner) GetOrgID() int64                              { return m.orgID }
func (m *Miner) GetSiteID() int64                             { return m.siteID }
func (m *Miner) GetSerialNumber() string                      { return m.desc.GetSerialNumber() }
func (m *Miner) GetConnectionInfo() networking.ConnectionInfo { return m.connInfo }

// GetWebViewURL returns nil: a fleet-node miner sits on the node's LAN and has no
// URL the cloud can link to directly.
func (m *Miner) GetWebViewURL() *url.URL { return nil }

func (m *Miner) Reboot(ctx context.Context) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_Reboot{Reboot: &gatewaypb.RebootAction{}}})
}

func (m *Miner) StartMining(ctx context.Context) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_StartMining{StartMining: &gatewaypb.StartMiningAction{}}})
}

func (m *Miner) StopMining(ctx context.Context) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_StopMining{StopMining: &gatewaypb.StopMiningAction{}}})
}

func (m *Miner) BlinkLED(ctx context.Context) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_BlinkLed{BlinkLed: &gatewaypb.BlinkLedAction{}}})
}

func (m *Miner) Curtail(ctx context.Context, req sdk.CurtailRequest) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_Curtail{
		Curtail: &gatewaypb.CurtailAction{Level: curtailmentpb.CurtailmentLevel(req.Level)},
	}})
}

func (m *Miner) Uncurtail(ctx context.Context, _ sdk.UncurtailRequest) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_Uncurtail{Uncurtail: &gatewaypb.UncurtailAction{}}})
}

func (m *Miner) SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_SetCoolingMode{
		SetCoolingMode: &gatewaypb.SetCoolingModeAction{Mode: payload.Mode},
	}})
}

func (m *Miner) SetPowerTarget(ctx context.Context, payload dto.PowerTargetPayload) error {
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_SetPowerTarget{
		SetPowerTarget: &gatewaypb.SetPowerTargetAction{PerformanceMode: payload.PerformanceMode},
	}})
}

func (m *Miner) dispatch(ctx context.Context, mc *gatewaypb.MinerCommand) error {
	// Pace per fleet node so a large batch can't oversubscribe the node (-> BUSY);
	// the DB command queue holds the backlog while this worker waits for a slot.
	if m.gate != nil {
		release, err := m.gate.Acquire(ctx, m.fleetNodeID)
		if err != nil {
			return fleeterror.NewPlainError(
				fmt.Sprintf("timed out waiting for a fleet node command slot: %v", err),
				connect.CodeResourceExhausted,
			)
		}
		defer release()
	}
	mc.Target = m.desc
	payload, err := proto.Marshal(&gatewaypb.AgentCommand{
		Command: &gatewaypb.AgentCommand_MinerCommand{MinerCommand: mc},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("marshal miner command: %v", err)
	}
	ack, err := m.sender.SendCommand(ctx, m.fleetNodeID, &gatewaypb.ControlCommand{
		CommandId: id.GenerateID(),
		Payload:   payload,
	})
	if err != nil {
		if errors.Is(err, control.ErrNoActiveStream) {
			// Retryable, not permanent (Unavailable is not in the queue's permanent-fail
			// set), so a node mid-reconnect re-attempts rather than dropping the command.
			return fleeterror.NewUnavailableErrorf("fleet node has no active control stream; retry shortly")
		}
		return err
	}
	return ackToError(ack)
}

// maxAckReasonBytes mirrors the node's send-side cap so a buggy/hostile node can't
// bloat logs or the queue with an oversized ack message.
const maxAckReasonBytes = 4096

// clampAckReason truncates an untrusted ack message to maxAckReasonBytes on a UTF-8
// rune boundary so it stays valid when persisted.
func clampAckReason(s string) string {
	if len(s) <= maxAckReasonBytes {
		return s
	}
	cut := maxAckReasonBytes - 3
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// ackToError maps a terminal ack to an error (nil = success). The AckCode drives the
// error category so the execution service reacts right: evict on auth failure,
// permanent-fail on unimplemented, retry on busy.
func ackToError(ack *gatewaypb.ControlAck) error {
	if ack.GetCode() == gatewaypb.AckCode_ACK_CODE_OK && ack.GetSucceeded() {
		return nil
	}
	// Node-supplied: the gateway protovalidates inbound acks, but clamp here too
	// (defense-in-depth) before it's persisted.
	reason := clampAckReason(ack.GetErrorMessage())
	if reason == "" {
		reason = "code " + ack.GetCode().String()
	}
	// if/else (not switch) so the exhaustive linter doesn't demand a case per code.
	code := ack.GetCode()
	if code == gatewaypb.AckCode_ACK_CODE_BAD_REQUEST {
		return fleeterror.NewInvalidArgumentErrorf("fleet node rejected command: %s", reason)
	}
	if code == gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED {
		return fleeterror.NewUnauthenticatedErrorf("miner authentication failed: %s", reason)
	}
	if code == gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED || code == gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE {
		return fleeterror.NewUnimplementedErrorf("command not supported: %s", reason)
	}
	if code == gatewaypb.AckCode_ACK_CODE_BUSY {
		// Retryable, not permanent: ResourceExhausted stays out of the permanent-fail
		// set, so a momentarily-saturated node re-attempts rather than dropping the batch.
		return fleeterror.NewPlainError(
			fmt.Sprintf("fleet node busy; retry shortly: %s", reason),
			connect.CodeResourceExhausted,
		)
	}
	return fleeterror.NewInternalErrorf("fleet node reported command failure: %s", reason)
}

func (m *Miner) UpdateMiningPools(_ context.Context, _ dto.UpdateMiningPoolsPayload) error {
	return errUnsupported("UpdateMiningPools")
}

func (m *Miner) UpdateMinerPassword(_ context.Context, _ dto.UpdateMinerPasswordPayload) error {
	return errUnsupported("UpdateMinerPassword")
}

func (m *Miner) DownloadLogs(_ context.Context, _ string) error {
	return errUnsupported("DownloadLogs")
}

func (m *Miner) FirmwareUpdate(_ context.Context, _ sdk.FirmwareFile) error {
	return errUnsupported("FirmwareUpdate")
}

func (m *Miner) Unpair(_ context.Context) error {
	return errUnsupported("Unpair")
}

func (m *Miner) GetDeviceMetrics(_ context.Context) (modelsV2.DeviceMetrics, error) {
	return modelsV2.DeviceMetrics{}, errUnsupported("GetDeviceMetrics")
}

func (m *Miner) GetDeviceStatus(_ context.Context) (minermodels.MinerStatus, error) {
	return minermodels.MinerStatusUnknown, errUnsupported("GetDeviceStatus")
}

func (m *Miner) GetErrors(_ context.Context) (models.DeviceErrors, error) {
	return models.DeviceErrors{}, errUnsupported("GetErrors")
}

func (m *Miner) GetCoolingMode(_ context.Context) (commonpb.CoolingMode, error) {
	return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, errUnsupported("GetCoolingMode")
}

func (m *Miner) GetMiningPools(_ context.Context) ([]interfaces.MinerConfiguredPool, error) {
	return nil, errUnsupported("GetMiningPools")
}

func errUnsupported(op string) error {
	return fleeterror.NewUnimplementedErrorf("%s is not yet supported for fleet-node-paired miners", op)
}
