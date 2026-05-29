// Package devicerollup holds shared dependencies + helpers for the
// site- and building-level stats RPCs. Both surfaces consume an
// identical telemetry-collector interface, the same set of unit
// conversions, and the same per-fleet rollup loop; centralising them
// here keeps the two services from drifting on units, NaN handling, or
// state-bucket semantics.
package devicerollup

import (
	"context"
	"math"

	minerModels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// Unit conversions shared by every per-fleet rollup. Values from the
// telemetry store come in hashes-per-second, watts, and joules-per-hash;
// the FE displays in TH/s, kW, and J/TH respectively.
const (
	HashToTeraHashConversion                   = 1e12
	WattsToKilowattsConversion                 = 1000.0
	JoulesPerHashToJoulesPerTeraHashMultiplier = 1e12
)

// TelemetryCollector is the slice of the telemetry service the rollup
// helpers need. Mirrors the contract used by collection.Service so
// existing wiring works.
type TelemetryCollector interface {
	GetLatestDeviceMetrics(ctx context.Context, deviceIDs []minerModels.DeviceIdentifier) (map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics, error)
}

// DeviceQueryer is the slice of device-store methods the rollup helpers
// need. Sites + buildings stats both use this exact shape; the
// by-collections query is only used by building rollups but kept on
// the shared interface so callers don't have to widen it locally.
type DeviceQueryer interface {
	GetDeviceIdentifiersByOrgWithFilter(ctx context.Context, orgID int64, filter *interfaces.MinerFilter) ([]string, error)
	GetMinerStateCountsByDeviceIDs(ctx context.Context, orgID int64, deviceIdentifiers []string) (interfaces.MinerStateCounts, error)
	GetMinerStateCountsByCollections(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]interfaces.MinerStateCounts, error)
}

// MetricsRollup is the per-fleet aggregate of latest telemetry across a
// set of devices. Values are unit-converted to the proto contract (TH/s,
// kW, J/TH); zero values mean "no reporting device contributed."
//
// Per-field reporting counts surface the "device reported but this field
// was nil" case so the FE can distinguish missing telemetry from genuine
// zero. ReportingCount is the union (any field present); the per-field
// counts are subsets.
type MetricsRollup struct {
	ReportingCount           int32
	HashrateReportingCount   int32
	EfficiencyReportingCount int32
	PowerReportingCount      int32
	TotalHashrateThs         float64
	TotalPowerKw             float64
	AvgEfficiencyJth         float64
}

// AggregateLatestMetrics sums hashrate + power and averages efficiency
// across the supplied device set. Devices missing from `metrics` are
// skipped silently — they simply don't contribute. Per-field values are
// validated to be finite (not NaN / ±Inf) and non-negative before they
// count; an invalid value behaves the same as "field absent" — it
// doesn't increment that field's reporting count and doesn't poison the
// aggregate. A device with all three fields invalid still increments
// ReportingCount (the latest-metrics record itself is present) but
// contributes nothing to any rollup. Empty input returns the zero
// value with ReportingCount = 0.
//
// This is defense in depth against plugins that return NaN/Inf for
// disconnected or mis-reporting hardware: without these checks one bad
// metric value would silently flip site- or building-level totals to
// NaN/Inf and break the FE.
func AggregateLatestMetrics(
	metrics map[minerModels.DeviceIdentifier]modelsV2.DeviceMetrics,
	deviceIDs []minerModels.DeviceIdentifier,
) MetricsRollup {
	var (
		reportingCount int32
		hashrateN      int32
		powerN         int32
		efficiencyN    int32
		hashrateSum    float64
		powerSum       float64
		efficiencySum  float64
	)
	finiteNonNegative := func(v float64) bool {
		// math.IsInf(v, 0) catches both +Inf and -Inf; the v >= 0 clause
		// also rejects -0 silently (rounds to 0 in the sum, harmless).
		return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
	}
	for _, devID := range deviceIDs {
		m, ok := metrics[devID]
		if !ok {
			continue
		}
		reportingCount++
		if m.HashrateHS != nil && finiteNonNegative(m.HashrateHS.Value) {
			hashrateSum += m.HashrateHS.Value
			hashrateN++
		}
		if m.PowerW != nil && finiteNonNegative(m.PowerW.Value) {
			powerSum += m.PowerW.Value
			powerN++
		}
		if m.EfficiencyJH != nil && finiteNonNegative(m.EfficiencyJH.Value) {
			efficiencySum += m.EfficiencyJH.Value
			efficiencyN++
		}
	}

	out := MetricsRollup{
		ReportingCount:           reportingCount,
		HashrateReportingCount:   hashrateN,
		EfficiencyReportingCount: efficiencyN,
		PowerReportingCount:      powerN,
	}
	if reportingCount == 0 {
		return out
	}
	out.TotalHashrateThs = hashrateSum / HashToTeraHashConversion
	out.TotalPowerKw = powerSum / WattsToKilowattsConversion
	if efficiencyN > 0 {
		avg := (efficiencySum / float64(efficiencyN)) * JoulesPerHashToJoulesPerTeraHashMultiplier
		// Guard against NaN / negative noise from rounding around zero.
		if math.IsNaN(avg) || avg < 0 {
			avg = 0
		}
		out.AvgEfficiencyJth = avg
	}
	return out
}
