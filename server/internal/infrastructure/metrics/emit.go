package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"math"
)

// DeviceLabels is the canonical label set for per-device gauges.
type DeviceLabels struct {
	OrganizationID string
	SiteID         string
	DeviceID       string
	DeviceGroup    string
	Driver         string
}

type CommandLabels struct {
	OrganizationID string
	SiteID         string
	Kind           string
	Result         string
}

// TelemetryPollLabels has deliberately no DeviceID: poll rows aggregate per
// (organization, site, result), so a per-device label would be discarded.
type TelemetryPollLabels struct {
	OrganizationID string
	SiteID         string
	Result         string
}

// MQTTSourceLabels identifies one MQTT curtailment source; SourceName is
// carried on the kind label.
type MQTTSourceLabels struct {
	OrganizationID string
	SourceName     string
}

func (l DeviceLabels) toLabels() Labels {
	return Labels{
		OrganizationID: l.OrganizationID,
		SiteID:         l.SiteID,
		DeviceID:       l.DeviceID,
		DeviceGroup:    l.DeviceGroup,
		Driver:         l.Driver,
	}
}

// Device gauges persist through the gauge throttle (see gaugeThrottle);
// MQTT, system, and command emitters below stay unthrottled on purpose.

func (p *Provider) EmitDeviceOnline(_ context.Context, labels DeviceLabels, online bool) {
	value := 0.0
	if online {
		value = 1.0
	}
	p.recordStateGauge(Sample{
		Metric: MetricDeviceOnline,
		Labels: labels.toLabels(),
		Value:  value,
	})
}

// hashingChangeTolerance persists material ratio moves (onset, recovery, the
// 1.0 clearing sentinel) immediately; ±few-% poll jitter heartbeats instead.
const hashingChangeTolerance = 0.05

func (p *Provider) EmitDeviceHashing(_ context.Context, labels DeviceLabels, ratio float64) {
	p.recordDeviceGauge(Sample{
		Metric: MetricDeviceHashing,
		Labels: labels.toLabels(),
		Value:  ratio,
	}, hashingChangeTolerance)
}

func (p *Provider) EmitDeviceHashrate(_ context.Context, labels DeviceLabels, observedTHs, expectedTHs float64) {
	base := labels.toLabels()
	p.recordContinuousGauge(Sample{
		Metric: MetricDeviceHashrateTerahash,
		Labels: base,
		Value:  observedTHs,
	})
	p.recordContinuousGauge(Sample{
		Metric: MetricDeviceHashrateExpectedTerahash,
		Labels: base,
		Value:  expectedTHs,
	})
}

// temperatureChangeTolerance persists regime changes (heat-up, cool-down)
// immediately; steady-state ±1-3C poll jitter heartbeats instead.
const temperatureChangeTolerance = 5.0

// EmitDeviceTemperature records max+avg gauges for one sensor kind.
func (p *Provider) EmitDeviceTemperature(_ context.Context, labels DeviceLabels, sensorKind string, maxC, avgC float64) {
	if !IsKnownSensorKind(sensorKind) {
		slog.Error("metrics: unknown sensor_kind, dropping temperature emit", "sensor_kind", sensorKind)
		return
	}
	base := labels.toLabels()
	base.SensorKind = sensorKind
	p.recordDeviceGauge(Sample{
		Metric: MetricDeviceTemperatureMaxCelsius,
		Labels: base,
		Value:  maxC,
	}, temperatureChangeTolerance)
	p.recordDeviceGauge(Sample{
		Metric: MetricDeviceTemperatureAvgCelsius,
		Labels: base,
		Value:  avgC,
	}, temperatureChangeTolerance)
}

// EmitDevicePoolConnected records the fleet_device_pool_connected gauge.
func (p *Provider) EmitDevicePoolConnected(_ context.Context, labels DeviceLabels, connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	p.recordStateGauge(Sample{
		Metric: MetricDevicePoolConnected,
		Labels: labels.toLabels(),
		Value:  value,
	})
}

// EmitCommand records a single increment on fleet_command_total. The
// Grafana rules sum() these rows over a window to derive the per-org
// failure rate.
func (p *Provider) EmitCommand(_ context.Context, labels CommandLabels) {
	if !IsKnownResult(labels.Result) {
		slog.Error("metrics: unknown command result, dropping increment",
			"result", labels.Result, "kind", labels.Kind)
		return
	}
	p.record(Sample{
		Metric: MetricCommandTotal,
		Labels: Labels{
			OrganizationID: labels.OrganizationID,
			SiteID:         labels.SiteID,
			Kind:           labels.Kind,
			Result:         labels.Result,
		},
		Value: 1,
	})
}

// EmitTelemetryPoll records one increment on fleet_telemetry_poll_total,
// persisted aggregated once per PollAggregationInterval; see pollAggregator.
func (p *Provider) EmitTelemetryPoll(_ context.Context, labels TelemetryPollLabels) {
	if p == nil || !p.enabled {
		return
	}
	if !IsKnownResult(labels.Result) {
		slog.Error("metrics: unknown telemetry poll result, dropping increment",
			"result", labels.Result)
		return
	}
	p.pollAgg.add(Labels{
		OrganizationID: labels.OrganizationID,
		SiteID:         labels.SiteID,
		Result:         labels.Result,
	})
}

// EmitSystemCPUUsedPercent records the fleet_system_cpu_used_percent gauge.
func (p *Provider) EmitSystemCPUUsedPercent(_ context.Context, percent float64) {
	p.emitSystemPercent(MetricSystemCPUUsedPercent, percent)
}

// EmitSystemMemoryUsedPercent records the fleet_system_memory_used_percent gauge.
func (p *Provider) EmitSystemMemoryUsedPercent(_ context.Context, percent float64) {
	p.emitSystemPercent(MetricSystemMemoryUsedPercent, percent)
}

// EmitSystemDiskUsedPercent records the fleet_system_disk_used_percent gauge
// for the collector's configured filesystem path.
func (p *Provider) EmitSystemDiskUsedPercent(_ context.Context, percent float64) {
	p.emitSystemPercent(MetricSystemDiskUsedPercent, percent)
}

// EmitSystemHeartbeat records the fleet_system_heartbeat gauge; the Fleet
// Heartbeat Stale rule alerts when these samples stop landing.
func (p *Provider) EmitSystemHeartbeat(_ context.Context) {
	p.record(Sample{
		Metric: MetricSystemHeartbeat,
		Value:  1,
	})
}

// emitSystemPercent stays unthrottled: system gauges are host-scoped (one
// series each) and the heartbeat's staleness IS the alert signal.
func (p *Provider) emitSystemPercent(metric string, percent float64) {
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 {
		slog.Error("metrics: non-finite or negative percent, dropping emit",
			"metric", metric, "value", percent)
		return
	}
	p.record(Sample{
		Metric: metric,
		Value:  min(percent, 100),
	})
}

// EmitMQTTSourceConnected records the fleet_mqtt_source_connected gauge,
// unthrottled: curtailment rules evaluate every 10s and cardinality is tiny.
func (p *Provider) EmitMQTTSourceConnected(_ context.Context, labels MQTTSourceLabels, connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	p.record(Sample{
		Metric: MetricMQTTSourceConnected,
		Labels: Labels{
			OrganizationID: labels.OrganizationID,
			Kind:           labels.SourceName,
		},
		Value: value,
	})
}

// EmitMQTTCurtailmentActive records the fleet_mqtt_curtailment_active gauge.
// Deliberately unthrottled, like EmitMQTTSourceConnected.
func (p *Provider) EmitMQTTCurtailmentActive(_ context.Context, labels MQTTSourceLabels, active bool) {
	value := 0.0
	if active {
		value = 1.0
	}
	p.record(Sample{
		Metric: MetricMQTTCurtailmentActive,
		Labels: Labels{
			OrganizationID: labels.OrganizationID,
			Kind:           labels.SourceName,
		},
		Value: value,
	})
}

func validateLabelKey(key string) error {
	if !IsKnownLabel(key) {
		return fmt.Errorf("metrics: label key %q is not in the contract allowlist", key)
	}
	return nil
}
