package models

import (
	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

func ToInfluxPoint(telemetry models.Telemetry) *influxdb3.Point {
	point := influxdb3.NewPointWithMeasurement(telemetry.Measurement).
		SetTimestamp(telemetry.Timestamp)

	for key, value := range telemetry.Tags {
		point.SetTag(key, value)
	}

	for key, value := range telemetry.Fields {
		point.SetField(key, value)
	}

	return point
}

func ToTelemetry(pv *influxdb3.PointValues) models.Telemetry {
	fields := pv.Fields
	for _, fieldName := range pv.GetFieldNames() {
		fields[fieldName] = pv.GetField(fieldName)
	}

	tags := make(map[string]string)
	for _, tagName := range pv.GetTagNames() {
		if tagValue, exists := pv.GetTag(tagName); exists {
			tags[tagName] = tagValue
		}
	}

	return models.Telemetry{
		Measurement: pv.GetMeasurement(),
		Fields:      fields,
		Tags:        tags,
		Timestamp:   pv.Timestamp,
	}
}
