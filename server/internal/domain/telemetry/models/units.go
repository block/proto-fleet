package models

// Unit conversion constants: raw storage → API display format.
// Storage uses base SI units (H/s, W, J/H) while API uses scaled units (TH/s, kW, J/TH).
const (
	HsToThsConversion   = 1e12 // H/s to TH/s: 1 TH/s = 10^12 H/s
	WattsToKwConversion = 1e3  // W to kW: 1 kW = 10^3 W
	JhToJthConversion   = 1e12 // J/H to J/TH: multiply by 10^12 (inverse relationship)
)

// ConvertToDisplayUnits converts a measurement value from raw storage format to API display format.
// Storage format uses H/s for hashrate, W for power, J/H for efficiency.
// Display format uses TH/s for hashrate, kW for power, J/TH for efficiency.
// Temperature, fan speed, and other measurements pass through unchanged.
func ConvertToDisplayUnits(value float64, measurementType MeasurementType) float64 {
	switch measurementType {
	case MeasurementTypeHashrate:
		return value / HsToThsConversion
	case MeasurementTypePower:
		return value / WattsToKwConversion
	case MeasurementTypeEfficiency:
		return value * JhToJthConversion
	case MeasurementTypeUnknown, MeasurementTypeTemperature, MeasurementTypeFanSpeed,
		MeasurementTypeVoltage, MeasurementTypeCurrent, MeasurementTypeUptime, MeasurementTypeErrorRate:
		return value
	default:
		return value
	}
}
