package models

import (
	"time"

	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
)

// DeviceIdentifier is a type alias for backward compatibility - no breaking changes
type DeviceIdentifier = minerModels.DeviceIdentifier

// Device struct remains in telemetry package for telemetry-specific concerns
type Device struct {
	ID            DeviceIdentifier `json:"id"`
	LastUpdatedAt time.Time        `json:"last_updated_at"`
}

// ToDeviceIdentifiers converts a slice of device ID strings to DeviceIdentifiers.
func ToDeviceIdentifiers(deviceIDs []string) []DeviceIdentifier {
	result := make([]DeviceIdentifier, len(deviceIDs))
	for i, id := range deviceIDs {
		result[i] = DeviceIdentifier(id)
	}
	return result
}
