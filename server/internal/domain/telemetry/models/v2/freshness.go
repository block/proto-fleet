package models

import (
	"math"
	"time"
)

// usableMeasurements centralizes the device-level fields and finite-value
// predicate used by both collector-clock observations and server persistence.
func (m *DeviceMetrics) usableMeasurements() [5]*MetricValue {
	values := [5]*MetricValue{m.HashrateHS, m.TempC, m.PowerW, m.FanRPM, m.EfficiencyJH}
	for i, value := range values {
		if value == nil || math.IsNaN(value.Value) || math.IsInf(value.Value, 0) {
			values[i] = nil
		}
	}
	return values
}

// HasUsableMeasurement reports finite device-level evidence. Zero is valid;
// status alone is not evidence. Observation clock validation is separate.
func (m *DeviceMetrics) HasUsableMeasurement() bool {
	for _, value := range m.usableMeasurements() {
		if value != nil {
			return true
		}
	}
	return false
}

// FreshnessObservationTime returns the oldest timestamp among usable device
// measurements in the collector's clock. Measurement metadata may expose stale
// values inside a recently assembled snapshot. Status alone is not telemetry.
func (m *DeviceMetrics) FreshnessObservationTime() time.Time {
	now := time.Now()
	if m.Timestamp.IsZero() || m.Timestamp.After(now) {
		return time.Time{}
	}
	oldest := m.Timestamp
	usable := false
	for _, value := range m.usableMeasurements() {
		if value == nil {
			continue
		}
		if value.MetaData != nil && value.MetaData.Timestamp != nil && value.MetaData.Timestamp.After(now) {
			return time.Time{}
		}
		usable = true
		if value.MetaData != nil && value.MetaData.Timestamp != nil && value.MetaData.Timestamp.Before(oldest) {
			oldest = *value.MetaData.Timestamp
		}
	}
	if !usable {
		return time.Time{}
	}
	return oldest
}
