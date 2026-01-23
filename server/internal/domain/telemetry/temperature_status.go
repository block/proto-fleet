package telemetry

// Temperature threshold constants in Celsius
const (
	// Temperature thresholds for status categorization
	TempColdMaxC     = 0.0  // Below 0°C = COLD
	TempOkMinC       = 0.0  // 0°C to 70°C = OK
	TempOkMaxC       = 70.0 // Upper bound for OK status
	TempHotMinC      = 70.0 // 70°C to 90°C = HOT
	TempHotMaxC      = 90.0 // Upper bound for HOT status
	TempCriticalMinC = 90.0 // Above 90°C = CRITICAL
)
