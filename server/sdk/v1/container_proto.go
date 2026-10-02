package sdk

import (
	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func containerSnapshotToProto(s *ContainerSnapshot) *pb.ContainerSnapshot {
	if s == nil {
		return nil
	}
	out := &pb.ContainerSnapshot{ObservedAt: timestamppb.New(s.ObservedAt), TopologyComplete: s.TopologyComplete}
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
		out.Plant = &pb.ContainerPlant{ObservedAt: timestamppb.New(p.ObservedAt), TotalPowerW: p.TotalPowerW, FacilityPowerW: p.FacilityPowerW, Pue: p.PUE, InletTemperatureC: p.InletTemperatureC, OutletTemperatureC: p.OutletTemperatureC, AlarmActive: p.AlarmActive, CriticalAlarmActive: p.CriticalAlarmActive}
	}
	for _, tank := range s.Tanks {
		t := &pb.ContainerTank{Position: tank.Position}
		for _, m := range tank.Modules {
			module := &pb.ContainerModule{Position: m.Position, ReportedId: m.ReportedID, State: string(m.State), ObservedAt: timestamppb.New(m.ObservedAt)}
			if v := m.Metrics; v != nil {
				module.Metrics = &pb.ContainerModuleMetrics{ObservedAt: timestamppb.New(v.ObservedAt), HashrateHs: v.HashrateHS, PowerW: v.PowerW, TemperatureC: v.TemperatureC}
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
	out := &ContainerSnapshot{ObservedAt: s.ObservedAt.AsTime(), TopologyComplete: s.TopologyComplete, Tanks: []ContainerTank{}}
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
		out.Plant = &ContainerPlant{ObservedAt: p.ObservedAt.AsTime(), TotalPowerW: p.TotalPowerW, FacilityPowerW: p.FacilityPowerW, PUE: p.Pue, InletTemperatureC: p.InletTemperatureC, OutletTemperatureC: p.OutletTemperatureC, AlarmActive: p.AlarmActive, CriticalAlarmActive: p.CriticalAlarmActive}
	}
	for _, tank := range s.Tanks {
		t := ContainerTank{Position: tank.Position, Modules: []ContainerModule{}}
		for _, m := range tank.Modules {
			module := ContainerModule{Position: m.Position, ReportedID: m.ReportedId, State: ModuleState(m.State), ObservedAt: m.ObservedAt.AsTime()}
			if v := m.Metrics; v != nil {
				module.Metrics = &ContainerModuleMetrics{ObservedAt: v.ObservedAt.AsTime(), HashrateHS: v.HashrateHs, PowerW: v.PowerW, TemperatureC: v.TemperatureC}
			}
			t.Modules = append(t.Modules, module)
		}
		out.Tanks = append(out.Tanks, t)
	}
	return out
}
