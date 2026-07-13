// The metric contract is intentionally minimal and frozen — every metric name,
// label key, and unit listed here is part of the public surface that users'
// PromQL rules and Grafana dashboards depend on.
package metrics

import (
	"slices"
	"strconv"
)

// Namespace is the prefix every Proto Fleet metric must carry. The PromQL
// subset compiler rejects vector selectors whose metric name does not start
// with this prefix; that prevents user rules from reading anything outside
// our contract surface.
const Namespace = "fleet_"

// Metric names
const (
	// MetricDeviceOnline is a per-device gauge: 1 when the device is reachable
	// and reporting telemetry, 0 when the telemetry pipeline has marked it
	// unreachable. The series stops being emitted when the device is removed
	// from the fleet — default rules guard for that case with
	// absent_over_time(fleet_device_online[10m]).
	MetricDeviceOnline = "fleet_device_online"

	// MetricDeviceHashing is a per-device observed/expected hashrate ratio while the device is expected to be hashing (lower degraded, 0 stopped), and a non-alerting 1.0 once it is no longer expected to (paused, unknown, offline) so a stale low sample can't keep the Device Hashrate Low rule firing; a still-expected device with a missing or invalid reading emits nothing so a gap can't clear a real low. The below-expected threshold lives in that rule.
	MetricDeviceHashing = "fleet_device_hashing"

	// MetricDeviceHashrateTerahash is the device's currently observed hashrate
	// expressed in terahash per second.
	MetricDeviceHashrateTerahash = "fleet_device_hashrate_terahash"

	// MetricDeviceHashrateExpectedTerahash is the device's nameplate / expected
	// hashrate in terahash per second. The Hashrate template compares observed
	// against expected to produce the "below expected by X%" signal.
	MetricDeviceHashrateExpectedTerahash = "fleet_device_hashrate_expected_terahash"

	// MetricDeviceTemperatureMaxCelsius is the maximum temperature reading
	// observed across the device's sensors of a given kind, expressed in
	// degrees Celsius. The sensor_kind label distinguishes board, chip,
	// inlet, outlet, ambient, etc.
	MetricDeviceTemperatureMaxCelsius = "fleet_device_temperature_max_celsius"

	// MetricDeviceTemperatureAvgCelsius is the average temperature across the
	// device's sensors of a given kind. Same labels as the _max counterpart.
	MetricDeviceTemperatureAvgCelsius = "fleet_device_temperature_avg_celsius"

	// MetricDevicePoolConnected is a per-device gauge: 1 when the device is
	// connected to its primary mining pool, 0 otherwise.
	MetricDevicePoolConnected = "fleet_device_pool_connected"

	// MetricCommandTotal is a counter incremented every time a dispatched
	// command reaches a terminal state. Labelled with kind (the command type)
	// and result (success or failure — see ResultSuccess / ResultFailure).
	MetricCommandTotal = "fleet_command_total"

	// MetricTelemetryPollTotal is a counter incremented for every telemetry
	// poll attempt against a device. Labelled with result (success or
	// failure — see ResultSuccess / ResultFailure). Persisted rows are
	// aggregated per (organization, site, result) over the provider's
	// PollAggregationInterval with value = poll count, so consumers must
	// sum(value) over a window, never count rows.
	MetricTelemetryPollTotal = "fleet_telemetry_poll_total"

	// Host system gauges emitted by the optional system-monitoring collector.
	// All four are host-scoped: every label column stays empty (like the
	// ingest-stalled sentinel), and the provisioned Grafana rules fan alerts
	// out per organization by joining fleet_active_organization in SQL.
	// User-authored PromQL gets an injected organization_id="<caller-org>"
	// matcher, so these series match nothing there.

	// MetricSystemCPUUsedPercent is the host CPU utilization (0-100) over the
	// collector's poll interval.
	MetricSystemCPUUsedPercent = "fleet_system_cpu_used_percent"

	// MetricSystemMemoryUsedPercent is the host RAM used percent (0-100).
	MetricSystemMemoryUsedPercent = "fleet_system_memory_used_percent"

	// MetricSystemDiskUsedPercent is the used percent (0-100) of the
	// filesystem at the collector's configured path.
	MetricSystemDiskUsedPercent = "fleet_system_disk_used_percent"

	// MetricSystemHeartbeat is always 1; a fresh sample lands every collector
	// tick, so staleness means fleet-api is down or the metrics writer is
	// wedged. The Fleet Heartbeat Stale rule alerts on it.
	MetricSystemHeartbeat = "fleet_system_heartbeat"

	// MetricMQTTSourceConnected is a per-source gauge: 1 when the MQTT
	// curtailment source has at least one broker connection subscribed to its
	// signal topic, 0 when it cannot receive curtailment signals. Labelled
	// with kind (the source name). A source that is disabled, removed, or
	// renamed gets one final non-alerting sample so rules on the retired
	// series resolve promptly; then emission stops.
	MetricMQTTSourceConnected = "fleet_mqtt_source_connected"

	// MetricMQTTCurtailmentActive is a per-source gauge: 1 while a curtailment
	// event started by that MQTT source's automation is non-terminal (pending,
	// curtailing, or restoring), 0 once miners are fully restored. Labelled
	// with kind (the source name). Still emitted for a source disabled
	// mid-curtailment, so the alert cannot resolve while miners stay down.
	MetricMQTTCurtailmentActive = "fleet_mqtt_curtailment_active"
)

// Label keys. User-authored PromQL may aggregate by any of these.
const (
	// LabelOrganizationID identifies the owning organisation. The PromQL
	// compiler injects organization_id="<caller-org>" into every vector.
	LabelOrganizationID = "organization_id"

	// LabelSiteID identifies the site a device is placed at.
	LabelSiteID = "site_id"

	// LabelDeviceID is the stable device identifier.
	LabelDeviceID = "device_id"

	// LabelDeviceGroup is the collection / group name a device belongs to.
	// Optional; only populated for devices that are members of a group.
	LabelDeviceGroup = "device_group"

	// LabelDriver is the plugin driver name (e.g. proto, antminer, asicrs,
	// virtual). Useful for filtering rules to a single driver family.
	LabelDriver = "driver"

	// LabelSensorKind narrows a temperature reading to a specific sensor
	// family. Allowed values: board, chip, inlet, outlet, ambient, hotspot.
	LabelSensorKind = "sensor_kind"

	// LabelKind is the command type label on fleet_command_total (e.g.
	// reboot, start_mining, stop_mining, set_power_target) and the source
	// name on the fleet_mqtt_* gauges.
	LabelKind = "kind"

	// LabelResult is the success/failure outcome label on counters.
	// Constrained to ResultSuccess or ResultFailure.
	LabelResult = "result"
)

// Result values.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// Sensor kinds.
const (
	SensorKindBoard   = "board"
	SensorKindChip    = "chip"
	SensorKindInlet   = "inlet"
	SensorKindOutlet  = "outlet"
	SensorKindAmbient = "ambient"
	SensorKindHotspot = "hotspot"
)

// AllMetricNames is the canonical list of metric names emitted by Proto Fleet.
var AllMetricNames = []string{
	MetricDeviceOnline,
	MetricDeviceHashing,
	MetricDeviceHashrateTerahash,
	MetricDeviceHashrateExpectedTerahash,
	MetricDeviceTemperatureMaxCelsius,
	MetricDeviceTemperatureAvgCelsius,
	MetricDevicePoolConnected,
	MetricCommandTotal,
	MetricTelemetryPollTotal,
	MetricSystemCPUUsedPercent,
	MetricSystemMemoryUsedPercent,
	MetricSystemDiskUsedPercent,
	MetricSystemHeartbeat,
	MetricMQTTSourceConnected,
	MetricMQTTCurtailmentActive,
}

// AllLabelKeys is the canonical list of label keys Proto Fleet attaches to its metrics.
var AllLabelKeys = []string{
	LabelOrganizationID,
	LabelSiteID,
	LabelDeviceID,
	LabelDeviceGroup,
	LabelDriver,
	LabelSensorKind,
	LabelKind,
	LabelResult,
}

// AllResults is the closed set of values for the result label.
var AllResults = []string{ResultSuccess, ResultFailure}

// AllSensorKinds is the closed set of values for the sensor_kind label.
var AllSensorKinds = []string{
	SensorKindBoard,
	SensorKindChip,
	SensorKindInlet,
	SensorKindOutlet,
	SensorKindAmbient,
	SensorKindHotspot,
}

// IsKnownMetric reports whether name appears in AllMetricNames.
// Used by the rule compiler and by tests.
func IsKnownMetric(name string) bool {
	return slices.Contains(AllMetricNames, name)
}

// IsKnownLabel reports whether key is one of the allowlisted label keys.
func IsKnownLabel(key string) bool {
	return slices.Contains(AllLabelKeys, key)
}

// IsKnownResult reports whether v is a permitted value for the result label.
func IsKnownResult(v string) bool {
	return slices.Contains(AllResults, v)
}

// IsKnownSensorKind reports whether v is a permitted value for the
// sensor_kind label.
func IsKnownSensorKind(v string) bool {
	return slices.Contains(AllSensorKinds, v)
}

func OrgIDToLabel(orgID int64) string {
	if orgID == 0 {
		return ""
	}
	return strconv.FormatInt(orgID, 10)
}

func SiteIDToLabel(siteID int64) string {
	if siteID == 0 {
		return ""
	}
	return strconv.FormatInt(siteID, 10)
}
