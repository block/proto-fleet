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
	assert.Equal(t, "v2", handshake.APIVersion, "Expected credentials-auth driver to report API version v2")

	// Check required capabilities
	requiredCaps := []string{
		sdk.CapabilityPollingHost,
		sdk.CapabilityDiscovery,
		sdk.CapabilityPairing,
		sdk.CapabilityPowerModeEfficiency,
		sdk.CapabilityCurtailFull,
		sdk.CapabilityCurtailEfficiency,
		sdk.CapabilityBasicAuth,
		sdk.CapabilityLogLevels,
	}

	for _, cap := range requiredCaps {
		assert.True(t, caps[cap], "Expected capability '%s' to be true", cap)
	}

	assert.Equal(t, []string{"443"}, driver.GetDiscoveryPorts(ctx))
}

// TestDriverGetDefaultCredentials verifies the driver advertises the factory
// default credentials used for auto-authentication during pairing.
func TestDriverGetDefaultCredentials(t *testing.T) {
	// Arrange
	d, err := driver.New(443)
	require.NoError(t, err)

	// Act
	creds := d.GetDefaultCredentials(t.Context(), "Proto", "")

	// Assert
	require.Len(t, creds, 1)
	assert.Equal(t, sdk.UsernamePassword{Username: "admin", Password: "proto"}, creds[0])
}

// TestNewDevice_MissingCredentialsRejected verifies an empty secret bundle is
// rejected rather than silently upgraded to factory-default credentials.
func TestNewDevice_MissingCredentialsRejected(t *testing.T) {
	// Arrange
	d, err := driver.New(443)
	require.NoError(t, err)
	deviceInfo := sdk.DeviceInfo{Host: "192.168.1.100", Port: 443, URLScheme: "https", SerialNumber: "PROTO123"}

	// Act
	_, err = d.NewDevice(t.Context(), deviceInfo.SerialNumber, deviceInfo, sdk.SecretBundle{})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials are required")
}

// TestNewDevice_EmptyPasswordRejected verifies an explicit empty password is
// rejected rather than silently upgraded to the factory default.
func TestNewDevice_EmptyPasswordRejected(t *testing.T) {
	// Arrange
	d, err := driver.New(443)
	require.NoError(t, err)
	deviceInfo := sdk.DeviceInfo{Host: "192.168.1.100", Port: 443, URLScheme: "https", SerialNumber: "PROTO123"}

	// Act
	_, err = d.NewDevice(t.Context(), deviceInfo.SerialNumber, deviceInfo,
		sdk.SecretBundle{Kind: sdk.UsernamePassword{Username: "admin", Password: ""}})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password is required")
}

func TestNewDevice_BearerTokenRejected(t *testing.T) {
	// Arrange
	d, err := driver.New(443)
	require.NoError(t, err)
	deviceInfo := sdk.DeviceInfo{Host: "192.168.1.100", Port: 443, URLScheme: "https", SerialNumber: "PROTO123"}

	// Act
	_, err = d.NewDevice(t.Context(), deviceInfo.SerialNumber, deviceInfo,
		sdk.SecretBundle{Kind: sdk.BearerToken{Token: "legacy-token"}})

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected UsernamePassword")
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
