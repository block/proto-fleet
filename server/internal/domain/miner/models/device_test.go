package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeviceID_String_ShouldReturnStringRepresentation(t *testing.T) {
	deviceID := DeviceIdentifier("test-device-123")
	result := deviceID.String()
	assert.Equal(t, "test-device-123", result)
}

func TestDeviceID_WithEmptyString_ShouldReturnEmptyString(t *testing.T) {
	deviceID := DeviceIdentifier("")
	result := deviceID.String()
	assert.Equal(t, "", result)
}

func TestDeviceID_WithSpecialCharacters_ShouldReturnExactString(t *testing.T) {
	deviceID := DeviceIdentifier("device@#$%123")
	result := deviceID.String()
	assert.Equal(t, "device@#$%123", result)
}

func TestDeviceID_WithUnicodeCharacters_ShouldReturnExactString(t *testing.T) {
	deviceID := DeviceIdentifier("device-测试-123")
	result := deviceID.String()
	assert.Equal(t, "device-测试-123", result)
}

func TestDeviceID_Equality_ShouldWorkCorrectly(t *testing.T) {
	deviceID1 := DeviceIdentifier("test-device")
	deviceID2 := DeviceIdentifier("test-device")
	deviceID3 := DeviceIdentifier("different-device")

	assert.Equal(t, deviceID1, deviceID2)
	assert.NotEqual(t, deviceID1, deviceID3)
}
