package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	errorspb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// minerCommandTimeout bounds a single miner command. It must stay below the server's
// WorkerExecutionTimeout (default 30s) minus ack slack, or a slow command can be
// retried while the node still runs it (duplicate reboot/curtail). var so tests shrink it.
var minerCommandTimeout = 25 * time.Second

const (
	supportedMiningPoolSlots        = 3
	maxSupportedMiningPoolPriority  = supportedMiningPoolSlots - 1
	maxGetErrorsReports             = 512
	maxErrorVendorAttributes        = 32
	maxErrorVendorAttributeKeyLen   = 128
	maxErrorVendorAttributeValueLen = 1024
)

// driverGetter is the plugin-manager seam the executor needs; *plugins.Manager satisfies it.
type driverGetter interface {
	GetDriverByDriverName(driverName string) (sdk.Driver, error)
}

// secretProvider builds the auth bundle to reach a miner. Production decrypts the
// opaque descriptor credential with the node-local key; tests can inject an empty
// provider for no-secret drivers.
type secretProvider interface {
	SecretBundle(target *pb.MinerConnectionDescriptor) (sdk.SecretBundle, error)
}

// nodeSecretProvider returns an empty bundle for tests and no-secret drivers.
type nodeSecretProvider struct{}

func (nodeSecretProvider) SecretBundle(_ *pb.MinerConnectionDescriptor) (sdk.SecretBundle, error) {
	return sdk.SecretBundle{}, nil
}

func (r *RunCmd) handleMinerCommand(ctx context.Context, stream acker, commandID string, mc *pb.MinerCommand, logger *slog.Logger) {
	if r.driverGetter == nil || r.minerSecrets == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node has no plugins loaded", logger)
		return
	}
	// handleCommand validated only the outer ControlCommand; validate the inner one here.
	if vErr := protovalidate.Validate(mc); vErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid miner command: %v", vErr), logger)
		return
	}
	target := mc.GetTarget()
	if err := validateDialTarget(target); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, err.Error(), logger)
		return
	}
	port, err := sdk.ParsePort(target.GetPort())
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid port: %v", err), logger)
		return
	}
	driver, err := r.driverGetter.GetDriverByDriverName(target.GetDriverName())
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, fmt.Sprintf("no plugin for driver %q: %v", target.GetDriverName(), err), logger)
		return
	}
	bundle, err := r.minerSecrets.SecretBundle(target)
	if err != nil {
		code, msg := classifyMinerCommandError("build secret bundle", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}

	cmdCtx, cancel := context.WithTimeout(ctx, minerCommandTimeout)
	defer cancel()

	result, err := driver.NewDevice(cmdCtx, target.GetDeviceIdentifier(), sdk.DeviceInfo{
		Host:         target.GetIpAddress(),
		Port:         port,
		URLScheme:    target.GetUrlScheme(),
		SerialNumber: target.GetSerialNumber(),
		MacAddress:   target.GetMacAddress(),
	}, bundle)
	if err != nil {
		code, msg := classifyMinerCommandError("connect to miner", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}
	dev := result.Device
	defer func() {
		// Best-effort release on a ctx that outlives a timed-out command.
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer closeCancel()
		if cerr := dev.Close(closeCtx); cerr != nil {
			logger.Warn("closing device after command", "command_id", commandID, "err", cerr)
		}
	}()

	payload, err := runMinerAction(cmdCtx, dev, mc)
	if err != nil {
		code, msg := classifyMinerCommandError("execute command", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}
	r.sendAckWithPayload(stream, commandID, pb.AckCode_ACK_CODE_OK, "", payload, logger)
}

// validateDialTarget rejects descriptors the node should never dial: a non-IP, public,
// or link-local address, or a scheme the drivers can't dial. Loopback is allowed for the
// dev virtual driver; mirrors the discovery path's private-address policy.
func validateDialTarget(t *pb.MinerConnectionDescriptor) error {
	addr, err := netip.ParseAddr(t.GetIpAddress())
	if err != nil {
		return fmt.Errorf("ip_address %q is not a valid IP", t.GetIpAddress())
	}
	if a := addr.Unmap(); !a.IsPrivate() && !a.IsLoopback() {
		return fmt.Errorf("ip_address %q is not a private or loopback address", t.GetIpAddress())
	}
	// Restrict to schemes the dial path accepts (networking.ProtocolFromString); rejects empty/unknown.
	if _, err := networking.ProtocolFromString(t.GetUrlScheme()); err != nil {
		return fmt.Errorf("unsupported url_scheme %q", t.GetUrlScheme())
	}
	return nil
}

func runMinerAction(ctx context.Context, dev sdk.Device, mc *pb.MinerCommand) ([]byte, error) {
	switch a := mc.GetAction().(type) {
	case *pb.MinerCommand_Reboot:
		return nil, dev.Reboot(ctx)
	case *pb.MinerCommand_StartMining:
		return nil, dev.StartMining(ctx)
	case *pb.MinerCommand_StopMining:
		return nil, dev.StopMining(ctx)
	case *pb.MinerCommand_BlinkLed:
		return nil, dev.BlinkLED(ctx)
	case *pb.MinerCommand_Curtail:
		level, err := toSDKCurtailLevel(a.Curtail.GetLevel())
		if err != nil {
			return nil, err
		}
		curtailer, ok := dev.(sdk.DeviceCurtailment)
		if !ok {
			return nil, sdk.NewErrUnsupportedCapability("curtailment")
		}
		return nil, curtailer.Curtail(ctx, sdk.CurtailRequest{Level: level})
	case *pb.MinerCommand_Uncurtail:
		curtailer, ok := dev.(sdk.DeviceCurtailment)
		if !ok {
			return nil, sdk.NewErrUnsupportedCapability("curtailment")
		}
		return nil, curtailer.Uncurtail(ctx, sdk.UncurtailRequest{})
	case *pb.MinerCommand_SetCoolingMode:
		mode, err := toSDKCoolingMode(a.SetCoolingMode.GetMode())
		if err != nil {
			return nil, err
		}
		return nil, dev.SetCoolingMode(ctx, mode)
	case *pb.MinerCommand_SetPowerTarget:
		mode, err := toSDKPerformanceMode(a.SetPowerTarget.GetPerformanceMode())
		if err != nil {
			return nil, err
		}
		return nil, dev.SetPowerTarget(ctx, mode)
	case *pb.MinerCommand_UpdateMiningPools:
		return nil, dev.UpdateMiningPools(ctx, toSDKMiningPoolConfigs(a.UpdateMiningPools.GetPools()))
	case *pb.MinerCommand_GetMiningPools:
		pools, err := dev.GetMiningPools(ctx)
		if err != nil {
			return nil, err
		}
		payload, err := getMiningPoolsResultPayload(pools)
		if err != nil {
			return nil, err
		}
		return payload, nil
	case *pb.MinerCommand_GetErrors:
		deviceErrors, err := dev.GetErrors(ctx)
		if err != nil {
			return nil, err
		}
		payload, truncated, reason, err := getErrorsResultPayload(mc.GetTarget().GetDeviceIdentifier(), deviceErrors)
		if err != nil {
			return nil, err
		}
		if truncated {
			return nil, cmdErr(pb.AckCode_ACK_CODE_PARTIAL, "%s", reason)
		}
		return payload, nil
	default:
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unrecognized miner command action")
	}
}

func toSDKMiningPoolConfigs(pools []*pb.MiningPoolConfig) []sdk.MiningPoolConfig {
	sdkPools := make([]sdk.MiningPoolConfig, 0, len(pools))
	for _, pool := range pools {
		sdkPools = append(sdkPools, sdk.MiningPoolConfig{
			Priority:   pool.GetPriority(),
			URL:        pool.GetUrl(),
			WorkerName: pool.GetUsername(),
		})
	}
	return sdkPools
}

func getMiningPoolsResultPayload(pools []sdk.ConfiguredPool) ([]byte, error) {
	result := &pb.GetMiningPoolsResult{Pools: miningPoolConfigsFromSDK(pools)}
	if err := validateGetMiningPoolsResult(result); err != nil {
		return nil, err
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal get mining pools result: %w", err)
	}
	return payload, nil
}

func validateGetMiningPoolsResult(result *pb.GetMiningPoolsResult) error {
	if err := protovalidate.Validate(result); err != nil {
		return fmt.Errorf("invalid get mining pools result: %w", err)
	}
	for _, pool := range result.GetPools() {
		if err := sv2.ValidatePoolURL(pool.GetUrl()); err != nil {
			return fmt.Errorf("invalid get mining pools result: %w", err)
		}
	}
	return nil
}

func miningPoolConfigsFromSDK(pools []sdk.ConfiguredPool) []*pb.MiningPoolConfig {
	var defaultPool, backup1Pool, backup2Pool *pb.MiningPoolConfig
	for _, pool := range pools {
		config := &pb.MiningPoolConfig{
			Priority: pool.Priority,
			Url:      pool.URL,
			Username: pool.Username,
		}
		switch pool.Priority {
		case 0:
			if defaultPool == nil {
				defaultPool = config
			}
		case 1:
			if backup1Pool == nil {
				backup1Pool = config
			}
		case 2:
			if backup2Pool == nil {
				backup2Pool = config
			}
		default:
			if pool.Priority > maxSupportedMiningPoolPriority {
				continue
			}
			return []*pb.MiningPoolConfig{config}
		}
	}

	configured := make([]*pb.MiningPoolConfig, 0, supportedMiningPoolSlots)
	for _, pool := range []*pb.MiningPoolConfig{defaultPool, backup1Pool, backup2Pool} {
		if pool != nil {
			configured = append(configured, pool)
		}
	}
	return configured
}

func getErrorsResultPayload(targetDeviceID string, deviceErrors sdk.DeviceErrors) ([]byte, bool, string, error) {
	result, omittedReports, err := getErrorsResultFromSDK(targetDeviceID, deviceErrors)
	if err != nil {
		return nil, false, "", err
	}
	if omittedReports > 0 {
		return nil, true, fmt.Sprintf("get errors result has %d report(s); max %d can be returned in one control ack", omittedReports+len(result.GetErrors()), maxGetErrorsReports), nil
	}
	if err := protovalidate.Validate(result); err != nil {
		return nil, false, "", fmt.Errorf("invalid get errors result: %w", err)
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		return nil, false, "", fmt.Errorf("marshal get errors result: %w", err)
	}
	if len(payload) > maxAckPayloadBytes {
		return nil, true, fmt.Sprintf("get errors result payload is %d bytes; max %d can be returned in one control ack", len(payload), maxAckPayloadBytes), nil
	}
	return payload, false, "", nil
}

func getErrorsResultFromSDK(targetDeviceID string, deviceErrors sdk.DeviceErrors) (*pb.GetErrorsResult, int, error) {
	pluginErrors := deviceErrors.Errors
	omittedReports := 0
	if len(pluginErrors) > maxGetErrorsReports {
		omittedReports = len(pluginErrors) - maxGetErrorsReports
		pluginErrors = pluginErrors[:maxGetErrorsReports]
	}
	result := &pb.GetErrorsResult{
		DeviceId: targetDeviceID,
		Errors:   make([]*pb.MinerErrorReport, 0, len(pluginErrors)),
	}
	for _, sdkErr := range pluginErrors {
		minerError, err := gatewayMinerError(sdkErr.MinerError)
		if err != nil {
			return nil, 0, err
		}
		severity, err := gatewaySeverity(sdkErr.Severity)
		if err != nil {
			return nil, 0, err
		}
		componentType, err := gatewayComponentType(sdkErr.ComponentType)
		if err != nil {
			return nil, 0, err
		}
		vendorAttributes, err := boundedVendorAttributes(sdkErr.VendorAttributes)
		if err != nil {
			return nil, 0, err
		}
		report := &pb.MinerErrorReport{
			MinerError:        minerError,
			CauseSummary:      sdkErr.CauseSummary,
			RecommendedAction: sdkErr.RecommendedAction,
			Severity:          severity,
			VendorAttributes:  vendorAttributes,
			DeviceId:          targetDeviceID,
			ComponentId:       sdkErr.ComponentID,
			Impact:            sdkErr.Impact,
			Summary:           sdkErr.Summary,
			ComponentType:     componentType,
		}
		if !sdkErr.FirstSeenAt.IsZero() {
			report.FirstSeenAt = timestamppb.New(sdkErr.FirstSeenAt)
		}
		if !sdkErr.LastSeenAt.IsZero() {
			report.LastSeenAt = timestamppb.New(sdkErr.LastSeenAt)
		}
		if sdkErr.ClosedAt != nil && !sdkErr.ClosedAt.IsZero() {
			report.ClosedAt = timestamppb.New(*sdkErr.ClosedAt)
		}
		result.Errors = append(result.Errors, report)
	}
	return result, omittedReports, nil
}

func gatewayMinerError(value sdk.MinerError) (errorspb.MinerError, error) {
	raw := int32(value)
	if _, ok := errorspb.MinerError_name[raw]; !ok {
		return errorspb.MinerError_MINER_ERROR_UNSPECIFIED, fmt.Errorf("invalid get errors result: miner_error %d is not defined", raw)
	}
	return errorspb.MinerError(raw), nil
}

func gatewaySeverity(value sdk.Severity) (errorspb.Severity, error) {
	raw := int32(value)
	if _, ok := errorspb.Severity_name[raw]; !ok {
		return errorspb.Severity_SEVERITY_UNSPECIFIED, fmt.Errorf("invalid get errors result: severity %d is not defined", raw)
	}
	return errorspb.Severity(raw), nil
}

func gatewayComponentType(value sdk.ComponentType) (errorspb.ComponentType, error) {
	raw := int32(value)
	if _, ok := errorspb.ComponentType_name[raw]; !ok {
		return errorspb.ComponentType_COMPONENT_TYPE_UNSPECIFIED, fmt.Errorf("invalid get errors result: component_type %d is not defined", raw)
	}
	return errorspb.ComponentType(raw), nil
}

func boundedVendorAttributes(attrs map[string]string) (map[string]string, error) {
	if len(attrs) == 0 {
		return nil, nil
	}
	if len(attrs) > maxErrorVendorAttributes {
		return nil, fmt.Errorf("invalid get errors result: vendor_attributes has %d entries, max %d", len(attrs), maxErrorVendorAttributes)
	}
	bounded := make(map[string]string, len(attrs))
	for key, value := range attrs {
		if key == "" {
			return nil, fmt.Errorf("invalid get errors result: vendor_attributes key must not be empty")
		}
		if len(key) > maxErrorVendorAttributeKeyLen {
			return nil, fmt.Errorf("invalid get errors result: vendor_attributes key %q exceeds %d bytes", key, maxErrorVendorAttributeKeyLen)
		}
		if len(value) > maxErrorVendorAttributeValueLen {
			return nil, fmt.Errorf("invalid get errors result: vendor_attributes value for key %q exceeds %d bytes", key, maxErrorVendorAttributeValueLen)
		}
		bounded[key] = value
	}
	return bounded, nil
}

// Reject undefined / non-actionable (UNSPECIFIED) enum values with BAD_REQUEST rather
// than casting them to a plugin. if/else (not switch) to sidestep the exhaustive linter.
func toSDKCoolingMode(m commonpb.CoolingMode) (sdk.CoolingMode, error) {
	if m == commonpb.CoolingMode_COOLING_MODE_AIR_COOLED {
		return sdk.CoolingModeAirCooled, nil
	}
	if m == commonpb.CoolingMode_COOLING_MODE_IMMERSION_COOLED {
		return sdk.CoolingModeImmersionCooled, nil
	}
	if m == commonpb.CoolingMode_COOLING_MODE_MANUAL {
		return sdk.CoolingModeManual, nil
	}
	return sdk.CoolingModeUnspecified, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unsupported cooling mode: %s", m)
}

func toSDKPerformanceMode(m minercommandpb.PerformanceMode) (sdk.PerformanceMode, error) {
	if m == minercommandpb.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE {
		return sdk.PerformanceModeMaximumHashrate, nil
	}
	if m == minercommandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY {
		return sdk.PerformanceModeEfficiency, nil
	}
	return sdk.PerformanceModeUnspecified, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unsupported performance mode: %s", m)
}

func toSDKCurtailLevel(l curtailmentpb.CurtailmentLevel) (sdk.CurtailLevel, error) {
	if l == curtailmentpb.CurtailmentLevel_CURTAILMENT_LEVEL_EFFICIENCY {
		return sdk.CurtailLevelEfficiency, nil
	}
	if l == curtailmentpb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL {
		return sdk.CurtailLevelFull, nil
	}
	return sdk.CurtailLevelUnspecified, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unsupported curtail level: %s", l)
}

// classifyMinerCommandError maps an SDK/plugin error to an ack code so the server reacts
// right (evict on auth, permanent-fail on unimplemented). if/else to dodge the exhaustive linter.
func classifyMinerCommandError(stage string, err error) (pb.AckCode, string) {
	msg := fmt.Sprintf("%s: %v", stage, err)
	// A typed command error (e.g. an undefined enum) carries its own ack code.
	var ce *commandError
	if errors.As(err, &ce) {
		return ce.code, msg
	}
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) {
		if sdkErr.Code == sdk.ErrCodeAuthenticationFailed {
			return pb.AckCode_ACK_CODE_UNAUTHENTICATED, msg
		}
		if sdkErr.Code == sdk.ErrCodeUnsupportedCapability || sdkErr.Code == sdk.ErrCodeCurtailCapabilityNotSupported {
			return pb.AckCode_ACK_CODE_UNIMPLEMENTED, msg
		}
	}
	if st, ok := grpcstatus.FromError(err); ok {
		if st.Code() == codes.Unauthenticated {
			return pb.AckCode_ACK_CODE_UNAUTHENTICATED, msg
		}
		if st.Code() == codes.Unimplemented {
			return pb.AckCode_ACK_CODE_UNIMPLEMENTED, msg
		}
	}
	return pb.AckCode_ACK_CODE_INTERNAL, msg
}
