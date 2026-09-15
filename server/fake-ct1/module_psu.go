package main

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
	power := 0.0
	warning := false
	voltage := boardVoltage
	if m != nil {
		if m.FixedVoltageSet {
			voltage = float64(m.FixedMillivolts) / 1000
		}
		for _, slot := range m.Slots {
			if slot.Board == nil {
				continue
			}
			if slot.Board.isMining() {
				power += slot.Board.targetWatts()
			}
			if s.Config.DemoModuleStates && m.ID == demoAttentionModule && slot.Board.State == miningStateError {
				warning = true
			}
		}
	}
	temperature := 50.0
	if warning {
		temperature = 80
	}
	return psuIdentity{Serial: "PSU-" + b.ModuleID, Vendor: "Proto simulator", Model: "CT1 PSU", PsuType: "CT1"},
		psuElectricalHealth{InputVoltage: 415, InputPower: power / 0.96, OutputVoltage: voltage, OutputPower: power},
		psuThermalHealth{InletTemperature: temperature, SrHeatsinkTemperature: temperature + 8,
			PfcHeatsinkTemperature: temperature + 5, OverTemperatureWarning: warning, ThermalStateValid: true}
}
