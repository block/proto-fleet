package models

import "time"

type TimeRange struct {
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
}

type TimeSeriesTelemetryQuery struct {
	DeviceIDs        []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes []MeasurementType  `json:"measurement_types,omitempty"`
	TimeRange        TimeRange          `json:"time_range"`
	Limit            *int               `json:"limit,omitempty"`
	Tags             map[string]string  `json:"tags,omitempty"`
}

type StreamQuery struct {
	DeviceIDs         []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes  []MeasurementType  `json:"measurement_types,omitempty"`
	IncludeHeartbeat  bool               `json:"include_heartbeat"`
	HeartbeatInterval *time.Duration     `json:"heartbeat_interval,omitempty"`
	Tags              map[string]string  `json:"tags,omitempty"`
}

type CombinedMetricsQuery struct {
	DeviceIDs        []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes []MeasurementType  `json:"measurement_types,omitempty"`
	AggregationTypes []AggregationType  `json:"aggregation_types,omitempty"`
	TimeRange        TimeRange          `json:"time_range"`
	WindowDuration   *time.Duration     `json:"window_duration,omitempty"`
	PaginationToken  string             `json:"pagination_token,omitempty"`
	PageSize         int                `json:"page_size,omitempty"`
	SlideInterval    *time.Duration     `json:"slide_interval,omitempty"`
	OrganizationID   int64              `json:"organization_id,omitempty"`
}

type StreamCombinedMetricsQuery struct {
	DeviceIDs        []DeviceIdentifier `json:"device_ids,omitempty"`
	MeasurementTypes []MeasurementType  `json:"measurement_types,omitempty"`
	AggregationTypes []AggregationType  `json:"aggregation_types,omitempty"`
	Granularity      time.Duration      `json:"granularity,omitempty"`
	UpdateInterval   time.Duration      `json:"update_interval,omitempty"`
	OrganizationID   int64              `json:"organization_id,omitempty"`
}

type AggregatedValue struct {
	Type  AggregationType `json:"type"`
	Value float64         `json:"value"`
}

type Metric struct {
	MeasurementType  MeasurementType   `json:"measurement_type"`
	AggregatedValues []AggregatedValue `json:"aggregated_values"`
	OpenTime         time.Time         `json:"open_time"`
	DeviceCount      int32             `json:"device_count"`
}
type CombinedMetric struct {
	Metrics                 []Metric                 `json:"metrics"`
	NextPageToken           string                   `json:"next_page_token,omitempty"` // for pagination
	TemperatureStatusCounts []TemperatureStatusCount `json:"temperature_status_counts,omitempty"`
	UptimeStatusCounts      []UptimeStatusCount      `json:"uptime_status_counts,omitempty"`
	MinerStateCounts        *MinerStateCounts        `json:"miner_state_counts,omitempty"`
}

// TemperatureStatusCount represents temperature status distribution at a point in time
type TemperatureStatusCount struct {
	Timestamp     time.Time `json:"timestamp"`
	ColdCount     int32     `json:"cold_count"`     // Count of miners < 0°C
	OkCount       int32     `json:"ok_count"`       // Count of miners 0-70°C
	HotCount      int32     `json:"hot_count"`      // Count of miners 70-90°C
	CriticalCount int32     `json:"critical_count"` // Count of miners > 90°C
}

type MinerStateCountsRow struct {
	OrgID         int64
	HashingCount  int32
	BrokenCount   int32
	OfflineCount  int32
	SleepingCount int32
}

type UptimeStatusCount struct {
	Timestamp       time.Time `json:"timestamp"`
	HashingCount    int32     `json:"hashing_count"`
	NotHashingCount int32     `json:"not_hashing_count"`
	BrokenCount     int32     `json:"broken_count"`
}
