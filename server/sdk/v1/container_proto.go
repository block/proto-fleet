package sdk

import (
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func containerSnapshotToProto(s *ContainerSnapshot) *pb.ContainerSnapshot {
	if s == nil {
		return nil
	}
	out := &pb.ContainerSnapshot{ObservedAt: timestamppb.New(s.ObservedAt), TopologyComplete: s.TopologyComplete, Metrics: rollupToProto(s.Metrics)}
	if s.HA != nil {
		out.Ha = &pb.ContainerHA{ObservedAt: timestamppb.New(s.HA.ObservedAt), Degraded: s.HA.Degraded}
	}
	for _, b := range s.UnlocatedHashboards {
		out.UnlocatedHashboards = append(out.UnlocatedHashboards, hashboardToProto(b))
	}
	for _, f := range s.Fans {
		out.Fans = append(out.Fans, &pb.ContainerFan{ComponentId: f.ComponentID, ObservedAt: timestamppb.New(f.ObservedAt), SpeedPercent: f.SpeedPercent, PowerW: f.PowerW, Direction: f.Direction, AlarmActive: f.AlarmActive})
	}
	if c := s.Controls; c != nil {
		out.Controls = &pb.ContainerControls{ObservedAt: timestamppb.New(c.ObservedAt), Supported: c.Supported, MainCircuitBreakerSupported: c.MainCircuitBreakerSupported, MainCircuitBreakerClosed: c.MainCircuitBreakerClosed, AlarmResetSupported: c.AlarmResetSupported, AlarmMuteSupported: c.AlarmMuteSupported, DryCoolerFanReverseSupported: c.DryCoolerFanReverseSupported}
		for _, v := range c.Controls {
			out.Controls.Controls = append(out.Controls.Controls, &pb.ContainerControl{Name: v.Name, Enabled: v.Enabled})
		}
		for _, v := range c.Setpoints {
			out.Controls.Setpoints = append(out.Controls.Setpoints, &pb.ContainerSetpoint{Name: v.Name, Value: v.Value, Unit: v.Unit})
		}
	}
	if p := s.Plant; p != nil {
		out.Plant = &pb.ContainerPlant{DryCoolerInletTemperatureC: p.DryCoolerInletTemperatureC, ObservedAt: timestamppb.New(p.ObservedAt), TotalPowerW: p.TotalPowerW, FacilityPowerW: p.FacilityPowerW, Pue: p.PUE, InletTemperatureC: p.InletTemperatureC, OutletTemperatureC: p.OutletTemperatureC, AlarmActive: p.AlarmActive, CriticalAlarmActive: p.CriticalAlarmActive}
	}
	for _, tank := range s.Tanks {
		t := &pb.ContainerTank{Position: tank.Position, Metrics: rollupToProto(tank.Metrics), TopologyComplete: tank.TopologyComplete}
		for _, m := range tank.Modules {
			module := &pb.ContainerModule{Position: m.Position, ReportedId: m.ReportedID, IdentityConfirmed: m.IdentityConfirmed, State: m.State.String(), ObservedAt: timestamppb.New(m.ObservedAt)}
			if v := m.Issues; v != nil {
				module.Issues = &pb.ContainerModuleIssues{ObservedAt: timestamppb.New(v.ObservedAt), Complete: v.Complete}
				if v.SourceTimes != nil {
					module.Issues.SourceTimes = map[string]*timestamppb.Timestamp{}
					for key, at := range v.SourceTimes {
						module.Issues.SourceTimes[key] = timestamppb.New(at)
					}
				}
				for _, issue := range v.Items {
					module.Issues.Items = append(module.Issues.Items, &pb.ContainerModuleIssue{Component: issue.Component, ComponentId: issue.ComponentID, Cause: issue.Cause, Severity: issue.Severity, CanonicalError: issue.CanonicalError, ObservedAt: timestamppb.New(issue.ObservedAt)})
				}
			}
			for _, b := range m.Hashboards {
				module.Hashboards = append(module.Hashboards, hashboardToProto(b))
			}
			if v := m.Metrics; v != nil {
				module.Metrics = &pb.ContainerModuleMetrics{ObservedAt: timestamppb.New(v.ObservedAt), EfficiencyJh: v.EfficiencyJH, Rollup: rollupToProto(v.Rollup), HashrateHs: v.HashrateHS, PowerW: v.PowerW, TemperatureC: v.TemperatureC}
			}
			t.Modules = append(t.Modules, module)
		}
		out.Tanks = append(out.Tanks, t)
	}
	return out
}
func containerSnapshotFromProto(s *pb.ContainerSnapshot) *ContainerSnapshot {
	if s == nil {
		return nil
	}
	out := &ContainerSnapshot{ObservedAt: s.ObservedAt.AsTime(), TopologyComplete: s.TopologyComplete, Metrics: rollupFromProto(s.Metrics), Tanks: []ContainerTank{}}
	if s.Ha != nil {
		out.HA = &ContainerHA{ObservedAt: s.Ha.ObservedAt.AsTime(), Degraded: s.Ha.Degraded}
	}

	for _, b := range s.UnlocatedHashboards {
		if b != nil {
			out.UnlocatedHashboards = append(out.UnlocatedHashboards, hashboardFromProto(b))
		}
	}
	for _, f := range s.Fans {
		if f == nil {
			continue
		}
		out.Fans = append(out.Fans, ContainerFan{ComponentID: f.ComponentId, ObservedAt: f.ObservedAt.AsTime(), SpeedPercent: f.SpeedPercent, PowerW: f.PowerW, Direction: f.Direction, AlarmActive: f.AlarmActive})
	}
	if c := s.Controls; c != nil {
		out.Controls = &ContainerControls{ObservedAt: c.ObservedAt.AsTime(), Supported: c.Supported, MainCircuitBreakerSupported: c.MainCircuitBreakerSupported, MainCircuitBreakerClosed: c.MainCircuitBreakerClosed, AlarmResetSupported: c.AlarmResetSupported, AlarmMuteSupported: c.AlarmMuteSupported, DryCoolerFanReverseSupported: c.DryCoolerFanReverseSupported}
		for _, v := range c.Controls {
			if v != nil {
				out.Controls.Controls = append(out.Controls.Controls, ContainerControl{Name: v.Name, Enabled: v.Enabled})
			}
		}
		for _, v := range c.Setpoints {
			if v != nil {
				out.Controls.Setpoints = append(out.Controls.Setpoints, ContainerSetpoint{Name: v.Name, Value: v.Value, Unit: v.Unit})
			}
		}
	}
	if p := s.Plant; p != nil {
		out.Plant = &ContainerPlant{DryCoolerInletTemperatureC: p.DryCoolerInletTemperatureC, ObservedAt: p.ObservedAt.AsTime(), TotalPowerW: p.TotalPowerW, FacilityPowerW: p.FacilityPowerW, PUE: p.Pue, InletTemperatureC: p.InletTemperatureC, OutletTemperatureC: p.OutletTemperatureC, AlarmActive: p.AlarmActive, CriticalAlarmActive: p.CriticalAlarmActive}
	}
	for _, tank := range s.Tanks {
		t := ContainerTank{Position: tank.Position, Metrics: rollupFromProto(tank.Metrics), TopologyComplete: tank.TopologyComplete, Modules: []ContainerModule{}}
		for _, m := range tank.Modules {
			module := ContainerModule{Position: m.Position, ReportedID: m.ReportedId, IdentityConfirmed: m.IdentityConfirmed, State: parseContainerStatus(m.State), ObservedAt: m.ObservedAt.AsTime()}
			if v := m.Issues; v != nil {
				module.Issues = &ContainerModuleIssues{ObservedAt: v.ObservedAt.AsTime(), Complete: v.Complete}
				if len(v.SourceTimes) > 0 {
					module.Issues.SourceTimes = map[string]time.Time{}
					for key, at := range v.SourceTimes {
						module.Issues.SourceTimes[key] = at.AsTime()
					}
				}
				for _, issue := range v.Items {
					if issue != nil {
						module.Issues.Items = append(module.Issues.Items, ContainerModuleIssue{Component: issue.Component, ComponentID: issue.ComponentId, Cause: issue.Cause, Severity: issue.Severity, CanonicalError: issue.CanonicalError, ObservedAt: issue.ObservedAt.AsTime()})
					}
				}
			}
			for _, b := range m.Hashboards {
				if b != nil {
					module.Hashboards = append(module.Hashboards, hashboardFromProto(b))
				}
			}
			if v := m.Metrics; v != nil {
				module.Metrics = &ContainerModuleMetrics{ObservedAt: v.ObservedAt.AsTime(), EfficiencyJH: v.EfficiencyJh, Rollup: rollupFromProto(v.Rollup), HashrateHS: v.HashrateHs, PowerW: v.PowerW, TemperatureC: v.TemperatureC}
			}
			t.Modules = append(t.Modules, module)
		}
		out.Tanks = append(out.Tanks, t)
	}
	return out
}

func coverageToProto(c MetricCoverage) *pb.MetricCoverage {
	return &pb.MetricCoverage{Reporting: c.Reporting, Expected: c.Expected, TopologyComplete: c.TopologyComplete, OldestAt: timestamppb.New(c.OldestAt), NewestAt: timestamppb.New(c.NewestAt)}
}
func coverageFromProto(c *pb.MetricCoverage) MetricCoverage {
	if c == nil {
		return MetricCoverage{}
	}
	return MetricCoverage{Reporting: c.Reporting, Expected: c.Expected, TopologyComplete: c.TopologyComplete, OldestAt: c.OldestAt.AsTime(), NewestAt: c.NewestAt.AsTime()}
}
func rollupToProto(r *ContainerRollup) *pb.ContainerRollup {
	if r == nil {
		return nil
	}
	out := &pb.ContainerRollup{UnlocatedHashboards: r.UnlocatedHashboards, HashrateHs: r.HashrateHS, PowerW: r.PowerW, EfficiencyJh: r.EfficiencyJH, TemperatureC: r.TemperatureC, MinTemperatureC: r.MinTemperatureC, MaxTemperatureC: r.MaxTemperatureC, Hashrate: coverageToProto(r.Hashrate), Power: coverageToProto(r.Power), Temperature: coverageToProto(r.Temperature), PowerSource: r.PowerSource, Attribution: r.Attribution, SourceTimes: map[string]*timestamppb.Timestamp{}}
	for k, v := range r.SourceTimes {
		out.SourceTimes[k] = timestamppb.New(v)
	}
	return out
}
func rollupFromProto(r *pb.ContainerRollup) *ContainerRollup {
	if r == nil {
		return nil
	}
	out := &ContainerRollup{UnlocatedHashboards: r.UnlocatedHashboards, HashrateHS: r.HashrateHs, PowerW: r.PowerW, EfficiencyJH: r.EfficiencyJh, TemperatureC: r.TemperatureC, MinTemperatureC: r.MinTemperatureC, MaxTemperatureC: r.MaxTemperatureC, Hashrate: coverageFromProto(r.Hashrate), Power: coverageFromProto(r.Power), Temperature: coverageFromProto(r.Temperature), PowerSource: r.PowerSource, Attribution: r.Attribution, SourceTimes: map[string]time.Time{}}
	for k, v := range r.SourceTimes {
		out.SourceTimes[k] = v.AsTime()
	}
	return out
}
func feedToProto(f *ContainerFeedEvidence) *pb.ContainerFeedEvidence {
	if f == nil {
		return nil
	}
	return &pb.ContainerFeedEvidence{PowerFeedId: f.PowerFeedID, ObservedPowerFeedId: f.ObservedPowerFeedID, ExpectedPowerFeedId: f.ExpectedPowerFeedID, WiringStatus: f.WiringStatus, WiringReasons: f.WiringReasons, ConfiguredTank: f.ConfiguredTank, ConfiguredModule: f.ConfiguredModule}
}
func feedFromProto(f *pb.ContainerFeedEvidence) *ContainerFeedEvidence {
	if f == nil {
		return nil
	}
	return &ContainerFeedEvidence{PowerFeedID: f.PowerFeedId, ObservedPowerFeedID: f.ObservedPowerFeedId, ExpectedPowerFeedID: f.ExpectedPowerFeedId, WiringStatus: f.WiringStatus, WiringReasons: f.WiringReasons, ConfiguredTank: f.ConfiguredTank, ConfiguredModule: f.ConfiguredModule}
}

func hashboardToProto(b ContainerHashboard) *pb.ContainerHashboard {
	var claims []*pb.HashboardIdentityClaim
	for _, c := range b.IdentityClaims {
		claims = append(claims, &pb.HashboardIdentityClaim{MacAddress: c.MACAddress, Serial: c.Serial})
	}
	return &pb.ContainerHashboard{IdentityClaims: claims, UnavailableReason: b.UnavailableReason, Position: b.Position, Occupancy: b.Occupancy, MacAddress: b.MACAddress, Serial: b.Serial, State: b.State.String(), MiningState: b.MiningState, Isolated: b.Isolated, ObservedAt: timestamppb.New(b.ObservedAt), StateObservedAt: timestamppb.New(b.StateObservedAt), PowerW: b.PowerW, EfficiencyJh: b.EfficiencyJH, Feed: feedToProto(b.Feed), HashrateHs: b.HashrateHS, TemperatureC: b.TemperatureC, HashrailOk: b.HashrailOK, RefclksOk: b.RefclksOK, MetadataJson: b.MetadataJSON, HealthJson: b.HealthJSON}
}

func hashboardFromProto(b *pb.ContainerHashboard) ContainerHashboard {
	var claims []HashboardIdentityClaim
	for _, c := range b.IdentityClaims {
		if c != nil {
			claims = append(claims, HashboardIdentityClaim{MACAddress: c.MacAddress, Serial: c.Serial})
		}
	}
	return ContainerHashboard{IdentityClaims: claims, UnavailableReason: b.UnavailableReason, Position: b.Position, Occupancy: b.Occupancy, MACAddress: b.MacAddress, Serial: b.Serial, State: parseContainerStatus(b.State), MiningState: b.MiningState, Isolated: b.Isolated, ObservedAt: b.ObservedAt.AsTime(), StateObservedAt: b.StateObservedAt.AsTime(), PowerW: b.PowerW, EfficiencyJH: b.EfficiencyJh, Feed: feedFromProto(b.Feed), HashrateHS: b.HashrateHs, TemperatureC: b.TemperatureC, HashrailOK: b.HashrailOk, RefclksOK: b.RefclksOk, MetadataJSON: b.MetadataJson, HealthJSON: b.HealthJson}
}

// Invalid wire values survive as invalid enums for ingestion validation.
func parseContainerStatus(value string) MinerStatus {
	if value == "" {
		return MinerStatusUnknown
	}
	status, err := MinerStatusUnknown.Parse(value)
	if err != nil {
		return MinerStatus(-1)
	}
	return status
}
