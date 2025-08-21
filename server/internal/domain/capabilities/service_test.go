package capabilities

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

func TestLoadConfig(t *testing.T) {
	testCapabilitiesPath := filepath.Join("../../..", "miner-configs", "capabilities.yaml")
	config, err := LoadCapabilities(testCapabilitiesPath)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Check that we loaded the correct number of miners
	require.Len(t, config.Miners, 3)

	// Verify that specific miners were loaded
	assert.NotNil(t, config.Miners["proto-rig"])
	assert.NotNil(t, config.Miners["bitmain-antminer-s19"])
	assert.NotNil(t, config.Miners["bitmain-antminer-s21"])

	// Check a few specific values
	assert.Equal(t, "Proto", config.Miners["proto-rig"].Manufacturer)
	assert.True(t, config.Miners["proto-rig"].Commands.CoolingModeSupported)
	assert.Len(t, config.Miners["proto-rig"].Commands.CoolingModesAvailable, 2)
	assert.Equal(t, int32(30), config.Miners["proto-rig"].Telemetry.PollingIntervalSecondsRecommended)
}

func TestService_GetCapabilitiesForDevice(t *testing.T) {
	testCapabilitiesPath := filepath.Join("../../..", "miner-configs", "capabilities.yaml")
	service, err := NewService(testCapabilitiesPath)
	require.NoError(t, err)

	exactMatchTests := []struct {
		name       string
		device     *pairingpb.Device
		expectedID string
	}{
		{
			name: "proto rig with manufacturer and model match",
			device: &pairingpb.Device{
				Model:        "Rig",
				Manufacturer: "Proto",
			},
			expectedID: "proto-rig",
		},
		{
			name: "proto rig with partial model match",
			device: &pairingpb.Device{
				Model:        "Rig V1",
				Manufacturer: "Proto",
			},
			expectedID: "proto-rig",
		},
		{
			name: "antminer s19 with exact model match",
			device: &pairingpb.Device{
				Model:        "Antminer S19",
				Manufacturer: "Bitmain",
			},
			expectedID: "bitmain-antminer-s19",
		},
		{
			name: "antminer s21 with exact model match",
			device: &pairingpb.Device{
				Model:        "Antminer S21",
				Manufacturer: "Bitmain",
			},
			expectedID: "bitmain-antminer-s21",
		},
		{
			name: "antminer S21 XP variant partial exact model match",
			device: &pairingpb.Device{
				Model:        "Antminer S21 XP",
				Manufacturer: "Bitmain",
			},
			expectedID: "bitmain-antminer-s21",
		},
	}

	for _, tt := range exactMatchTests {
		t.Run(tt.name, func(t *testing.T) {
			caps := service.GetCapabilitiesForDevice(t.Context(), tt.device)
			require.NotNil(t, caps)

			switch tt.expectedID {
			case "proto-rig":
				assert.Equal(t, "Proto", caps.Manufacturer)
				assert.True(t, caps.Commands.CoolingModeSupported)
				assert.Len(t, caps.Commands.CoolingModesAvailable, 2)
				assert.Equal(t, int32(30), caps.Telemetry.PollingIntervalSecondsRecommended)
			case "bitmain-antminer-s19", "bitmain-antminer-s21":
				assert.Equal(t, "Bitmain", caps.Manufacturer)
				assert.False(t, caps.Commands.CoolingModeSupported)
				assert.Empty(t, caps.Commands.CoolingModesAvailable)
				assert.Equal(t, int32(60), caps.Telemetry.PollingIntervalSecondsRecommended)
			}
		})
	}

	// Test cases where we expect nil capabilities
	nilTests := []struct {
		name   string
		device *pairingpb.Device
	}{
		{
			name:   "nil device returns nil",
			device: nil,
		},
		{
			name: "unknown miner returns nil",
			device: &pairingpb.Device{
				Model:        "unknown-miner",
				Manufacturer: "Unknown",
			},
		},
		{
			name: "empty model and manufacturer returns nil",
			device: &pairingpb.Device{
				Model:        "",
				Manufacturer: "",
			},
		},
	}

	for _, tt := range nilTests {
		t.Run(tt.name, func(t *testing.T) {
			caps := service.GetCapabilitiesForDevice(t.Context(), tt.device)
			assert.Nil(t, caps)
		})
	}
}
