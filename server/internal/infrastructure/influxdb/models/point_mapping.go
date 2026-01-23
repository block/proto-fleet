package models

import (
	"fmt"
	"log/slog"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
)

const (
	deviceMetricsMeasurement = "device_metrics"

	deviceIDTag       = "device_id"
	componentTypeTag  = "component_type"
	componentIndexTag = "component_index"
	healthTag         = "health"

	hashRateHSField       = "hash_rate_hs"
	hashRateHSKindField   = "hash_rate_hs_kind"
	tempCField            = "temp_c"
	tempCKindField        = "temp_c_kind"
	fanRPMField           = "fan_rpm"
	fanRPMKindField       = "fan_rpm_kind"
	powerWField           = "power_w"
	powerWKindField       = "power_w_kind"
	efficiencyJHField     = "efficiency_jh"
	efficiencyJHKindField = "efficiency_jh_kind"
	voltageVField         = "voltage_v"
	voltageVKindField     = "voltage_v_kind"
	currentAField         = "current_a"
	currentAKindField     = "current_a_kind"
	inletTempCField       = "inlet_temp_c"
	inletTempCKindField   = "inlet_temp_c_kind"
	outletTempCField      = "outlet_temp_c"
	outletTempCKindField  = "outlet_temp_c_kind"
	ambientTempCField     = "ambient_temp_c"
	ambientTempCKindField = "ambient_temp_c_kind"
	chipCountField        = "chip_count"
	chipCountKindField    = "chip_count_kind"
	chipFrequencyMHzField = "chip_frequency_mhz"
)

// setMetricOnPoint adds a metric value and its kind to an InfluxDB point.
// If the metric is nil, returns the point unchanged. Otherwise, sets both the value and kind fields.
func setMetricOnPoint(point *influxdb3.Point, metric *modelsV2.MetricValue, valueField, kindField string) *influxdb3.Point {
	if metric == nil {
		return point
	}
	return point.
		SetDoubleField(valueField, metric.Value).
		SetStringField(kindField, metric.Kind.String())
}

// DeviceMetricsToPoints converts a DeviceMetrics instance to a slice of InfluxDB points.
// Points will be for both device and component-level metrics.
func DeviceMetricsToPoints(telemetry modelsV2.DeviceMetrics) []*influxdb3.Point {
	var points []*influxdb3.Point

	// Device-level point
	devicePoint := influxdb3.NewPointWithMeasurement(deviceMetricsMeasurement).
		SetTag(deviceIDTag, telemetry.DeviceID).
		SetTag(healthTag, telemetry.Health.String()).
		SetTimestamp(telemetry.Timestamp)

	// Add device-level metrics as fields
	devicePoint = setMetricOnPoint(devicePoint, telemetry.HashrateHS, hashRateHSField, hashRateHSKindField)
	devicePoint = setMetricOnPoint(devicePoint, telemetry.TempC, tempCField, tempCKindField)
	devicePoint = setMetricOnPoint(devicePoint, telemetry.FanRPM, fanRPMField, fanRPMKindField)
	devicePoint = setMetricOnPoint(devicePoint, telemetry.PowerW, powerWField, powerWKindField)
	devicePoint = setMetricOnPoint(devicePoint, telemetry.EfficiencyJH, efficiencyJHField, efficiencyJHKindField)
	points = append(points, devicePoint)

	// Component-level points
	// TODO(DASH-782): Implement component-level points conversion

	return points
}

// ToDeviceMetrics converts InfluxDB point values to a DeviceMetrics instance.
// It processes both device-level and component-level points.
func ToDeviceMetrics(devicePoint *influxdb3.PointValues, _ ...*influxdb3.PointValues) (modelsV2.DeviceMetrics, error) {
	if devicePoint == nil {
		return modelsV2.DeviceMetrics{}, fmt.Errorf("nil device metrics point")
	}
	measurement := devicePoint.GetMeasurement()
	if measurement != "" && measurement != deviceMetricsMeasurement {
		return modelsV2.DeviceMetrics{}, fmt.Errorf("invalid device metrics point: got measurement %q, want %q", measurement, deviceMetricsMeasurement)
	}
	telemetry := modelsV2.DeviceMetrics{}

	// Get device_id - try tag first, then string field (LVC returns fields)
	id, ok := devicePoint.GetTag(deviceIDTag)
	if !ok {
		idPtr := devicePoint.GetStringField(deviceIDTag)
		if idPtr == nil {
			return modelsV2.DeviceMetrics{}, fmt.Errorf("missing device_id")
		}
		id = *idPtr
	}
	telemetry.DeviceID = id
	telemetry.Timestamp = devicePoint.Timestamp

	// Get health - try tag first, then string field (LVC returns fields)
	healthStr, ok := devicePoint.GetTag(healthTag)
	if !ok {
		healthPtr := devicePoint.GetStringField(healthTag)
		if healthPtr == nil {
			slog.Debug("missing health, defaulting to unknown")
			unknown := modelsV2.HealthUnknown
			healthStr = unknown.String()
		} else {
			healthStr = *healthPtr
		}
	}
	health, err := modelsV2.ParseHealthStatus(healthStr)
	if err != nil {
		return modelsV2.DeviceMetrics{}, fmt.Errorf("invalid health: %w", err)
	}
	telemetry.Health = health

	// Parse device-level metrics
	telemetry.HashrateHS = getMetricFromPoint(devicePoint, hashRateHSField, hashRateHSKindField)
	telemetry.TempC = getMetricFromPoint(devicePoint, tempCField, tempCKindField)
	telemetry.FanRPM = getMetricFromPoint(devicePoint, fanRPMField, fanRPMKindField)
	telemetry.PowerW = getMetricFromPoint(devicePoint, powerWField, powerWKindField)
	telemetry.EfficiencyJH = getMetricFromPoint(devicePoint, efficiencyJHField, efficiencyJHKindField)

	// TODO(DASH-782): Parse component-level points

	return telemetry, nil
}

// getMetricFromPoint extracts a metric value and its kind from an InfluxDB point.
// Returns nil if the value field doesn't exist, otherwise returns a MetricValue with the value and kind.
func getMetricFromPoint(pv *influxdb3.PointValues, valueField, kindField string) *modelsV2.MetricValue {
	value := pv.GetDoubleField(valueField)
	if value == nil {
		return nil
	}
	return &modelsV2.MetricValue{
		Value: *value,
		Kind:  getMetricKindFromPoint(pv, kindField),
	}
}

func getMetricKindFromPoint(pv *influxdb3.PointValues, kindField string) modelsV2.MetricKind {
	kindStr := pv.GetStringField(kindField)
	if kindStr == nil {
		return modelsV2.MetricKindGauge // Default to gauge if not specified
	}
	kind, err := modelsV2.ParseMetricKind(*kindStr)
	if err != nil {
		slog.Debug("invalid metric kind, defaulting to gauge", slog.String("kind", *kindStr))
		return modelsV2.MetricKindGauge
	}
	return kind
}
