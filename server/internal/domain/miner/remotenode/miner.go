// Package remotenode provides the remote-node Miner adapter: an interfaces.Miner
// whose control commands are marshaled and dispatched to a fleet node over the
// ControlStream rather than dialed directly. The fleet node reconstructs a local
// device and executes the command against the LAN miner.
package remotenode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"time"
	"unicode/utf8"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
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

// Keep the remote diagnostics wait aligned with the cloud command worker budget
// and above the fleet node's minerCommandTimeout, while still bounding callers
// such as telemetry error polling that use a long-lived worker context.
var remoteGetErrorsCommandTimeout = 30 * time.Second

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
	ack, err := m.send(ctx, mc)
	if err != nil {
		return err
	}
	return ackToError(ack)
}

func (m *Miner) send(ctx context.Context, mc *gatewaypb.MinerCommand) (*gatewaypb.ControlAck, error) {
	// Pace per fleet node so a large batch can't oversubscribe the node (-> BUSY);
	// the DB command queue holds the backlog while this worker waits for a slot.
	if m.gate != nil {
		release, err := m.gate.Acquire(ctx, m.fleetNodeID)
		if err != nil {
			return nil, fleeterror.NewPlainError(
				fmt.Sprintf("timed out waiting for a fleet node command slot: %v", err),
				connect.CodeResourceExhausted,
			)
		}
		defer release()
	}
	mc.Target = m.desc
	if err := protovalidate.Validate(mc); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid fleet node miner command: %v", err)
	}
	payload, err := proto.Marshal(&gatewaypb.AgentCommand{
		Command: &gatewaypb.AgentCommand_MinerCommand{MinerCommand: mc},
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("marshal miner command: %v", err)
	}
	ack, err := m.sender.SendCommand(ctx, m.fleetNodeID, &gatewaypb.ControlCommand{
		CommandId: id.GenerateID(),
		Payload:   payload,
	})
	if err != nil {
		if errors.Is(err, control.ErrNoActiveStream) {
			// Retryable, not permanent (Unavailable is not in the queue's permanent-fail
			// set), so a node mid-reconnect re-attempts rather than dropping the command.
			return nil, fleeterror.NewUnavailableErrorf("fleet node has no active control stream; retry shortly")
		}
		return nil, err
	}
	return ack, nil
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

func (m *Miner) UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
	pools, err := miningPoolConfigsFromPayload(payload)
	if err != nil {
		return err
	}
	return m.dispatch(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_UpdateMiningPools{
		UpdateMiningPools: &gatewaypb.UpdateMiningPoolsAction{Pools: pools},
	}})
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

func (m *Miner) GetErrors(ctx context.Context) (models.DeviceErrors, error) {
	commandCtx, cancel := context.WithTimeout(ctx, remoteGetErrorsCommandTimeout)
	defer cancel()

	ack, err := m.send(commandCtx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetErrors{
		GetErrors: &gatewaypb.GetErrorsAction{},
	}})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return models.DeviceErrors{}, fleeterror.NewConnectionError(m.desc.GetDeviceIdentifier(), err)
		}
		return models.DeviceErrors{}, err
	}
	if err := ackToError(ack); err != nil {
		return models.DeviceErrors{}, err
	}

	var result gatewaypb.GetErrorsResult
	if err := proto.Unmarshal(ack.GetPayload(), &result); err != nil {
		return models.DeviceErrors{}, fleeterror.NewInternalErrorf("unmarshal get errors result: %v", err)
	}
	if err := protovalidate.Validate(&result); err != nil {
		return models.DeviceErrors{}, fleeterror.NewInternalErrorf("invalid get errors result: %v", err)
	}
	if result.GetDeviceId() != m.desc.GetDeviceIdentifier() {
		return models.DeviceErrors{}, fleeterror.NewInternalErrorf(
			"invalid get errors result: device_id %q does not match requested device %q",
			result.GetDeviceId(), m.desc.GetDeviceIdentifier())
	}
	deviceErrors, err := deviceErrorsFromResult(&result)
	if err != nil {
		return models.DeviceErrors{}, err
	}
	return deviceErrors, nil
}

func (m *Miner) GetCoolingMode(_ context.Context) (commonpb.CoolingMode, error) {
	return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, errUnsupported("GetCoolingMode")
}

func (m *Miner) GetMiningPools(ctx context.Context) ([]interfaces.MinerConfiguredPool, error) {
	ack, err := m.send(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_GetMiningPools{
		GetMiningPools: &gatewaypb.GetMiningPoolsAction{},
	}})
	if err != nil {
		return nil, err
	}
	if err := ackToError(ack); err != nil {
		return nil, err
	}

	var result gatewaypb.GetMiningPoolsResult
	if err := proto.Unmarshal(ack.GetPayload(), &result); err != nil {
		return nil, fleeterror.NewInternalErrorf("unmarshal get mining pools result: %v", err)
	}
	if err := protovalidate.Validate(&result); err != nil {
		return nil, fleeterror.NewInternalErrorf("invalid get mining pools result: %v", err)
	}
	for _, pool := range result.GetPools() {
		if err := sv2.ValidatePoolURL(pool.GetUrl()); err != nil {
			return nil, fleeterror.NewInternalErrorf("invalid get mining pools result: %v", err)
		}
	}

	pools := make([]interfaces.MinerConfiguredPool, 0, len(result.GetPools()))
	for _, pool := range result.GetPools() {
		pools = append(pools, interfaces.MinerConfiguredPool{
			Priority: pool.GetPriority(),
			URL:      pool.GetUrl(),
			Username: pool.GetUsername(),
		})
	}
	return pools, nil
}

func errUnsupported(op string) error {
	return fleeterror.NewUnimplementedErrorf("%s is not yet supported for fleet-node-paired miners", op)
}

func deviceErrorsFromResult(result *gatewaypb.GetErrorsResult) (models.DeviceErrors, error) {
	deviceID := result.GetDeviceId()
	out := models.DeviceErrors{
		DeviceID: deviceID,
		Errors:   make([]models.ErrorMessage, 0, len(result.GetErrors())),
	}
	for _, report := range result.GetErrors() {
		if report.GetDeviceId() != deviceID {
			return models.DeviceErrors{}, fleeterror.NewInternalErrorf(
				"invalid get errors result: error device_id %q does not match result device_id %q",
				report.GetDeviceId(), deviceID)
		}
		// #nosec G115 -- protovalidate enforces a defined non-negative enum before conversion.
		minerError := models.MinerError(report.GetMinerError())
		errMsg := models.ErrorMessage{
			MinerError:        minerError,
			CauseSummary:      report.GetCauseSummary(),
			RecommendedAction: report.GetRecommendedAction(),
			Severity:          severityFromResult(report.GetSeverity(), minerError, deviceID),
			VendorAttributes:  report.GetVendorAttributes(),
			DeviceID:          report.GetDeviceId(),
			ComponentID:       report.ComponentId,
			ComponentType:     componentTypeFromResult(report.GetComponentType()),
			Impact:            report.GetImpact(),
			Summary:           report.GetSummary(),
			VendorCode:        report.GetVendorAttributes()["vendor_code"],
			Firmware:          report.GetVendorAttributes()["firmware"],
		}
		if report.GetFirstSeenAt() != nil {
			errMsg.FirstSeenAt = report.GetFirstSeenAt().AsTime()
		}
		if report.GetLastSeenAt() != nil {
			errMsg.LastSeenAt = report.GetLastSeenAt().AsTime()
		}
		if report.GetClosedAt() != nil {
			closedAt := report.GetClosedAt().AsTime()
			errMsg.ClosedAt = &closedAt
		}
		out.Errors = append(out.Errors, errMsg)
	}
	return out, nil
}

func severityFromResult(value errorspb.Severity, minerError models.MinerError, deviceID string) models.Severity {
	// #nosec G115 -- protovalidate enforces a defined non-negative enum before conversion.
	severity := models.Severity(value)
	if severity != models.SeverityUnspecified {
		return severity
	}
	if info, ok := models.GetMinerErrorInfo()[minerError]; ok {
		severity = info.DefaultSeverity
	} else {
		severity = models.SeverityInfo
	}
	slog.Warn("plugin emitted error with SeverityUnspecified; normalized to default severity",
		"device_id", deviceID,
		"miner_error", minerError,
		"normalized_severity", severity,
	)
	return severity
}

func componentTypeFromResult(value errorspb.ComponentType) models.ComponentType {
	switch value {
	case errorspb.ComponentType_COMPONENT_TYPE_UNSPECIFIED:
		return models.ComponentTypeUnspecified
	case errorspb.ComponentType_COMPONENT_TYPE_PSU:
		return models.ComponentTypePSU
	case errorspb.ComponentType_COMPONENT_TYPE_HASH_BOARD:
		return models.ComponentTypeHashBoards
	case errorspb.ComponentType_COMPONENT_TYPE_FAN:
		return models.ComponentTypeFans
	case errorspb.ComponentType_COMPONENT_TYPE_CONTROL_BOARD:
		return models.ComponentTypeControlBoard
	case errorspb.ComponentType_COMPONENT_TYPE_EEPROM, errorspb.ComponentType_COMPONENT_TYPE_IO_MODULE:
		return models.ComponentTypeUnspecified
	default:
		return models.ComponentTypeUnspecified
	}
}

func miningPoolConfigsFromPayload(payload dto.UpdateMiningPoolsPayload) ([]*gatewaypb.MiningPoolConfig, error) {
	pools := make([]*gatewaypb.MiningPoolConfig, 0, 3)

	pool, err := miningPoolConfigFromDTO(payload.DefaultPool, "default")
	if err != nil {
		return nil, err
	}
	pools = append(pools, pool)

	if payload.Backup1Pool != nil {
		pool, err := miningPoolConfigFromDTO(*payload.Backup1Pool, "backup1")
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	if payload.Backup2Pool != nil {
		pool, err := miningPoolConfigFromDTO(*payload.Backup2Pool, "backup2")
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	return pools, nil
}

func miningPoolConfigFromDTO(pool dto.MiningPool, poolName string) (*gatewaypb.MiningPoolConfig, error) {
	if pool.Priority > math.MaxInt32 {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"%s pool priority %d exceeds int32 maximum", poolName, pool.Priority)
	}
	return &gatewaypb.MiningPoolConfig{
		Priority: int32(pool.Priority), //nolint:gosec // G115: Priority validated above to fit in int32.
		Url:      pool.URL,
		Username: pool.Username,
	}, nil
}
