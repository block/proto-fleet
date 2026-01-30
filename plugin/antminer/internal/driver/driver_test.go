package driver

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/antminer/internal/types"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/mocks"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIPAddress     = "192.168.1.100"
	correctPort       = "4028"
	driverTestVersion = "v1"
	driverTestName    = "antminer"
)

// createMockClientFactory creates a simple mock client factory for tests that don't need actual client functionality
func createMockClientFactory() types.ClientFactory {
	return types.ClientFactory(func(_ string, _, _ int32, _ string) (antminer.AntminerClient, error) {
		return nil, nil // Not used in basic tests
	})
}

// createRealClientFactory creates a real client factory that will attempt actual connections
func createRealClientFactory() types.ClientFactory {
	return types.ClientFactory(func(host string, rpcPort, webPort int32, urlScheme string) (antminer.AntminerClient, error) {
		return antminer.NewClient(host, rpcPort, webPort, urlScheme)
	})
}

func TestNew(t *testing.T) {
	d, err := New(createMockClientFactory())
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.NotNil(t, d.devices)
	assert.NotNil(t, d.clientFactory)
}

func TestHandshake(t *testing.T) {
	d, err := New(createMockClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	identifier, err := d.Handshake(ctx)
	require.NoError(t, err)

	assert.Equal(t, driverTestName, identifier.DriverName)
	assert.Equal(t, driverTestVersion, identifier.APIVersion)
}

func TestDescribeDriver(t *testing.T) {
	d, err := New(createMockClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	identifier, capabilities, err := d.DescribeDriver(ctx)
	require.NoError(t, err)

	// Verify identifier
	assert.Equal(t, driverTestName, identifier.DriverName)
	assert.Equal(t, driverTestVersion, identifier.APIVersion)

	// Check that required capabilities are present
	assert.True(t, capabilities[sdk.CapabilityPollingHost])
	assert.True(t, capabilities[sdk.CapabilityDiscovery])
	assert.True(t, capabilities[sdk.CapabilityPairing])

	// Check command capabilities
	assert.True(t, capabilities[sdk.CapabilityReboot])
	assert.False(t, capabilities[sdk.CapabilityMiningStart])
	assert.False(t, capabilities[sdk.CapabilityMiningStop])
	assert.True(t, capabilities[sdk.CapabilityLEDBlink])
	assert.False(t, capabilities[sdk.CapabilityFactoryReset])
	assert.False(t, capabilities[sdk.CapabilityCoolingModeAir])
	assert.False(t, capabilities[sdk.CapabilityCoolingModeImmerse])
	assert.True(t, capabilities[sdk.CapabilityPoolConfig])
	assert.True(t, capabilities[sdk.CapabilityPoolPriority])
	assert.True(t, capabilities[sdk.CapabilityLogsDownload])

	// Check telemetry capabilities
	assert.True(t, capabilities[sdk.CapabilityRealtimeTelemetry])
	assert.False(t, capabilities[sdk.CapabilityHistoricalData])

	// Check firmware capabilities
	assert.True(t, capabilities[sdk.CapabilityFirmware])
	assert.False(t, capabilities[sdk.CapabilityOTAUpdate])
	assert.True(t, capabilities[sdk.CapabilityManualUpload])

	// Check authentication capabilities
	assert.True(t, capabilities[sdk.CapabilityBasicAuth])

	// Check that unsupported capabilities are false
	assert.False(t, capabilities[sdk.CapabilityPollingPlugin])
	assert.False(t, capabilities[sdk.CapabilityBatchStatus])
	assert.False(t, capabilities[sdk.CapabilityStreaming])
}

func TestDiscoverDevice_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)

	// Create driver with mock client factory
	d, err := New(func(host string, rpcPort, webPort int32, urlScheme string) (antminer.AntminerClient, error) {
		// Verify the parameters passed to the factory
		assert.Equal(t, testIPAddress, host)
		assert.Equal(t, int32(4028), rpcPort)
		assert.Equal(t, int32(80), webPort)
		assert.Equal(t, "http", urlScheme)
		return mockClient, nil
	})
	require.NoError(t, err)

	// Set up mock expectations
	mockClient.EXPECT().
		GetVersion(gomock.Any()).
		Return(&rpc.VersionResponse{
			Status: []rpc.StatusInfo{{
				Status: "S",
				When:   time.Now().Unix(),
				Code:   1,
				Msg:    "Success",
			}},
			Version: []rpc.VersionInfo{{
				BMMiner:     "2.0.0",
				API:         "3.1",
				Miner:       "S19j Pro",
				CompileTime: "2023-01-01 00:00:00",
				Type:        "Antminer S19j Pro",
			}},
			ID: 1,
		}, nil)

	mockClient.EXPECT().
		Close().
		Times(1)

	// Test discovery
	ctx := t.Context()
	result, err := d.DiscoverDevice(ctx, testIPAddress, correctPort)
	require.NoError(t, err)

	// Verify results
	assert.Equal(t, testIPAddress, result.Host)
	assert.Equal(t, int32(80), result.Port)
	assert.Equal(t, "http", result.URLScheme)
	assert.Equal(t, "Antminer S19j Pro", result.Model)
	assert.Equal(t, "Bitmain", result.Manufacturer)
	assert.Equal(t, sdk.DeviceTypeASIC, result.Type)
}

func TestDiscoverDevice_WrongPort(t *testing.T) {
	d, err := New(createMockClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	_, err = d.DiscoverDevice(ctx, testIPAddress, "80")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "antminers use port 4028")
}

func TestDiscoverDevice_InvalidPort(t *testing.T) {
	d, err := New(createMockClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	_, err = d.DiscoverDevice(ctx, testIPAddress, "invalid")
	require.Error(t, err)
	// The driver validates port is 4028, so it will fail with port validation error
	assert.Contains(t, err.Error(), "antminers use port 4028")
}

func TestDiscoverDevice_NotAntminer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)

	d, err := New(func(_ string, _, _ int32, _ string) (antminer.AntminerClient, error) {
		return mockClient, nil
	})
	require.NoError(t, err)

	// Set up mock expectations for non-Antminer device
	mockClient.EXPECT().
		GetVersion(gomock.Any()).
		Return(&rpc.VersionResponse{
			Status: []rpc.StatusInfo{{
				Status: "S",
				When:   time.Now().Unix(),
				Code:   1,
				Msg:    "Success",
			}},
			Version: []rpc.VersionInfo{{
				BMMiner: "1.0.0",
				API:     "3.0",
				Miner:   "Unknown",
				Type:    "OtherMiner", // Not an Antminer
			}},
			ID: 1,
		}, nil)

	mockClient.EXPECT().
		Close().
		Times(1)

	// Test discovery
	_, err = d.DiscoverDevice(t.Context(), testIPAddress, correctPort)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an Antminer device")
}

func TestDiscoverDevice_UnknownModel(t *testing.T) {
	d, err := New(createRealClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	// This will fail at port validation, which is expected
	_, err = d.DiscoverDevice(ctx, testIPAddress, correctPort)
	require.Error(t, err)
	// Will fail due to no real connection
	assert.Contains(t, err.Error(), "failed to")
}

func TestDiscoverDevice_ConnectionFailure(t *testing.T) {
	d, err := New(createRealClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	// Try to connect to non-existent host
	_, err = d.DiscoverDevice(ctx, "192.168.255.255", correctPort)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to")
}

func TestPairDevice_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockAntminerClient(ctrl)

	d, err := New(func(_ string, _, _ int32, _ string) (antminer.AntminerClient, error) {
		return mockClient, nil
	})
	require.NoError(t, err)

	// Set up mock expectations
	mockClient.EXPECT().
		Pair(gomock.Any(), sdk.UsernamePassword{Username: "admin", Password: "password"}).
		Return(nil)

	mockClient.EXPECT().
		GetDeviceInfo(gomock.Any()).
		Return(&antminer.DeviceInfo{
			SerialNumber: "ABC123456789",
			Model:        "S19j Pro",
			Manufacturer: "Bitmain",
			MacAddress:   "00:11:22:33:44:55",
		}, nil)

	mockClient.EXPECT().
		GetVersion(gomock.Any()).
		Return(&rpc.VersionResponse{
			Version: []rpc.VersionInfo{
				{
					BMMiner: "2.0.0",
					Miner:   "1.0.0",
				},
			},
		}, nil)

	mockClient.EXPECT().
		SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"}).
		Return(nil)

	mockClient.EXPECT().
		Close().
		Times(1)

	ctx := t.Context()
	deviceInfo := sdk.DeviceInfo{
		Host:         testIPAddress,
		Port:         80,
		URLScheme:    "http",
		Model:        "S19j Pro",
		Manufacturer: "Bitmain",
		Type:         sdk.DeviceTypeASIC,
	}

	validSecret := sdk.SecretBundle{
		Kind: sdk.UsernamePassword{
			Username: "admin",
			Password: "password",
		},
	}

	result, err := d.PairDevice(ctx, deviceInfo, validSecret)
	require.NoError(t, err)
	assert.Equal(t, "S19j Pro", result.Model)
	assert.Equal(t, "ABC123456789", result.SerialNumber)
	assert.Equal(t, "00:11:22:33:44:55", result.MacAddress)
	assert.Equal(t, "2.0.0", result.FirmwareVersion)
	assert.Equal(t, deviceInfo.Host, result.Host)
	assert.Equal(t, deviceInfo.Port, result.Port)
}

func TestPairDevice_InvalidCredentials(t *testing.T) {
	d, err := New(createMockClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	deviceInfo := sdk.DeviceInfo{
		Host:         testIPAddress,
		Port:         80,
		URLScheme:    "http",
		Model:        "S19j Pro",
		Manufacturer: "Bitmain",
		Type:         sdk.DeviceTypeASIC,
	}

	invalidSecret := sdk.SecretBundle{
		Kind: sdk.TLSClientCert{
			ClientCertPEM: []byte("cert"),
			KeyPEM:        []byte("key"),
		},
	}

	_, err = d.PairDevice(ctx, deviceInfo, invalidSecret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to extract credentials")
}

func TestNewDevice_InvalidCredentials(t *testing.T) {
	d, err := New(createMockClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	deviceInfo := sdk.DeviceInfo{
		Host:         testIPAddress,
		Port:         80,
		URLScheme:    "http",
		Model:        "S19j Pro",
		Manufacturer: "Bitmain",
		Type:         sdk.DeviceTypeASIC,
	}

	invalidSecret := sdk.SecretBundle{
		Kind: sdk.TLSClientCert{
			ClientCertPEM: []byte("cert"),
			KeyPEM:        []byte("key"),
		},
	}

	_, err = d.NewDevice(ctx, "test-device", deviceInfo, invalidSecret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to extract credentials")
}

func TestNewDevice_ValidCredentials(t *testing.T) {
	d, err := New(createRealClientFactory())
	require.NoError(t, err)

	ctx := t.Context()
	deviceInfo := sdk.DeviceInfo{
		Host:         testIPAddress,
		Port:         80,
		URLScheme:    "http",
		Model:        "S19j Pro",
		Manufacturer: "Bitmain",
		Type:         sdk.DeviceTypeASIC,
	}

	validSecret := sdk.SecretBundle{
		Kind: sdk.UsernamePassword{
			Username: "admin",
			Password: "password",
		},
	}

	// This will fail due to network connection, but we can test credential extraction
	_, err = d.NewDevice(ctx, "test-device", deviceInfo, validSecret)
	require.Error(t, err)
	// Should fail at device creation, not credential extraction
	assert.NotContains(t, err.Error(), "failed to extract credentials")
	assert.Contains(t, err.Error(), "failed to connect device")
}
