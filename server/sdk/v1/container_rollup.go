package sdk

import (
	"fmt"
	"math"
	"time"
)

// CT1 physical layout and freshness limits shared by collection and rollups.
const (
	CT1TankCount                = 6
	CT1ModulesPerTank           = 16
	CT1HashboardsPerModule      = 3
	ContainerReadingMaxAge      = 2 * time.Minute
	ContainerClockSkewAllowance = 5 * time.Second
)

// MetricCoverage counts installed boards for hashrate, modules for equipment
// temperature, and the single dedicated meter for power. Unknown topology is
// independent of reporting counts: equal counts alone do not prove completeness.
type MetricCoverage struct {
	Reporting        int32     `json:"reporting"`
	Expected         int32     `json:"expected"`
	TopologyComplete bool      `json:"topology_complete"`
	OldestAt         time.Time `json:"oldest_at"`
	NewestAt         time.Time `json:"newest_at"`
}

func (c *MetricCoverage) Complete() bool {
	return c.TopologyComplete && c.Expected > 0 && c.Reporting == c.Expected
}

type ContainerRollup struct {
	HashrateHS      *float64       `json:"hashrate_hs"`
	PowerW          *float64       `json:"power_w"`
	EfficiencyJH    *float64       `json:"efficiency_jh"`
	TemperatureC    *float64       `json:"temperature_c"`
	MinTemperatureC *float64       `json:"min_temperature_c"`
	MaxTemperatureC *float64       `json:"max_temperature_c"`
	Hashrate        MetricCoverage `json:"hashrate"`
	Power           MetricCoverage `json:"power"`
	Temperature     MetricCoverage `json:"temperature"`
	PowerSource     string         `json:"power_source"`
	Attribution     string         `json:"attribution"`
	// Ordering evidence is retained even when a measurement is unavailable.
	SourceTimes map[string]time.Time `json:"source_times"`
}

type ContainerFeedEvidence struct {
	ObservedPowerFeedID string   `json:"observed_power_feed_id"`
	PowerFeedID         string   `json:"power_feed_id"`
	ExpectedPowerFeedID string   `json:"expected_power_feed_id"`
	WiringStatus        string   `json:"wiring_status"`
	WiringReasons       []string `json:"wiring_reasons,omitempty"`
	ConfiguredTank      int32    `json:"configured_tank"`
	ConfiguredModule    int32    `json:"configured_module"`
}

func finiteReading(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
func FreshContainerReading(at, now time.Time) bool {
	return !at.IsZero() && now.Sub(at) < ContainerReadingMaxAge && !at.After(now.Add(ContainerClockSkewAllowance))
}
func (c *MetricCoverage) add(at time.Time) {
	c.Reporting++
	if c.OldestAt.IsZero() || at.Before(c.OldestAt) {
		c.OldestAt = at
	}
	if at.After(c.NewestAt) {
		c.NewestAt = at
	}
}
func addReading(total **float64, value float64) {
	if *total == nil {
		*total = new(float64)
	}
	**total += value
}
func newRollup(old *ContainerRollup, complete bool, now time.Time) *ContainerRollup {
	r := &ContainerRollup{SourceTimes: map[string]time.Time{}}
	r.Hashrate.TopologyComplete = complete
	r.Temperature.TopologyComplete = complete
	if old != nil {
		r.PowerW, r.Power, r.PowerSource, r.Attribution = old.PowerW, old.Power, old.PowerSource, old.Attribution
		for k, v := range old.SourceTimes {
			r.SourceTimes[k] = v
		}
	}
	if !finiteReading(r.PowerW) || (r.PowerW != nil && *r.PowerW < 0) || !FreshContainerReading(r.Power.OldestAt, now) || !FreshContainerReading(r.Power.NewestAt, now) {
		r.PowerW = nil
		r.Power.Reporting = 0
	}
	return r
}
func finishRollup(r *ContainerRollup) {
	if r.Temperature.Reporting > 0 {
		*r.TemperatureC /= float64(r.Temperature.Reporting)
	}
	if !finiteReading(r.HashrateHS) {
		r.HashrateHS = nil
	}
	if !finiteReading(r.TemperatureC) {
		r.TemperatureC = nil
	}
	if r.Hashrate.Complete() && r.Power.Complete() && r.HashrateHS != nil && *r.HashrateHS > 0 && finiteReading(r.PowerW) && *r.PowerW >= 0 {
		v := *r.PowerW / *r.HashrateHS
		if finiteReading(&v) {
			r.EfficiencyJH = &v
		}
	}
}

// CalculateContainerRollups uses only this poll's accepted board placement. The
// store invokes it again after durable-identity reconciliation so rejected
// identities cannot contribute to persisted totals. Dedicated power is retained.
func CalculateContainerRollups(s *ContainerSnapshot) {
	s.Metrics = newRollup(s.Metrics, s.TopologyComplete, s.ObservedAt)
	for ti := range s.Tanks {
		t := &s.Tanks[ti]
		t.Metrics = newRollup(t.Metrics, t.TopologyComplete, s.ObservedAt)
		for mi := range t.Modules {
			m := &t.Modules[mi]
			complete := len(m.Hashboards) == CT1HashboardsPerModule
			for _, b := range m.Hashboards {
				if b.Occupancy == "unknown" || b.UnavailableReason != "" {
					complete = false
				}
			}
			var prior *ContainerRollup
			if m.Metrics != nil {
				prior = m.Metrics.Rollup
			}
			mr := newRollup(prior, complete, s.ObservedAt)
			for _, b := range m.Hashboards {
				if b.Occupancy == "empty" {
					continue
				}
				mr.Hashrate.Expected++
				mr.Temperature.Expected++
				if b.Occupancy != "occupied" {
					continue
				}
				key := fmt.Sprintf("board:%s:%s", b.MACAddress, b.Serial)
				if !b.ObservedAt.IsZero() && !b.ObservedAt.After(s.ObservedAt.Add(ContainerClockSkewAllowance)) {
					mr.SourceTimes[key] = b.ObservedAt
				}
				if !FreshContainerReading(b.ObservedAt, s.ObservedAt) {
					continue
				}
				if finiteReading(b.HashrateHS) && *b.HashrateHS >= 0 {
					addReading(&mr.HashrateHS, *b.HashrateHS)
					mr.Hashrate.add(b.ObservedAt)
				}
				if finiteReading(b.TemperatureC) {
					addReading(&mr.TemperatureC, *b.TemperatureC)
					mr.Temperature.add(b.ObservedAt)
				}
			}
			finishRollup(mr)
			if m.Metrics == nil {
				m.Metrics = &ContainerModuleMetrics{ObservedAt: s.ObservedAt}
			}
			m.Metrics.Rollup = mr
			m.Metrics.HashrateHS, m.Metrics.PowerW, m.Metrics.TemperatureC, m.Metrics.EfficiencyJH = mr.HashrateHS, mr.PowerW, mr.TemperatureC, mr.EfficiencyJH
			for _, r := range []*ContainerRollup{t.Metrics, s.Metrics} {
				r.Hashrate.Expected += mr.Hashrate.Expected
				r.Hashrate.Reporting += mr.Hashrate.Reporting
				r.Hashrate.TopologyComplete = r.Hashrate.TopologyComplete && complete
				if mr.HashrateHS != nil {
					addReading(&r.HashrateHS, *mr.HashrateHS)
				}
				if !mr.Hashrate.OldestAt.IsZero() && (r.Hashrate.OldestAt.IsZero() || mr.Hashrate.OldestAt.Before(r.Hashrate.OldestAt)) {
					r.Hashrate.OldestAt = mr.Hashrate.OldestAt
				}
				if mr.Hashrate.NewestAt.After(r.Hashrate.NewestAt) {
					r.Hashrate.NewestAt = mr.Hashrate.NewestAt
				}
				if mr.Temperature.Expected > 0 {
					r.Temperature.Expected++
				}
				r.Temperature.TopologyComplete = r.Temperature.TopologyComplete && complete
				// Partial module means are not equally comparable with complete means.
				if mr.Temperature.Complete() && mr.TemperatureC != nil {
					v := *mr.TemperatureC
					addReading(&r.TemperatureC, v)
					r.Temperature.add(mr.Temperature.OldestAt)
					if mr.Temperature.NewestAt.After(r.Temperature.NewestAt) {
						r.Temperature.NewestAt = mr.Temperature.NewestAt
					}
					if r.MinTemperatureC == nil || v < *r.MinTemperatureC {
						r.MinTemperatureC = new(v)
					}
					if r.MaxTemperatureC == nil || v > *r.MaxTemperatureC {
						r.MaxTemperatureC = new(v)
					}
				}
			}
		}
		finishRollup(t.Metrics)
	}
	finishRollup(s.Metrics)
}
