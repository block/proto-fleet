package ct1

import (
	"context"
	"fmt"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// RunContainerAlarmAction sends one mute or reset command to the primary
// controller. The controller pulses a PLC command bit and returns no readback,
// so success only acknowledges that the controller accepted the command; the
// effect must be confirmed from a later plant observation. The command is sent
// at most once: a refusal proves nothing was written, while any other failure
// may have reached the PLC and is reported as an unconfirmed outcome.
func (d *Device) RunContainerAlarmAction(ctx context.Context, req sdk.ContainerAlarmRequest) error {
	if err := req.Validate(); err != nil {
		return sdk.InvalidWriteRequest(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	c, err := d.selectClient(ctx, true)
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
	}
	controls, err := c.power.GetContainerControls(ctx, &mdk.GetContainerControlsRequest{})
	if err != nil {
		return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
	}
	switch req.Action {
	case sdk.ContainerAlarmActionMute:
		if controls.AlarmMuteSupported == nil || !*controls.AlarmMuteSupported {
			return sdk.UnsupportedWrite(sdk.CapabilityContainerAlarm)
		}
		// The live command path may be narrower than the static capability.
		// Older controllers omit the flag, which keeps the capability.
		health, err := c.telemetry.GetContainerHealth(ctx, &mdk.GetContainerHealthRequest{})
		if err != nil {
			return sdk.WriteRefused(err, sdk.WriteStageConfigurationRead, sdk.WriteReasonReadFailed)
		}
		if health.AlarmMuteSupported != nil && !*health.AlarmMuteSupported {
			return sdk.UnsupportedWrite(sdk.CapabilityContainerAlarm)
		}
		_, err = c.power.MuteContainerAlarm(ctx, &mdk.MuteContainerAlarmRequest{})
		return alarmWriteError(req.Action, err)
	default:
		if controls.AlarmResetSupported == nil || !*controls.AlarmResetSupported {
			return sdk.UnsupportedWrite(sdk.CapabilityContainerAlarm)
		}
		_, err = c.power.ResetContainerAlarm(ctx, &mdk.ResetContainerAlarmRequest{})
		return alarmWriteError(req.Action, err)
	}
}

// alarmWriteError classifies the single dispatched command. Only the codes the
// controller uses for "refused before acting" prove nothing was written; a
// Frigate refusal reason alone does not, because it can accompany a failure
// after the PLC pulse. Everything else, including timeouts, cannot rule out
// that the PLC saw the pulse.
func alarmWriteError(action string, err error) error {
	if err == nil {
		return nil
	}
	if alarmRefused(err) {
		return sdk.WriteRefused(fmt.Errorf("CT1 refused container alarm %s; nothing was sent to the controller: %w", action, frigateError(err)), sdk.WriteStageDispatch, sdk.WriteReasonRejected)
	}
	return sdk.WithWriteOutcome(fmt.Errorf("CT1 container alarm %s outcome unconfirmed; the command may have reached the controller: %w", action, err), sdk.WriteUnknown, sdk.WriteStageDispatch, sdk.WriteReasonTransportFailed)
}

func alarmRefused(err error) bool {
	code := status.Code(err)
	return code == codes.InvalidArgument || code == codes.FailedPrecondition || code == codes.Unimplemented
}
