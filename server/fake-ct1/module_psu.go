package main

import "math"

// The module's three boards expose the same physical PSU snapshot.
type psuIdentity struct {
	Serial  string `json:"serial"`
	Vendor  string `json:"vendor"`
	Model   string `json:"model"`
	PsuType string `json:"psuType"`
}

type psuElectricalHealth struct {
	InputVoltage  float64 `json:"inputVoltage"`
	InputPower    float64 `json:"inputPower"`
	OutputVoltage float64 `json:"outputVoltage"`
	OutputPower   float64 `json:"outputPower"`
	SampleAgeMs   int     `json:"sampleAgeMs"`
}

type psuThermalHealth struct {
	InletTemperature       float64 `json:"inletTemperature"`
	SrHeatsinkTemperature  float64 `json:"srHeatsinkTemperature"`
	PfcHeatsinkTemperature float64 `json:"pfcHeatsinkTemperature"`
	OverTemperatureWarning bool    `json:"overTemperatureWarning"`
	OverTemperatureFault   bool    `json:"overTemperatureFault"`
	ThermalStateValid      bool    `json:"thermalStateValid"`
	TemperaturesAgeMs      int     `json:"temperaturesAgeMs"`
	ThermalStateAgeMs      int     `json:"thermalStateAgeMs"`
}

func (s *ContainerState) modulePsu(b *Hashboard) (psuIdentity, psuElectricalHealth, psuThermalHealth) {
	m := s.modulesByID[b.ModuleID]
	power, warning, valid := 0.0, false, true
	age := 0
	for _, slot := range m.Slots {
		if slot.Board == nil {
			continue
		}
		sample := s.boardSample(slot.Board)
		power += sample.Power
		age = max(age, int(math.Max(0, s.observedAt.Sub(sample.Timestamp).Seconds()*1000)))
		valid = valid && slot.Board.State != miningStateDisconnected
		warning = warning || (s.Config.DemoModuleStates && m.TankID == demoFaultTank && m.ID == demoAttentionModule && slot.Board.State == miningStateError)
	}
	sample := s.boardSample(m.Slots[0].Board)
	inlet, sr, pfc := 29.0, 51.0, 45.0
	if power == 0 {
		decay := math.Exp(-math.Max(0, sample.Timestamp.Sub(m.Slots[0].Board.StateChangedAt).Seconds()) / 300)
		sr = 29 + (sr-29)*decay
		pfc = 29 + (pfc-29)*decay
	}
	variation := 2 * s.waveAt(sample.Timestamp, m.TankID*100+m.ID, 300)
	inlet += variation
	sr += variation
	pfc += variation
	if warning {
		inlet, sr, pfc = 80, 88, 85
	}
	return psuIdentity{Serial: "PSU-" + b.ModuleID, Vendor: "Proto simulator", Model: "CT1 PSU", PsuType: "CT1"},
		psuElectricalHealth{InputVoltage: 480 + 5*s.waveAt(sample.Timestamp, 600, 300), InputPower: power / psuEfficiency, OutputVoltage: sample.Voltage, OutputPower: power, SampleAgeMs: age},
		psuThermalHealth{InletTemperature: inlet, SrHeatsinkTemperature: sr, PfcHeatsinkTemperature: pfc, OverTemperatureWarning: warning, ThermalStateValid: valid, TemperaturesAgeMs: age, ThermalStateAgeMs: age}
}
