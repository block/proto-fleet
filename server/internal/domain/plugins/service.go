package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	capabilitiespb "github.com/btc-mining/proto-fleet/server/generated/grpc/capabilities/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
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

// IsPluginAvailableForType checks if a plugin is available for the given miner type
func (s *Service) IsPluginAvailableForType(minerType models.Type) bool {
	return s.manager.HasPluginForMinerType(minerType)
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
			MinerTypes:   plugin.MinerTypes,
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
	MinerTypes   []models.Type
}

// TryDiscoverWithPlugin attempts to discover a device using plugins
func (s *Service) TryDiscoverWithPlugin(ctx context.Context, ipAddress, port string, preferredType *models.Type) (*discoverymodels.DiscoveredDevice, error) {
	var preferredTypeError error
	if preferredType != nil {
		if s.manager.HasPluginForMinerType(*preferredType) {
			discoverer := NewDiscoverer(s.manager, *preferredType)
			device, preferredTypeError := discoverer.Discover(ctx, ipAddress, port)
			if preferredTypeError == nil {
				return device, nil
			}
			slog.Debug("Plugin discovery failed for preferred type",
				"type", *preferredType,
				"error", preferredTypeError)
		}
	}

	multiDiscoverer := NewMultiTypeDiscoverer(s.manager)
	device, err := multiDiscoverer.Discover(ctx, ipAddress, port)
	if err != nil {
		if preferredTypeError != nil {
			slog.Debug("Both preferred type and multi-type plugin discovery failed",
				"preferred_type_error", preferredTypeError,
				"multi_type_error", err)
			return nil, fleeterror.NewInternalErrorf("preferred type discovery error: %v; multi-type discovery error: %v", preferredTypeError, err)
		}
		return nil, err
	}

	return device, nil
}

// GetPluginCapabilities returns the capabilities of a plugin for a given miner type
func (s *Service) GetPluginCapabilities(minerType models.Type) (sdk.Capabilities, error) {
	plugin, exists := s.manager.GetPluginForMinerType(minerType)
	if !exists {
		return nil, fleeterror.NewInternalErrorf("no plugin available for miner type %s", minerType)
	}

	return plugin.Caps, nil
}

// GetMinerCapabilitiesForDevice returns the protobuf MinerCapabilities for a device.
// It determines the miner type from the device and retrieves capabilities from the
// corresponding plugin. Returns nil if no plugin is available for the device type.
func (s *Service) GetMinerCapabilitiesForDevice(_ context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities {
	if device == nil {
		return nil
	}

	minerType, err := models.TypeFromDeviceInfo(device.Type, device.Model)
	if err != nil {
		slog.Debug("Failed to determine miner type for device",
			"type", device.Type,
			"model", device.Model,
			"error", err)
		return nil
	}

	plugin, exists := s.manager.GetPluginForMinerType(minerType)
	if !exists {
		slog.Debug("No plugin available for miner type", "type", minerType)
		return nil
	}

	return ConvertToMinerCapabilities(plugin.Caps, device.Manufacturer)
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

// GetSupportedMinerTypes returns all miner types that have plugin support
func (s *Service) GetSupportedMinerTypes() []models.Type {
	plugins := s.manager.GetAllPlugins()
	typeSet := make(map[models.Type]bool)

	for _, plugin := range plugins {
		for _, minerType := range plugin.MinerTypes {
			typeSet[minerType] = true
		}
	}

	result := make([]models.Type, 0, len(typeSet))
	for minerType := range typeSet {
		result = append(result, minerType)
	}

	return result
}

// CreateDiscoverers creates plugin-based discoverers for all supported miner types
func (s *Service) CreateDiscoverers() []minerdiscovery.Discoverer {
	supportedTypes := s.GetSupportedMinerTypes()
	discoverers := make([]minerdiscovery.Discoverer, 0, len(supportedTypes)+1)

	for _, minerType := range supportedTypes {
		discoverer := NewDiscoverer(s.manager, minerType)
		discoverers = append(discoverers, discoverer)
	}

	multiDiscoverer := NewMultiTypeDiscoverer(s.manager)
	discoverers = append(discoverers, multiDiscoverer)

	return discoverers
}

// Shutdown gracefully shuts down the plugin service
func (s *Service) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down plugin service")
	return s.manager.Shutdown(ctx)
}
