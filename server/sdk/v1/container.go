package sdk

import "time"

// ModuleState describes a whole module, not a count of its faulty boards.
type ModuleState string

const (
	ModuleUnknown   ModuleState = "unknown"
	ModuleHashing   ModuleState = "hashing"
	ModuleAttention ModuleState = "attention"
	ModuleOffline   ModuleState = "offline"
	ModuleSleeping  ModuleState = "sleeping"
	ModuleIsolated  ModuleState = "isolated"
)

// ContainerSnapshot is the latest observed topology and module health. Positions
// are selectors; ReportedID is firmware identity evidence, not a Fleet miner ID.
// Nil snapshots mean collection failed and must never erase a previous snapshot.
type ContainerSnapshot struct {
	Fans     []ContainerFan     `json:"fans,omitempty"`
	Controls *ContainerControls `json:"controls,omitempty"`
	// TopologyComplete permits reconciliation of missing members. Partial or
	// telemetry-only observations never remove inventory.
	TopologyComplete bool            `json:"topology_complete"`
	Plant            *ContainerPlant `json:"plant,omitempty"`
	ObservedAt       time.Time       `json:"observed_at"`
	Tanks            []ContainerTank `json:"tanks"`
}

type ContainerTank struct {
	Position int32             `json:"position"`
	Modules  []ContainerModule `json:"modules"`
}

type ContainerModule struct {
	Metrics    *ContainerModuleMetrics `json:"metrics,omitempty"`
	Position   int32                   `json:"position"`
	ReportedID string                  `json:"reported_id,omitempty"`
	State      ModuleState             `json:"state"`
	// ObservedAt is the source evidence time, not the collection time.
	ObservedAt time.Time `json:"observed_at"`
}

// ContainerModuleMetrics uses SI units. Power is module PSU input power, not
// board power or a share of facility power. Temperature is mean ASIC temperature.
// A nil value means unavailable, never zero. ObservedAt is source evidence time.
type ContainerModuleMetrics struct {
	ObservedAt   time.Time `json:"observed_at"`
	HashrateHS   *float64  `json:"hashrate_hs,omitempty"`
	PowerW       *float64  `json:"power_w,omitempty"`
	TemperatureC *float64  `json:"temperature_c,omitempty"`
}

// ContainerPlant retains the firmware's own plant aggregate, independently
// timestamped from topology. Absent values are unavailable, never inferred zero.
type ContainerPlant struct {
	ObservedAt          time.Time `json:"observed_at"`
	TotalPowerW         *float64  `json:"total_power_w,omitempty"`
	FacilityPowerW      *float64  `json:"facility_power_w,omitempty"`
	PUE                 *float64  `json:"pue,omitempty"`
	InletTemperatureC   *float64  `json:"inlet_temperature_c,omitempty"`
	OutletTemperatureC  *float64  `json:"outlet_temperature_c,omitempty"`
	AlarmActive         bool      `json:"alarm_active"`
	CriticalAlarmActive bool      `json:"critical_alarm_active"`
}

// ContainerFan retains the controller's component identity and source timestamp.
// Absent readings are unknown; speed is percentage, not RPM or PWM.
type ContainerFan struct {
	ComponentID  string    `json:"component_id"`
	ObservedAt   time.Time `json:"observed_at"`
	SpeedPercent *float64  `json:"speed_percent,omitempty"`
	PowerW       *float64  `json:"power_w,omitempty"`
	Direction    *string   `json:"direction,omitempty"`
	AlarmActive  *bool     `json:"alarm_active,omitempty"`
}

// ContainerControls is independently collected. CT1 supplies no source time for
// this endpoint, so ObservedAt is the successful read completion time. Missing
// capabilities are unknown, not unsupported. Nil means no successful read.
type ContainerControls struct {
	ObservedAt                   time.Time           `json:"observed_at"`
	Supported                    *bool               `json:"supported,omitempty"`
	Controls                     []ContainerControl  `json:"controls,omitempty"`
	Setpoints                    []ContainerSetpoint `json:"setpoints,omitempty"`
	MainCircuitBreakerSupported  *bool               `json:"main_circuit_breaker_supported,omitempty"`
	MainCircuitBreakerClosed     *bool               `json:"main_circuit_breaker_closed,omitempty"`
	AlarmResetSupported          *bool               `json:"alarm_reset_supported,omitempty"`
	AlarmMuteSupported           *bool               `json:"alarm_mute_supported,omitempty"`
	DryCoolerFanReverseSupported *bool               `json:"dry_cooler_fan_reverse_supported,omitempty"`
}

type ContainerControl struct {
	Name string `json:"name"`
	// Nil means the controller cannot report an authoritative state.
	Enabled *bool `json:"enabled,omitempty"`
}

type ContainerSetpoint struct {
	Name  string   `json:"name"`
	Value *float64 `json:"value,omitempty"`
	Unit  string   `json:"unit"`
}
