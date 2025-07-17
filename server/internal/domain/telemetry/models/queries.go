package models

import "time"

type TimeRange struct {
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
}

type TimeWindow struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

type MetadataFilter struct {
	DeviceTypes []string          `json:"device_types,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Status      *ComponentStatus  `json:"status,omitempty"`
}

type LatestTelemetryQuery struct {
	DeviceIDs        []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes []MeasurementType  `json:"measurement_types,omitempty"`
	MaxAge           *time.Duration     `json:"max_age,omitempty"`
	Tags             map[string]string  `json:"tags,omitempty"`
}

type TimeSeriesTelemetryQuery struct {
	DeviceIDs        []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes []MeasurementType  `json:"measurement_types,omitempty"`
	TimeRange        TimeRange          `json:"time_range"`
	Limit            *int               `json:"limit,omitempty"`
	Tags             map[string]string  `json:"tags,omitempty"`
}

type MetadataQuery struct {
	DeviceIDs []DeviceIdentifier `json:"device_ids,omitempty"`
	Filter    *MetadataFilter    `json:"filter,omitempty"`
}

type StreamQuery struct {
	DeviceIDs         []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes  []MeasurementType  `json:"measurement_types,omitempty"`
	IncludeHeartbeat  bool               `json:"include_heartbeat"`
	HeartbeatInterval *time.Duration     `json:"heartbeat_interval,omitempty"`
	Tags              map[string]string  `json:"tags,omitempty"`
}

type AggregationQuery struct {
	DeviceIDs        []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes []MeasurementType  `json:"measurement_types,omitempty"`
	TimeRange        TimeRange          `json:"time_range"`
	AggregationType  AggregationType    `json:"aggregation_type"`
	GroupByInterval  *time.Duration     `json:"group_by_interval,omitempty"`
	Tags             map[string]string  `json:"tags,omitempty"`
}
