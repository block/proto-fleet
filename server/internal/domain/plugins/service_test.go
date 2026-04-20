package plugins

import (
	"testing"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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

func TestService_IsPluginAvailableByDriverName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Act + Assert - no plugins registered
	assert.False(t, service.GetManager().HasPluginForDriverName("antminer"))

	// Arrange
	mockPlugin := &LoadedPlugin{
		Name: "antminer-plugin",
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Act + Assert
	assert.True(t, service.GetManager().HasPluginForDriverName("antminer"))
	assert.False(t, service.GetManager().HasPluginForDriverName("whatsminer"))
}

func TestService_GetAvailablePlugins(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Act + Assert - no plugins
	plugins := service.GetAvailablePlugins()
	assert.Empty(t, plugins)

	// Arrange
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
	}

	manager.plugins["plugin1"] = mockPlugin1
	manager.plugins["plugin2"] = mockPlugin2

	// Act
	plugins = service.GetAvailablePlugins()

	// Assert
	assert.Len(t, plugins, 2)

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

	require.NotNil(t, plugin2Info)
	assert.Equal(t, "plugin2", plugin2Info.Name)
	assert.Equal(t, "/path/to/plugin2", plugin2Info.Path)
	assert.Equal(t, "driver2", plugin2Info.DriverName)
	assert.Equal(t, "v2", plugin2Info.APIVersion)
	assert.True(t, plugin2Info.Capabilities[sdk.CapabilityPairing])
}

func TestService_GetPluginCapabilitiesByDriverName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Act + Assert - no plugin
	caps, err := service.GetPluginCapabilitiesByDriverName("antminer")
	assert.Nil(t, caps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin")

	// Arrange
	mockCaps := sdk.Capabilities{
		sdk.CapabilityDiscovery: true,
		sdk.CapabilityPairing:   true,
	}
	mockPlugin := &LoadedPlugin{
		Name: "antminer-plugin",
		Caps: mockCaps,
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Act + Assert
	caps, err = service.GetPluginCapabilitiesByDriverName("antminer")
	require.NoError(t, err)
	assert.Equal(t, mockCaps, caps)
}

func TestService_GetDefaultDiscoveryPorts_ReturnsAllAdvertisedPorts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	manager.plugins["proto-plugin"] = &LoadedPlugin{
		Name: "proto-plugin",
		Identifier: sdk.DriverIdentifier{
			DriverName: "proto",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: []string{"443", "8080"},
	}
	manager.plugins["antminer-plugin"] = &LoadedPlugin{
		Name: "antminer-plugin",
		Identifier: sdk.DriverIdentifier{
			DriverName: "antminer",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: []string{"4028", "443"},
	}
	manager.plugins["asicrs-plugin"] = &LoadedPlugin{
		Name: "asicrs-plugin",
		Identifier: sdk.DriverIdentifier{
			DriverName: "asicrs",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: []string{"443", "4028"},
	}
	manager.plugins["virtual-plugin"] = &LoadedPlugin{
		Name: "virtual-plugin",
		Identifier: sdk.DriverIdentifier{
			DriverName: "virtual",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: []string{"4028"},
	}

	assert.Equal(t, []string{"4028", "443", "8080"}, service.GetDefaultDiscoveryPorts(t.Context()))
}

func TestService_GetDiscoveryPorts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	manager.plugins["proto-plugin"] = &LoadedPlugin{
		Name: "proto-plugin",
		Identifier: sdk.DriverIdentifier{
			DriverName: "proto",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: []string{"443", "8080"},
	}
	manager.plugins["antminer-plugin"] = &LoadedPlugin{
		Name: "antminer-plugin",
		Identifier: sdk.DriverIdentifier{
			DriverName: "antminer",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: []string{"4028", "443"},
	}
	manager.plugins["virtual-plugin"] = &LoadedPlugin{
		Name: "virtual-plugin",
		Identifier: sdk.DriverIdentifier{
			DriverName: "virtual",
		},
		Caps: sdk.Capabilities{
			sdk.CapabilityDiscovery: true,
		},
		DiscoveryPorts: []string{"4028"},
	}

	assert.Equal(t, []string{"4028", "443", "8080"}, service.GetDiscoveryPorts(t.Context()))
}

func TestService_ValidatePluginHealth_NoPlugins(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	ctx := t.Context()
	err := service.ValidatePluginHealth(ctx)

	assert.NoError(t, err)
}

func TestService_ValidatePluginHealth_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Arrange
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockDriver.EXPECT().
		Handshake(gomock.Any()).
		Return(sdk.DriverIdentifier{
			DriverName: "mock-driver",
			APIVersion: "v1",
		}, nil)

	mockPlugin := &LoadedPlugin{
		Name:   "test-plugin",
		Driver: mockDriver,
	}
	manager.plugins["test-plugin"] = mockPlugin

	// Act
	ctx := t.Context()
	err := service.ValidatePluginHealth(ctx)

	// Assert
	assert.NoError(t, err)
}

func TestService_CreateDiscoverer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Act
	discoverer := service.CreateDiscoverer()

	// Assert
	multiDiscoverer, ok := discoverer.(*MultiTypeDiscoverer)
	assert.True(t, ok)
	assert.NotNil(t, multiDiscoverer)
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

func TestService_GetMinerCapabilitiesForDevice_NilDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	ctx := t.Context()
	result := service.GetMinerCapabilitiesForDevice(ctx, nil)

	assert.Nil(t, result)
}

func TestService_GetMinerCapabilitiesForDevice_NoPluginForDriverName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	device := &pairingpb.Device{
		DriverName:   "antminer",
		Model:        "Antminer S19",
		Manufacturer: "Bitmain",
	}

	ctx := t.Context()
	result := service.GetMinerCapabilitiesForDevice(ctx, device)

	assert.Nil(t, result)
}

func TestService_GetMinerCapabilitiesForDevice_EmptyDriverName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	device := &pairingpb.Device{
		DriverName:   "",
		Model:        "Unknown Model",
		Manufacturer: "Unknown",
	}

	ctx := t.Context()
	result := service.GetMinerCapabilitiesForDevice(ctx, device)

	assert.Nil(t, result)
}

func TestService_GetMinerCapabilitiesForDevice_AntminerSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Arrange
	antminerCaps := sdk.Capabilities{
		sdk.CapabilityBasicAuth:         true,
		sdk.CapabilityReboot:            true,
		sdk.CapabilityLEDBlink:          true,
		sdk.CapabilityPoolConfig:        true,
		sdk.CapabilityRealtimeTelemetry: true,
		sdk.CapabilityHashrateReported:  true,
		sdk.CapabilityManualUpload:      true,
	}

	mockPlugin := &LoadedPlugin{
		Name: "antminer-plugin",
		Caps: antminerCaps,
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin
	manager.plugins["antminer-plugin"] = mockPlugin

	device := &pairingpb.Device{
		DriverName:   "antminer",
		Model:        "Antminer S19",
		Manufacturer: "Bitmain",
	}

	// Act
	ctx := t.Context()
	result := service.GetMinerCapabilitiesForDevice(ctx, device)

	// Assert
	require.NotNil(t, result)
	assert.Equal(t, "Bitmain", result.Manufacturer)

	require.NotNil(t, result.Telemetry)
	assert.True(t, result.Telemetry.RealtimeTelemetrySupported)

	require.NotNil(t, result.Commands)
	assert.True(t, result.Commands.RebootSupported)
	assert.True(t, result.Commands.LedBlinkSupported)
	assert.False(t, result.Commands.AirCoolingSupported)
	assert.False(t, result.Commands.ImmersionCoolingSupported)
}

func TestService_GetMinerCapabilitiesForDevice_ProtoSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Arrange
	protoCaps := sdk.Capabilities{
		sdk.CapabilityAsymmetricAuth:     true,
		sdk.CapabilityReboot:             true,
		sdk.CapabilityMiningStart:        true,
		sdk.CapabilityMiningStop:         true,
		sdk.CapabilityCoolingModeAir:     true,
		sdk.CapabilityCoolingModeImmerse: true,
		sdk.CapabilityPoolConfig:         true,
		sdk.CapabilityRealtimeTelemetry:  true,
		sdk.CapabilityHistoricalData:     true,
		sdk.CapabilityOTAUpdate:          true,
	}

	mockPlugin := &LoadedPlugin{
		Name: "proto-plugin",
		Caps: protoCaps,
	}
	manager.pluginsByDriverName["proto"] = mockPlugin
	manager.plugins["proto-plugin"] = mockPlugin

	device := &pairingpb.Device{
		DriverName:   "proto",
		Model:        "Rig 1",
		Manufacturer: "Proto",
	}

	// Act
	ctx := t.Context()
	result := service.GetMinerCapabilitiesForDevice(ctx, device)

	// Assert
	require.NotNil(t, result)
	assert.Equal(t, "Proto", result.Manufacturer)

	require.NotNil(t, result.Telemetry)
	assert.True(t, result.Telemetry.RealtimeTelemetrySupported)
	assert.True(t, result.Telemetry.HistoricalDataSupported)

	require.NotNil(t, result.Commands)
	assert.True(t, result.Commands.AirCoolingSupported)
	assert.True(t, result.Commands.ImmersionCoolingSupported)
}

func TestService_GetMinerCapabilitiesForDevice_WithModelCapabilitiesProvider(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	manager := NewManager(&Config{})
	service := createTestServiceForServiceTest(t, ctrl, manager)

	// Arrange
	mockDriver := sdkMocks.NewMockDriver(ctrl)
	mockModelCapProvider := sdkMocks.NewMockModelCapabilitiesProvider(ctrl)

	baseCaps := sdk.Capabilities{
		sdk.CapabilityBasicAuth:           true,
		sdk.CapabilityReboot:              true,
		sdk.CapabilityPowerModeEfficiency: true,
	}

	mockModelCapProvider.EXPECT().
		GetCapabilitiesForModel(gomock.Any(), "Antminer S21").
		Return(sdk.Capabilities{
			sdk.CapabilityPowerModeEfficiency: false,
		})

	type combinedDriver struct {
		sdk.Driver
		sdk.ModelCapabilitiesProvider
	}
	combined := &combinedDriver{
		Driver:                    mockDriver,
		ModelCapabilitiesProvider: mockModelCapProvider,
	}

	mockPlugin := &LoadedPlugin{
		Name:   "antminer-plugin",
		Caps:   baseCaps,
		Driver: combined,
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin
	manager.plugins["antminer-plugin"] = mockPlugin

	device := &pairingpb.Device{
		DriverName:   "antminer",
		Model:        "Antminer S21",
		Manufacturer: "Bitmain",
	}

	// Act
	ctx := t.Context()
	result := service.GetMinerCapabilitiesForDevice(ctx, device)

	// Assert
	require.NotNil(t, result)
	require.NotNil(t, result.Commands)
	assert.False(t, result.Commands.PowerModeEfficiencySupported)
}

func TestMergeCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		base     sdk.Capabilities
		override sdk.Capabilities
		expected sdk.Capabilities
	}{
		{
			name: "override single capability",
			base: sdk.Capabilities{
				sdk.CapabilityReboot:              true,
				sdk.CapabilityPowerModeEfficiency: true,
			},
			override: sdk.Capabilities{
				sdk.CapabilityPowerModeEfficiency: false,
			},
			expected: sdk.Capabilities{
				sdk.CapabilityReboot:              true,
				sdk.CapabilityPowerModeEfficiency: false,
			},
		},
		{
			name: "add new capability",
			base: sdk.Capabilities{
				sdk.CapabilityReboot: true,
			},
			override: sdk.Capabilities{
				sdk.CapabilityLEDBlink: true,
			},
			expected: sdk.Capabilities{
				sdk.CapabilityReboot:   true,
				sdk.CapabilityLEDBlink: true,
			},
		},
		{
			name: "empty override",
			base: sdk.Capabilities{
				sdk.CapabilityReboot: true,
			},
			override: sdk.Capabilities{},
			expected: sdk.Capabilities{
				sdk.CapabilityReboot: true,
			},
		},
		{
			name: "empty base",
			base: sdk.Capabilities{},
			override: sdk.Capabilities{
				sdk.CapabilityReboot: true,
			},
			expected: sdk.Capabilities{
				sdk.CapabilityReboot: true,
			},
		},
		{
			name: "multiple overrides",
			base: sdk.Capabilities{
				sdk.CapabilityReboot:              true,
				sdk.CapabilityLEDBlink:            true,
				sdk.CapabilityPowerModeEfficiency: true,
			},
			override: sdk.Capabilities{
				sdk.CapabilityLEDBlink:            false,
				sdk.CapabilityPowerModeEfficiency: false,
			},
			expected: sdk.Capabilities{
				sdk.CapabilityReboot:              true,
				sdk.CapabilityLEDBlink:            false,
				sdk.CapabilityPowerModeEfficiency: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeCapabilities(tt.base, tt.override)
			assert.Equal(t, tt.expected, result)
		})
	}
}
