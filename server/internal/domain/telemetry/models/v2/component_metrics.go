package models

import "time"

// ComponentInfo contains common metadata for all hardware components.
// It provides identification (Index, Name) and health status information.
type ComponentInfo struct {
	Index int    `json:"index"`
	Name  string `json:"name"`

	Status       ComponentStatus `json:"status"`
	StatusReason *string         `json:"status_reason,omitempty"`

	Timestamp *time.Time `json:"timestamp,omitempty"` // Optional timestamp, only use if it differs from device timestamp
}

// HashBoardMetrics represents telemetry from an ASIC hashboard.
// Hash boards are the primary computing components containing ASIC chips.
type HashBoardMetrics struct {
	ComponentInfo
	SerialNumber *string `json:"serial_number,omitempty"`

	HashRateHS *MetricValue `json:"hash_rate_hs,omitempty"`
	TempC      *MetricValue `json:"temp_c,omitempty"`

	VoltageV *MetricValue `json:"voltage_v,omitempty"`
	CurrentA *MetricValue `json:"current_a,omitempty"`

	InletTempC   *MetricValue `json:"inlet_temp_c,omitempty"`
	OutletTempC  *MetricValue `json:"outlet_temp_c,omitempty"`
	AmbientTempC *MetricValue `json:"ambient_temp_c,omitempty"`

	ChipCount        *int          `json:"chip_count,omitempty"`         // Total chips on this board
	ChipFrequencyMHz *MetricValue  `json:"chip_frequency_mhz,omitempty"` // Board-level chip frequency
	ASICs            []ASICMetrics `json:"asics,omitempty"`              // Individual ASIC chip telemetry
	FanMetrics       []FanMetrics  `json:"fan_metrics,omitempty"`        // Fans associated with this hashboard
}

// ASICMetrics represents telemetry from an individual ASIC chip.
// ASICs are sub-components of hash boards and can have independent health status.
type ASICMetrics struct {
	ComponentInfo

	TempC        *MetricValue `json:"temp_c,omitempty"`
	FrequencyMHz *MetricValue `json:"frequency_mhz,omitempty"`
	VoltageV     *MetricValue `json:"voltage_v,omitempty"`
	HashrateHS   *MetricValue `json:"hashrate_hs,omitempty"`
}

// PSUMetrics represents telemetry from a power supply unit.
// PSUs can report both input (from wall) and output (to device) measurements.
type PSUMetrics struct {
	ComponentInfo
	OutputPowerW   *MetricValue `json:"output_power_w,omitempty"`
	OutputVoltageV *MetricValue `json:"output_voltage_v,omitempty"`

	InputPowerW    *MetricValue `json:"input_power_w,omitempty"`
	InputVoltageV  *MetricValue `json:"input_voltage_v,omitempty"`
	InputCurrentA  *MetricValue `json:"input_current_a,omitempty"`
	OutputCurrentA *MetricValue `json:"output_current_a,omitempty"`

	HotSpotTempC      *MetricValue `json:"hotspot_temp_c,omitempty"`
	EfficiencyPercent *MetricValue `json:"efficiency_percent,omitempty"`
	FanMetrics        []FanMetrics `json:"fan_metrics,omitempty"`
}

// FanMetrics represents telemetry from a cooling fan.
// Fans can report speed in both RPM (absolute) and percent (relative to max).
type FanMetrics struct {
	ComponentInfo

	RPM     *MetricValue `json:"rpm,omitempty"`
	TempC   *MetricValue `json:"temp_c,omitempty"`
	Percent *MetricValue `json:"percent,omitempty"`
}

// ControlBoardMetrics represents telemetry from the device control board.
// Currently a placeholder for future control board specific metrics.
type ControlBoardMetrics struct {
	ComponentInfo
}

// SensorMetrics represents miscellaneous sensors on the device
// that don't fit into other specific component categories.
// Examples: ambient temperature, humidity, vibration sensors, etc.
type SensorMetrics struct {
	ComponentInfo

	Type  string       `json:"type,omitempty"`  // Sensor type (e.g., "humidity", "vibration")
	Unit  string       `json:"unit,omitempty"`  // Measurement unit (e.g., "C", "rpm", "%")
	Value *MetricValue `json:"value,omitempty"` // Sensor reading
}
