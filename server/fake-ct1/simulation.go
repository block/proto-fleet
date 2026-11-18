package main

import (
	"fmt"
	"math"
	"time"
)

const psuEfficiency = 14.9 / 15.5

// All endpoints use this sampling clock. Values are functions of time, seed,
// location and controls, never of request order. Call while holding s.mu.
func (s *ContainerState) advance(now time.Time) {
	s.sampleTime = now.Truncate(5 * time.Second)
	s.observedAt = now
	s.advanceFeedMapping(now)
	for _, b := range s.eachBoard() {
		if b.PreviousState != b.State {
			b.StateChangedAt = s.sampleTime
			b.PreviousState = b.State
			b.DisconnectedSample = nil
			if b.State == miningStateDisconnected {
				sample := s.boardSample(b)
				b.DisconnectedSample = &sample
			}
		}
	}
}

func (s *ContainerState) wave(device int, period float64) float64 {
	return s.waveAt(s.sampleTime, device, period)
}

func (s *ContainerState) waveAt(at time.Time, device int, period float64) float64 {
	seconds := at.Sub(s.StartTime).Seconds()
	phase := float64(device)*2.399963 + float64(s.Config.Seed)*.731
	return .65*math.Sin(seconds/period+phase) + .35*math.Sin(seconds/(period*3.7)+phase*1.7)
}

type boardSample struct {
	Power, Hashrate, Voltage, BoardTemp, AsicTemp float64
	Timestamp                                     time.Time
}

func (s *ContainerState) boardSample(b *Hashboard) boardSample {
	if !s.mappingFeedEnabled(b.PowerFeedID) {
		return boardSample{Timestamp: s.sampleTime}
	}
	// Offline readings retain their last simulated sample, while sample age grows.
	if b.State == miningStateDisconnected && b.DisconnectedSample != nil {
		return *b.DisconnectedSample
	}
	at := s.sampleTime
	if b.State == miningStateDisconnected {
		at = b.StateChangedAt
	}
	id := (b.TankID*100+b.ModuleNum)*3 + b.SlotIndex
	v := boardVoltage + .2*s.waveAt(at, b.TankID*100+b.ModuleNum, 170)
	if m := s.modulesByID[b.ModuleID]; m.FixedVoltageSet {
		v = float64(m.FixedMillivolts) / 1000
	}
	temperature := boardTempC + 1.8*s.waveAt(at, id, 240)
	asicTemperature := asicTempC + 1.8*s.waveAt(at, id, 240)
	power, hr := 0.0, 0.0
	if b.isMining() {
		power = b.targetWatts() * (1 + .005*s.waveAt(at, id, 180))
		hr = power / (14.9 + .08*s.waveAt(at, id+19, 310))
	} else {
		decay := math.Exp(-math.Max(0, at.Sub(b.StateChangedAt).Seconds()) / 300)
		temperature = plantInletTempC + (temperature-plantInletTempC)*decay
		asicTemperature = plantInletTempC + (asicTemperature-plantInletTempC)*decay
	}
	return boardSample{power, hr, v, temperature, asicTemperature, at}
}

// Flow, pump readings and fan power are imposed demo conditions, not a
// calibrated hydraulic model. CU1 feed current remains unavailable.
func (s *ContainerState) containerTelemetry() containerTelemetryResponse {
	active, _ := s.boardCounts()
	mining := active > 0
	pump, cooler := s.ContainerControls["coolant_pump"], s.ContainerControls["dry_cooler"]
	fans := make([]plantCoolingFan, 0, len(dryCoolerFanIDs))
	fanPower := 0.0
	targetLoad := 0.0
	for _, b := range s.eachBoard() {
		if b.isMining() {
			targetLoad += b.targetWatts()
		}
	}
	fraction := targetLoad / (float64(max(1, s.BoardCount())) * boardPowerTargetW)
	speed := 80 + 32*(fraction-78.0/96)
	if !s.ContainerControls["dry_cooler_auto"] {
		speed = s.ContainerSetpoints["dry_cooler_manual_speed_percent"]
	}
	for i, id := range dryCoolerFanIDs {
		v := 0.0
		if cooler {
			v = math.Max(0, math.Min(100, speed*(1+s.wave(i+500, 240)/80)))
		}
		power := 3000 * math.Pow(v/80, 3)
		fans = append(fans, plantCoolingFan{ComponentID: id, PowerW: power, SpeedPct: v, AlarmActive: i == 3})
		fanPower += power
	}
	ac := 0.0
	feeds := make([]powerFeed, 0, len(s.Modules))
	pdus := make([]powerDistribution, 0, len(s.Tanks))
	voltage := 480 + 5*s.wave(600, 300)
	for _, tank := range s.Tanks {
		pdu := powerDistribution{ComponentID: fmt.Sprintf("pdu-%d", tank.ID)}
		for _, m := range tank.Modules {
			power := 0.0
			faulted := false
			for _, slot := range m.Slots {
				if slot.Board != nil {
					power += s.boardSample(slot.Board).Power
					faulted = faulted || slot.Board.State == miningStateError
				}
			}
			power /= psuEfficiency
			ac += power
			pdu.ActivePowerW += power
			if power > 0 {
				pdu.ActiveFeedCount++
			}
			feeds = append(feeds, powerFeed{PowerFeedID: fmt.Sprintf("pdu-%d-outlet-%02d", tank.ID, m.ID), Active: power > 0, Faulted: faulted, PowerW: power, VoltageV: voltage})
		}
		pdus = append(pdus, pdu)
	}
	pumpPower := 0.0
	if pump {
		pumpPower = 16000
	}
	// A nominal 50 Hz pump supplies a 100 kPa pressure rise. Keep a positive
	// static pressure when stopped, with zero speed and zero pressure differences.
	pumpSpeed, pumpPressureRise := 0.0, 0.0
	if pump {
		pumpSpeed = math.Max(0, s.ContainerSetpoints["coolant_pump_speed_hz"]*(1+.01*s.wave(706, 360)))
		pumpPressureRise = 100000 * math.Pow(pumpSpeed/50, 2)
	}
	pumpInletPressure := 190000.0
	pumpOutletPressure := pumpInletPressure + pumpPressureRise
	// Assign 80% of the demo loop's pressure drop to the cooler, with the
	// remaining 20% split across the connecting pipework. Fan state is independent.
	coolerInletPressure := pumpOutletPressure - .1*pumpPressureRise
	coolerOutletPressure := pumpInletPressure + .1*pumpPressureRise
	// A 1 kW controls allowance plus 0.5 kW for each enabled AC unit.
	other := 1000.0
	for _, name := range []string{"air_conditioner_1", "air_conditioner_2"} {
		if s.ContainerControls[name] {
			other += 500
		}
	}
	facility := ac + pumpPower + fanPower + other
	pue := 0.0
	if ac > 0 {
		pue = facility / ac
	}
	inlet := plantInletTempC + .5*s.wave(700, 360)
	if s.ContainerControls["dry_cooler_auto"] && cooler {
		inlet += s.ContainerSetpoints["dry_cooler_auto_temp_c"] - plantInletTempC
	}
	rise, flow := 0.0, 0.0
	if mining && pump && cooler {
		rise = 7.5 + .3*s.wave(701, 600) // leaves room for ±0.2°C outlet offsets
		flow = (ac + pumpPower) / 1000 * 60 / (.8 * 2 * rise)
	}
	outlet := inlet + rise
	outletA, outletB := outlet, outlet
	if rise > 0 {
		outletA -= .2
		outletB += .2
	}
	ambient := plantAmbientTempC + .5*s.wave(702, 900)
	pf := .99 + .005*s.wave(800, 360)
	phases := make([]phaseTel, 0, 3)
	for i, name := range []string{"PHASE_A", "PHASE_B", "PHASE_C"} {
		weight := []float64{.334, .333, .333}[i]
		power := facility * weight
		phases = append(phases, phaseTel{Phase: name, PowerW: power, VoltageV: voltage, CurrentA: power * math.Sqrt(3) / (voltage * pf)})
	}
	controls := make([]containerControl, 0, 6)
	for _, name := range []string{"air_conditioner_1", "air_conditioner_2", "coolant_pump", "dry_cooler", "dry_cooler_auto", "tank_led_light"} {
		controls = append(controls, containerControl{Name: name, Enabled: s.ContainerControls[name], StateKnown: true})
	}
	setpoints := make([]containerSetpoint, 0, len(containerSetpointDefaults))
	for _, point := range containerSetpointDefaults {
		setpoints = append(setpoints, s.containerSetpoint(point.Name))
	}
	return containerTelemetryResponse{
		Timestamp:          s.sampleTime,
		Alarms:             s.plantAlarmsDTO(),
		AlarmMuteSupported: true,
		Phases:             phases,
		CoolingFans:        fans,
		PowerDistributions: pdus,
		PowerFeeds:         feeds,
		Controls:           controls,
		Setpoints:          setpoints,
		Tank: tankTelemetry{
			State: tankState(mining), Powered: true,
			InletTemperature: inlet, OutletTemperature: outlet, AmbientTemperature: ambient,
			PumpActive: pump, CoolerActive: cooler, TotalWattage: ac,
		},
		Plant: plantTelemetry{
			// The dry cooler receives the mixed return from both tank outlets.
			DryCoolerInletTemperatureC: (outletA + outletB) / 2,
			OperatingState:             plantState(mining), Powered: true,
			InletTemperatureC: inlet, OutletTemperatureC: outlet,
			OutletATemperatureC: outletA, OutletBTemperatureC: outletB,
			AmbientTemperatureC: ambient, OutsideAmbientTemperatureC: ambient,
			AlarmActive:               len(s.plantAlarms) > 0,
			CriticalAlarmActive:       s.criticalAlarmActive(),
			BuzzerActive:              s.buzzerActive,
			InsideAmbientTemperatureC: 27 + s.wave(703, 900),
			CoolantPumpActive:         pump, HeatRejectionActive: cooler,
			CoolantPumpSpeedHz:            pumpSpeed,
			PumpInletPressurePa:           pumpInletPressure,
			PumpOutletPressurePa:          pumpOutletPressure,
			PumpPressureDiffPa:            pumpOutletPressure - pumpInletPressure,
			HeatRejectionInletPressurePa:  coolerInletPressure,
			HeatRejectionOutletPressurePa: coolerOutletPressure,
			HeatRejectionPressureDiffPa:   coolerInletPressure - coolerOutletPressure,
			TankALiquidLevelMm:            500 + 5*s.wave(704, 600), TankBLiquidLevelMm: 450 + 5*s.wave(705, 600),
			TotalPowerW: ac, HeatRejectionPowerW: fanPower, FacilityPowerW: facility,
			PowerFactor: pf, PUE: pue, FlowRateLpm: flow,
		},
	}
}
