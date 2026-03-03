package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	capabilitiespb "github.com/btc-mining/proto-fleet/server/generated/grpc/capabilities/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	sdk "github.com/btc-mining/proto-fleet/server/sdk/v1"
)

// Service provides high-level plugin integration services
type Service struct {
	manager *Manager
}

// NewService creates a new plugin service
func NewService(manager *Manager) *Service {
	return &Service{
		manager: manager,
	}
}

// GetManager returns the plugin manager
func (s *Service) GetManager() *Manager {
	return s.manager
}

// GetAvailablePlugins returns information about all loaded plugins
func (s *Service) GetAvailablePlugins() []PluginInfo {
	plugins := s.manager.GetAllPlugins()
	result := make([]PluginInfo, 0, len(plugins))

	for _, plugin := range plugins {
		info := PluginInfo{
			Name:         plugin.Name,
			Path:         plugin.Path,
			DriverName:   plugin.Identifier.DriverName,
			APIVersion:   plugin.Identifier.APIVersion,
			Capabilities: plugin.Caps,
		}
		result = append(result, info)
	}

	return result
}

// PluginInfo contains information about a loaded plugin
type PluginInfo struct {
	Name         string
	Path         string
	DriverName   string
	APIVersion   string
	Capabilities sdk.Capabilities
}

// GetPluginCapabilitiesByDriverName returns the capabilities of a plugin for a given driver name
func (s *Service) GetPluginCapabilitiesByDriverName(driverName string) (sdk.Capabilities, error) {
	plugin, exists := s.manager.GetPluginByDriverName(driverName)
	if !exists {
		return nil, fleeterror.NewInternalErrorf("no plugin available for driver name %s", driverName)
	}

	return plugin.Caps, nil
}

// GetMinerCapabilitiesForDevice returns the protobuf MinerCapabilities for a device.
// It determines the miner type from the device and retrieves capabilities from the
// corresponding plugin. If the plugin implements ModelCapabilitiesProvider, model-specific
// capability overrides are merged with the base capabilities.
// Returns nil if no plugin is available for the device type.
func (s *Service) GetMinerCapabilitiesForDevice(ctx context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities {
	if device == nil {
		return nil
	}

	plugin := s.resolvePluginForDevice(device)
	if plugin == nil {
		return nil
	}

	caps := plugin.Caps

	if modelProvider, ok := plugin.Driver.(sdk.ModelCapabilitiesProvider); ok {
		if modelCaps := modelProvider.GetCapabilitiesForModel(ctx, device.Model); modelCaps != nil {
			caps = mergeCapabilities(caps, modelCaps)
		}
	}

	return ConvertToMinerCapabilities(caps, device.Manufacturer)
}

// resolvePluginForDevice finds the plugin for a device by driver name.
func (s *Service) resolvePluginForDevice(device *pairingpb.Device) *LoadedPlugin {
	if device.DriverName == "" {
		return nil
	}

	plugin, exists := s.manager.GetPluginByDriverName(device.DriverName)
	if !exists {
		return nil
	}
	return plugin
}

// mergeCapabilities merges model-specific capabilities with base capabilities.
// Model-specific capabilities override base capabilities.
func mergeCapabilities(base, override sdk.Capabilities) sdk.Capabilities {
	result := make(sdk.Capabilities, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

// ValidatePluginHealth checks if all loaded plugins are healthy
func (s *Service) ValidatePluginHealth(ctx context.Context) error {
	plugins := s.manager.GetAllPlugins()

	if len(plugins) == 0 {
		slog.Info("No plugins loaded, skipping health check")
		return nil
	}

	// Check plugins concurrently for faster health validation
	var wg sync.WaitGroup
	errorsChan := make(chan error, len(plugins))

	for name, plugin := range plugins {
		wg.Add(1)
		go func(name string, plugin *LoadedPlugin) {
			defer wg.Done()

			_, err := plugin.Driver.Handshake(ctx)
			if err != nil {
				errorsChan <- fmt.Errorf("plugin %s health check failed: %w", name, err)
				slog.Error("Plugin health check failed", "plugin", name, "error", err)
			} else {
				slog.Debug("Plugin health check passed", "plugin", name)
			}
		}(name, plugin)
	}

	// Wait for all health checks to complete
	wg.Wait()
	close(errorsChan)

	// Collect all errors
	var healthErrors []error
	for err := range errorsChan {
		healthErrors = append(healthErrors, err)
	}

	if len(healthErrors) > 0 {
		return fleeterror.NewInternalErrorf("plugin health check failures: %v", errors.Join(healthErrors...))
	}

	slog.Debug("All plugin health checks passed", "count", len(plugins))
	return nil
}

// CreateDiscoverer creates a plugin-based discoverer that tries all available plugins.
func (s *Service) CreateDiscoverer() minerdiscovery.Discoverer {
	return NewMultiTypeDiscoverer(s.manager)
}

// Shutdown gracefully shuts down the plugin service
func (s *Service) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down plugin service")
	return s.manager.Shutdown(ctx)
}
