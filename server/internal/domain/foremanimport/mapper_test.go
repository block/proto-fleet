package foremanimport

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard Antminer with firmware",
			input:    "Antminer S21 Pro (vnish)",
			expected: "S21 Pro",
		},
		{
			name:     "standard Whatsminer",
			input:    "Whatsminer M60S",
			expected: "M60S",
		},
		{
			name:     "Antminer with capacity suffix",
			input:    "Antminer S21 (200T)",
			expected: "S21",
		},
		{
			name:     "AvalonMiner with capacity",
			input:    "AvalonMiner 1466 (153T)",
			expected: "1466",
		},
		{
			name:     "single word returns as-is",
			input:    "BitAxe",
			expected: "BitAxe",
		},
		{
			name:     "empty string returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "two words no parentheses",
			input:    "Teraflux AT1500",
			expected: "AT1500",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			result := parseModel(tc.input)

			// Assert
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPoolNameFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips port",
			input:    "mine.ocean.xyz:3334",
			expected: "mine.ocean.xyz",
		},
		{
			name:     "no port returns as-is",
			input:    "mine.ocean.xyz",
			expected: "mine.ocean.xyz",
		},
		{
			name:     "stratum prefix with port strips scheme and port",
			input:    "stratum+tcp://ca.stratum.braiins.com:3333",
			expected: "ca.stratum.braiins.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			result := poolNameFromURL(tc.input)

			// Assert
			assert.Equal(t, tc.expected, result)
		})
	}
}
