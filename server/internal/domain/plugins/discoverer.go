package plugins

import (
	"context"
	"log/slog"
	"strings"
	"time"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

// Canonical device type constants
const (
	DeviceTypeASIC     = "asic"
	DeviceTypeGPU      = "gpu"
	DeviceTypeFPGA     = "fpga"
	DeviceTypeAntminer = "antminer"
	DeviceTypeProto    = "proto"
	DeviceTypeUnknown  = "unknown"
)

// Discoverer implements the minerdiscovery.Discoverer interface using plugins
type Discoverer struct {
	manager   *Manager
	minerType models.Type
}

// NewDiscoverer creates a new plugin-based discoverer for a specific miner type.
// The discoverer will use the plugin registered for the given minerType to discover devices
// on the network. If no plugin is registered for the miner type, discovery attempts will fail.
//
// Parameters:
//   - manager: The plugin manager that holds loaded plugins
//   - minerType: The specific miner type (e.g., TypeAntminer, TypeProto) that this discoverer handles
//
// TODO(DASH-818): Refactor to move away from models.Type once minimal miner plugins have been thoroughly validated in lab.
func NewDiscoverer(manager *Manager, minerType models.Type) *Discoverer {
	return &Discoverer{
		manager:   manager,
		minerType: minerType,
	}
}

// Discover attempts to discover a device using the plugin for this discoverer's miner type.
// It queries the plugin to identify the device at the given IP address and port, returning
// detailed device information including manufacturer, model, serial number, and capabilities.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - ipAddress: The IP address of the device to discover
//   - port: The port number to connect to on the device
//
// Returns the discovered device information or an error if discovery fails.
func (d *Discoverer) Discover(ctx context.Context, ipAddress string, port string) (*discoverymodels.DiscoveredDevice, error) {
	plugin, exists := d.manager.GetPluginForMinerType(d.minerType)
	if !exists {
		return nil, fleeterror.NewInternalErrorf("no plugin available for miner type %s", d.minerType)
	}

	// Check if plugin supports discovery
	if !plugin.Caps[sdk.CapabilityDiscovery] {
		return nil, fleeterror.NewInternalErrorf("plugin %s does not support discovery", plugin.Name)
	}

	deviceInfo, err := plugin.Driver.DiscoverDevice(ctx, ipAddress, port)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("plugin discovery failed: %v", err)
	}

	// Convert SDK DeviceInfo to Fleet Device
	fleetDevice := convertSDKDeviceInfoToFleetDeviceWithType(deviceInfo, ipAddress, port, d.minerType.String())

	// Create DiscoveredDevice
	discoveredDevice := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: fleetDevice.DeviceIdentifier,
			IpAddress:        fleetDevice.IpAddress,
			Port:             fleetDevice.Port,
			UrlScheme:        fleetDevice.UrlScheme,
			SerialNumber:     fleetDevice.SerialNumber,
			Model:            fleetDevice.Model,
			Manufacturer:     fleetDevice.Manufacturer,
			Type:             fleetDevice.Type,
			MacAddress:       fleetDevice.MacAddress,
			Capabilities:     fleetDevice.Capabilities,
		},
		OrgID:           0,
		FirstDiscovered: time.Now(),
		LastSeen:        time.Now(),
	}

	slog.Debug("Plugin discovered device successfully",
		"plugin", plugin.Name,
		"device", deviceInfo.SerialNumber,
		"model", deviceInfo.Model,
		"manufacturer", deviceInfo.Manufacturer)

	return discoveredDevice, nil
}

// GetMinerType returns the miner type this discoverer handles
func (d *Discoverer) GetMinerType() models.Type {
	return d.minerType
}

// convertSDKDeviceTypeToString converts SDK DeviceType to a string representation.
// If a fallback type is provided (e.g., "proto", "antminer"), it takes precedence over
// generic hardware types (ASIC, GPU, FPGA) since the fallback represents the specific
// miner type that discovered this device.
func convertSDKDeviceTypeToString(deviceInfo sdk.DeviceInfo, fallbackType string) string {
	// If we have a fallback type (specific miner type like "proto"), use it
	// This ensures devices discovered by the proto plugin get type "proto", not "asic"
	if fallbackType != "" {
		return fallbackType
	}

	// Only use hardware type if no fallback is provided
	switch deviceInfo.Type {
	case sdk.DeviceTypeASIC:
		return DeviceTypeASIC
	case sdk.DeviceTypeGPU:
		return DeviceTypeGPU
	case sdk.DeviceTypeFPGA:
		return DeviceTypeFPGA
	case sdk.DeviceTypeUnspecified:
		return determineDeviceTypeFromDiscovery(deviceInfo)
	default:
		return determineDeviceTypeFromDiscovery(deviceInfo)
	}
}

// createFleetDevice creates a Fleet pb.Device from SDK DeviceInfo and connection details.
// This is a common helper to avoid duplication in device conversion logic.
func createFleetDevice(deviceInfo sdk.DeviceInfo, ipAddress, port, deviceType string) *pb.Device {
	// Normalize MAC address to canonical format (uppercase with dashes)
	macAddress := deviceInfo.MacAddress
	if macAddress != "" {
		macAddress = networking.NormalizeMAC(macAddress)
	}

	return &pb.Device{
		DeviceIdentifier: "",
		IpAddress:        ipAddress,
		Port:             port,
		UrlScheme:        deviceInfo.URLScheme,
		SerialNumber:     deviceInfo.SerialNumber,
		Model:            deviceInfo.Model,
		Manufacturer:     deviceInfo.Manufacturer,
		Type:             deviceType,
		MacAddress:       macAddress,
		Capabilities:     nil,
	}
}

// convertSDKDeviceInfoToFleetDeviceWithType converts SDK DeviceInfo to Fleet pb.Device format with fallback type.
// This function uses the provided fallback type when the device type is unspecified.
func convertSDKDeviceInfoToFleetDeviceWithType(deviceInfo sdk.DeviceInfo, ipAddress, port, fallbackType string) *pb.Device {
	deviceType := convertSDKDeviceTypeToString(deviceInfo, fallbackType)

	return createFleetDevice(deviceInfo, ipAddress, port, deviceType)
}

// MultiTypeDiscoverer tries to discover devices using all available plugins
// TODO(DASH-818): Merge this into the Manager, this primarily depends on removing the need
// the current GetMinerType method on Discoverer interface with something more flexible.:
type MultiTypeDiscoverer struct {
	manager *Manager
}

// NewMultiTypeDiscoverer creates a discoverer that tries all available plugins
func NewMultiTypeDiscoverer(manager *Manager) *MultiTypeDiscoverer {
	return &MultiTypeDiscoverer{
		manager: manager,
	}
}

// Discover tries to discover a device using all available plugins until one succeeds
func (d *MultiTypeDiscoverer) Discover(ctx context.Context, ipAddress string, port string) (*discoverymodels.DiscoveredDevice, error) {
	plugins := d.manager.GetAllPlugins()

	if len(plugins) == 0 {
		return nil, fleeterror.NewInternalError("no plugins available for discovery")
	}

	var lastErr error

	for name, plugin := range plugins {
		if !plugin.Caps[sdk.CapabilityDiscovery] {
			continue
		}

		slog.Debug("Trying plugin for device discovery",
			"plugin", name,
			"ip", ipAddress,
			"port", port)

		deviceInfo, err := plugin.Driver.DiscoverDevice(ctx, ipAddress, port)
		if err != nil {
			slog.Debug("Plugin discovery failed",
				"plugin", name,
				"error", err)
			lastErr = err
			continue
		}

		fleetDevice := convertSDKDeviceInfoToFleetDevice(deviceInfo, ipAddress, port)

		discoveredDevice := &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: fleetDevice.DeviceIdentifier,
				IpAddress:        fleetDevice.IpAddress,
				Port:             fleetDevice.Port,
				UrlScheme:        fleetDevice.UrlScheme,
				SerialNumber:     fleetDevice.SerialNumber,
				Model:            fleetDevice.Model,
				Manufacturer:     fleetDevice.Manufacturer,
				Type:             fleetDevice.Type,
				MacAddress:       fleetDevice.MacAddress,
				Capabilities:     fleetDevice.Capabilities,
			},
			OrgID:           0,
			FirstDiscovered: time.Now(),
			LastSeen:        time.Now(),
		}

		slog.Info("Plugin discovered device successfully",
			"plugin", name,
			"device", deviceInfo.SerialNumber,
			"model", deviceInfo.Model,
			"manufacturer", deviceInfo.Manufacturer,
			"type", fleetDevice.Type)

		return discoveredDevice, nil
	}

	// If we get here, no plugin succeeded
	if lastErr != nil {
		return nil, fleeterror.NewInternalErrorf("all plugin discovery attempts failed, last error: %v", lastErr)
	}

	return nil, fleeterror.NewInternalError("no plugins with discovery capability available")
}

// GetMinerType returns unknown since this discoverer tries multiple types
func (d *MultiTypeDiscoverer) GetMinerType() models.Type {
	return models.TypeUnknown
}

// convertSDKDeviceInfoToFleetDevice converts SDK DeviceInfo to Fleet pb.Device format.
// Uses discovery results directly without unnecessary models.Type mapping.
// When the device type is unspecified, it determines the type from manufacturer and model information.
func convertSDKDeviceInfoToFleetDevice(deviceInfo sdk.DeviceInfo, ipAddress, port string) *pb.Device {
	// Use empty fallback to trigger device type determination from discovery info
	deviceType := convertSDKDeviceTypeToString(deviceInfo, "")

	return createFleetDevice(deviceInfo, ipAddress, port, deviceType)
}

// determineDeviceTypeFromDiscovery determines device type string directly from discovery results
// No models.Type middleman - just use what the plugin discovered
// Returns canonical device type constants
func determineDeviceTypeFromDiscovery(deviceInfo sdk.DeviceInfo) string {
	// Priority 1: Use manufacturer information from actual discovery
	if deviceInfo.Manufacturer != "" {
		manufacturer := strings.ToLower(deviceInfo.Manufacturer)

		if strings.Contains(manufacturer, "bitmain") {
			return DeviceTypeAntminer
		}
		if strings.Contains(manufacturer, "proto") {
			return DeviceTypeProto
		}
	}

	// Priority 2: Use model information if manufacturer is unclear
	if deviceInfo.Model != "" {
		model := strings.ToLower(deviceInfo.Model)

		if strings.HasPrefix(model, "antminer") {
			return DeviceTypeAntminer
		}
	}

	// Priority 3: If we can't determine from discovery, use a generic type
	// This is better than trying to guess from plugin names
	slog.Debug("Could not determine device type from discovery results, using 'unknown'",
		"manufacturer", deviceInfo.Manufacturer,
		"model", deviceInfo.Model)
	return DeviceTypeUnknown
}
