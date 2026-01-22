package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertToDisplayUnits(t *testing.T) {
	tests := []struct {
		name            string
		value           float64
		measurementType MeasurementType
		expected        float64
	}{
		{
			name:            "hashrate converts from H/s to TH/s",
			value:           1e12,
			measurementType: MeasurementTypeHashrate,
			expected:        1.0,
		},
		{
			name:            "hashrate converts fractional values",
			value:           500e9,
			measurementType: MeasurementTypeHashrate,
			expected:        0.5,
		},
		{
			name:            "power converts from W to kW",
			value:           3000,
			measurementType: MeasurementTypePower,
			expected:        3.0,
		},
		{
			name:            "power converts fractional values",
			value:           1500,
			measurementType: MeasurementTypePower,
			expected:        1.5,
		},
		{
			name:            "efficiency converts from J/H to J/TH",
			value:           30e-12,
			measurementType: MeasurementTypeEfficiency,
			expected:        30.0,
		},
		{
			name:            "temperature passes through unchanged",
			value:           75.5,
			measurementType: MeasurementTypeTemperature,
			expected:        75.5,
		},
		{
			name:            "fan speed passes through unchanged",
			value:           6000,
			measurementType: MeasurementTypeFanSpeed,
			expected:        6000,
		},
		{
			name:            "voltage passes through unchanged",
			value:           12000,
			measurementType: MeasurementTypeVoltage,
			expected:        12000,
		},
		{
			name:            "current passes through unchanged",
			value:           5000,
			measurementType: MeasurementTypeCurrent,
			expected:        5000,
		},
		{
			name:            "uptime passes through unchanged",
			value:           3600,
			measurementType: MeasurementTypeUptime,
			expected:        3600,
		},
		{
			name:            "error rate passes through unchanged",
			value:           0.05,
			measurementType: MeasurementTypeErrorRate,
			expected:        0.05,
		},
		{
			name:            "unknown type passes through unchanged",
			value:           42.0,
			measurementType: MeasurementTypeUnknown,
			expected:        42.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange - values set in test case

			// Act
			result := ConvertToDisplayUnits(tt.value, tt.measurementType)

			// Assert
			assert.InDelta(t, tt.expected, result, 1e-9)
		})
	}
}
