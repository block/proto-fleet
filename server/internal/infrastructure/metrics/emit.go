package metrics

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

func (l DeviceLabels) toAttrs(extra ...attribute.KeyValue) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 4+len(extra))
	if l.OrganizationID != "" {
		attrs = append(attrs, attribute.String(LabelOrganizationID, l.OrganizationID))
	}
	if l.DeviceID != "" {
		attrs = append(attrs, attribute.String(LabelDeviceID, l.DeviceID))
	}
	if l.DeviceGroup != "" {
		attrs = append(attrs, attribute.String(LabelDeviceGroup, l.DeviceGroup))
	}
	if l.Driver != "" {
		attrs = append(attrs, attribute.String(LabelDriver, l.Driver))
	}
	attrs = append(attrs, extra...)
	return attrs
}

func (p *Provider) EmitDeviceOnline(ctx context.Context, labels DeviceLabels, online bool) {
	if p == nil || p.insts == nil {
		return
	}
	val := int64(0)
	if online {
		val = 1
	}
	p.insts.deviceOnline.Record(ctx, val, metric.WithAttributes(labels.toAttrs()...))
}

func (p *Provider) EmitDeviceHashrate(ctx context.Context, labels DeviceLabels, observedTHs, expectedTHs float64) {
	if p == nil || p.insts == nil {
		return
	}
	attrs := metric.WithAttributes(labels.toAttrs()...)
	p.insts.deviceHashrateTerahash.Record(ctx, observedTHs, attrs)
	if expectedTHs > 0 {
		p.insts.deviceHashrateExpectedTerahz.Record(ctx, expectedTHs, attrs)
	}
}

// EmitDeviceTemperature records max+avg gauges for one sensor kind.
func (p *Provider) EmitDeviceTemperature(ctx context.Context, labels DeviceLabels, sensorKind string, maxC, avgC float64) {
	if p == nil || p.insts == nil {
		return
	}
	if !IsKnownSensorKind(sensorKind) {
		slog.Error("metrics: unknown sensor_kind, dropping temperature emit", "sensor_kind", sensorKind)
		return
	}
	attrs := metric.WithAttributes(labels.toAttrs(attribute.String(LabelSensorKind, sensorKind))...)
	p.insts.deviceTemperatureMax.Record(ctx, maxC, attrs)
	p.insts.deviceTemperatureAvg.Record(ctx, avgC, attrs)
}

// EmitDevicePoolConnected records the fleet_device_pool_connected gauge.
func (p *Provider) EmitDevicePoolConnected(ctx context.Context, labels DeviceLabels, connected bool) {
	if p == nil || p.insts == nil {
		return
	}
	val := int64(0)
	if connected {
		val = 1
	}
	p.insts.devicePoolConnected.Record(ctx, val, metric.WithAttributes(labels.toAttrs()...))
}

// EmitCommand increments fleet_command_total.
func (p *Provider) EmitCommand(ctx context.Context, labels CommandLabels) {
	if p == nil || p.insts == nil {
		return
	}
	if !IsKnownResult(labels.Result) {
		slog.Error("metrics: unknown command result, dropping increment",
			"result", labels.Result, "kind", labels.Kind)
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String(LabelKind, labels.Kind),
		attribute.String(LabelResult, labels.Result),
	}
	if labels.OrganizationID != "" {
		attrs = append(attrs, attribute.String(LabelOrganizationID, labels.OrganizationID))
	}
	p.insts.commandTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// EmitTelemetryPoll increments fleet_telemetry_poll_total.
func (p *Provider) EmitTelemetryPoll(ctx context.Context, labels TelemetryPollLabels) {
	if p == nil || p.insts == nil {
		return
	}
	if !IsKnownResult(labels.Result) {
		slog.Error("metrics: unknown telemetry poll result, dropping increment",
			"result", labels.Result)
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String(LabelResult, labels.Result),
	}
	if labels.OrganizationID != "" {
		attrs = append(attrs, attribute.String(LabelOrganizationID, labels.OrganizationID))
	}
	if labels.DeviceID != "" {
		attrs = append(attrs, attribute.String(LabelDeviceID, labels.DeviceID))
	}
	p.insts.telemetryPollTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func validateLabelKey(key string) error {
	if !IsKnownLabel(key) {
		return fmt.Errorf("metrics: label key %q is not in the contract allowlist", key)
	}
	return nil
}
