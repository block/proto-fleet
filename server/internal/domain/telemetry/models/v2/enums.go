package models

import "fmt"

const (
	metricKindUnknownStr      = "metric_kind_unknown"
	healthUnknownStr          = "health_unknown"
	componentStatusUnknownStr = "component_status_unknown"
	componentTypeUnknownStr   = "component_type_unknown"
)

// MetricKind represents the type of metric value and how it should be interpreted.
//
// This classification affects how metrics are aggregated, displayed, and stored:
//   - Gauge: Current value at a point in time (e.g., temperature, RPM)
//   - Rate: Change per unit time (e.g., hashrate as H/s)
//   - Counter: Monotonically increasing value (e.g., shares accepted, uptime seconds)
//
// This aligns with the SDK MetricKind enum for consistency.
type MetricKind int

// MetricKind values
const (
	MetricKindUnknown MetricKind = iota // Unknown metric kind
	MetricKindGauge                     // Point-in-time measurement (default)
	MetricKindRate                      // Rate of change per second
	MetricKindCounter                   // Monotonically increasing counter
)

func (mk *MetricKind) String() string {
	if mk == nil {
		return metricKindUnknownStr
	}
	switch *mk {
	case MetricKindUnknown:
		return metricKindUnknownStr
	case MetricKindGauge:
		return "metric_kind_gauge"
	case MetricKindRate:
		return "metric_kind_rate"
	case MetricKindCounter:
		return "metric_kind_counter"
	default:
		return metricKindUnknownStr
	}
}

// MarshalJSON implements json.Marshaler for MetricKind.
func (mk *MetricKind) MarshalJSON() ([]byte, error) {
	return []byte(`"` + mk.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler for MetricKind.
func (mk *MetricKind) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid JSON string for MetricKind")
	}
	s := string(data[1 : len(data)-1])
	parsed, err := ParseMetricKind(s)
	if err != nil {
		return err
	}
	*mk = parsed
	return nil
}

// ParseMetricKind parses a string into a MetricKind.
// Returns an error if the string is not a valid metric kind.
func ParseMetricKind(s string) (MetricKind, error) {
	switch s {
	case "metric_kind_gauge":
		return MetricKindGauge, nil
	case "metric_kind_rate":
		return MetricKindRate, nil
	case "metric_kind_counter":
		return MetricKindCounter, nil
	case metricKindUnknownStr:
		return MetricKindUnknown, nil
	default:
		return MetricKindUnknown, fmt.Errorf("invalid metric kind: %q", s)
	}
}

// HealthStatus represents the overall health state of a mining device.
//
// Health status provides a high-level assessment combining operational state
// and hardware health. The distinction between active/inactive helps operators
// understand whether a healthy device is intentionally idle or actively mining.
//
// Status progression typically follows: Unknown → Healthy → Warning → Critical
type HealthStatus int

// HealthStatus values
const (
	HealthUnknown         HealthStatus = iota // Unknown health state (e.g., device unreachable)
	HealthHealthyActive                       // Mining and all systems healthy
	HealthHealthyInactive                     // All systems healthy but not actively mining
	HealthWarning                             // Degraded performance but still operational
	HealthCritical                            // Failed, non-functional, or requires immediate attention
)

func (h *HealthStatus) String() string {
	if h == nil {
		return healthUnknownStr
	}
	switch *h {
	case HealthUnknown:
		return healthUnknownStr
	case HealthHealthyActive:
		return "health_healthy_active"
	case HealthHealthyInactive:
		return "health_healthy_inactive"
	case HealthWarning:
		return "health_warning"
	case HealthCritical:
		return "health_critical"
	default:
		return healthUnknownStr
	}
}

// MarshalJSON implements json.Marshaler for HealthStatus.
func (h *HealthStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + h.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler for HealthStatus.
func (h *HealthStatus) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid JSON string for HealthStatus")
	}
	s := string(data[1 : len(data)-1])
	parsed, err := ParseHealthStatus(s)
	if err != nil {
		return err
	}
	*h = parsed
	return nil
}

// ParseHealthStatus parses a string into a HealthStatus.
// Returns an error if the string is not a valid health status.
func ParseHealthStatus(s string) (HealthStatus, error) {
	switch s {
	case healthUnknownStr:
		return HealthUnknown, nil
	case "health_healthy_active":
		return HealthHealthyActive, nil
	case "health_healthy_inactive":
		return HealthHealthyInactive, nil
	case "health_warning":
		return HealthWarning, nil
	case "health_critical":
		return HealthCritical, nil
	default:
		return HealthUnknown, fmt.Errorf("invalid health status: %q", s)
	}
}

// ComponentStatus represents the health and operational state of an individual component.
//
// Component status is more granular than device-level health and helps identify
// which specific parts are causing device-level issues. The Offline and Disabled
// states distinguish between unexpected failures and intentional deactivation.
//
// Typical status progression: Unknown → Healthy → Warning → Critical/Offline
type ComponentStatus int

// ComponentStatus values
const (
	ComponentStatusUnknown  ComponentStatus = iota // Unknown status (e.g., no telemetry data)
	ComponentStatusHealthy                         // Operating normally within acceptable parameters
	ComponentStatusWarning                         // Degraded performance but still functional
	ComponentStatusCritical                        // Failed, malfunctioning, or out of safe operating range
	ComponentStatusOffline                         // Not responding or unreachable
	ComponentStatusDisabled                        // Intentionally disabled by operator or firmware
)

func (cs *ComponentStatus) String() string {
	if cs == nil {
		return componentStatusUnknownStr
	}
	switch *cs {
	case ComponentStatusUnknown:
		return componentStatusUnknownStr
	case ComponentStatusHealthy:
		return "component_status_healthy"
	case ComponentStatusWarning:
		return "component_status_warning"
	case ComponentStatusCritical:
		return "component_status_critical"
	case ComponentStatusOffline:
		return "component_status_offline"
	case ComponentStatusDisabled:
		return "component_status_disabled"
	default:
		return componentStatusUnknownStr
	}
}

// MarshalJSON implements json.Marshaler for ComponentStatus.
func (cs *ComponentStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + cs.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler for ComponentStatus.
func (cs *ComponentStatus) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid JSON string for ComponentStatus")
	}
	s := string(data[1 : len(data)-1])
	parsed, err := ParseComponentStatus(s)
	if err != nil {
		return err
	}
	*cs = parsed
	return nil
}

// ParseComponentStatus parses a string into a ComponentStatus.
// Returns an error if the string is not a valid component status.
func ParseComponentStatus(s string) (ComponentStatus, error) {
	switch s {
	case componentStatusUnknownStr:
		return ComponentStatusUnknown, nil
	case "component_status_healthy":
		return ComponentStatusHealthy, nil
	case "component_status_warning":
		return ComponentStatusWarning, nil
	case "component_status_critical":
		return ComponentStatusCritical, nil
	case "component_status_offline":
		return ComponentStatusOffline, nil
	case "component_status_disabled":
		return ComponentStatusDisabled, nil
	default:
		return ComponentStatusUnknown, fmt.Errorf("invalid component status: %q", s)
	}
}

// ComponentType represents normalized component types in mining hardware.
//
// This enum provides a standardized way to classify hardware components across
// different device manufacturers and models. It enables generic component handling
// and filtering in the telemetry system.
//
// Note: ComponentType is currently defined but not actively used in the structs.
// It may be added to ComponentInfo in the future for runtime component classification.
type ComponentType int

// ComponentType values
const (
	ComponentTypeUnknown      ComponentType = iota // Unknown type
	ComponentTypeHashBoard                         // ASIC hash boards (primary computing components)
	ComponentTypeFan                               // Cooling fans
	ComponentTypePSU                               // Power supply units
	ComponentTypeControlBoard                      // Control boards (future use)
	ComponentTypeSensor                            // Miscellaneous sensors (future use)
)

func (ct *ComponentType) String() string {
	if ct == nil {
		return componentTypeUnknownStr
	}
	switch *ct {
	case ComponentTypeUnknown:
		return componentTypeUnknownStr
	case ComponentTypeHashBoard:
		return "component_type_hash_board"
	case ComponentTypeFan:
		return "component_type_fan"
	case ComponentTypePSU:
		return "component_type_psu"
	case ComponentTypeControlBoard:
		return "component_type_control_board"
	case ComponentTypeSensor:
		return "component_type_sensor"
	default:
		return componentTypeUnknownStr
	}
}

// MarshalJSON implements json.Marshaler for ComponentType.
func (ct *ComponentType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + ct.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler for ComponentType.
func (ct *ComponentType) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid JSON string for ComponentType")
	}
	s := string(data[1 : len(data)-1])
	parsed, err := ParseComponentType(s)
	if err != nil {
		return err
	}
	*ct = parsed
	return nil
}

// ParseComponentType parses a string into a ComponentType.
// Returns an error if the string is not a valid component type.
func ParseComponentType(s string) (ComponentType, error) {
	switch s {
	case componentTypeUnknownStr:
		return ComponentTypeUnknown, nil
	case "component_type_hash_board":
		return ComponentTypeHashBoard, nil
	case "component_type_fan":
		return ComponentTypeFan, nil
	case "component_type_psu":
		return ComponentTypePSU, nil
	case "component_type_control_board":
		return ComponentTypeControlBoard, nil
	case "component_type_sensor":
		return ComponentTypeSensor, nil
	default:
		return ComponentTypeUnknown, fmt.Errorf("invalid component type: %q", s)
	}
}
