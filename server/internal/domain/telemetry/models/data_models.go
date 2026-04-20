package models

import (
	"time"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
)

// TelemetryUpdate represents a streaming update from the telemetry system
type TelemetryUpdate struct {
	Type UpdateType `json:"type"`
	// DeviceIdentifier is the unique device identifier string (e.g., "proto-miner-001"),
	// not the database primary key (device.id BIGINT).
	DeviceIdentifier DeviceIdentifier  `json:"device_identifier,omitempty"`
	Timestamp        time.Time         `json:"timestamp"`
	MeasurementName  string            `json:"measurement_name,omitempty"`  // e.g., "temperature_c", "hashrate_ths"
	MeasurementValue float64           `json:"measurement_value,omitempty"` // Raw measurement value
	Error            *string           `json:"error,omitempty"`
	Status           *ComponentStatus  `json:"status,omitempty"`
	DeviceStatus     *mm.MinerStatus   `json:"device_status,omitempty"`      // e.g., ACTIVE, INACTIVE, ERROR
	MinerStateCounts *MinerStateCounts `json:"miner_state_counts,omitempty"` // Counts of miners in different states
}

type MinerStateCounts struct {
	Hashing  int32 `json:"hashing"`
	Broken   int32 `json:"broken"`
	Offline  int32 `json:"offline"`
	Sleeping int32 `json:"sleeping"`
}
