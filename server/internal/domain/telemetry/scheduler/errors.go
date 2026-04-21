package scheduler

import (
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// DeviceNotManagedErr is returned when a device is not managed by the scheduler.
type DeviceNotManagedErr struct {
	fleeterror.FleetError
	DeviceID models.DeviceIdentifier // ID of the device that is not managed
}

func NewDeviceNotManagedErr(deviceID models.DeviceIdentifier) DeviceNotManagedErr {
	return DeviceNotManagedErr{
		FleetError: fleeterror.NewInternalError("device not managed by scheduler"),
		DeviceID:   deviceID,
	}
}

func (e DeviceNotManagedErr) Error() string {
	return "device not managed by scheduler: " + e.DeviceID.String() + ": " + e.FleetError.Error()
}

type DeviceAlreadyScheduledErr struct {
	fleeterror.FleetError
	DeviceID models.DeviceIdentifier // ID of the device that is already scheduled
}

func NewDeviceAlreadyScheduledErr(deviceID models.DeviceIdentifier) DeviceAlreadyScheduledErr {
	return DeviceAlreadyScheduledErr{
		FleetError: fleeterror.NewInternalError("device already scheduled"),
		DeviceID:   deviceID,
	}
}

func (e DeviceAlreadyScheduledErr) Error() string {
	return "device already scheduled: " + e.DeviceID.String() + ": " + e.FleetError.Error()
}
