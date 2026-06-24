package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/plugins/mappers"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Keep node-side plugin work below TelemetryService's default 5s MetricTimeout so
// a successful near-deadline sample still has slack to marshal and return its ack.
var telemetryCommandTimeout = 4 * time.Second

type telemetryFetcher interface {
	Fetch(ctx context.Context, req *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error)
}

type pluginTelemetryFetcher struct {
	manager      *plugins.Manager
	minerSecrets secretProvider
}

func newPluginTelemetryFetcher(manager *plugins.Manager, minerSecrets secretProvider) (*pluginTelemetryFetcher, error) {
	if manager == nil {
		return nil, fmt.Errorf("plugin manager is required")
	}
	if minerSecrets == nil {
		return nil, fmt.Errorf("miner secret provider is required")
	}
	return &pluginTelemetryFetcher{
		manager:      manager,
		minerSecrets: minerSecrets,
	}, nil
}

func (r *RunCmd) handleTelemetryCommand(ctx context.Context, stream acker, commandID string, req *telemetrypb.FleetNodeTelemetryRequest, logger *slog.Logger) {
	if r.telemetry == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "telemetry unavailable: no plugins loaded", logger)
		return
	}
	if vErr := protovalidate.Validate(req); vErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid telemetry request: %v", vErr), logger)
		return
	}
	cmdCtx, cancel := context.WithTimeout(ctx, telemetryTimeout(req))
	defer cancel()

	result, err := r.telemetry.Fetch(cmdCtx, req)
	if err != nil {
		code := pb.AckCode_ACK_CODE_INTERNAL
		var ce *commandError
		if errors.As(err, &ce) {
			code = ce.code
		}
		r.sendAck(stream, commandID, code, err.Error(), logger)
		return
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, fmt.Sprintf("marshal telemetry result: %v", err), logger)
		return
	}
	r.sendAckWithPayload(stream, commandID, pb.AckCode_ACK_CODE_OK, "", payload, logger)
}

func (f *pluginTelemetryFetcher) Fetch(ctx context.Context, req *telemetrypb.FleetNodeTelemetryRequest) (*telemetrypb.FleetNodeTelemetryResult, error) {
	plugin, err := f.manager.GetPluginByDriverNameWithCapability(req.GetDriverName(), sdk.CapabilityRealtimeTelemetry)
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "no telemetry-capable driver %q: %v", req.GetDriverName(), err)
	}

	target := telemetryDialTarget(req)
	if err := validateDialTarget(target); err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%v", err)
	}
	port, err := sdk.ParsePort(req.GetPort())
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid port %q: %v", req.GetPort(), err)
	}
	deviceInfo := sdk.DeviceInfo{
		Host:            req.GetIpAddress(),
		Port:            port,
		URLScheme:       req.GetUrlScheme(),
		SerialNumber:    req.GetSerialNumber(),
		Model:           req.GetModel(),
		Manufacturer:    req.GetManufacturer(),
		MacAddress:      req.GetMacAddress(),
		FirmwareVersion: req.GetFirmwareVersion(),
	}
	secret, err := f.minerSecrets.SecretBundle(target)
	if err != nil {
		code, msg := classifyMinerCommandError("build telemetry secret bundle", err)
		return nil, cmdErr(code, "%s", msg)
	}
	created, err := plugin.Driver.NewDevice(ctx, req.GetDeviceIdentifier(), deviceInfo, secret)
	if err != nil {
		code, msg := classifyTelemetryError("create telemetry device", err)
		return nil, cmdErr(code, "%s", msg)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = created.Device.Close(closeCtx)
	}()

	sdkMetrics, err := created.Device.Status(ctx)
	if err != nil {
		code, msg := classifyTelemetryError("fetch telemetry", err)
		return nil, cmdErr(code, "%s", msg)
	}
	v2Metrics := mappers.SDKDeviceMetricsToV2(sdkMetrics)
	return telemetryResultFromV2(req.GetDeviceIdentifier(), v2Metrics, deviceStatusFromSDKHealth(sdkMetrics.Health)), nil
}

func telemetryDialTarget(req *telemetrypb.FleetNodeTelemetryRequest) *pb.MinerConnectionDescriptor {
	return &pb.MinerConnectionDescriptor{
		DeviceIdentifier:   req.GetDeviceIdentifier(),
		DriverName:         req.GetDriverName(),
		IpAddress:          req.GetIpAddress(),
		Port:               req.GetPort(),
		UrlScheme:          req.GetUrlScheme(),
		SerialNumber:       req.GetSerialNumber(),
		MacAddress:         req.GetMacAddress(),
		CredentialUsername: req.GetCredentialUsername(),
		CredentialPassword: req.GetCredentialPassword(),
	}
}

func telemetryTimeout(req *telemetrypb.FleetNodeTelemetryRequest) time.Duration {
	if req.GetTimeout() != nil {
		if timeout := req.GetTimeout().AsDuration(); timeout > 0 {
			return timeout
		}
	}
	return telemetryCommandTimeout
}

func classifyTelemetryError(stage string, err error) (pb.AckCode, string) {
	msg := fmt.Sprintf("%s: %v", stage, err)
	if isNodeAuthFailure(err) {
		return pb.AckCode_ACK_CODE_UNAUTHENTICATED, msg
	}
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) {
		if sdkErr.Code == sdk.ErrCodeDeviceUnavailable {
			return pb.AckCode_ACK_CODE_SCAN_FAILED, msg
		}
	}
	if st, ok := grpcstatus.FromError(err); ok {
		code := st.Code()
		if code == codes.Unavailable || code == codes.DeadlineExceeded || code == codes.NotFound {
			return pb.AckCode_ACK_CODE_SCAN_FAILED, msg
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return pb.AckCode_ACK_CODE_SCAN_FAILED, msg
	}
	return pb.AckCode_ACK_CODE_INTERNAL, msg
}

func telemetryResultFromV2(deviceIdentifier string, metrics modelsV2.DeviceMetrics, status telemetrypb.DeviceStatus) *telemetrypb.FleetNodeTelemetryResult {
	if metrics.Timestamp.IsZero() {
		metrics.Timestamp = time.Now().UTC()
	}
	result := &telemetrypb.FleetNodeTelemetryResult{
		DeviceIdentifier: deviceIdentifier,
		Timestamp:        timestamppb.New(metrics.Timestamp),
		FirmwareVersion:  metrics.FirmwareVersion,
		DeviceStatus:     status,
		HealthStatus:     healthStatusFromV2(metrics.Health),
		HashrateHs:       metricValue(metrics.HashrateHS),
		TempC:            metricValue(metrics.TempC),
		FanRpm:           metricValue(metrics.FanRPM),
		PowerW:           metricValue(metrics.PowerW),
		EfficiencyJh:     metricValue(metrics.EfficiencyJH),
	}
	if metrics.HealthReason != nil {
		result.HealthReason = truncateUTF8(*metrics.HealthReason, maxAckErrorMessageBytes)
	}
	if metrics.DefaultPasswordActive != nil {
		result.DefaultPasswordActive = metrics.DefaultPasswordActive
	}
	return result
}

func metricValue(metric *modelsV2.MetricValue) *float64 {
	if metric == nil {
		return nil
	}
	value := metric.Value
	return &value
}

func healthStatusFromV2(health modelsV2.HealthStatus) telemetrypb.DeviceHealthStatus {
	switch health {
	case modelsV2.HealthHealthyActive:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_ACTIVE
	case modelsV2.HealthHealthyInactive:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_HEALTHY_INACTIVE
	case modelsV2.HealthWarning:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_WARNING
	case modelsV2.HealthCritical:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_CRITICAL
	case modelsV2.HealthUnknown:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_UNKNOWN
	default:
		return telemetrypb.DeviceHealthStatus_DEVICE_HEALTH_STATUS_UNKNOWN
	}
}

func deviceStatusFromSDKHealth(health sdk.HealthStatus) telemetrypb.DeviceStatus {
	switch health {
	case sdk.HealthHealthyActive:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE
	case sdk.HealthHealthyInactive:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_INACTIVE
	case sdk.HealthWarning:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_ONLINE
	case sdk.HealthCritical:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_ERROR
	case sdk.HealthNeedsMiningPool:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL
	case sdk.HealthUnknown, sdk.HealthStatusUnspecified:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_OFFLINE
	default:
		return telemetrypb.DeviceStatus_DEVICE_STATUS_OFFLINE
	}
}
