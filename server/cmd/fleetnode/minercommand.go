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

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	curtailmentpb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// minerCommandTimeout bounds a single miner command. It must stay below the server's
// WorkerExecutionTimeout (default 30s) minus ack slack, or a slow command can be
// retried while the node still runs it (duplicate reboot/curtail). var so tests shrink it.
var minerCommandTimeout = 25 * time.Second

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

	if err := runMinerAction(cmdCtx, dev, mc); err != nil {
		code, msg := classifyMinerCommandError("execute command", err)
		r.sendAck(stream, commandID, code, msg, logger)
		return
	}
	r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_OK, "", logger)
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

func runMinerAction(ctx context.Context, dev sdk.Device, mc *pb.MinerCommand) error {
	switch a := mc.GetAction().(type) {
	case *pb.MinerCommand_Reboot:
		return dev.Reboot(ctx)
	case *pb.MinerCommand_StartMining:
		return dev.StartMining(ctx)
	case *pb.MinerCommand_StopMining:
		return dev.StopMining(ctx)
	case *pb.MinerCommand_BlinkLed:
		return dev.BlinkLED(ctx)
	case *pb.MinerCommand_Curtail:
		level, err := toSDKCurtailLevel(a.Curtail.GetLevel())
		if err != nil {
			return err
		}
		curtailer, ok := dev.(sdk.DeviceCurtailment)
		if !ok {
			return sdk.NewErrUnsupportedCapability("curtailment")
		}
		return curtailer.Curtail(ctx, sdk.CurtailRequest{Level: level})
	case *pb.MinerCommand_Uncurtail:
		curtailer, ok := dev.(sdk.DeviceCurtailment)
		if !ok {
			return sdk.NewErrUnsupportedCapability("curtailment")
		}
		return curtailer.Uncurtail(ctx, sdk.UncurtailRequest{})
	case *pb.MinerCommand_SetCoolingMode:
		mode, err := toSDKCoolingMode(a.SetCoolingMode.GetMode())
		if err != nil {
			return err
		}
		return dev.SetCoolingMode(ctx, mode)
	case *pb.MinerCommand_SetPowerTarget:
		mode, err := toSDKPerformanceMode(a.SetPowerTarget.GetPerformanceMode())
		if err != nil {
			return err
		}
		return dev.SetPowerTarget(ctx, mode)
	default:
		return cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unrecognized miner command action")
	}
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
