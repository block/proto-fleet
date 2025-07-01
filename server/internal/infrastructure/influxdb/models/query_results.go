package models

import (
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

type QueryResultMapper struct{}

func (m *QueryResultMapper) ToDeviceMetadata(result *influxdb3.PointValues) models.DeviceMetadata {
	deviceID := ""
	if id, ok := result.GetTag("device_id"); ok {
		deviceID = id
	}

	deviceType := ""
	if dt, ok := result.GetTag("device_type"); ok {
		deviceType = dt
	}

	location := ""
	if loc, ok := result.GetTag("location"); ok {
		location = loc
	}

	lastSeen := result.Timestamp

	tags := make(map[string]string)
	for key, value := range result.Tags {
		if key != "device_id" && key != "device_type" && key != "location" {
			tags[key] = value
		}
	}

	return models.DeviceMetadata{
		DeviceID:   models.DeviceID(deviceID),
		DeviceType: deviceType,
		LastSeen:   lastSeen,
		Status:     models.ComponentStatusUnknown, // Default status
		Location:   location,
		Tags:       tags,
	}
}

func (m *QueryResultMapper) ToAggregatedTelemetry(result map[string]interface{}, aggType models.AggregationType) models.AggregatedTelemetry {
	deviceID := ""
	if id, ok := result["device_id"].(string); ok {
		deviceID = id
	}

	measurementType := models.MeasurementTypeUnknown
	if mt, ok := result["measurement_type"].(string); ok {
		measurementType = parseMeasurementType(mt)
	}

	var aggregatedValue float64
	var dataPoints int

	if val, ok := result["aggregated_value"].(float64); ok {
		aggregatedValue = val
	}
	if val, ok := result["data_points"].(int64); ok {
		dataPoints = int(val)
	} else if val, ok := result["data_points"].(int); ok {
		dataPoints = val
	}

	timestamp := time.Now()
	if t, ok := result["time"].(time.Time); ok {
		timestamp = t
	}

	tags := make(map[string]string)
	for key, value := range result {
		if strVal, ok := value.(string); ok && key != "device_id" && key != "measurement_type" {
			tags[key] = strVal
		}
	}

	return models.AggregatedTelemetry{
		DeviceID:        models.DeviceID(deviceID),
		MeasurementType: measurementType,
		Value:           aggregatedValue,
		AggregationType: aggType,
		DataPoints:      dataPoints,
		TimeWindow: models.TimeWindow{
			StartTime: timestamp, // Simplified - would need proper window extraction
			EndTime:   timestamp,
		},
		Tags: tags,
	}
}

func parseMeasurementType(s string) models.MeasurementType {
	switch s {
	case "temperature":
		return models.MeasurementTypeTemperature
	case "hashrate":
		return models.MeasurementTypeHashrate
	case "power":
		return models.MeasurementTypePower
	case "efficiency":
		return models.MeasurementTypeEfficiency
	case "fan_speed":
		return models.MeasurementTypeFanSpeed
	case "voltage":
		return models.MeasurementTypeVoltage
	case "current":
		return models.MeasurementTypeCurrent
	case "uptime":
		return models.MeasurementTypeUptime
	case "error_rate":
		return models.MeasurementTypeErrorRate
	default:
		return models.MeasurementTypeUnknown
	}
}
