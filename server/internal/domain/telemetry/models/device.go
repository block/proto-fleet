package models

import (
	"time"

	minerModels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
)

// DeviceID is a type alias for backward compatibility - no breaking changes
type DeviceID = minerModels.DeviceID

// Device struct remains in telemetry package for telemetry-specific concerns
type Device struct {
	ID            DeviceID  `json:"id"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

// NewDeviceIDFromString creates a DeviceID from a string for compatibility
func NewDeviceIDFromString(s string) DeviceID {
	return DeviceID(s)
}
