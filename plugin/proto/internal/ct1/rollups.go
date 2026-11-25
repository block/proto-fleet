package ct1

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const powerAttributionConflict = "conflict"

func powerRollup(value *float64, at, now time.Time, source, attribution string) *sdk.ContainerRollup {
	r := &sdk.ContainerRollup{PowerSource: source, Attribution: attribution, SourceTimes: map[string]time.Time{}, Power: sdk.MetricCoverage{Expected: 1, TopologyComplete: attribution != powerAttributionConflict}}
	if !at.IsZero() && !at.After(now.Add(clockSkewAllowance)) {
		r.SourceTimes[source] = at
	}
	if nonnegative(value) && sdk.FreshContainerReading(at, now) && attribution != powerAttributionConflict {
		r.PowerW = value
		r.Power.Reporting, r.Power.OldestAt, r.Power.NewestAt = 1, at, at
	}
	return r
}

// Until firmware returns typed PDU attribution, use its documented CT1 naming
// convention and cross-check every available configured/observed feed. Never
// turn conflicting evidence into a tank measurement or sum module PSU readings.
func collectRollups(s *sdk.ContainerSnapshot, wire *mdk.GetContainerHealthResponse, accepted bool) {
	if !accepted {
		wire = nil
	}
	at := sourceTime(wire.GetTimestamp())
	var facility *float64
	if p := wire.GetPlant(); p != nil {
		facility = p.FacilityPowerW
	}
	s.Metrics = powerRollup(facility, at, s.ObservedAt, "facility", "facility_meter")
	for i := range s.Tanks {
		t := &s.Tanks[i]
		id := fmt.Sprintf("pdu-%d", t.Position)
		var value *float64
		count := 0
		conflict := false
		for _, p := range wire.GetPowerDistributions() {
			if p.GetComponentId() == id {
				count++
				value = p.ActivePowerW
			}
		}
		for _, other := range s.Tanks {
			for _, m := range other.Modules {
				for _, b := range m.Hashboards {
					f := b.Feed
					if f == nil {
						continue
					}
					feeds := []string{f.PowerFeedID, f.ExpectedPowerFeedID, f.ObservedPowerFeedID}
					for _, feed := range feeds {
						if feed == "" {
							continue
						}
						prefix, outlet, ok := strings.Cut(feed, "-outlet-")
						n, err := strconv.Atoi(outlet)
						if other.Position == t.Position && (!ok || err != nil || n != int(m.Position) || prefix != id) {
							conflict = true
						}
						if other.Position != t.Position && prefix == id {
							conflict = true
						}
					}
					if other.Position == t.Position && ((f.ConfiguredTank != 0 && f.ConfiguredTank != t.Position) || (f.ConfiguredModule != 0 && f.ConfiguredModule != m.Position) || len(f.WiringReasons) > 0 || strings.Contains(strings.ToLower(f.WiringStatus), "conflict") || strings.Contains(strings.ToLower(f.WiringStatus), "mismatch")) {
						conflict = true
					}
				}
			}
		}
		attribution := "naming_convention"
		if conflict || count > 1 {
			attribution = powerAttributionConflict
		}
		t.Metrics = powerRollup(value, at, s.ObservedAt, id, attribution)
	}
}
