// Package telemetry: broadcaster_metrics.go translates the telemetry
// broadcaster's per-device updates into emissions on the metrics contract
// declared in server/internal/infrastructure/metrics.
package telemetry

import (
	"context"
	"log/slog"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

// MetricsEmitter is the subset of metrics.Provider the telemetry observer depends on.
type MetricsEmitter interface {
	EmitDeviceOnline(ctx context.Context, labels metrics.DeviceLabels, online bool)
	EmitDeviceHashrate(ctx context.Context, labels metrics.DeviceLabels, observedTHs, expectedTHs float64)
	EmitDeviceTemperature(ctx context.Context, labels metrics.DeviceLabels, sensorKind string, maxC, avgC float64)
	EmitDevicePoolConnected(ctx context.Context, labels metrics.DeviceLabels, connected bool)
	EmitTelemetryPoll(ctx context.Context, labels metrics.TelemetryPollLabels)
}

// nopMetricsEmitter is the default emitter installed when notifications are disabled.
type nopMetricsEmitter struct{}

func (nopMetricsEmitter) EmitDeviceOnline(context.Context, metrics.DeviceLabels, bool) {
}
func (nopMetricsEmitter) EmitDeviceHashrate(context.Context, metrics.DeviceLabels, float64, float64) {
}
func (nopMetricsEmitter) EmitDeviceTemperature(context.Context, metrics.DeviceLabels, string, float64, float64) {
}
func (nopMetricsEmitter) EmitDevicePoolConnected(context.Context, metrics.DeviceLabels, bool) {
}
func (nopMetricsEmitter) EmitTelemetryPoll(context.Context, metrics.TelemetryPollLabels) {
}

func NoMetrics() MetricsEmitter { return nopMetricsEmitter{} }

const hertzPerTerahertz = 1e12

type metricsObserver struct {
	emitter MetricsEmitter
}

func newMetricsObserver(emitter MetricsEmitter) *metricsObserver {
	if emitter == nil {
		emitter = NoMetrics()
	}
	return &metricsObserver{emitter: emitter}
}

// onDeviceMetrics is called by the metrics writer pathway every time a device returns a successful telemetry sample.
// The aggregation for sensor kinds happens here, per-board / per-chip detail collapses to _max and _avg.
func (o *metricsObserver) onDeviceMetrics(ctx context.Context, orgID int64, driver string, dm modelsV2.DeviceMetrics) {
	if o == nil {
		return
	}
	labels := metrics.DeviceLabels{
		OrganizationID: metrics.OrgIDToLabel(orgID),
		DeviceID:       dm.DeviceIdentifier,
		Driver:         driver,
	}

	if dm.HashrateHS != nil {
		observedTHs := dm.HashrateHS.Value / hertzPerTerahertz
		expectedTHs := 0.0
		if dm.HashrateHS.MetaData != nil && dm.HashrateHS.MetaData.Max != nil {
			// Some plugins report the nameplate as the Max in the
			// MetaData window. This matches what the existing
			// dashboard surfaces.
			expectedTHs = *dm.HashrateHS.MetaData.Max / hertzPerTerahertz
		}
		o.emitter.EmitDeviceHashrate(ctx, labels, observedTHs, expectedTHs)
	}

	// Temperature aggregation per sensor kind.
	for _, agg := range aggregateTemperatures(dm) {
		o.emitter.EmitDeviceTemperature(ctx, labels, agg.kind, agg.maxC, agg.avgC)
	}

	// Pool connected
	o.emitter.EmitDevicePoolConnected(ctx, labels, isLikelyPoolConnected(dm.Health))
}

// onDeviceStatus is called from the status writer every time the cached device status is updated.
func (o *metricsObserver) onDeviceStatus(ctx context.Context, orgID int64, driver string, deviceID models.DeviceIdentifier, status mm.MinerStatus) {
	if o == nil {
		return
	}
	labels := metrics.DeviceLabels{
		OrganizationID: metrics.OrgIDToLabel(orgID),
		DeviceID:       string(deviceID),
		Driver:         driver,
	}
	o.emitter.EmitDeviceOnline(ctx, labels, isOnlineStatus(status))
}

// onDeviceRemoved is called when a device leaves the fleet.
func (o *metricsObserver) onDeviceRemoved(_ context.Context, _ models.DeviceIdentifier) {
	// no-op: do not emit a final 0 — the contract intentionally lets the series vanish.
}

// onPollResult is called by the telemetry workers for every poll attempt.
func (o *metricsObserver) onPollResult(ctx context.Context, orgID int64, deviceID models.DeviceIdentifier, success bool) {
	if o == nil {
		return
	}
	result := metrics.ResultSuccess
	if !success {
		result = metrics.ResultFailure
	}
	o.emitter.EmitTelemetryPoll(ctx, metrics.TelemetryPollLabels{
		OrganizationID: metrics.OrgIDToLabel(orgID),
		DeviceID:       string(deviceID),
		Result:         result,
	})
}

// isOnlineStatus reports whether a MinerStatus should map to fleet_device_online=1.
func isOnlineStatus(status mm.MinerStatus) bool {
	switch status {
	case mm.MinerStatusOffline, mm.MinerStatusError, mm.MinerStatusUnknown:
		return false

	case mm.MinerStatusActive,
		mm.MinerStatusInactive,
		mm.MinerStatusMaintenance,
		mm.MinerStatusNeedsMiningPool,
		mm.MinerStatusUpdating,
		mm.MinerStatusRebootRequired:
		return true
	}
	return false
}

// isLikelyPoolConnected approximates pool connectivity from the device health status.
func isLikelyPoolConnected(health modelsV2.HealthStatus) bool {
	switch health {
	case modelsV2.HealthHealthyActive, modelsV2.HealthWarning:
		return true
	case modelsV2.HealthUnknown, modelsV2.HealthHealthyInactive, modelsV2.HealthCritical:
		return false
	}
	return false
}

// temperatureAggregate is the per-sensor-kind result of walking a DeviceMetrics value.
type temperatureAggregate struct {
	kind string
	maxC float64
	avgC float64
}

// aggregateTemperatures collapses every sensor reading on the device into a (kind, max, avg) tuple.
func aggregateTemperatures(dm modelsV2.DeviceMetrics) []temperatureAggregate {
	type accum struct {
		count int
		sum   float64
		max   float64
		set   bool
	}
	accs := map[string]*accum{}
	add := func(kind string, c float64) {
		if !metrics.IsKnownSensorKind(kind) {
			return
		}
		a, ok := accs[kind]
		if !ok {
			a = &accum{}
			accs[kind] = a
		}
		a.count++
		a.sum += c
		if !a.set || c > a.max {
			a.max = c
			a.set = true
		}
	}

	// Hashboard-level board + chip + inlet/outlet temperatures.
	for _, hb := range dm.HashBoards {
		if hb.TempC != nil {
			add(metrics.SensorKindBoard, hb.TempC.Value)
		}
		if hb.InletTempC != nil {
			add(metrics.SensorKindInlet, hb.InletTempC.Value)
		}
		if hb.OutletTempC != nil {
			add(metrics.SensorKindOutlet, hb.OutletTempC.Value)
		}
		if hb.AmbientTempC != nil {
			add(metrics.SensorKindAmbient, hb.AmbientTempC.Value)
		}
		for _, asic := range hb.ASICs {
			if asic.TempC != nil {
				add(metrics.SensorKindChip, asic.TempC.Value)
			}
		}
	}

	// PSU hot-spot.
	for _, psu := range dm.PSUMetrics {
		if psu.HotSpotTempC != nil {
			add(metrics.SensorKindHotspot, psu.HotSpotTempC.Value)
		}
	}

	// Fan-mounted ambient sensors.
	for _, fan := range dm.FanMetrics {
		if fan.TempC != nil {
			add(metrics.SensorKindAmbient, fan.TempC.Value)
		}
	}

	// Generic sensors keyed by their declared Type.
	for _, sm := range dm.SensorMetrics {
		if sm.Value == nil {
			continue
		}
		add(sensorKindFromType(sm.Type), sm.Value.Value)
	}

	// If the device only reports an aggregated TempC, use it as the board-kind reading.
	if len(accs) == 0 && dm.TempC != nil {
		add(metrics.SensorKindBoard, dm.TempC.Value)
	}

	out := make([]temperatureAggregate, 0, len(accs))
	for kind, a := range accs {
		if a.count == 0 {
			continue
		}
		out = append(out, temperatureAggregate{
			kind: kind,
			maxC: a.max,
			avgC: a.sum / float64(a.count),
		})
	}
	return out
}

// sensorKindFromType maps a generic sensor's free-form Type field to a contract sensor_kind.
// Returns "" for unknown types and sample is later dropped.
func sensorKindFromType(t string) string {
	switch t {
	case "ambient", "intake":
		return metrics.SensorKindAmbient
	case "inlet":
		return metrics.SensorKindInlet
	case "outlet", "exhaust":
		return metrics.SensorKindOutlet
	case "board":
		return metrics.SensorKindBoard
	case "chip", "asic":
		return metrics.SensorKindChip
	case "hotspot", "hot_spot":
		return metrics.SensorKindHotspot
	default:
		return ""
	}
}

// driverForDevice returns the driver name for the given deviceID by asking the miner manager.
func driverForDevice(ctx context.Context, mg MinerGetter, deviceID models.DeviceIdentifier) string {
	if mg == nil {
		return ""
	}
	miner, err := mg.GetMinerFromDeviceIdentifier(ctx, deviceID)
	if err != nil {
		slog.Debug("metricsObserver: failed to resolve driver for device",
			"device_id", deviceID, "error", err)
		return ""
	}
	return miner.GetDriverName()
}

// orgForDevice returns the org id for the given deviceID by asking the miner manager.
func orgForDevice(ctx context.Context, mg MinerGetter, deviceID models.DeviceIdentifier) int64 {
	if mg == nil {
		return 0
	}
	miner, err := mg.GetMinerFromDeviceIdentifier(ctx, deviceID)
	if err != nil {
		slog.Debug("metricsObserver: failed to resolve org for device",
			"device_id", deviceID, "error", err)
		return 0
	}
	return miner.GetOrgID()
}
