package interfaces

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSortConfig_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		config   *SortConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name:     "unspecified field",
			config:   &SortConfig{Field: SortFieldUnspecified, Direction: SortDirectionAsc},
			expected: false,
		},
		{
			name:     "unspecified direction",
			config:   &SortConfig{Field: SortFieldName, Direction: SortDirectionUnspecified},
			expected: false,
		},
		{
			name:     "valid config - hashrate",
			config:   &SortConfig{Field: SortFieldHashrate, Direction: SortDirectionDesc},
			expected: true,
		},
		{
			name:     "valid config - firmware",
			config:   &SortConfig{Field: SortFieldFirmware, Direction: SortDirectionDesc},
			expected: true,
		},
		{
			name:     "invalid config - location",
			config:   &SortConfig{Field: SortFieldLocation, Direction: SortDirectionAsc},
			expected: false,
		},
		{
			name:     "invalid config - issue count",
			config:   &SortConfig{Field: SortFieldIssueCount, Direction: SortDirectionDesc},
			expected: false,
		},
		{
			name:     "field out of range",
			config:   &SortConfig{Field: 100, Direction: SortDirectionAsc},
			expected: false,
		},
		{
			name:     "direction out of range",
			config:   &SortConfig{Field: SortFieldName, Direction: 100},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.config.IsValid())
		})
	}
}

func TestSortConfig_IsUnspecified(t *testing.T) {
	tests := []struct {
		name     string
		config   *SortConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: true,
		},
		{
			name:     "unspecified field",
			config:   &SortConfig{Field: SortFieldUnspecified},
			expected: true,
		},
		{
			name:     "specified field",
			config:   &SortConfig{Field: SortFieldName},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.config.IsUnspecified())
		})
	}
}

func TestSortConfig_IsTelemetrySort(t *testing.T) {
	tests := []struct {
		name     string
		config   *SortConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name:     "name field",
			config:   &SortConfig{Field: SortFieldName},
			expected: false,
		},
		{
			name:     "hashrate field",
			config:   &SortConfig{Field: SortFieldHashrate},
			expected: true,
		},
		{
			name:     "temperature field",
			config:   &SortConfig{Field: SortFieldTemperature},
			expected: true,
		},
		{
			name:     "power field",
			config:   &SortConfig{Field: SortFieldPower},
			expected: true,
		},
		{
			name:     "efficiency field",
			config:   &SortConfig{Field: SortFieldEfficiency},
			expected: true,
		},
		{
			name:     "firmware field",
			config:   &SortConfig{Field: SortFieldFirmware},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.config.IsTelemetrySort())
		})
	}
}

func TestSortConfig_String(t *testing.T) {
	tests := []struct {
		name     string
		config   *SortConfig
		expected string
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: "SortConfig{nil}",
		},
		{
			name:     "valid config",
			config:   &SortConfig{Field: SortFieldHashrate, Direction: SortDirectionDesc},
			expected: "SortConfig{Field:6, Direction:2}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.config.String())
		})
	}
}
