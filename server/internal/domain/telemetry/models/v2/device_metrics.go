package models

import "time"

// DeviceMetrics represents the complete telemetry snapshot for a mining device.
// It includes device-level health and aggregated metrics, as well as detailed
// component-level metrics for hashboards, PSUs, fans, control boards, and sensors.
type DeviceMetrics struct {
	// Identity
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`

	// Device-level health
	Health       HealthStatus `json:"health"`
	HealthReason *string      `json:"health_reason,omitempty"` // Human-readable reason for health status

	// Device-level metrics (aggregated from components)
	HashrateHS   *MetricValue `json:"hashrate_hs,omitempty"`   // H/s - sum of all hashrates
	TempC        *MetricValue `json:"temp_c,omitempty"`        // °C - max of all temps
	FanRPM       *MetricValue `json:"fan_rpm,omitempty"`       // RPM - max of all fan speeds
	PowerW       *MetricValue `json:"power_w,omitempty"`       // W - sum of all power draws
	EfficiencyJH *MetricValue `json:"efficiency_jh,omitempty"` // J/H - power efficiency

	// Component-level metrics
	HashBoards          []HashBoardMetrics    `json:"hash_boards,omitempty"`
	PSUMetrics          []PSUMetrics          `json:"psu_metrics,omitempty"`
	ControlBoardMetrics []ControlBoardMetrics `json:"control_board_metrics,omitempty"`
	FanMetrics          []FanMetrics          `json:"fan_metrics,omitempty"`
	SensorMetrics       []SensorMetrics       `json:"sensor_metrics,omitempty"`
}
