package sdk

import (
	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func containerSnapshotToProto(s *ContainerSnapshot) *pb.ContainerSnapshot {
	if s == nil {
		return nil
	}
	out := &pb.ContainerSnapshot{ObservedAt: timestamppb.New(s.ObservedAt)}
	if p := s.Plant; p != nil {
		out.Plant = &pb.ContainerPlant{ObservedAt: timestamppb.New(p.ObservedAt), TotalPowerW: p.TotalPowerW, FacilityPowerW: p.FacilityPowerW, Pue: p.PUE, InletTemperatureC: p.InletTemperatureC, OutletTemperatureC: p.OutletTemperatureC, AlarmActive: p.AlarmActive, CriticalAlarmActive: p.CriticalAlarmActive}
	}
	for _, tank := range s.Tanks {
		t := &pb.ContainerTank{Position: tank.Position}
		for _, m := range tank.Modules {
			t.Modules = append(t.Modules, &pb.ContainerModule{Position: m.Position, ReportedId: m.ReportedID, State: string(m.State), ObservedAt: timestamppb.New(m.ObservedAt)})
		}
		out.Tanks = append(out.Tanks, t)
	}
	return out
}
func containerSnapshotFromProto(s *pb.ContainerSnapshot) *ContainerSnapshot {
	if s == nil {
		return nil
	}
	out := &ContainerSnapshot{ObservedAt: s.ObservedAt.AsTime(), Tanks: []ContainerTank{}}
	if p := s.Plant; p != nil {
		out.Plant = &ContainerPlant{ObservedAt: p.ObservedAt.AsTime(), TotalPowerW: p.TotalPowerW, FacilityPowerW: p.FacilityPowerW, PUE: p.Pue, InletTemperatureC: p.InletTemperatureC, OutletTemperatureC: p.OutletTemperatureC, AlarmActive: p.AlarmActive, CriticalAlarmActive: p.CriticalAlarmActive}
	}
	for _, tank := range s.Tanks {
		t := ContainerTank{Position: tank.Position, Modules: []ContainerModule{}}
		for _, m := range tank.Modules {
			t.Modules = append(t.Modules, ContainerModule{Position: m.Position, ReportedID: m.ReportedId, State: ModuleState(m.State), ObservedAt: m.ObservedAt.AsTime()})
		}
		out.Tanks = append(out.Tanks, t)
	}
	return out
}
