package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldIncludeUptimeStatusCounts(t *testing.T) {
	tests := []struct {
		name             string
		measurementTypes []MeasurementType
		expected         bool
	}{
		{
			name:     "nil measurement list preserves default uptime counts",
			expected: true,
		},
		{
			name:             "empty measurement list preserves default uptime counts",
			measurementTypes: []MeasurementType{},
			expected:         true,
		},
		{
			name:             "explicit non-uptime measurements skip uptime counts",
			measurementTypes: []MeasurementType{MeasurementTypeHashrate, MeasurementTypePower},
			expected:         false,
		},
		{
			name:             "explicit uptime measurement includes uptime counts",
			measurementTypes: []MeasurementType{MeasurementTypeHashrate, MeasurementTypeUptime},
			expected:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ShouldIncludeUptimeStatusCounts(tt.measurementTypes))
		})
	}
}
