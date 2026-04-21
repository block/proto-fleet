package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

const (
	dispenseDriver = "driver"
)

// LoadedPlugin represents a successfully loaded plugin
type LoadedPlugin struct {
	Name           string
	Path           string
	Client         *plugin.Client
	Driver         sdk.Driver
	Identifier     sdk.DriverIdentifier
	Caps           sdk.Capabilities
	DiscoveryPorts []string
}

// Manager handles loading and managing plugins
type Manager struct {
	config              *Config
	plugins             map[string]*LoadedPlugin
	pluginsByDriverName map[string]*LoadedPlugin
	mu                  sync.RWMutex
}

// NewManager creates a new plugin manager
func NewManager(config *Config) *Manager {
	return &Manager{
		config:              config,
		plugins:             make(map[string]*LoadedPlugin),
		pluginsByDriverName: make(map[string]*LoadedPlugin),
	}
}

// LoadPlugins discovers and loads all plugins from the configured directory.
//
// SECURITY NOTE: This function loads and executes all executable files found in the plugins
// directory. Plugins run with the same privileges as the Fleet server process. To maintain
// security:
//   - Ensure the plugins directory has restricted permissions (recommended: 0750 or stricter)
//   - Only place trusted plugin binaries in this directory
//   - Consider implementing plugin signature verification for production deployments
//   - Review plugin source code before deployment
//   - Run the Fleet server with minimal required privileges
//
// The function will skip non-executable files and continue loading other plugins if individual
// plugin loading fails, collecting all errors and returning them at the end.
func (m *Manager) LoadPlugins(ctx context.Context) error {
	if !m.config.Enabled {
		slog.Info("Plugin system disabled")
		return nil
	}

	pluginsDir, err := m.config.GetPluginsDir()
	if err != nil {
		return err // already a fleeterror from GetPluginsDir
	}

	if _, err := os.Stat(pluginsDir); os.IsNotExist(err) {
		slog.Info("Plugins directory does not exist, creating it", "dir", pluginsDir)
		if err := os.MkdirAll(pluginsDir, 0750); err != nil {
			return fleeterror.NewInternalErrorf("failed to create plugins directory: %v", err)
		}
		return nil
	}

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to read plugins directory: %v", err)
	}

	// Collect executable plugin files
	var pluginsToLoad []struct {
		name string
		path string
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(pluginsDir, entry.Name())

		if !isExecutable(pluginPath) {
			slog.Debug("Skipping non-executable file", "path", pluginPath)
			continue
		}

		pluginsToLoad = append(pluginsToLoad, struct {
			name string
			path string
		}{name: entry.Name(), path: pluginPath})
	}

	if len(pluginsToLoad) == 0 {
		slog.Debug("No plugins found to load")
		return nil
	}

	// Load plugins concurrently for faster startup
	var wg sync.WaitGroup
	errorsChan := make(chan error, len(pluginsToLoad))

	for _, plugin := range pluginsToLoad {
		wg.Add(1)
		go func(name, path string) {
			defer wg.Done()

			slog.Debug("Loading plugin", "path", path)

			if err := m.loadPlugin(ctx, name, path); err != nil {
				slog.Error("Failed to load plugin", "path", path, "error", err)
				errorsChan <- fmt.Errorf("plugin %s: %w", name, err)
			}
		}(plugin.name, plugin.path)
	}

	// Wait for all plugins to finish loading
	wg.Wait()
	close(errorsChan)

	// Collect all errors
	var loadErrors []error
	for err := range errorsChan {
		loadErrors = append(loadErrors, err)
	}

	slog.Debug("Plugin loading completed",
		"loaded", len(m.plugins),
		"errors", len(loadErrors))

	if len(loadErrors) > 0 {
		return fleeterror.NewInternalErrorf("plugin loading errors: %v", errors.Join(loadErrors...))
	}

	return nil
}

// loadPlugin loads a single plugin
func (m *Manager) loadPlugin(ctx context.Context, name, path string) error {
	cmd := exec.Command(path)

	// Configure hclog level for the go-plugin framework
	hclogLevel := hclog.LevelFromString(m.config.LogLevel)
	if hclogLevel == hclog.NoLevel {
		hclogLevel = hclog.Info
	}

	clientConfig := &plugin.ClientConfig{
		HandshakeConfig:  sdk.HandshakeConfig,
		Plugins:          sdk.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		StartTimeout:     time.Duration(m.config.MaxStartupTimeSeconds) * time.Second,
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "plugin." + name, Level: hclogLevel}),
		Stderr:           os.Stderr,
	}

	client := plugin.NewClient(clientConfig)

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to connect to plugin: %w", err)
	}

	raw, err := rpcClient.Dispense(dispenseDriver)
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to dispense driver interface: %w", err)
	}

	driver, ok := raw.(sdk.Driver)
	if !ok {
		client.Kill()
		return fleeterror.NewInternalError("plugin does not implement Driver interface")
	}

	identifier, err := driver.Handshake(ctx)
	if err != nil {
		client.Kill()
		return fmt.Errorf("plugin handshake failed: %w", err)
	}

	_, caps, err := driver.DescribeDriver(ctx)
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to get driver capabilities: %w", err)
	}

	var discoveryPorts []string
	if provider, ok := driver.(sdk.DiscoveryPortsProvider); ok {
		discoveryPorts = append(discoveryPorts, provider.GetDiscoveryPorts(ctx)...)
	}

	loadedPlugin := &LoadedPlugin{
		Name:           name,
		Path:           path,
		Client:         client,
		Driver:         driver,
		Identifier:     identifier,
		Caps:           caps,
		DiscoveryPorts: discoveryPorts,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if identifier.DriverName == "" {
		client.Kill()
		return fmt.Errorf("plugin %s returned empty driver name — all plugins must declare a DriverName via Handshake", name)
	}

	m.plugins[name] = loadedPlugin

	if existing, exists := m.pluginsByDriverName[identifier.DriverName]; exists {
		delete(m.plugins, name)
		client.Kill()
		return fmt.Errorf("driver name %q already claimed by plugin %s", identifier.DriverName, existing.Name)
	}
	m.pluginsByDriverName[identifier.DriverName] = loadedPlugin

	slog.Debug("Plugin loaded successfully",
		"name", name,
		"driver", identifier.DriverName,
		"version", identifier.APIVersion,
		"capabilities", caps,
		"discovery_ports", discoveryPorts)

	return nil
}

// isExecutable checks if a file has executable permissions.
//
// SECURITY NOTE: This function only checks the executable permission bit and does not
// validate the file's integrity, signature, or contents. Any executable file in the
// plugins directory will be loaded and executed. Ensure proper access controls on the
// plugins directory to prevent unauthorized plugin installation.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode()&0111 != 0
}

// GetPlugin returns a loaded plugin by name
func (m *Manager) GetPlugin(name string) (*LoadedPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.plugins[name]
	return plugin, exists
}

// GetPluginByDriverName returns a plugin registered under the given driver name
func (m *Manager) GetPluginByDriverName(driverName string) (*LoadedPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.pluginsByDriverName[driverName]
	return plugin, exists
}

// HasPluginForDriverName checks if there's a plugin registered under the given driver name
func (m *Manager) HasPluginForDriverName(driverName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.pluginsByDriverName[driverName]
	return exists
}

// GetCapabilitiesForDriverName returns the capabilities of a plugin by driver name.
// Returns nil if no plugin is registered for the given driver name.
func (m *Manager) GetCapabilitiesForDriverName(driverName string) sdk.Capabilities {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.pluginsByDriverName[driverName]
	if !exists {
		return nil
	}
	return plugin.Caps
}

// GetDriverByDriverName returns the SDK driver for a given driver name
func (m *Manager) GetDriverByDriverName(driverName string) (sdk.Driver, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.pluginsByDriverName[driverName]
	if !exists {
		return nil, fleeterror.NewInternalErrorf("no plugin found for driver name: %s", driverName)
	}

	return plugin.Driver, nil
}

// GetPluginByDriverNameWithCapability retrieves a plugin by driver name and verifies it has the required capability.
func (m *Manager) GetPluginByDriverNameWithCapability(driverName string, capability string) (*LoadedPlugin, error) {
	plugin, exists := m.GetPluginByDriverName(driverName)
	if !exists {
		return nil, fleeterror.NewInternalErrorf("no plugin available for driver name %s", driverName)
	}

	if !plugin.Caps[capability] {
		return nil, fleeterror.NewInternalErrorf("plugin %s does not support capability %s", plugin.Name, capability)
	}

	return plugin, nil
}

// GetAllPlugins returns all loaded plugins
func (m *Manager) GetAllPlugins() map[string]*LoadedPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*LoadedPlugin)
	for name, plugin := range m.plugins {
		result[name] = plugin
	}
	return result
}

// RegisterPluginForTest registers a plugin for testing purposes.
// This method is only intended for use in tests and bypasses normal plugin loading.
func (m *Manager) RegisterPluginForTest(plugin *LoadedPlugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin cannot be nil")
	}
	if plugin.Name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.plugins[plugin.Name] = plugin

	if plugin.Identifier.DriverName != "" {
		if _, exists := m.pluginsByDriverName[plugin.Identifier.DriverName]; !exists {
			m.pluginsByDriverName[plugin.Identifier.DriverName] = plugin
		}
	}

	return nil
}

// Shutdown gracefully shuts down all loaded plugins
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Info("Shutting down plugin manager", "count", len(m.plugins))

	var shutdownErrors []error
	for name, plugin := range m.plugins {
		slog.Debug("Shutting down plugin", "name", name)

		plugin.Client.Kill()

		// Create a timer for the grace period that respects the context timeout
		gracePeriod := time.Duration(m.config.ShutdownGracePeriodSeconds) * time.Second
		timer := time.NewTimer(gracePeriod)
		defer timer.Stop()

		select {
		case <-timer.C:
			if !plugin.Client.Exited() {
				slog.Warn("Plugin shutdown timed out after grace period",
					"name", name,
					"grace_period_seconds", m.config.ShutdownGracePeriodSeconds)
				shutdownErrors = append(shutdownErrors, fmt.Errorf("plugin %s shutdown timeout", name))
				continue
			}
			slog.Debug("Plugin exited cleanly", "name", name)
		case <-ctx.Done():
			slog.Warn("Plugin shutdown cancelled due to context timeout", "name", name)
			shutdownErrors = append(shutdownErrors, fmt.Errorf("plugin %s shutdown timeout: %w", name, ctx.Err()))
		}
	}

	m.plugins = make(map[string]*LoadedPlugin)
	m.pluginsByDriverName = make(map[string]*LoadedPlugin)

	if len(shutdownErrors) > 0 {
		return fleeterror.NewInternalErrorf("plugin shutdown errors: %v", errors.Join(shutdownErrors...))
	}

	slog.Info("Plugin manager shutdown completed")
	return nil
}
