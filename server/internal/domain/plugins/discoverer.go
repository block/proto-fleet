package plugins

import (
	"context"
	"log/slog"
	"strings"
	"time"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
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

// MultiTypeDiscoverer discovers devices by trying all available plugins.
type MultiTypeDiscoverer struct {
	manager *Manager
}

// NewMultiTypeDiscoverer creates a discoverer that tries all available plugins.
func NewMultiTypeDiscoverer(manager *Manager) *MultiTypeDiscoverer {
	return &MultiTypeDiscoverer{
		manager: manager,
	}
}

// Discover tries to discover a device using all available plugins until one succeeds.
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

		// Use plugin's miner type for consistent type storage
		var pluginType string
		if len(plugin.MinerTypes) > 0 {
			pluginType = plugin.MinerTypes[0].String()
		}
		fleetDevice := convertSDKDeviceInfoToFleetDevice(deviceInfo, ipAddress, port, pluginType)

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
			IsActive:        true,
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

	if lastErr != nil {
		return nil, fleeterror.NewInternalErrorf("all plugin discovery attempts failed, last error: %v", lastErr)
	}

	return nil, fleeterror.NewInternalError("no plugins with discovery capability available")
}

// convertSDKDeviceInfoToFleetDevice converts SDK DeviceInfo to Fleet pb.Device format.
// The pluginType (from the plugin's MinerTypes) takes precedence over SDK device type.
func convertSDKDeviceInfoToFleetDevice(deviceInfo sdk.DeviceInfo, ipAddress, port, pluginType string) *pb.Device {
	deviceType := determineDeviceType(deviceInfo, pluginType)

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

// determineDeviceType determines the device type string.
// Priority: pluginType > manufacturer/model inference > SDK device type
func determineDeviceType(deviceInfo sdk.DeviceInfo, pluginType string) string {
	// Plugin's miner type takes precedence - this ensures Proto devices get "proto" type
	if pluginType != "" {
		return pluginType
	}

	// Try to infer from manufacturer
	if deviceInfo.Manufacturer != "" {
		manufacturer := strings.ToLower(deviceInfo.Manufacturer)
		if strings.Contains(manufacturer, "bitmain") {
			return DeviceTypeAntminer
		}
		if strings.Contains(manufacturer, "proto") {
			return DeviceTypeProto
		}
	}

	// Try to infer from model
	if deviceInfo.Model != "" {
		model := strings.ToLower(deviceInfo.Model)
		if strings.HasPrefix(model, "antminer") {
			return DeviceTypeAntminer
		}
	}

	// Fall back to SDK device type
	switch deviceInfo.Type {
	case sdk.DeviceTypeASIC:
		return DeviceTypeASIC
	case sdk.DeviceTypeGPU:
		return DeviceTypeGPU
	case sdk.DeviceTypeFPGA:
		return DeviceTypeFPGA
	case sdk.DeviceTypeUnspecified:
		slog.Debug("Could not determine device type, using 'unknown'",
			"manufacturer", deviceInfo.Manufacturer,
			"model", deviceInfo.Model)
		return DeviceTypeUnknown
	}
	return DeviceTypeUnknown
}
