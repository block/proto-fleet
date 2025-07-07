package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceID_String_ShouldReturnStringRepresentation(t *testing.T) {
	deviceID := DeviceID("test-device-123")
	result := deviceID.String()
	assert.Equal(t, "test-device-123", result)
}

func TestDeviceID_WithEmptyString_ShouldReturnEmptyString(t *testing.T) {
	deviceID := DeviceID("")
	result := deviceID.String()
	assert.Equal(t, "", result)
}

func TestDeviceID_WithSpecialCharacters_ShouldReturnExactString(t *testing.T) {
	deviceID := DeviceID("device@#$%123")
	result := deviceID.String()
	assert.Equal(t, "device@#$%123", result)
}

func TestDeviceID_WithUnicodeCharacters_ShouldReturnExactString(t *testing.T) {
	deviceID := DeviceID("device-测试-123")
	result := deviceID.String()
	assert.Equal(t, "device-测试-123", result)
}

func TestDeviceID_Equality_ShouldWorkCorrectly(t *testing.T) {
	deviceID1 := DeviceID("test-device")
	deviceID2 := DeviceID("test-device")
	deviceID3 := DeviceID("different-device")

	assert.Equal(t, deviceID1, deviceID2)
	assert.NotEqual(t, deviceID1, deviceID3)
}

func TestType_String_ShouldReturnCorrectString(t *testing.T) {
	tests := []struct {
		input    Type
		expected string
	}{
		{TypeAntminer, "antminer"},
		{TypeProto, "proto"},
		{TypeWhatsminer, "whatsminer"},
		{TypeAvalon, "avalon"},
		{TypeUnknown, "unknown"},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, test.input.String())
	}
}

func TestTypeFromString_ShouldParseCorrectly(t *testing.T) {
	tests := []struct {
		input       string
		expected    Type
		shouldError bool
	}{
		{"antminer", TypeAntminer, false},
		{"proto", TypeProto, false},
		{"proto_miner", TypeProto, false}, // Legacy support
		{"whatsminer", TypeWhatsminer, false},
		{"avalon", TypeAvalon, false},
		{"unknown", TypeUnknown, false},
		{"", TypeUnknown, false},
		{"invalid", TypeUnknown, true},
	}

	for _, test := range tests {
		result, err := TypeFromString(test.input)
		if test.shouldError {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, test.expected, result)
		}
	}
}
