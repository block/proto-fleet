package ct1

import (
	"math"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// PSUElectrical is the module-owned PSU observation repeated by each board.
type PSUElectrical struct {
	InputPower  *float64 `json:"inputPower"`
	SampleAgeMs *float64 `json:"sampleAgeMs"`
}

func moduleMetrics(boards []Board, now time.Time) *sdk.ContainerModuleMetrics {
	result := &sdk.ContainerModuleMetrics{ObservedAt: now}
	hashrate, temperature := 0.0, 0.0
	hashComplete, tempComplete := len(boards) > 0, len(boards) > 0
	var powerAt, oldestSource time.Time
	for _, b := range boards {
		h := b.Health
		at, power := modulePSUReading(h, now)
		if at.After(powerAt) || (!at.IsZero() && at.Equal(powerAt) && power == nil) {
			powerAt, result.PowerW = at, power
		}
		// Even stale health carries source-ordering evidence. Record its age
		// before rejecting its measurements so empty evidence cannot become new.
		if h != nil && !h.Timestamp.IsZero() && (oldestSource.IsZero() || h.Timestamp.Before(oldestSource)) {
			oldestSource = h.Timestamp
		}
		if h == nil || h.Timestamp.IsZero() || now.Sub(h.Timestamp) >= healthMaxAge || h.Timestamp.After(now.Add(clockSkewAllowance)) {
			hashComplete, tempComplete = false, false
			continue
		}
		if h.Timestamp.Before(result.ObservedAt) {
			result.ObservedAt = h.Timestamp
		}
		if nonnegative(h.Hashrate) {
			hashrate += *h.Hashrate * 1e12
		} else {
			hashComplete = false
		}
		if finite(h.AverageAsicTemperatureC) {
			temperature += *h.AverageAsicTemperatureC
		} else {
			tempComplete = false
		}
	}
	if hashComplete && finite(&hashrate) {
		result.HashrateHS = &hashrate
	}
	if tempComplete && finite(&temperature) {
		temperature /= float64(len(boards))
		result.TemperatureC = &temperature
	}
	if !powerAt.IsZero() && powerAt.Before(result.ObservedAt) {
		result.ObservedAt = powerAt
	}
	if result.HashrateHS == nil && result.PowerW == nil && result.TemperatureC == nil {
		if !oldestSource.IsZero() {
			result.ObservedAt = oldestSource
		} else {
			// No source timestamp exists: record the absence observed by this
			// poll at collection time, independently of source-dated evidence.
			result.ObservedAt = now
		}
	}
	result.Rollup = &sdk.ContainerRollup{PowerW: result.PowerW, PowerSource: "module_psu", Attribution: "shared_psu", SourceTimes: map[string]time.Time{}, Power: sdk.MetricCoverage{Expected: 1, TopologyComplete: true}}
	if !powerAt.IsZero() {
		result.Rollup.SourceTimes["module_psu"] = powerAt
	}
	if result.PowerW != nil {
		result.Rollup.Power = sdk.MetricCoverage{Expected: 1, Reporting: 1, TopologyComplete: true, OldestAt: powerAt, NewestAt: powerAt}
	}
	return result
}

// A trustworthy PSU timestamp supersedes older samples even when its value is
// invalid or stale. Repeated board reports must select the newest observation,
// not the newest valid value; invalid duplicates at the same time also withdraw it.
func modulePSUReading(h *Health, now time.Time) (time.Time, *float64) {
	if h == nil || h.PSUElectrical == nil || h.Timestamp.IsZero() || h.Timestamp.After(now.Add(clockSkewAllowance)) {
		return time.Time{}, nil
	}
	p := h.PSUElectrical
	at := h.Timestamp
	if p.SampleAgeMs != nil {
		if !nonnegative(p.SampleAgeMs) || *p.SampleAgeMs > float64(math.MaxInt64/int64(time.Millisecond)) {
			return time.Time{}, nil
		}
		at = at.Add(-time.Duration(*p.SampleAgeMs * float64(time.Millisecond)))
	}
	if !sdk.FreshContainerReading(at, now) || !nonnegative(p.InputPower) {
		return at, nil
	}
	return at, p.InputPower
}

func finite(v *float64) bool      { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
func nonnegative(v *float64) bool { return finite(v) && *v >= 0 }
