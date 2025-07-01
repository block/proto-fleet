package models

type MeasurementType int

// MeasurementType constants define the types of telemetry measurements
const (
	MeasurementTypeUnknown MeasurementType = iota
	MeasurementTypeTemperature
	MeasurementTypeHashrate
	MeasurementTypePower
	MeasurementTypeEfficiency
	MeasurementTypeFanSpeed
	MeasurementTypeVoltage
	MeasurementTypeCurrent
	MeasurementTypeUptime
	MeasurementTypeErrorRate
)

func (m MeasurementType) String() string {
	switch m {
	case MeasurementTypeUnknown:
		return "unknown"
	case MeasurementTypeTemperature:
		return "temperature"
	case MeasurementTypeHashrate:
		return "hashrate"
	case MeasurementTypePower:
		return "power"
	case MeasurementTypeEfficiency:
		return "efficiency"
	case MeasurementTypeFanSpeed:
		return "fan_speed"
	case MeasurementTypeVoltage:
		return "voltage"
	case MeasurementTypeCurrent:
		return "current"
	case MeasurementTypeUptime:
		return "uptime"
	case MeasurementTypeErrorRate:
		return "error_rate"
	default:
		return "unknown"
	}
}

type AggregationType int

// AggregationType constants define the types of aggregations
const (
	AggregationTypeUnknown AggregationType = iota
	AggregationTypeAverage
	AggregationTypeMin
	AggregationTypeMax
	AggregationTypeSum
	AggregationTypeCount
)

func (a AggregationType) String() string {
	switch a {
	case AggregationTypeUnknown:
		return "unknown"
	case AggregationTypeAverage:
		return "avg"
	case AggregationTypeMin:
		return "min"
	case AggregationTypeMax:
		return "max"
	case AggregationTypeSum:
		return "sum"
	case AggregationTypeCount:
		return "count"
	default:
		return "unknown"
	}
}

type ComponentStatus int

// ComponentStatus constants define the operational status of device components
const (
	ComponentStatusUnknown ComponentStatus = iota
	ComponentStatusHealthy
	ComponentStatusWarning
	ComponentStatusCritical
	ComponentStatusOffline
)

func (c ComponentStatus) String() string {
	switch c {
	case ComponentStatusUnknown:
		return "unknown"
	case ComponentStatusHealthy:
		return "healthy"
	case ComponentStatusWarning:
		return "warning"
	case ComponentStatusCritical:
		return "critical"
	case ComponentStatusOffline:
		return "offline"
	default:
		return "unknown"
	}
}

type UpdateType int

// UpdateType constants define the types of telemetry updates
const (
	UpdateTypeUnknown UpdateType = iota
	UpdateTypeTelemetry
	UpdateTypeHeartbeat
	UpdateTypeError
	UpdateTypeDeviceStatus
)

func (u UpdateType) String() string {
	switch u {
	case UpdateTypeUnknown:
		return "unknown"
	case UpdateTypeTelemetry:
		return "telemetry"
	case UpdateTypeHeartbeat:
		return "heartbeat"
	case UpdateTypeError:
		return "error"
	case UpdateTypeDeviceStatus:
		return "device_status"
	default:
		return "unknown"
	}
}
