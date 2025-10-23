package plugins

import (
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/btc-mining/proto-fleet/server/sdk/v1/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create test service with all required services
func createTestServiceForServiceTest(_ *testing.T, _ *gomock.Controller, manager *Manager) *Service {
	return NewService(manager)
}

func TestNewService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})

	service := createTestServiceForServiceTest(t, ctrl, manager)

	assert.NotNil(t, service)
	assert.Equal(t, manager, service.manager)
}

func TestService_GetManager(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	assert.Equal(t, manager, service.GetManager())
}

func TestService_IsPluginAvailableForType(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Test with no plugins
	assert.False(t, service.IsPluginAvailableForType(models.TypeAntminer))

	// Add a mock plugin
	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		MinerTypes: []models.Type{models.TypeAntminer},
	}
	manager.pluginsByType[models.TypeAntminer] = mockPlugin

	// Test with plugin available
	assert.True(t, service.IsPluginAvailableForType(models.TypeAntminer))
	assert.False(t, service.IsPluginAvailableForType(models.TypeWhatsminer))
}

func TestService_GetAvailablePlugins(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Test with no plugins
	plugins := service.GetAvailablePlugins()
	assert.Empty(t, plugins)

	// Add mock plugins
	mockPlugin1 := &LoadedPlugin{
		Name: "plugin1",
		Path: "/path/to/plugin1",
		Identifier: sdk.DriverIdentifier{
			DriverName: "driver1",
			APIVersion: "v1",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		MinerTypes: []models.Type{models.TypeAntminer},
	}
	mockPlugin2 := &LoadedPlugin{
		Name: "plugin2",
		Path: "/path/to/plugin2",
		Identifier: sdk.DriverIdentifier{
			DriverName: "driver2",
			APIVersion: "v2",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityPairing: true,
		},
		MinerTypes: []models.Type{models.TypeWhatsminer},
	}

	manager.plugins["plugin1"] = mockPlugin1
	manager.plugins["plugin2"] = mockPlugin2

	// Test with plugins
	plugins = service.GetAvailablePlugins()
	assert.Len(t, plugins, 2)

	// Find plugin1 in results
	var plugin1Info *PluginInfo
	var plugin2Info *PluginInfo
	for i := range plugins {
		if plugins[i].Name == "plugin1" {
			plugin1Info = &plugins[i]
		} else if plugins[i].Name == "plugin2" {
			plugin2Info = &plugins[i]
		}
	}

	require.NotNil(t, plugin1Info)
	assert.Equal(t, "plugin1", plugin1Info.Name)
	assert.Equal(t, "/path/to/plugin1", plugin1Info.Path)
	assert.Equal(t, "driver1", plugin1Info.DriverName)
	assert.Equal(t, "v1", plugin1Info.APIVersion)
	assert.True(t, plugin1Info.Capabilities[sdk.CapabilityDiscovery])
	assert.ElementsMatch(t, []models.Type{models.TypeAntminer}, plugin1Info.MinerTypes)

	require.NotNil(t, plugin2Info)
	assert.Equal(t, "plugin2", plugin2Info.Name)
	assert.Equal(t, "/path/to/plugin2", plugin2Info.Path)
	assert.Equal(t, "driver2", plugin2Info.DriverName)
	assert.Equal(t, "v2", plugin2Info.APIVersion)
	assert.True(t, plugin2Info.Capabilities[sdk.CapabilityPairing])
	assert.ElementsMatch(t, []models.Type{models.TypeWhatsminer}, plugin2Info.MinerTypes)
}

func TestService_GetPluginCapabilities(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Test with no plugin
	caps, err := service.GetPluginCapabilities(models.TypeAntminer)
	assert.Nil(t, caps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin available for miner type")

	// Add mock plugin
	mockCaps := sdk.Capabilities{
		sdk.CapabilityDiscovery: true,
		sdk.CapabilityPairing:   true,
	}
	mockPlugin := &LoadedPlugin{
		Name:       "antminer-plugin",
		Caps:       mockCaps,
		MinerTypes: []models.Type{models.TypeAntminer},
	}
	manager.pluginsByType[models.TypeAntminer] = mockPlugin

	// Test with plugin available
	caps, err = service.GetPluginCapabilities(models.TypeAntminer)
	require.NoError(t, err)
	assert.Equal(t, mockCaps, caps)
}

func TestService_ValidatePluginHealth_NoPlugins(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	ctx := t.Context()
	err := service.ValidatePluginHealth(ctx)

	assert.NoError(t, err) // Should succeed with no plugins
}

func TestService_ValidatePluginHealth_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Create mock driver with handshake expectation
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		Handshake(gomock.Any()).
		Return(sdk.DriverIdentifier{
			DriverName: "mock-driver",
			APIVersion: "v1",
		}, nil)

	// Add mock plugin with working driver
	mockPlugin := &LoadedPlugin{
		Name:   "test-plugin",
		Driver: mockDriver,
	}
	manager.plugins["test-plugin"] = mockPlugin

	ctx := t.Context()
	err := service.ValidatePluginHealth(ctx)

	assert.NoError(t, err)
}

func TestService_GetSupportedMinerTypes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Test with no plugins
	types := service.GetSupportedMinerTypes()
	assert.Empty(t, types)

	// Add mock plugins with different types
	mockPlugin1 := &LoadedPlugin{
		Name:       "plugin1",
		MinerTypes: []models.Type{models.TypeAntminer, models.TypeWhatsminer},
	}
	mockPlugin2 := &LoadedPlugin{
		Name:       "plugin2",
		MinerTypes: []models.Type{models.TypeAvalon},
	}
	mockPlugin3 := &LoadedPlugin{
		Name:       "plugin3",
		MinerTypes: []models.Type{models.TypeAntminer}, // Duplicate type
	}

	manager.plugins["plugin1"] = mockPlugin1
	manager.plugins["plugin2"] = mockPlugin2
	manager.plugins["plugin3"] = mockPlugin3

	// Test with plugins
	types = service.GetSupportedMinerTypes()

	// Should contain unique types only
	expectedTypes := []models.Type{models.TypeAntminer, models.TypeWhatsminer, models.TypeAvalon}
	assert.Len(t, types, 3)
	assert.ElementsMatch(t, expectedTypes, types)
}

func TestService_CreateDiscoverers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Test with no plugins
	discoverers := service.CreateDiscoverers()
	assert.Len(t, discoverers, 1) // Should have multi-type discoverer

	// Verify the multi-type discoverer
	multiDiscoverer, ok := discoverers[0].(*MultiTypeDiscoverer)
	assert.True(t, ok)
	assert.Equal(t, models.TypeUnknown, multiDiscoverer.GetMinerType())

	// Add mock plugins
	mockPlugin1 := &LoadedPlugin{
		Name:       "plugin1",
		MinerTypes: []models.Type{models.TypeAntminer},
	}
	mockPlugin2 := &LoadedPlugin{
		Name:       "plugin2",
		MinerTypes: []models.Type{models.TypeWhatsminer},
	}

	manager.plugins["plugin1"] = mockPlugin1
	manager.plugins["plugin2"] = mockPlugin2
	manager.pluginsByType[models.TypeAntminer] = mockPlugin1
	manager.pluginsByType[models.TypeWhatsminer] = mockPlugin2

	// Test with plugins
	discoverers = service.CreateDiscoverers()
	assert.Len(t, discoverers, 3) // 2 type-specific + 1 multi-type

	// Check that we have discoverers for each type
	var antminerDiscoverer, whatsminerDiscoverer, multiTypeDiscoverer bool
	for _, discoverer := range discoverers {
		switch discoverer.GetMinerType() {
		case models.TypeAntminer:
			antminerDiscoverer = true
		case models.TypeWhatsminer:
			whatsminerDiscoverer = true
		case models.TypeUnknown:
			multiTypeDiscoverer = true
		case models.TypeProto, models.TypeAvalon:
			// Other types not tested in this specific test
		default:
			// Other types not tested in this specific test
		}
	}

	assert.True(t, antminerDiscoverer, "should have Antminer discoverer")
	assert.True(t, whatsminerDiscoverer, "should have Whatsminer discoverer")
	assert.True(t, multiTypeDiscoverer, "should have multi-type discoverer")
}

func TestService_TryDiscoverWithPlugin_NoPlugins(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	ctx := t.Context()
	device, err := service.TryDiscoverWithPlugin(ctx, "192.168.1.100", "80", nil)

	assert.Nil(t, device)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugins available for discovery")
}

func TestService_TryDiscoverWithPlugin_PreferredType(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Add mock plugin for preferred type
	mockDeviceInfo := sdk.DeviceInfo{
		Type:         sdk.DeviceTypeASIC,
		SerialNumber: "PREFERRED123",
		Model:        "S19",
		Manufacturer: "Bitmain",
		URLScheme:    "http",
		MacAddress:   "00:11:22:33:44:55",
	}

	// Create mock driver with discovery expectation
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		DiscoverDevice(gomock.Any(), "192.168.1.100", "80").
		Return(mockDeviceInfo, nil)

	mockPlugin := &LoadedPlugin{
		Name:   "antminer-plugin",
		Driver: mockDriver,
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		MinerTypes: []models.Type{models.TypeAntminer},
	}

	manager.pluginsByType[models.TypeAntminer] = mockPlugin
	manager.plugins["antminer-plugin"] = mockPlugin

	preferredType := models.TypeAntminer
	ctx := t.Context()
	device, err := service.TryDiscoverWithPlugin(ctx, "192.168.1.100", "80", &preferredType)

	require.NoError(t, err)
	require.NotNil(t, device)
	assert.Equal(t, "PREFERRED123", device.SerialNumber)
}

func TestService_Shutdown(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	ctx := t.Context()
	err := service.Shutdown(ctx)

	assert.NoError(t, err)
}
