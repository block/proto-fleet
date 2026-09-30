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
	hashComplete, tempComplete := len(boards) == boardsPerModule, len(boards) == boardsPerModule
	var powerAt, oldestSource time.Time
	for _, b := range boards {
		h := b.Health
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
		if p := h.PSUElectrical; p != nil && nonnegative(p.InputPower) {
			at := h.Timestamp
			if p.SampleAgeMs != nil {
				if !nonnegative(p.SampleAgeMs) || *p.SampleAgeMs >= float64(healthMaxAge.Milliseconds()) {
					continue
				}
				at = at.Add(-time.Duration(*p.SampleAgeMs * float64(time.Millisecond)))
			}
			if now.Sub(at) < healthMaxAge && (powerAt.IsZero() || at.After(powerAt)) {
				powerAt, result.PowerW = at, p.InputPower
			}
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
	return result
}

func finite(v *float64) bool      { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
func nonnegative(v *float64) bool { return finite(v) && *v >= 0 }
