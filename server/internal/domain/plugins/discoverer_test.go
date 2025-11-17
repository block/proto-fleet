package plugins

import (
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	"github.com/btc-mining/proto-fleet/server/sdk/v1/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDiscoverer(t *testing.T) {
	manager := NewManager(&Config{})
	minerType := models.TypeAntminer

	discoverer := NewDiscoverer(manager, minerType)

	assert.NotNil(t, discoverer)
	assert.Equal(t, manager, discoverer.manager)
	assert.Equal(t, minerType, discoverer.minerType)
}

func TestDiscoverer_GetMinerType(t *testing.T) {
	manager := NewManager(&Config{})
	minerType := models.TypeAntminer

	discoverer := NewDiscoverer(manager, minerType)

	assert.Equal(t, minerType, discoverer.GetMinerType())
}

func TestDiscoverer_Discover_NoPlugin(t *testing.T) {
	manager := NewManager(&Config{})
	discoverer := NewDiscoverer(manager, models.TypeAntminer)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin available for miner type")
}

func TestDiscoverer_Discover_PluginNoDiscoveryCapability(t *testing.T) {
	manager := NewManager(&Config{})

	// Add mock plugin without discovery capability
	mockPlugin := &LoadedPlugin{
		Name: "test-plugin",
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true, // Has pairing but not discovery
		},
	}
	manager.pluginsByType[models.TypeAntminer] = mockPlugin

	discoverer := NewDiscoverer(manager, models.TypeAntminer)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support discovery")
}

func TestNewMultiTypeDiscoverer(t *testing.T) {
	manager := NewManager(&Config{})

	discoverer := NewMultiTypeDiscoverer(manager)

	assert.NotNil(t, discoverer)
	assert.Equal(t, manager, discoverer.manager)
}

func TestMultiTypeDiscoverer_GetMinerType(t *testing.T) {
	manager := NewManager(&Config{})
	discoverer := NewMultiTypeDiscoverer(manager)

	assert.Equal(t, models.TypeUnknown, discoverer.GetMinerType())
}

func TestMultiTypeDiscoverer_Discover_NoPlugins(t *testing.T) {
	manager := NewManager(&Config{})
	discoverer := NewMultiTypeDiscoverer(manager)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugins available for discovery")
}

func TestMultiTypeDiscoverer_Discover_NoDiscoveryCapablePlugins(t *testing.T) {
	manager := NewManager(&Config{})

	// Add mock plugin without discovery capability
	mockPlugin := &LoadedPlugin{
		Name: "test-plugin",
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true, // Has pairing but not discovery
		},
		MinerTypes: []models.Type{models.TypeAntminer},
	}
	manager.plugins["test-plugin"] = mockPlugin

	discoverer := NewMultiTypeDiscoverer(manager)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugins with discovery capability available")
}

func TestMultiTypeDiscoverer_Discover_Success(t *testing.T) {
	// Test different device types and manufacturers to ensure private functions work correctly
	testCases := []struct {
		name         string
		deviceInfo   sdk.DeviceInfo
		expectedType string
	}{
		{
			name: "ASIC device with explicit type",
			deviceInfo: sdk.DeviceInfo{
				Type:         sdk.DeviceTypeASIC,
				SerialNumber: "TEST123",
				Model:        "S19",
				Manufacturer: "Bitmain",
				URLScheme:    "http",
				MacAddress:   "00-11-22-33-44-55",
			},
			expectedType: DeviceTypeASIC,
		},
		{
			name: "Unspecified type with Bitmain manufacturer",
			deviceInfo: sdk.DeviceInfo{
				Type:         sdk.DeviceTypeUnspecified,
				SerialNumber: "BITMAIN123",
				Model:        "S19 Pro",
				Manufacturer: "Bitmain",
				URLScheme:    "https",
				MacAddress:   "00-11-22-33-44-66",
			},
			expectedType: DeviceTypeAntminer,
		},
		{
			name: "Model-based detection for Antminer",
			deviceInfo: sdk.DeviceInfo{
				Type:         sdk.DeviceTypeUnspecified,
				SerialNumber: "MODEL123",
				Model:        "Antminer S19 Pro",
				Manufacturer: "", // Empty manufacturer
				URLScheme:    "http",
				MacAddress:   "00-11-22-33-44-88",
			},
			expectedType: DeviceTypeAntminer,
		},
		{
			name: "Unknown manufacturer and model fallback",
			deviceInfo: sdk.DeviceInfo{
				Type:         sdk.DeviceTypeUnspecified,
				SerialNumber: "UNKNOWN123",
				Model:        "Unknown Model",
				Manufacturer: "Unknown Manufacturer",
				URLScheme:    "http",
				MacAddress:   "00-11-22-33-44-99",
			},
			expectedType: DeviceTypeUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Create fresh manager for each test
			manager := NewManager(&Config{})

			// Create mock driver with expectations
			mockDriver := mocks.NewMockDriver(ctrl)
			mockDriver.EXPECT().
				DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
				Return(tc.deviceInfo, nil)

			// Add mock plugin with discovery capability
			mockPlugin := &LoadedPlugin{
				Name:   "test-plugin",
				Driver: mockDriver,
				Caps: sdk.Capabilities{
					sdk.CapabilityDiscovery: true,
				},
				MinerTypes: []models.Type{models.TypeAntminer},
			}
			manager.plugins["test-plugin"] = mockPlugin

			discoverer := NewMultiTypeDiscoverer(manager)

			ctx := t.Context()
			device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

			require.NoError(t, err)
			require.NotNil(t, device)

			// Verify the device type determination worked correctly
			assert.Equal(t, tc.expectedType, device.Type)
			assert.Equal(t, "192.168.1.100", device.IpAddress)
			assert.Equal(t, "80", device.Port)
			assert.Equal(t, tc.deviceInfo.SerialNumber, device.SerialNumber)
			assert.Equal(t, tc.deviceInfo.Model, device.Model)
			assert.Equal(t, tc.deviceInfo.Manufacturer, device.Manufacturer)
			assert.Equal(t, tc.deviceInfo.MacAddress, device.MacAddress)
			assert.Equal(t, tc.deviceInfo.URLScheme, device.UrlScheme)
			assert.Equal(t, int64(0), device.OrgID)
			assert.False(t, device.FirstDiscovered.IsZero())
			assert.False(t, device.LastSeen.IsZero())
		})
	}
}

func TestMultiTypeDiscoverer_Discover_FirstPluginFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	// Create successful mock device info
	successDeviceInfo := sdk.DeviceInfo{
		Type:         sdk.DeviceTypeASIC,
		SerialNumber: "SUCCESS123",
		Model:        "S19",
		Manufacturer: "Bitmain",
		URLScheme:    "http",
		MacAddress:   "00:11:22:33:44:55",
	}

	// Create failing mock driver - use AnyTimes() since iteration order is not guaranteed
	failingDriver := mocks.NewMockDriver(ctrl)
	failingDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(sdk.DeviceInfo{}, fleeterror.NewInternalError("mock discovery failure")).
		AnyTimes()

	// Create successful mock driver - use AnyTimes() since iteration order is not guaranteed
	successDriver := mocks.NewMockDriver(ctrl)
	successDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(successDeviceInfo, nil).
		AnyTimes()

	// Add failing plugin
	failingPlugin := &LoadedPlugin{
		Name:   "failing-plugin",
		Driver: failingDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		MinerTypes: []models.Type{models.TypeWhatsminer},
	}
	manager.plugins["failing-plugin"] = failingPlugin

	// Add successful plugin
	successPlugin := &LoadedPlugin{
		Name:   "success-plugin",
		Driver: successDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		MinerTypes: []models.Type{models.TypeAntminer},
	}
	manager.plugins["success-plugin"] = successPlugin

	discoverer := NewMultiTypeDiscoverer(manager)

	ctx := t.Context()
	device, err := discoverer.Discover(ctx, "192.168.1.100", "80")

	require.NoError(t, err)
	require.NotNil(t, device)

	// Should get the successful discovery result
	assert.Equal(t, "SUCCESS123", device.SerialNumber)
}
