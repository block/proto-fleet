package sdk

import (
	"context"
	"fmt"
)

// CapabilityContainerAlarm denotes command transport support for plant alarm
// mute and reset. Each action additionally requires current support from the
// controller's advertised controls.
const CapabilityContainerAlarm = "container_alarm"

// Plant alarm actions accepted by ContainerAlarmRequest.
const (
	ContainerAlarmActionMute  = "mute"
	ContainerAlarmActionReset = "reset"
)

// ContainerAlarmRequest names one plant alarm action. Mute silences the alarm
// buzzer; reset asks the controller to clear latched alarms. Neither clears a
// hardware fault.
type ContainerAlarmRequest struct {
	Action string `json:"action"`
}

func (r ContainerAlarmRequest) Validate() error {
	switch r.Action {
	case ContainerAlarmActionMute, ContainerAlarmActionReset:
		return nil
	default:
		return fmt.Errorf("unsupported container alarm action %q", r.Action)
	}
}

// DeviceContainerAlarmController is optional. Success acknowledges that the
// controller accepted the one-shot command; it carries no readback, so callers
// must obtain a later plant observation to confirm the effect.
type DeviceContainerAlarmController interface {
	RunContainerAlarmAction(ctx context.Context, req ContainerAlarmRequest) error
}
