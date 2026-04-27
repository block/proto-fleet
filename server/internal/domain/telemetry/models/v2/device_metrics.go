package models

import (
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// DeviceMetrics represents the complete telemetry snapshot for a mining device.
// It includes device-level health and aggregated metrics, as well as detailed
// component-level metrics for hashboards, PSUs, fans, control boards, and sensors.
type DeviceMetrics struct {
	// Identity
	// DeviceIdentifier is the unique device identifier string (e.g., "proto-miner-001"),
	// not the database primary key (device.id BIGINT).
	DeviceIdentifier string    `json:"device_identifier"`
	Timestamp        time.Time `json:"timestamp"`
	FirmwareVersion  string    `json:"firmware_version,omitempty"`

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

	// Consumed by the commit-time gate against SV1-only miners.
	StratumV2Support StratumV2SupportStatus `json:"stratum_v2_support,omitempty"`
}

type StratumV2SupportStatus int32

const (
	StratumV2SupportUnspecified StratumV2SupportStatus = 0
	StratumV2SupportUnknown     StratumV2SupportStatus = 1
	StratumV2SupportUnsupported StratumV2SupportStatus = 2
	StratumV2SupportSupported   StratumV2SupportStatus = 3
)

// DefaultMeasurementTypes returns the standard set of measurement types for conversion.
var DefaultMeasurementTypes = []models.MeasurementType{
	models.MeasurementTypeHashrate,
	models.MeasurementTypeTemperature,
	models.MeasurementTypePower,
	models.MeasurementTypeEfficiency,
	models.MeasurementTypeFanSpeed,
}

// ExtractRawMeasurement extracts a measurement value in raw storage units.
// Returns (value, timestamp, ok) where ok is false if the measurement is not available.
// Values are returned in storage units (H/s, W, J/H) - conversion to display units
// should happen in the handler layer.
func (m *DeviceMetrics) ExtractRawMeasurement(measurementType models.MeasurementType) (float64, time.Time, bool) {
	var rawValue float64
	switch measurementType {
	case models.MeasurementTypeHashrate:
		if m.HashrateHS == nil {
			return 0, time.Time{}, false
		}
		rawValue = m.HashrateHS.Value
	case models.MeasurementTypeTemperature:
		if m.TempC == nil {
			return 0, time.Time{}, false
		}
		rawValue = m.TempC.Value
	case models.MeasurementTypePower:
		if m.PowerW == nil {
			return 0, time.Time{}, false
		}
		rawValue = m.PowerW.Value
	case models.MeasurementTypeEfficiency:
		if m.EfficiencyJH == nil {
			return 0, time.Time{}, false
		}
		rawValue = m.EfficiencyJH.Value
	case models.MeasurementTypeFanSpeed:
		if m.FanRPM == nil {
			return 0, time.Time{}, false
		}
		rawValue = m.FanRPM.Value
	case models.MeasurementTypeUnknown, models.MeasurementTypeVoltage,
		models.MeasurementTypeCurrent, models.MeasurementTypeUptime, models.MeasurementTypeErrorRate:
		// DeviceMetrics doesn't have fields for these measurement types
		return 0, time.Time{}, false
	default:
		// Any unhandled MeasurementType should be treated as unavailable
		return 0, time.Time{}, false
	}

	return rawValue, m.Timestamp, true
}
