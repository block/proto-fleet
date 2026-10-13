package ct1

import (
	"encoding/json"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sourceTime(v *timestamppb.Timestamp) time.Time {
	if v == nil || v.CheckValid() != nil {
		return time.Time{}
	}
	return v.AsTime()
}

func mapResponse(v *mdk.GetContainerMapResponse) MapResponse {
	if v.GetContainerMap() == nil {
		return MapResponse{}
	}
	out := MapResponse{ContainerMap: &ContainerMap{}}
	for _, tank := range v.ContainerMap.Tanks {
		expected := int64(tank.GetModuleLayout().GetRows()) * int64(tank.GetModuleLayout().GetColumns())
		t := Tank{ID: tank.GetId(), ExpectedModules: -1}
		if expected >= 0 && expected <= sdk.CT1ModulesPerTank {
			t.ExpectedModules = int32(expected)
		} // CT1 has 16 module positions per tank.
		for _, module := range tank.GetModules() {
			m := Module{ID: module.GetId(), Slots: make([]Slot, boardsPerModule)}
			for i := range m.Slots {
				m.Slots[i].unreported = true
			}
			for _, slot := range module.GetSlots() {
				b := boardFromProto(slot.GetHashboard())
				location := slot.GetHashboard().GetLocation()
				configuredLocation := slot.GetConfiguredLocation()
				feed := &sdk.ContainerFeedEvidence{PowerFeedID: slot.GetPowerFeedId(), ObservedPowerFeedID: location.GetPowerFeedId(), ExpectedPowerFeedID: location.GetExpectedPowerFeedId(), WiringStatus: location.GetWiringStatus(), WiringReasons: location.GetWiringReasons(), ConfiguredTank: configuredLocation.GetTank(), ConfiguredModule: configuredLocation.GetModule()}
				value := Slot{Feed: feed, Occupied: slot.GetOccupied(), Hashboard: b, invalid: b != nil && b.raw == nil}
				position := slot.GetHashboard().GetLocation().GetHashboardPosition()
				configured := slot.GetConfiguredLocation().GetHashboardPosition()
				if position == 0 {
					position = configured
				}
				if position < 1 || position > boardsPerModule {
					// Preserve identity claims without inventing physical placement.
					value.invalid = true
					m.Slots = append(m.Slots, value)
					continue
				}
				if configured != 0 && configured != position {
					value.invalid = true
				}
				index := int(position) - 1
				if !m.Slots[index].unreported {
					m.Slots = append(m.Slots, value)
					continue
				}
				m.Slots[index] = value
			}
			t.Modules = append(t.Modules, m)
		}
		out.ContainerMap.Tanks = append(out.ContainerMap.Tanks, t)
	}
	return out
}

func boardFromProto(v *mdk.Hashboard) *Board {
	if v == nil {
		return nil
	}
	b := &Board{MACAddress: v.MacAddress, Serial: v.Serial, ModuleID: v.ModuleId, MiningState: v.MiningState.String(), Source: v.Source.String(), Isolated: v.Isolated}
	// Diagnostic JSON keeps MDK field names/units; typed normalization below does
	// not depend on this representation. Explicit defaults preserve false evidence.
	data, err := (protojson.MarshalOptions{EmitDefaultValues: true}).Marshal(v)
	if err != nil || sdk.ValidateHashboardJSON(data) != nil {
		return b
	}
	if json.Unmarshal(data, &b.raw) != nil {
		return b
	}
	if h := v.Health; h != nil {
		b.Health = &Health{Timestamp: sourceTime(h.Timestamp), AverageAsicTemperatureC: h.AverageAsicTemperatureC,
			HashrailOK: &h.HashrailOk, RefclksOK: &h.RefclksOk}
		// The board scalar has no presence bit. Positive values prove a reading;
		// ambiguous default zero remains unavailable until upstream supplies validity.
		if h.Power != 0 {
			b.Health.Power = &h.Power
		}
		if h.Hashrate != 0 {
			b.Health.Hashrate = &h.Hashrate
		}
		if e := h.LastShutdownError; e != nil {
			b.Health.LastShutdownError = &Issue{Cause: e.Cause.String()}
		}
		for _, e := range h.Errors {
			b.Health.Errors = append(b.Health.Errors, Issue{Cause: e.GetCause().String()})
		}
		for _, e := range h.Warnings {
			b.Health.Warnings = append(b.Health.Warnings, Issue{Cause: e.GetCause().String()})
		}
		if p := h.PsuElectrical; p != nil {
			// PsuElectricalHealth is a complete snapshot when present, per the pinned contract.
			age := float64(p.SampleAgeMs)
			b.Health.PSUElectrical = &PSUElectrical{InputPower: &p.InputPower, SampleAgeMs: &age}
		}
		if p := h.PsuThermal; p != nil {
			b.Health.PSUThermal = &PSUThermal{ThermalStateValid: p.ThermalStateValid, OverTemperatureFault: p.OverTemperatureFault, OverTemperatureWarning: p.OverTemperatureWarning}
			if p.ThermalStateAgeMs != nil {
				b.Health.PSUThermal.ThermalStateAgeMs = new(float64(*p.ThermalStateAgeMs))
			}
		}
	}
	return b
}

func plantObservation(v *mdk.GetContainerHealthResponse) plantResponse {
	out := plantResponse{Timestamp: sourceTime(v.GetTimestamp())}
	if p := v.GetPlant(); p != nil {
		out.Plant = &plantFields{DryCoolerInletTemperatureC: p.DryCoolerInletTemperatureC, TotalPowerW: p.TotalPowerW, FacilityPowerW: p.FacilityPowerW, PUE: p.Pue, InletTemperatureC: p.InletTemperatureC, OutletTemperatureC: p.OutletTemperatureC, AlarmActive: p.GetAlarmActive(), CriticalAlarmActive: p.GetCriticalAlarmActive()}
	}
	for _, f := range v.GetCoolingFans() {
		fan := fanResponse{ComponentID: f.GetComponentId(), SpeedPercent: f.SpeedPercent, PowerW: f.PowerW, AlarmActive: f.AlarmActive}
		if f.Direction != nil {
			fan.Direction = new(f.Direction.String())
		}
		out.CoolingFans = append(out.CoolingFans, fan)
	}
	return out
}

func controlObservation(v *mdk.GetContainerControlsResponse) controlsResponse {
	if v == nil {
		return controlsResponse{}
	}
	out := controlsResponse{Supported: &v.Supported, MainCircuitBreakerSupported: v.MainCircuitBreakerSupported, MainCircuitBreakerClosed: v.MainCircuitBreakerClosed, AlarmResetSupported: v.AlarmResetSupported, AlarmMuteSupported: v.AlarmMuteSupported, DryCoolerFanReverseSupported: v.DryCoolerFanReverseSupported}
	for _, c := range v.Controls {
		out.Controls = append(out.Controls, controlFields{Name: c.GetName(), Enabled: c.GetEnabled(), StateKnown: c.GetStateKnown()})
	}
	for _, s := range v.Setpoints {
		out.Setpoints = append(out.Setpoints, sdk.ContainerSetpoint{Name: s.GetName(), Value: new(s.GetValue()), Unit: s.GetUnit()})
	}
	return out
}
