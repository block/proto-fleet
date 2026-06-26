package miner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	diagnosticsModels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	remoteTelemetryStatusTTL          = 15 * time.Second
	remoteTelemetryAckSlack           = time.Second
	remoteTelemetryGateAcquireTimeout = 30 * time.Second
)

var remoteTelemetryDefaultCommandTimeout = 5 * time.Second

var _ interfaces.Miner = (*RemoteFleetNodeMiner)(nil)

type remoteTelemetryRoute struct {
	fleetNodeID        int64
	orgID              int64
	siteID             int64
	deviceIdentifier   string
	driverName         string
	manufacturer       string
	model              string
	firmwareVersion    string
	serialNumber       string
	macAddress         string
	ipAddress          string
	port               string
	urlScheme          string
	credentialUsername []byte
	credentialPassword []byte
}

type cachedTelemetryStatus struct {
	status    models.MinerStatus
	fetchedAt time.Time
}

type RemoteFleetNodeMiner struct {
	route          remoteTelemetryRoute
	sender         remotenode.CommandSender
	gate           remotenode.Gate
	delegate       interfaces.Miner
	connectionInfo networking.ConnectionInfo

	mu     sync.Mutex
	latest *cachedTelemetryStatus
	now    func() time.Time
}

func (s *Service) remoteRouteFromRow(row sqlc.GetFleetNodeTelemetryRouteByDeviceIdentifierRow) (remoteTelemetryRoute, error) {
	return remoteTelemetryRoute{
		fleetNodeID:        row.FleetNodeID,
		orgID:              row.OrgID,
		siteID:             row.SiteID.Int64,
		deviceIdentifier:   row.DeviceIdentifier,
		driverName:         row.DriverName,
		manufacturer:       row.Manufacturer.String,
		model:              row.Model.String,
		firmwareVersion:    row.FirmwareVersion.String,
		serialNumber:       row.SerialNumber.String,
		macAddress:         row.MacAddress,
		ipAddress:          row.IpAddress,
		port:               row.Port,
		urlScheme:          row.UrlScheme,
		credentialUsername: fleetNodeCredentialBytes(row.UsernameEnc),
		credentialPassword: fleetNodeCredentialBytes(row.PasswordEnc),
	}, nil
}

func newRemoteFleetNodeMiner(route remoteTelemetryRoute, sender remotenode.CommandSender, gate remotenode.Gate, delegate interfaces.Miner) (*RemoteFleetNodeMiner, error) {
	scheme, err := networking.ProtocolFromString(route.urlScheme)
	if err != nil {
		return nil, err
	}
	conn, err := networking.NewConnectionInfo(route.ipAddress, route.port, scheme)
	if err != nil {
		return nil, err
	}
	return &RemoteFleetNodeMiner{
		route:          route,
		sender:         sender,
		gate:           gate,
		delegate:       delegate,
		connectionInfo: *conn,
		now:            time.Now,
	}, nil
}

func (m *RemoteFleetNodeMiner) GetDriverName() string {
	return m.route.driverName
}

func (m *RemoteFleetNodeMiner) GetID() models.DeviceIdentifier {
	return models.DeviceIdentifier(m.route.deviceIdentifier)
}

func (m *RemoteFleetNodeMiner) GetOrgID() int64 {
	return m.route.orgID
}

func (m *RemoteFleetNodeMiner) GetSiteID() int64 {
	return m.route.siteID
}

func (m *RemoteFleetNodeMiner) GetSerialNumber() string {
	return m.route.serialNumber
}

func (m *RemoteFleetNodeMiner) GetConnectionInfo() networking.ConnectionInfo {
	return m.connectionInfo
}

func (m *RemoteFleetNodeMiner) GetWebViewURL() *url.URL {
	return m.connectionInfo.GetURL()
}

func (m *RemoteFleetNodeMiner) GetDeviceMetrics(ctx context.Context) (modelsV2.DeviceMetrics, error) {
	result, err := m.fetchTelemetry(ctx)
	if err != nil {
		return modelsV2.DeviceMetrics{}, err
	}
	metrics, err := telemetryResultToDeviceMetrics(result)
	if err != nil {
		return modelsV2.DeviceMetrics{}, err
	}
	if err := m.validateTelemetryDeviceIdentifier("result", metrics.DeviceIdentifier); err != nil {
		return modelsV2.DeviceMetrics{}, err
	}
	m.rememberStatus(result.GetDeviceStatus())
	return metrics, nil
}

func (m *RemoteFleetNodeMiner) GetDeviceStatus(ctx context.Context) (models.MinerStatus, error) {
	m.mu.Lock()
	if m.latest != nil && m.now().Sub(m.latest.fetchedAt) <= remoteTelemetryStatusTTL {
		status := m.latest.status
		m.mu.Unlock()
		return status, nil
	}
	m.mu.Unlock()

	result, err := m.fetchTelemetry(ctx)
	if err != nil {
		return models.MinerStatusOffline, err
	}
	if err := m.validateTelemetryDeviceIdentifier("status", result.GetDeviceIdentifier()); err != nil {
		return models.MinerStatusOffline, err
	}
	status := deviceStatusToMinerStatus(result.GetDeviceStatus())
	m.rememberStatus(result.GetDeviceStatus())
	return status, nil
}

func (m *RemoteFleetNodeMiner) validateTelemetryDeviceIdentifier(kind string, got string) error {
	if got == m.route.deviceIdentifier {
		return nil
	}
	return fleeterror.NewInternalErrorf(
		"fleet node telemetry %s device_identifier mismatch: got %q, want %q",
		kind,
		got,
		m.route.deviceIdentifier,
	)
}

func (m *RemoteFleetNodeMiner) fetchTelemetry(ctx context.Context) (*telemetrypb.FleetNodeTelemetryResult, error) {
	if m.sender == nil {
		return nil, fleeterror.NewConnectionError(m.route.deviceIdentifier, errors.New("fleet node control registry is not configured"))
	}
	commandTimeout := remoteTelemetryCommandTimeoutFromContext(ctx)
	if m.gate != nil {
		gateCtx, cancel := remoteTelemetryGateContext(ctx)
		release, err := m.gate.Acquire(gateCtx, m.route.fleetNodeID)
		cancel()
		if err != nil {
			return nil, fleeterror.NewPlainError(
				fmt.Sprintf("timed out waiting for a fleet node telemetry command slot: %v", err),
				connect.CodeResourceExhausted,
			)
		}
		defer release()
	}
	if err := ctx.Err(); err != nil {
		return nil, fleeterror.NewPlainError(
			fmt.Sprintf("fleet node telemetry context ended before command send: %v", err),
			connect.CodeDeadlineExceeded,
		)
	}
	commandCtx, cancel := remoteTelemetryCommandContext(ctx, commandTimeout)
	defer cancel()
	payload, err := proto.Marshal(&gatewaypb.AgentCommand{
		Command: &gatewaypb.AgentCommand_Telemetry{
			Telemetry: m.telemetryRequest(commandCtx),
		},
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("marshal fleet node telemetry command: %v", err)
	}
	ack, err := m.sender.SendCommand(commandCtx, m.route.fleetNodeID, &gatewaypb.ControlCommand{
		CommandId: id.GenerateID(),
		Payload:   payload,
	})
	if err != nil {
		if errors.Is(err, control.ErrNoActiveStream) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fleeterror.NewConnectionError(m.route.deviceIdentifier, err)
		}
		return nil, err
	}
	if ack.GetCode() != gatewaypb.AckCode_ACK_CODE_OK || !ack.GetSucceeded() {
		return nil, m.errorFromAck(ack)
	}
	if len(ack.GetPayload()) == 0 {
		return nil, fleeterror.NewInternalError("fleet node telemetry ack missing payload")
	}
	result := &telemetrypb.FleetNodeTelemetryResult{}
	if err := proto.Unmarshal(ack.GetPayload(), result); err != nil {
		return nil, fleeterror.NewInternalErrorf("unmarshal fleet node telemetry payload: %v", err)
	}
	if err := validateTelemetryResult(result); err != nil {
		return nil, err
	}
	if result.GetDeviceIdentifier() != m.route.deviceIdentifier {
		return nil, fleeterror.NewInternalErrorf(
			"fleet node telemetry result device_identifier mismatch: got %q, want %q",
			result.GetDeviceIdentifier(),
			m.route.deviceIdentifier,
		)
	}
	return result, nil
}

func (m *RemoteFleetNodeMiner) telemetryRequest(ctx context.Context) *telemetrypb.FleetNodeTelemetryRequest {
	req := &telemetrypb.FleetNodeTelemetryRequest{
		DeviceIdentifier:   m.route.deviceIdentifier,
		IpAddress:          m.route.ipAddress,
		Port:               m.route.port,
		UrlScheme:          m.route.urlScheme,
		DriverName:         m.route.driverName,
		Manufacturer:       m.route.manufacturer,
		Model:              m.route.model,
		FirmwareVersion:    m.route.firmwareVersion,
		SerialNumber:       m.route.serialNumber,
		MacAddress:         m.route.macAddress,
		CredentialUsername: m.route.credentialUsername,
		CredentialPassword: m.route.credentialPassword,
	}
	if timeout := remoteTelemetryTimeoutFromContext(ctx); timeout > 0 {
		req.Timeout = durationpb.New(timeout)
	}
	return req
}

func remoteTelemetryCommandTimeoutFromContext(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	timeout := time.Until(deadline)
	if timeout <= 0 {
		return 0
	}
	return timeout
}

func remoteTelemetryGateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, remoteTelemetryGateAcquireTimeout)
}

func remoteTelemetryCommandContext(ctx context.Context, commandTimeout time.Duration) (context.Context, context.CancelFunc) {
	if commandTimeout <= 0 {
		commandTimeout = remoteTelemetryDefaultCommandTimeout
	}
	return context.WithTimeout(ctx, commandTimeout)
}

func remoteTelemetryTimeoutFromContext(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	if remaining > remoteTelemetryAckSlack {
		return remaining - remoteTelemetryAckSlack
	}
	return remaining
}

func (m *RemoteFleetNodeMiner) errorFromAck(ack *gatewaypb.ControlAck) error {
	msg := ack.GetErrorMessage()
	if msg == "" {
		msg = fmt.Sprintf("fleet node telemetry command failed with ack code %s", ack.GetCode().String())
	}
	switch ack.GetCode() {
	case gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE:
		return fleeterror.NewUnimplementedError(msg)
	case gatewaypb.AckCode_ACK_CODE_BAD_REQUEST:
		return fleeterror.NewInvalidArgumentError(msg)
	case gatewaypb.AckCode_ACK_CODE_BUSY:
		return fleeterror.NewUnavailableErrorf("%s", msg)
	case gatewaypb.AckCode_ACK_CODE_FORBIDDEN:
		return fleeterror.NewForbiddenError(msg)
	case gatewaypb.AckCode_ACK_CODE_UNAUTHENTICATED:
		return fleeterror.NewUnauthenticatedErrorf("fleet node telemetry authentication failed: %s", msg)
	case gatewaypb.AckCode_ACK_CODE_OK:
		return fleeterror.NewInternalError(msg)
	case gatewaypb.AckCode_ACK_CODE_SCAN_FAILED:
		return fleeterror.NewConnectionError(m.route.deviceIdentifier, errors.New(msg))
	case gatewaypb.AckCode_ACK_CODE_REPORT_FAILED,
		gatewaypb.AckCode_ACK_CODE_PARTIAL,
		gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED,
		gatewaypb.AckCode_ACK_CODE_INTERNAL,
		gatewaypb.AckCode_ACK_CODE_UNSPECIFIED:
		return fleeterror.NewInternalError(msg)
	default:
		return fleeterror.NewInternalError(msg)
	}
}

func validateTelemetryResult(result *telemetrypb.FleetNodeTelemetryResult) error {
	if err := protovalidate.Validate(result); err != nil {
		return fleeterror.NewInternalErrorf("invalid fleet node telemetry payload: %v", err)
	}
	if ts := result.GetTimestamp().AsTime(); ts.Before(time.Unix(0, 0)) || ts.After(time.Now().Add(5*time.Minute)) {
		return fleeterror.NewInternalErrorf("invalid fleet node telemetry timestamp: %s", ts.Format(time.RFC3339Nano))
	}
	for name, value := range map[string]*float64{
		"hashrate_hs":   result.HashrateHs,
		"temp_c":        result.TempC,
		"fan_rpm":       result.FanRpm,
		"power_w":       result.PowerW,
		"efficiency_jh": result.EfficiencyJh,
	} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return fleeterror.NewInternalErrorf("invalid fleet node telemetry %s: non-finite value", name)
		}
	}
	if len(result.GetDeviceMetricsJson()) > 0 {
		var metrics modelsV2.DeviceMetrics
		if err := json.Unmarshal(result.GetDeviceMetricsJson(), &metrics); err != nil {
			return fleeterror.NewInternalErrorf("invalid fleet node telemetry device metrics payload: %v", err)
		}
	}
	return nil
}

func (m *RemoteFleetNodeMiner) rememberStatus(status telemetrypb.DeviceStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latest = &cachedTelemetryStatus{
		status:    deviceStatusToMinerStatus(status),
		fetchedAt: m.now(),
	}
}

func telemetryResultToDeviceMetrics(result *telemetrypb.FleetNodeTelemetryResult) (modelsV2.DeviceMetrics, error) {
	if len(result.GetDeviceMetricsJson()) > 0 {
		var metrics modelsV2.DeviceMetrics
		if err := json.Unmarshal(result.GetDeviceMetricsJson(), &metrics); err != nil {
			return modelsV2.DeviceMetrics{}, fleeterror.NewInternalErrorf("unmarshal fleet node telemetry device metrics: %v", err)
		}
		metrics.DeviceIdentifier = result.GetDeviceIdentifier()
		metrics.Timestamp = result.GetTimestamp().AsTime()
		metrics.FirmwareVersion = result.GetFirmwareVersion()
		return metrics, nil
	}
	var healthReason *string
	if result.GetHealthReason() != "" {
		reason := result.GetHealthReason()
		healthReason = &reason
	}
	return modelsV2.DeviceMetrics{
		DeviceIdentifier:      result.GetDeviceIdentifier(),
		Timestamp:             result.GetTimestamp().AsTime(),
		FirmwareVersion:       result.GetFirmwareVersion(),
		Health:                telemetryHealthToV2(result.GetHealthStatus(), result.GetDeviceStatus()),
		HealthReason:          healthReason,
		DefaultPasswordActive: result.DefaultPasswordActive,
		HashrateHS:            scalarMetricToV2(result.HashrateHs, modelsV2.MetricKindRate),
		TempC:                 scalarMetricToV2(result.TempC, modelsV2.MetricKindGauge),
		FanRPM:                scalarMetricToV2(result.FanRpm, modelsV2.MetricKindGauge),
		PowerW:                scalarMetricToV2(result.PowerW, modelsV2.MetricKindGauge),
		EfficiencyJH:          scalarMetricToV2(result.EfficiencyJh, modelsV2.MetricKindGauge),
	}, nil
}

func scalarMetricToV2(value *float64, kind modelsV2.MetricKind) *modelsV2.MetricValue {
	if value == nil {
		return nil
	}
	return &modelsV2.MetricValue{
		Value: *value,
		Kind:  kind,
	}
}

func telemetryHealthToV2(health telemetrypb.DeviceHealthStatus, status telemetrypb.DeviceStatus) modelsV2.HealthStatus {
	switch health {
	case telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_ACTIVE:
		return modelsV2.HealthHealthyActive
	case telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_INACTIVE:
		return modelsV2.HealthHealthyInactive
	case telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_WARNING:
		return modelsV2.HealthWarning
	case telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_CRITICAL:
		return modelsV2.HealthCritical
	case telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_UNKNOWN:
		return modelsV2.HealthUnknown
	case telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_UNSPECIFIED:
		return deviceStatusToHealth(status)
	default:
		return modelsV2.HealthUnknown
	}
}

func deviceStatusToHealth(status telemetrypb.DeviceStatus) modelsV2.HealthStatus {
	switch status {
	case telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE:
		return modelsV2.HealthHealthyActive
	case telemetrypb.DeviceStatus_DEVICE_STATUS_INACTIVE,
		telemetrypb.DeviceStatus_DEVICE_STATUS_MAINTENANCE,
		telemetrypb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
		return modelsV2.HealthHealthyInactive
	case telemetrypb.DeviceStatus_DEVICE_STATUS_ERROR:
		return modelsV2.HealthCritical
	case telemetrypb.DeviceStatus_DEVICE_STATUS_OFFLINE,
		telemetrypb.DeviceStatus_DEVICE_STATUS_UPDATING,
		telemetrypb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED,
		telemetrypb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED:
		return modelsV2.HealthUnknown
	default:
		return modelsV2.HealthUnknown
	}
}

func deviceStatusToMinerStatus(status telemetrypb.DeviceStatus) models.MinerStatus {
	switch status {
	case telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE:
		return models.MinerStatusActive
	case telemetrypb.DeviceStatus_DEVICE_STATUS_OFFLINE:
		return models.MinerStatusOffline
	case telemetrypb.DeviceStatus_DEVICE_STATUS_INACTIVE:
		return models.MinerStatusInactive
	case telemetrypb.DeviceStatus_DEVICE_STATUS_MAINTENANCE:
		return models.MinerStatusMaintenance
	case telemetrypb.DeviceStatus_DEVICE_STATUS_ERROR:
		return models.MinerStatusError
	case telemetrypb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL:
		return models.MinerStatusNeedsMiningPool
	case telemetrypb.DeviceStatus_DEVICE_STATUS_UPDATING:
		return models.MinerStatusUpdating
	case telemetrypb.DeviceStatus_DEVICE_STATUS_REBOOT_REQUIRED:
		return models.MinerStatusRebootRequired
	case telemetrypb.DeviceStatus_DEVICE_STATUS_UNSPECIFIED:
		return models.MinerStatusUnknown
	default:
		return models.MinerStatusUnknown
	}
}

func (m *RemoteFleetNodeMiner) unsupported(operation string) error {
	return fleeterror.NewUnimplementedErrorf("fleet node remote %s is not implemented", operation)
}

func (m *RemoteFleetNodeMiner) Reboot(ctx context.Context) error {
	if m.delegate != nil {
		return m.delegate.Reboot(ctx)
	}
	return m.unsupported("reboot")
}

func (m *RemoteFleetNodeMiner) StartMining(ctx context.Context) error {
	if m.delegate != nil {
		return m.delegate.StartMining(ctx)
	}
	return m.unsupported("start mining")
}

func (m *RemoteFleetNodeMiner) StopMining(ctx context.Context) error {
	if m.delegate != nil {
		return m.delegate.StopMining(ctx)
	}
	return m.unsupported("stop mining")
}

func (m *RemoteFleetNodeMiner) Curtail(ctx context.Context, req sdk.CurtailRequest) error {
	if m.delegate != nil {
		return m.delegate.Curtail(ctx, req)
	}
	return m.unsupported("curtailment")
}

func (m *RemoteFleetNodeMiner) Uncurtail(ctx context.Context, req sdk.UncurtailRequest) error {
	if m.delegate != nil {
		return m.delegate.Uncurtail(ctx, req)
	}
	return m.unsupported("curtailment")
}

func (m *RemoteFleetNodeMiner) SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error {
	if m.delegate != nil {
		return m.delegate.SetCoolingMode(ctx, payload)
	}
	return m.unsupported("set cooling mode")
}

func (m *RemoteFleetNodeMiner) GetCoolingMode(ctx context.Context) (commonpb.CoolingMode, error) {
	if m.delegate != nil {
		return m.delegate.GetCoolingMode(ctx)
	}
	return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, m.unsupported("get cooling mode")
}

func (m *RemoteFleetNodeMiner) SetPowerTarget(ctx context.Context, payload dto.PowerTargetPayload) error {
	if m.delegate != nil {
		return m.delegate.SetPowerTarget(ctx, payload)
	}
	return m.unsupported("set power target")
}

func (m *RemoteFleetNodeMiner) UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
	if m.delegate != nil {
		return m.delegate.UpdateMiningPools(ctx, payload)
	}
	return m.unsupported("update mining pools")
}

func (m *RemoteFleetNodeMiner) UpdateMinerPassword(ctx context.Context, payload dto.UpdateMinerPasswordPayload) error {
	if m.delegate != nil {
		return m.delegate.UpdateMinerPassword(ctx, payload)
	}
	return m.unsupported("update miner password")
}

func (m *RemoteFleetNodeMiner) UpdateMinerPasswordWithCredentials(ctx context.Context, payload dto.UpdateMinerPasswordPayload) (*gatewaypb.EncryptedCredentials, error) {
	if updater, ok := m.delegate.(interfaces.MinerPasswordCredentialUpdater); ok {
		return updater.UpdateMinerPasswordWithCredentials(ctx, payload)
	}
	return nil, m.unsupported("update miner password with encrypted credentials")
}

func (m *RemoteFleetNodeMiner) BlinkLED(ctx context.Context) error {
	if m.delegate != nil {
		return m.delegate.BlinkLED(ctx)
	}
	return m.unsupported("blink LED")
}

func (m *RemoteFleetNodeMiner) DownloadLogs(ctx context.Context, outputPath string) error {
	if m.delegate != nil {
		return m.delegate.DownloadLogs(ctx, outputPath)
	}
	return m.unsupported("download logs")
}

func (m *RemoteFleetNodeMiner) FirmwareUpdate(ctx context.Context, firmware sdk.FirmwareFile) error {
	if m.delegate != nil {
		return m.delegate.FirmwareUpdate(ctx, firmware)
	}
	return m.unsupported("firmware update")
}

func (m *RemoteFleetNodeMiner) Unpair(ctx context.Context) error {
	if m.delegate != nil {
		return m.delegate.Unpair(ctx)
	}
	return m.unsupported("unpair")
}

func (m *RemoteFleetNodeMiner) GetErrors(ctx context.Context) (diagnosticsModels.DeviceErrors, error) {
	if m.delegate != nil {
		return m.delegate.GetErrors(ctx)
	}
	return diagnosticsModels.DeviceErrors{DeviceID: m.route.deviceIdentifier}, m.unsupported("get errors")
}

func (m *RemoteFleetNodeMiner) GetMiningPools(ctx context.Context) ([]interfaces.MinerConfiguredPool, error) {
	if m.delegate != nil {
		return m.delegate.GetMiningPools(ctx)
	}
	return nil, m.unsupported("get mining pools")
}
