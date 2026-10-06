// Package telemetry: broadcaster_metrics.go translates telemetry writers'
// per-device updates into emissions on the metrics contract
// declared in server/internal/infrastructure/metrics.
package telemetry

import (
	"context"
	"log/slog"
	"math"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

// MetricsEmitter is the subset of metrics.Provider the telemetry observer depends on.
type MetricsEmitter interface {
	EmitDeviceOnline(ctx context.Context, labels metrics.DeviceLabels, online bool)
	EmitDeviceHashing(ctx context.Context, labels metrics.DeviceLabels, ratio float64)
	EmitDeviceHashrate(ctx context.Context, labels metrics.DeviceLabels, observedTHs, expectedTHs float64)
	EmitDeviceTemperature(ctx context.Context, labels metrics.DeviceLabels, sensorKind string, maxC, avgC float64)
	EmitDevicePoolConnected(ctx context.Context, labels metrics.DeviceLabels, connected bool)
	EmitTelemetryPoll(ctx context.Context, labels metrics.TelemetryPollLabels)
}

// nopMetricsEmitter is the default emitter installed when alerts are disabled.
type nopMetricsEmitter struct{}

func (nopMetricsEmitter) EmitDeviceOnline(context.Context, metrics.DeviceLabels, bool) {
}
func (nopMetricsEmitter) EmitDeviceHashing(context.Context, metrics.DeviceLabels, float64) {
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

// Defensive caps for plugin-supplied component arrays. Real devices have at
// most a handful of each component kind. Anything well above realistic limits
// is most likely a buggy or hostile plugin: we truncate before aggregation
// rather than letting a single device tie up the metrics writer.
const (
	maxHashBoardsPerDevice = 64
	maxASICsPerHashBoard   = 256
	maxPSUsPerDevice       = 16
	maxFansPerDevice       = 64
	maxSensorsPerDevice    = 256
)

// Plausibility window for temperature samples, in degrees Celsius. Readings
// outside this range are almost certainly a buggy plugin or a stuck/faulted
// sensor and are dropped rather than poisoning the aggregate (and any alerts
// derived from it).
const (
	minPlausibleTempC = -50.0
	maxPlausibleTempC = 200.0
)

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
func (o *metricsObserver) onDeviceMetrics(ctx context.Context, orgID, siteID int64, driver string, deviceID models.DeviceIdentifier, dm modelsV2.DeviceMetrics) {
	if o == nil {
		return
	}
	if dm.DeviceIdentifier != "" && dm.DeviceIdentifier != string(deviceID) {
		slog.Warn("metricsObserver: dropping telemetry sample with mismatched device identifier",
			"requested_device_id", deviceID,
			"reported_device_id", dm.DeviceIdentifier,
			"driver", driver,
		)
		return
	}
	labels := metrics.DeviceLabels{
		OrganizationID: metrics.OrgIDToLabel(orgID),
		SiteID:         metrics.SiteIDToLabel(siteID),
		DeviceID:       string(deviceID),
		Driver:         driver,
	}

	var ratio float64
	hasReading := false
	if dm.HashrateHS != nil {
		observedHS := dm.HashrateHS.Value
		var nameplateHS *float64
		if dm.HashrateHS.MetaData != nil && dm.HashrateHS.MetaData.Max != nil {
			// Some plugins report the nameplate as the Max in the
			// MetaData window. This matches what the existing
			// dashboard surfaces.
			nameplateHS = dm.HashrateHS.MetaData.Max
		}
		if observed, expected, ok := sanitizeHashrate(observedHS, nameplateHS, string(deviceID), driver); ok {
			o.emitter.EmitDeviceHashrate(ctx, labels, observed, expected)
			ratio, hasReading = hashingRatio(observed, expected), true
		}
	}
	// fleet_device_hashing: the ratio while a device expected to hash has a valid reading, a non-alerting 1.0 once it's no longer expected, and nothing for a still-expected device with a missing/invalid reading so a telemetry gap or buggy plugin can't clear a real low.
	switch {
	case !dm.Health.ExpectsHashing():
		o.emitter.EmitDeviceHashing(ctx, labels, 1)
	case hasReading:
		o.emitter.EmitDeviceHashing(ctx, labels, ratio)
	}

	// Temperature aggregation per sensor kind. The aggregator caps how many
	// plugin-supplied components it walks and drops non-finite / implausible
	// readings so a single device cannot poison the gauges.
	for _, agg := range aggregateTemperatures(dm, string(deviceID), driver) {
		o.emitter.EmitDeviceTemperature(ctx, labels, agg.kind, agg.maxC, agg.avgC)
	}

	// fleet_device_pool_connected is intentionally not emitted here.
	//
	// Until plugins surface an explicit "connected to configured pool" signal
	// — e.g. via a dedicated field on DeviceMetrics or a comparison of
	// GetMiningPools against the configured pool URL / worker — there is no
	// reliable way to derive pool connectivity from the data on dm. Sourcing
	// it from overall device health, as an earlier version of this file did,
	// produced false positives for intentionally inactive devices and missed
	// real pool disconnects / hijacks when the rest of the device was
	// healthy. The metric remains in the contract so user-authored rules and
	// dashboards keep compiling; the default DevicePoolDisconnected alert
	// has been removed from proto-fleet-defaults.yml until a correct emission
	// path lands.
}

// onDeviceStatus is called from the status writer every time the cached device status is updated.
func (o *metricsObserver) onDeviceStatus(ctx context.Context, orgID, siteID int64, driver string, deviceID models.DeviceIdentifier, status mm.MinerStatus) {
	if o == nil {
		return
	}
	labels := metrics.DeviceLabels{
		OrganizationID: metrics.OrgIDToLabel(orgID),
		SiteID:         metrics.SiteIDToLabel(siteID),
		DeviceID:       string(deviceID),
		Driver:         driver,
	}
	o.emitter.EmitDeviceOnline(ctx, labels, isOnlineStatus(status))
	if status == mm.MinerStatusOffline {
		// Clear a stale low sample only when truly offline; an Error/Critical device still reports telemetry and must keep alerting on low hashrate.
		o.emitter.EmitDeviceHashing(ctx, labels, 1)
	}
}

// onDeviceRemoved is called when a device leaves the fleet.
func (o *metricsObserver) onDeviceRemoved(_ context.Context, _ models.DeviceIdentifier) {
	// no-op: do not emit a final 0 — the contract intentionally lets the series vanish.
}

// onPollResult is called for every poll attempt; counts persist aggregated
// per (org, site, result), so no device identity is forwarded.
func (o *metricsObserver) onPollResult(ctx context.Context, orgID, siteID int64, _ models.DeviceIdentifier, success bool) {
	if o == nil {
		return
	}
	result := metrics.ResultSuccess
	if !success {
		result = metrics.ResultFailure
	}
	o.emitter.EmitTelemetryPoll(ctx, metrics.TelemetryPollLabels{
		OrganizationID: metrics.OrgIDToLabel(orgID),
		SiteID:         metrics.SiteIDToLabel(siteID),
		Result:         result,
	})
}

// hashingRatio is observed over expected (nameplate) hashrate; with no nameplate it collapses to 1.0 (hashing) / 0.0 (stopped) so the rule's threshold still catches a full stop.
func hashingRatio(observedTHs, expectedTHs float64) float64 {
	if expectedTHs > 0 {
		return observedTHs / expectedTHs
	}
	if observedTHs > 0 {
		return 1
	}
	return 0
}

// isOnlineStatus maps a MinerStatus to fleet_device_online=1; only a truly-unreachable device (MinerStatusOffline) is offline, so error/unknown devices stay online.
func isOnlineStatus(status mm.MinerStatus) bool {
	return status != mm.MinerStatusOffline
}

// temperatureAggregate is the per-sensor-kind result of walking a DeviceMetrics value.
type temperatureAggregate struct {
	kind string
	maxC float64
	avgC float64
}

// aggStats records bounded summaries of samples / components dropped by a
// single aggregation pass. It's only used to emit one warn log per pass so a
// runaway plugin can't drown the logger.
type aggStats struct {
	truncatedHashBoards int
	truncatedASICs      int
	truncatedPSUs       int
	truncatedFans       int
	truncatedSensors    int
	droppedNonFinite    int
	droppedOutOfRange   int
	droppedUnknownKind  int
}

func (s aggStats) any() bool {
	return s.truncatedHashBoards+s.truncatedASICs+s.truncatedPSUs+s.truncatedFans+s.truncatedSensors+
		s.droppedNonFinite+s.droppedOutOfRange+s.droppedUnknownKind > 0
}

func capLen(have, maxLen int) (n, truncated int) {
	if have <= maxLen {
		return have, 0
	}
	return maxLen, have - maxLen
}

// aggregateTemperatures collapses every sensor reading on the device into a
// (kind, max, avg) tuple. It is hardened against buggy or hostile plugins:
// component arrays are walked up to a fixed cap, and non-finite or
// out-of-range readings are dropped rather than poisoning the aggregate.
func aggregateTemperatures(dm modelsV2.DeviceMetrics, deviceID, driver string) []temperatureAggregate {
	type accum struct {
		count int
		sum   float64
		max   float64
		set   bool
	}
	accs := map[string]*accum{}
	stats := aggStats{}
	add := func(kind string, c float64) {
		if !metrics.IsKnownSensorKind(kind) {
			// kind == "" is the dominant case for the generic sensors path
			// (sensorKindFromType returned ""). Don't count those as drops:
			// they're filtered, not implausible.
			if kind != "" {
				stats.droppedUnknownKind++
			}
			return
		}
		if math.IsNaN(c) || math.IsInf(c, 0) {
			stats.droppedNonFinite++
			return
		}
		if c < minPlausibleTempC || c > maxPlausibleTempC {
			stats.droppedOutOfRange++
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
	hbN, hbTrunc := capLen(len(dm.HashBoards), maxHashBoardsPerDevice)
	stats.truncatedHashBoards = hbTrunc
	for i := range hbN {
		hb := dm.HashBoards[i]
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
		asicN, asicTrunc := capLen(len(hb.ASICs), maxASICsPerHashBoard)
		stats.truncatedASICs += asicTrunc
		for j := range asicN {
			asic := hb.ASICs[j]
			if asic.TempC != nil {
				add(metrics.SensorKindChip, asic.TempC.Value)
			}
		}
	}

	// PSU hot-spot.
	psuN, psuTrunc := capLen(len(dm.PSUMetrics), maxPSUsPerDevice)
	stats.truncatedPSUs = psuTrunc
	for i := range psuN {
		psu := dm.PSUMetrics[i]
		if psu.HotSpotTempC != nil {
			add(metrics.SensorKindHotspot, psu.HotSpotTempC.Value)
		}
	}

	// Fan-mounted ambient sensors.
	fanN, fanTrunc := capLen(len(dm.FanMetrics), maxFansPerDevice)
	stats.truncatedFans = fanTrunc
	for i := range fanN {
		fan := dm.FanMetrics[i]
		if fan.TempC != nil {
			add(metrics.SensorKindAmbient, fan.TempC.Value)
		}
	}

	// Generic sensors keyed by their declared Type.
	sensorN, sensorTrunc := capLen(len(dm.SensorMetrics), maxSensorsPerDevice)
	stats.truncatedSensors = sensorTrunc
	for i := range sensorN {
		sm := dm.SensorMetrics[i]
		if sm.Value == nil {
			continue
		}
		add(sensorKindFromType(sm.Type), sm.Value.Value)
	}

	// If the device only reports an aggregated TempC, use it as the board-kind reading.
	if len(accs) == 0 && dm.TempC != nil {
		add(metrics.SensorKindBoard, dm.TempC.Value)
	}

	if stats.any() {
		// One bounded summary line per aggregation pass. Drop counts are
		// integers; we deliberately do not log per-sample detail so a buggy
		// plugin can't amplify itself through the logger.
		slog.Warn("metricsObserver: dropped plugin-supplied telemetry samples",
			"device_id", deviceID,
			"driver", driver,
			"truncated_hash_boards", stats.truncatedHashBoards,
			"truncated_asics", stats.truncatedASICs,
			"truncated_psus", stats.truncatedPSUs,
			"truncated_fans", stats.truncatedFans,
			"truncated_sensors", stats.truncatedSensors,
			"dropped_non_finite", stats.droppedNonFinite,
			"dropped_out_of_range", stats.droppedOutOfRange,
			"dropped_unknown_kind", stats.droppedUnknownKind,
		)
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

// sanitizeHashrate validates plugin-supplied hashrate values before they reach
// the metrics emitter. NaN / Inf and negative values are dropped (with a
// single bounded log line) so a buggy plugin cannot poison the hashrate
// gauges or any rules built on top of them.
func sanitizeHashrate(observedHS float64, nameplateHS *float64, deviceID, driver string) (observedTHs, expectedTHs float64, ok bool) {
	if math.IsNaN(observedHS) || math.IsInf(observedHS, 0) || observedHS < 0 {
		slog.Warn("metricsObserver: dropping non-finite or negative observed hashrate",
			"device_id", deviceID,
			"driver", driver,
		)
		return 0, 0, false
	}
	observedTHs = observedHS / hertzPerTerahertz
	expectedTHs = 0.0
	if nameplateHS != nil {
		v := *nameplateHS
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			slog.Warn("metricsObserver: dropping non-finite or negative nameplate hashrate",
				"device_id", deviceID,
				"driver", driver,
			)
			// Fall through with expectedTHs left at 0 — the observed reading
			// is still useful even if the nameplate is bogus.
		} else {
			expectedTHs = v / hertzPerTerahertz
		}
	}
	return observedTHs, expectedTHs, true
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
