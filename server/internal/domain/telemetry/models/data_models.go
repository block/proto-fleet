package models

import "time"

// DeviceMetadata represents metadata about a device
type DeviceMetadata struct {
	DeviceID     DeviceIdentifier  `json:"device_id"`
	DeviceType   string            `json:"device_type,omitempty"`
	LastSeen     time.Time         `json:"last_seen"`
	Status       ComponentStatus   `json:"status"`
	Location     string            `json:"location,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
}

// AggregatedTelemetry represents aggregated telemetry data
type AggregatedTelemetry struct {
	DeviceID        DeviceIdentifier  `json:"device_id"`
	MeasurementType MeasurementType   `json:"measurement_type"`
	Value           float64           `json:"value"`
	AggregationType AggregationType   `json:"aggregation_type"`
	DataPoints      int               `json:"data_points"`
	TimeWindow      TimeWindow        `json:"time_window"`
	Tags            map[string]string `json:"tags,omitempty"`
}

// TelemetryUpdate represents a streaming update from the telemetry system
type TelemetryUpdate struct {
	Type      UpdateType       `json:"type"`
	DeviceID  DeviceIdentifier `json:"device_id,omitempty"`
	Timestamp time.Time        `json:"timestamp"`
	Data      *Telemetry       `json:"data,omitempty"`
	Error     *string          `json:"error,omitempty"`
	Status    *ComponentStatus `json:"status,omitempty"`
}
