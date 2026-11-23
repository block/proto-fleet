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
			name:            "power converts from W to kW",
			value:           3000,
			measurementType: MeasurementTypePower,
			expected:        3.0,
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
