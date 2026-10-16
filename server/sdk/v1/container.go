package sdk

import (
	"encoding/json"
	"time"
)

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
	// UnlocatedHashboards retains inventory observations without physical placement.
	// Position must be zero; readings contribute only to container totals after
	// identity reconciliation. These observations never create installations.
	UnlocatedHashboards []ContainerHashboard `json:"unlocated_hashboards,omitempty"`
	// InventoryAccepted is set only by server reconciliation, never by a plugin.
	InventoryAccepted bool               `json:"-"`
	Metrics           *ContainerRollup   `json:"metrics,omitempty"`
	Fans              []ContainerFan     `json:"fans,omitempty"`
	Controls          *ContainerControls `json:"controls,omitempty"`
	// TopologyComplete permits reconciliation of missing members. Partial or
	// telemetry-only observations never remove inventory.
	TopologyComplete bool            `json:"topology_complete"`
	Plant            *ContainerPlant `json:"plant,omitempty"`
	ObservedAt       time.Time       `json:"observed_at"`
	Tanks            []ContainerTank `json:"tanks"`
}

type ContainerTank struct {
	Metrics          *ContainerRollup  `json:"metrics,omitempty"`
	TopologyComplete bool              `json:"topology_complete"`
	Position         int32             `json:"position"`
	Modules          []ContainerModule `json:"modules"`
}

// ContainerModuleIssues is a current observation, not an issue history. Complete
// means all boards and the shared PSU supplied fresh health. Nil/partial evidence cannot prove health.
// ObservedAt is the oldest required source timestamp and controls freshness.
type ContainerModuleIssues struct {
	// SourceTimes orders independent board/PSU samples, including clear readings.
	SourceTimes map[string]time.Time   `json:"source_times,omitempty"`
	ObservedAt  time.Time              `json:"observed_at"`
	Complete    bool                   `json:"complete"`
	Items       []ContainerModuleIssue `json:"items"`
}
type ContainerModuleIssue struct {
	Component   string    `json:"component"`    // hashboard, psu, or module
	ComponentID string    `json:"component_id"` // board MAC, or module-owned psu
	Cause       string    `json:"cause"`
	Severity    string    `json:"severity"` // fault, warning, or observation
	ObservedAt  time.Time `json:"observed_at"`
}

type ContainerModule struct {
	Issues     *ContainerModuleIssues  `json:"issues,omitempty"`
	Hashboards []ContainerHashboard    `json:"hashboards,omitempty"`
	Metrics    *ContainerModuleMetrics `json:"metrics,omitempty"`
	Position   int32                   `json:"position"`
	ReportedID string                  `json:"reported_id,omitempty"`
	// IdentityConfirmed permits controls; partial identity evidence may still
	// identify measurements without confirming every board in the module.
	IdentityConfirmed bool        `json:"identity_confirmed"`
	State             ModuleState `json:"state"`
	// ObservedAt is the source evidence time, not the collection time.
	ObservedAt time.Time `json:"observed_at"`
}

// ContainerHashboard is an observation of a slot, not a module-owned hardware
// identity. Fleet resolves MAC/serial to durable boards and tracks installations.
// Empty and unknown slots have no identity. Missing observations never remove
// inventory. MetadataJSON and HealthJSON preserve the MDK v2 fields and units;
// the typed measurements use SI units and exclude stale or invalid evidence.
// PSU fields inside the firmware payload belong to the shared module PSU.
type ContainerHashboard struct {
	PowerW            *float64                 `json:"power_w,omitempty"`
	EfficiencyJH      *float64                 `json:"efficiency_jh,omitempty"`
	Feed              *ContainerFeedEvidence   `json:"feed,omitempty"`
	IdentityClaims    []HashboardIdentityClaim `json:"identity_claims,omitempty"`
	UnavailableReason string                   `json:"unavailable_reason,omitempty"`
	Position          int32                    `json:"position"`
	Occupancy         string                   `json:"occupancy"`
	MACAddress        string                   `json:"mac_address,omitempty"`
	Serial            string                   `json:"serial,omitempty"`
	State             ModuleState              `json:"state"`
	MiningState       string                   `json:"mining_state,omitempty"`
	Isolated          *bool                    `json:"isolated,omitempty"`
	ObservedAt        time.Time                `json:"observed_at"`
	StateObservedAt   time.Time                `json:"state_observed_at"`
	HashrateHS        *float64                 `json:"hashrate_hs,omitempty"`
	TemperatureC      *float64                 `json:"temperature_c,omitempty"`
	HashrailOK        *bool                    `json:"hashrail_ok,omitempty"`
	RefclksOK         *bool                    `json:"refclks_ok,omitempty"`
	MetadataJSON      json.RawMessage          `json:"metadata,omitempty"`
	HealthJSON        json.RawMessage          `json:"health,omitempty"`
}

// ContainerModuleMetrics uses SI units. Power is module PSU input power, not
// board power or a share of facility power. Temperature is mean ASIC temperature.
// A nil value means unavailable, never zero. ObservedAt is source evidence time.
type ContainerModuleMetrics struct {
	Rollup       *ContainerRollup `json:"rollup,omitempty"`
	EfficiencyJH *float64         `json:"efficiency_jh,omitempty"`
	ObservedAt   time.Time        `json:"observed_at"`
	HashrateHS   *float64         `json:"hashrate_hs,omitempty"`
	PowerW       *float64         `json:"power_w,omitempty"`
	TemperatureC *float64         `json:"temperature_c,omitempty"`
}

// ContainerPlant retains the firmware's own plant aggregate, independently
// timestamped from topology. Absent values are unavailable, never inferred zero.
type ContainerPlant struct {
	DryCoolerInletTemperatureC *float64  `json:"dry_cooler_inlet_temperature_c,omitempty"`
	ObservedAt                 time.Time `json:"observed_at"`
	TotalPowerW                *float64  `json:"total_power_w,omitempty"`
	FacilityPowerW             *float64  `json:"facility_power_w,omitempty"`
	PUE                        *float64  `json:"pue,omitempty"`
	InletTemperatureC          *float64  `json:"inlet_temperature_c,omitempty"`
	OutletTemperatureC         *float64  `json:"outlet_temperature_c,omitempty"`
	AlarmActive                bool      `json:"alarm_active"`
	CriticalAlarmActive        bool      `json:"critical_alarm_active"`
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

// Hashboard unavailability codes describe rejected evidence, not hardware faults.
const (
	HashboardInvalidObservation = "invalid_observation"
	HashboardAmbiguousSlot      = "ambiguous_slot"
	HashboardMissingIdentity    = "missing_identity"
	HashboardIdentityConflict   = "identity_conflict"
	HashboardControllerConflict = "controller_conflict"
	HashboardOlderObservation   = "older_observation"
)

// UnavailableHashboard clears untrusted identity and readings without claiming removal.
func UnavailableHashboard(position int32, reason string) ContainerHashboard {
	return ContainerHashboard{Position: position, Occupancy: "unknown", State: ModuleUnknown, UnavailableReason: reason}
}
