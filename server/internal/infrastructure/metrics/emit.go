package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// DeviceLabels is the canonical label set for per-device gauges.
type DeviceLabels struct {
	OrganizationID string
	DeviceID       string
	DeviceGroup    string
	Driver         string
}

type CommandLabels struct {
	OrganizationID string
	Kind           string
	Result         string
}

type TelemetryPollLabels struct {
	OrganizationID string
	DeviceID       string
	Result         string
}

func parseOrgID(value, ctxHint string) (int64, bool) {
	if value == "" {
		return 0, true
	}
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		slog.Error("metrics: non-integer organization_id, dropping sample",
			"value", value, "context", ctxHint, "error", err)
		return 0, false
	}
	return v, true
}

func (p *Provider) EmitDeviceOnline(ctx context.Context, labels DeviceLabels, online bool) {
	if p == nil || !p.enabled {
		return
	}
	orgID, ok := parseOrgID(labels.OrganizationID, labels.DeviceID)
	if !ok {
		return
	}
	v := online
	p.enqueue(sample{
		kind:        sampleKindDevice,
		time:        time.Now().UTC(),
		orgID:       orgID,
		deviceID:    labels.DeviceID,
		deviceGroup: labels.DeviceGroup,
		driver:      labels.Driver,
		online:      &v,
	})
}

func (p *Provider) EmitDeviceHashrate(ctx context.Context, labels DeviceLabels, observedTHs, expectedTHs float64) {
	if p == nil || !p.enabled {
		return
	}
	orgID, ok := parseOrgID(labels.OrganizationID, labels.DeviceID)
	if !ok {
		return
	}
	obs := observedTHs
	exp := expectedTHs
	p.enqueue(sample{
		kind:                sampleKindDevice,
		time:                time.Now().UTC(),
		orgID:               orgID,
		deviceID:            labels.DeviceID,
		deviceGroup:         labels.DeviceGroup,
		driver:              labels.Driver,
		hashrateTHs:         &obs,
		hashrateExpectedTHs: &exp,
	})
}

// EmitDeviceTemperature records the per-sensor-kind temperature row.
func (p *Provider) EmitDeviceTemperature(ctx context.Context, labels DeviceLabels, sensorKind string, maxC, avgC float64) {
	if p == nil || !p.enabled {
		return
	}
	if !IsKnownSensorKind(sensorKind) {
		slog.Error("metrics: unknown sensor_kind, dropping temperature emit", "sensor_kind", sensorKind)
		return
	}
	orgID, ok := parseOrgID(labels.OrganizationID, labels.DeviceID)
	if !ok {
		return
	}
	p.enqueue(sample{
		kind:            sampleKindTemperature,
		time:            time.Now().UTC(),
		orgID:           orgID,
		deviceID:        labels.DeviceID,
		deviceGroup:     labels.DeviceGroup,
		driver:          labels.Driver,
		sensorKind:      sensorKind,
		temperatureMaxC: maxC,
		temperatureAvgC: avgC,
		temperatureHasV: true,
	})
}

// EmitDevicePoolConnected records the fleet_device_pool_connected gauge.
func (p *Provider) EmitDevicePoolConnected(ctx context.Context, labels DeviceLabels, connected bool) {
	if p == nil || !p.enabled {
		return
	}
	orgID, ok := parseOrgID(labels.OrganizationID, labels.DeviceID)
	if !ok {
		return
	}
	v := connected
	p.enqueue(sample{
		kind:          sampleKindDevice,
		time:          time.Now().UTC(),
		orgID:         orgID,
		deviceID:      labels.DeviceID,
		deviceGroup:   labels.DeviceGroup,
		driver:        labels.Driver,
		poolConnected: &v,
	})
}

// EmitCommand increments fleet_command_total.
func (p *Provider) EmitCommand(ctx context.Context, labels CommandLabels) {
	if p == nil || !p.enabled {
		return
	}
	if !IsKnownResult(labels.Result) {
		slog.Error("metrics: unknown command result, dropping increment",
			"result", labels.Result, "kind", labels.Kind)
		return
	}
	orgID, ok := parseOrgID(labels.OrganizationID, labels.Kind)
	if !ok {
		return
	}
	p.enqueue(sample{
		kind:        sampleKindCommand,
		time:        time.Now().UTC(),
		orgID:       orgID,
		commandKind: labels.Kind,
		result:      labels.Result,
	})
}

// EmitTelemetryPoll increments fleet_telemetry_poll_total.
func (p *Provider) EmitTelemetryPoll(ctx context.Context, labels TelemetryPollLabels) {
	if p == nil || !p.enabled {
		return
	}
	if !IsKnownResult(labels.Result) {
		slog.Error("metrics: unknown telemetry poll result, dropping increment",
			"result", labels.Result)
		return
	}
	orgID, ok := parseOrgID(labels.OrganizationID, labels.DeviceID)
	if !ok {
		return
	}
	p.enqueue(sample{
		kind:     sampleKindTelemetryPoll,
		time:     time.Now().UTC(),
		orgID:    orgID,
		deviceID: labels.DeviceID,
		result:   labels.Result,
	})
}

func validateLabelKey(key string) error {
	if !IsKnownLabel(key) {
		return fmt.Errorf("metrics: label key %q is not in the contract allowlist", key)
	}
	return nil
}
