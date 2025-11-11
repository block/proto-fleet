package errors

// ErrorDefinition defines a type of error that can be triggered
type ErrorDefinition struct {
	Code         string                 `json:"code"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Category     string                 `json:"category"`
	DefaultLevel string                 `json:"default_level"` // Error or Warning
	Parameters   []ParameterDefinition  `json:"parameters"`
	Example      map[string]interface{} `json:"example,omitempty"`
}

// ParameterDefinition defines a parameter for an error type
type ParameterDefinition struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // number, string, boolean, array
	Required    bool        `json:"required"`
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
}

// GetErrorDefinitions returns all available error definitions
// These are generated from miner-firmware source code
func GetErrorDefinitions() []ErrorDefinition {
	// Use the generated definitions from Rust source
	return GetGeneratedErrorDefinitions()
}

// GetManualErrorDefinitions returns manually defined error definitions
// DEPRECATED: Use GetErrorDefinitions() which returns generated definitions
func GetManualErrorDefinitions() []ErrorDefinition {
	return []ErrorDefinition{
		// Hashboard Errors
		{
			Code:         "HashboardOverheat",
			Name:         "Hashboard Overheat",
			Description:  "Hashboard temperature exceeds safe operating limits",
			Category:     "Hashboard",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "hb_slot", Type: "number", Required: true, Description: "Hashboard slot number"},
				{Name: "hb_sn", Type: "string", Required: true, Description: "Hashboard serial number"},
				{Name: "temperature", Type: "number", Required: true, Description: "Temperature in Celsius"},
			},
		},
		{
			Code:         "HashboardPowerLost",
			Name:         "Hashboard Power Lost",
			Description:  "Hashboard has lost power",
			Category:     "Hashboard",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "hb_slot", Type: "number", Required: true, Description: "Hashboard slot number"},
				{Name: "hb_sn", Type: "string", Required: true, Description: "Hashboard serial number"},
			},
		},
		{
			Code:         "HashboardUsbConnectionLost",
			Name:         "Hashboard USB Connection Lost",
			Description:  "Lost USB connection to hashboard",
			Category:     "Hashboard",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "hb_slot", Type: "number", Required: true, Description: "Hashboard slot number"},
				{Name: "hb_sn", Type: "string", Required: true, Description: "Hashboard serial number"},
			},
		},

		// ASIC Errors
		{
			Code:         "AsicEnumeration",
			Name:         "ASIC Enumeration Failure",
			Description:  "Failed to enumerate expected number of ASICs",
			Category:     "ASIC",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "hb_slot", Type: "number", Required: true, Description: "Hashboard slot number"},
				{Name: "hb_sn", Type: "string", Required: true, Description: "Hashboard serial number"},
			},
		},
		{
			Code:         "AsicOverTemp",
			Name:         "ASIC Over Temperature",
			Description:  "ASIC temperature exceeds safe operating limits",
			Category:     "ASIC",
			DefaultLevel: "Warning",
			Parameters: []ParameterDefinition{
				{Name: "hb_slot", Type: "number", Required: true, Description: "Hashboard slot number"},
				{Name: "hb_sn", Type: "string", Required: true, Description: "Hashboard serial number"},
				{Name: "asic_index", Type: "number", Required: true, Description: "ASIC index"},
				{Name: "temperature", Type: "number", Required: true, Description: "Temperature in Celsius"},
			},
		},
		{
			Code:         "AsicNotHashing",
			Name:         "ASICs Not Hashing",
			Description:  "One or more ASICs are not producing valid hashes",
			Category:     "ASIC",
			DefaultLevel: "Warning",
			Parameters: []ParameterDefinition{
				{Name: "hb_slot", Type: "number", Required: true, Description: "Hashboard slot number"},
				{Name: "hb_sn", Type: "string", Required: true, Description: "Hashboard serial number"},
				{Name: "asics", Type: "array", Required: true, Description: "Array of non-hashing ASIC indices"},
			},
		},

		// PSU Errors
		{
			Code:         "PSUHardwareFault",
			Name:         "PSU Hardware Fault",
			Description:  "Power supply unit has detected a hardware fault",
			Category:     "PSU",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "psu_index", Type: "number", Required: false, Description: "PSU index", Default: 0},
				{Name: "fault_type", Type: "string", Required: true, Description: "Type of fault"},
			},
		},
		{
			Code:         "PSUCommunicationLost",
			Name:         "PSU Communication Lost",
			Description:  "Lost communication with power supply unit",
			Category:     "PSU",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "psu_index", Type: "number", Required: false, Description: "PSU index", Default: 0},
			},
		},

		// Cooling Errors
		{
			Code:         "FanSlow",
			Name:         "Fan Running Slow",
			Description:  "Fan RPM is below expected speed",
			Category:     "Cooling",
			DefaultLevel: "Warning",
			Parameters: []ParameterDefinition{
				{Name: "fan_bay_index", Type: "number", Required: true, Description: "Fan bay index"},
				{Name: "fan_id", Type: "number", Required: true, Description: "Fan identifier"},
				{Name: "fan_pwm_target_pct", Type: "number", Required: true, Description: "Target PWM percentage"},
				{Name: "fan_rpm_tach", Type: "number", Required: true, Description: "Actual fan RPM from tachometer"},
			},
		},
		{
			Code:         "FanNotSpinning",
			Name:         "Fan Not Spinning",
			Description:  "Fan has stopped spinning",
			Category:     "Cooling",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "fan_bay_index", Type: "number", Required: true, Description: "Fan bay index"},
				{Name: "fan_id", Type: "number", Required: true, Description: "Fan identifier"},
				{Name: "fan_pwm_target_pct", Type: "number", Required: true, Description: "Target PWM percentage"},
				{Name: "fan_rpm_tach", Type: "number", Required: true, Description: "Actual fan RPM from tachometer"},
			},
		},
		{
			Code:         "InsufficientCooling",
			Name:         "Insufficient Cooling",
			Description:  "Not enough operational fans for adequate cooling",
			Category:     "Cooling",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "bay_index", Type: "number", Required: true, Description: "Bay index"},
				{Name: "num_operational_fans", Type: "number", Required: true, Description: "Number of operational fans"},
				{Name: "num_expected_fans", Type: "number", Required: true, Description: "Number of expected fans"},
				{Name: "failed_fans", Type: "array", Required: true, Description: "Array of failed fan IDs"},
				{Name: "required_fans", Type: "array", Required: true, Description: "Array of required fan IDs"},
			},
		},

		// Pool Errors
		{
			Code:         "PoolConnectionLost",
			Name:         "Pool Connection Lost",
			Description:  "Lost connection to mining pool",
			Category:     "Pool",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "pool_id", Type: "number", Required: false, Description: "Pool ID", Default: 1},
				{Name: "pool_url", Type: "string", Required: false, Description: "Pool URL"},
			},
		},
		{
			Code:         "NoPoolConfigured",
			Name:         "No Pool Configured",
			Description:  "No mining pool has been configured",
			Category:     "Pool",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{},
		},

		// System Errors
		{
			Code:         "MixedHashboardTypes",
			Name:         "Mixed Hashboard Types",
			Description:  "Different hashboard types detected in the same system",
			Category:     "System",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "types", Type: "array", Required: false, Description: "Array of detected hashboard types"},
			},
		},
		{
			Code:         "NetworkInterfaceDown",
			Name:         "Network Interface Down",
			Description:  "Network interface is not operational",
			Category:     "System",
			DefaultLevel: "Error",
			Parameters: []ParameterDefinition{
				{Name: "interface", Type: "string", Required: false, Description: "Interface name", Default: "eth0"},
			},
		},
	}
}

// GetErrorByCode returns an error definition by its code
func GetErrorByCode(code string) *ErrorDefinition {
	for _, def := range GetErrorDefinitions() {
		if def.Code == code {
			return &def
		}
	}
	return nil
}

// GetErrorCategories returns all unique error categories
func GetErrorCategories() []string {
	categoryMap := make(map[string]bool)
	for _, def := range GetErrorDefinitions() {
		categoryMap[def.Category] = true
	}

	categories := make([]string, 0, len(categoryMap))
	for cat := range categoryMap {
		categories = append(categories, cat)
	}
	return categories
}