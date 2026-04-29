package main

import (
	"testing"

	"github.com/block/proto-fleet/plugin/proto/internal/driver"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDriverDescribe tests driver capability reporting.
func TestDriverDescribe(t *testing.T) {
	driver, err := driver.New(443)
	require.NoError(t, err, "Failed to create driver")

	ctx := t.Context()
	handshake, caps, err := driver.DescribeDriver(ctx)
	require.NoError(t, err, "DescribeDriver should not return error")

	assert.Equal(t, "proto", handshake.DriverName, "Expected driver name 'proto'")

	// Check required capabilities
	requiredCaps := []string{
		sdk.CapabilityPollingHost,
		sdk.CapabilityDiscovery,
		sdk.CapabilityPairing,
		sdk.CapabilityPowerModeEfficiency,
		sdk.CapabilityCurtailFull,
		sdk.CapabilityCurtailEfficiency,
	}

	for _, cap := range requiredCaps {
		assert.True(t, caps[cap], "Expected capability '%s' to be true", cap)
	}
	assert.False(t, caps[sdk.CapabilityCurtailPartial], "partial curtailment should remain unsupported")

	assert.Equal(t, []string{"443"}, driver.GetDiscoveryPorts(ctx))
}

func TestDriverGetDiscoveryPorts_Override(t *testing.T) {
	driver, err := driver.New(8080)
	require.NoError(t, err, "Failed to create driver")

	assert.Equal(t, []string{"8080"}, driver.GetDiscoveryPorts(t.Context()))
}

func TestDriverGetDiscoveryPorts_NonCanonicalOverride(t *testing.T) {
	driver, err := driver.New(9000)
	require.NoError(t, err, "Failed to create driver")

	assert.Equal(t, []string{"9000"}, driver.GetDiscoveryPorts(t.Context()))
}

// TestDeviceInfoValidation tests device info validation.
func TestDeviceInfoValidation(t *testing.T) {
	tests := []struct {
		name       string
		deviceInfo sdk.DeviceInfo
		wantValid  bool
	}{
		{
			name: "invalid port",
			deviceInfo: sdk.DeviceInfo{
				Host:         "192.168.1.100",
				Port:         0,
				URLScheme:    "https",
				SerialNumber: "PROTO123456789",
				Model:        "Rig",
				Manufacturer: "Proto",
				MacAddress:   "00:11:22:33:44:55",
			},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := driver.New(443)
			require.NoError(t, err, "Failed to create driver")

			_, err = driver.NewDevice(t.Context(), tt.deviceInfo.SerialNumber, tt.deviceInfo, sdk.SecretBundle{})
			if tt.wantValid {
				require.NoError(t, err, "Expected valid device info to not return error")
			} else {
				require.Error(t, err, "Expected invalid device info to return error")
			}
		})
	}
}
