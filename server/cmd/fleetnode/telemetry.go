package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"buf.build/go/protovalidate"
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
		if isNodeAuthFailure(err) {
			return nil, cmdErr(pb.AckCode_ACK_CODE_UNAUTHENTICATED, "telemetry authentication failed: %v", err)
		}
		return nil, cmdErr(pb.AckCode_ACK_CODE_INTERNAL, "create telemetry device: %v", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = created.Device.Close(closeCtx)
	}()

	sdkMetrics, err := created.Device.Status(ctx)
	if err != nil {
		if isNodeAuthFailure(err) {
			return nil, cmdErr(pb.AckCode_ACK_CODE_UNAUTHENTICATED, "telemetry authentication failed: %v", err)
		}
		return nil, cmdErr(pb.AckCode_ACK_CODE_INTERNAL, "fetch telemetry: %v", err)
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

func telemetryResultFromV2(deviceIdentifier string, metrics modelsV2.DeviceMetrics, status telemetrypb.DeviceStatus) *telemetrypb.FleetNodeTelemetryResult {
	if metrics.Timestamp.IsZero() {
		metrics.Timestamp = time.Now().UTC()
	}
	result := &telemetrypb.FleetNodeTelemetryResult{
		DeviceIdentifier: deviceIdentifier,
		Timestamp:        timestamppb.New(metrics.Timestamp),
		FirmwareVersion:  metrics.FirmwareVersion,
		DeviceStatus:     status,
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
