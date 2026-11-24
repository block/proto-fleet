package models

import (
	"math"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// DeviceMetrics represents the complete telemetry snapshot for a mining device.
// It includes device-level health and aggregated metrics, as well as detailed
// component-level metrics for hashboards, PSUs, fans, control boards, and sensors.
type DeviceMetrics struct {
	Container *sdk.ContainerSnapshot `json:"container,omitempty"`
	// Identity
	// DeviceIdentifier is the unique device identifier string (e.g., "proto-miner-001"),
	// not the database primary key (device.id BIGINT).
	DeviceIdentifier string    `json:"device_identifier"`
	Timestamp        time.Time `json:"timestamp"`
	// FreshnessObservedAt is a server-clock observation bound. It is never
	// accepted from a JSON payload; remote samples derive it from observation age.
	FreshnessObservedAt   time.Time `json:"-"`
	FreshnessSourceNodeID int64     `json:"-"`
	FirmwareVersion       string    `json:"firmware_version,omitempty"`

	// UptimeSeconds is whole elapsed seconds since boot; nil means unknown.
	// UptimeObservedAt is the sampling request start in the collector's clock.
	// Fleet Node samples are translated conservatively to the server's clock using
	// the observation age and server request start. Zero time provides no reboot evidence.
	// Cached samples retain both values unchanged.
	UptimeSeconds    *uint64   `json:"uptime_seconds,omitempty"`
	UptimeObservedAt time.Time `json:"uptime_observed_at,omitzero"`

	// Device-level health
	Health       HealthStatus `json:"health"`
	HealthReason *string      `json:"health_reason,omitempty"` // Human-readable reason for health status

	// DefaultPasswordActive is non-nil only when determined: true = still on the
	// factory password; nil = undetermined.
	DefaultPasswordActive *bool `json:"default_password_active,omitempty"`

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

// BootTimeLowerBound derives the earliest possible boot time from whole-second
// uptime. The sampling request start and an extra second for truncation keep
// delayed or cached observations from making an old boot appear newer.
func (m *DeviceMetrics) BootTimeLowerBound(now time.Time) (time.Time, bool) {
	if m.UptimeSeconds == nil || m.UptimeObservedAt.IsZero() || m.UptimeObservedAt.After(now) {
		return time.Time{}, false
	}
	uptime := *m.UptimeSeconds
	if uptime >= uint64(math.MaxInt64/int64(time.Second)) {
		return time.Time{}, false
	}
	// #nosec G115 -- the bound above ensures uptime plus one second fits in time.Duration.
	return m.UptimeObservedAt.Add(-time.Duration(uptime+1) * time.Second), true
}

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
