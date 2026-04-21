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

// InfluxDB measurement name constants
const (
	InfluxMeasurementUnknown     = "unknown"
	InfluxMeasurementTemperature = "temperature_c"
	InfluxMeasurementHashrate    = "hashrate_mhs"
	InfluxMeasurementPower       = "power_w"
	InfluxMeasurementEfficiency  = "efficiency_jh"
	InfluxMeasurementFanSpeed    = "fan_rpm"
	InfluxMeasurementVoltage     = "voltage_mv"
	InfluxMeasurementCurrent     = "current_ma"
	InfluxMeasurementUptime      = "uptime"
	InfluxMeasurementErrorRate   = "error_rate"
)

// MeasurementNameToType converts an InfluxDB measurement name string to a MeasurementType.
// This is the reverse of MeasurementType.InfluxMeasurementName().
func MeasurementNameToType(name string) MeasurementType {
	switch name {
	case InfluxMeasurementHashrate:
		return MeasurementTypeHashrate
	case InfluxMeasurementPower:
		return MeasurementTypePower
	case InfluxMeasurementTemperature:
		return MeasurementTypeTemperature
	case InfluxMeasurementEfficiency:
		return MeasurementTypeEfficiency
	case InfluxMeasurementFanSpeed:
		return MeasurementTypeFanSpeed
	case InfluxMeasurementVoltage:
		return MeasurementTypeVoltage
	case InfluxMeasurementCurrent:
		return MeasurementTypeCurrent
	case InfluxMeasurementUptime:
		return MeasurementTypeUptime
	case InfluxMeasurementErrorRate:
		return MeasurementTypeErrorRate
	default:
		return MeasurementTypeUnknown
	}
}

// InfluxMeasurementName returns the actual InfluxDB table/measurement name
// This maps domain model measurement types to the actual table names used in InfluxDB
func (m MeasurementType) InfluxMeasurementName() string {
	switch m {
	case MeasurementTypeUnknown:
		return InfluxMeasurementUnknown
	case MeasurementTypeTemperature:
		return InfluxMeasurementTemperature
	case MeasurementTypeHashrate:
		return InfluxMeasurementHashrate
	case MeasurementTypePower:
		return InfluxMeasurementPower
	case MeasurementTypeEfficiency:
		return InfluxMeasurementEfficiency
	case MeasurementTypeFanSpeed:
		return InfluxMeasurementFanSpeed
	case MeasurementTypeVoltage:
		return InfluxMeasurementVoltage
	case MeasurementTypeCurrent:
		return InfluxMeasurementCurrent
	case MeasurementTypeUptime:
		return InfluxMeasurementUptime
	case MeasurementTypeErrorRate:
		return InfluxMeasurementErrorRate
	default:
		return InfluxMeasurementUnknown
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
	AggregationTypeTotal
	AggregationTypeMeanChange
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
	case AggregationTypeTotal:
		return "total"
	case AggregationTypeMeanChange:
		return "mean_change"
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
	UpdateTypeMinerStateCounts
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
	case UpdateTypeMinerStateCounts:
		return "miner_state_counts"
	default:
		return "unknown"
	}
}
