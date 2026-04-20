package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	config := &Config{
		PluginsDir:            "./test-plugins",
		Enabled:               true,
		MaxStartupTimeSeconds: 30,
		LogLevel:              "info",
	}

	manager := NewManager(config)

	assert.NotNil(t, manager)
	assert.Equal(t, config, manager.config)
	assert.NotNil(t, manager.plugins)
	assert.Empty(t, manager.plugins)
	assert.NotNil(t, manager.pluginsByDriverName)
	assert.Empty(t, manager.pluginsByDriverName)
}

func TestManager_LoadPlugins_Disabled(t *testing.T) {
	config := &Config{
		Enabled: false,
	}
	manager := NewManager(config)

	ctx := t.Context()
	err := manager.LoadPlugins(ctx)

	require.NoError(t, err)
	assert.Empty(t, manager.GetAllPlugins())
}

func TestManager_LoadPlugins_DirectoryNotExists(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentDir := filepath.Join(tempDir, "non-existent")

	config := &Config{
		PluginsDir: nonExistentDir,
		Enabled:    true,
	}
	manager := NewManager(config)

	ctx := t.Context()
	err := manager.LoadPlugins(ctx)

	require.NoError(t, err)
	assert.Empty(t, manager.GetAllPlugins())

	// Verify directory was created
	_, err = os.Stat(nonExistentDir)
	assert.NoError(t, err)
}

func TestManager_LoadPlugins_EmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()

	config := &Config{
		PluginsDir: tempDir,
		Enabled:    true,
	}
	manager := NewManager(config)

	ctx := t.Context()
	err := manager.LoadPlugins(ctx)

	require.NoError(t, err)
	assert.Empty(t, manager.GetAllPlugins())
}

func TestManager_LoadPlugins_NonExecutableFiles(t *testing.T) {
	tempDir := t.TempDir()

	// Create a non-executable file
	nonExecFile := filepath.Join(tempDir, "not-executable.txt")
	err := os.WriteFile(nonExecFile, []byte("not a plugin"), 0644)
	require.NoError(t, err)

	config := &Config{
		PluginsDir: tempDir,
		Enabled:    true,
	}
	manager := NewManager(config)

	ctx := t.Context()
	err = manager.LoadPlugins(ctx)

	require.NoError(t, err)
	assert.Empty(t, manager.GetAllPlugins())
}

func TestManager_isExecutable(t *testing.T) {
	tempDir := t.TempDir()

	// Test executable file
	execFile := filepath.Join(tempDir, "executable")
	err := os.WriteFile(execFile, []byte("#!/bin/bash\necho test"), 0755)
	require.NoError(t, err)

	assert.True(t, isExecutable(execFile))

	// Test non-executable file
	nonExecFile := filepath.Join(tempDir, "non-executable")
	err = os.WriteFile(nonExecFile, []byte("not executable"), 0644)
	require.NoError(t, err)

	assert.False(t, isExecutable(nonExecFile))

	// Test non-existent file
	assert.False(t, isExecutable(filepath.Join(tempDir, "does-not-exist")))
}

func TestManager_GetPlugin(t *testing.T) {
	manager := NewManager(&Config{})

	// Test getting non-existent plugin
	plugin, exists := manager.GetPlugin("non-existent")
	assert.Nil(t, plugin)
	assert.False(t, exists)

	// Add a mock plugin directly for testing
	mockPlugin := &LoadedPlugin{
		Name: "test-plugin",
		Path: "/test/path",
	}
	manager.plugins["test-plugin"] = mockPlugin

	// Test getting existing plugin
	plugin, exists = manager.GetPlugin("test-plugin")
	assert.Equal(t, mockPlugin, plugin)
	assert.True(t, exists)
}

func TestManager_GetPluginByDriverName(t *testing.T) {
	manager := NewManager(&Config{})

	// Act
	plugin, exists := manager.GetPluginByDriverName("antminer")

	// Assert
	assert.Nil(t, plugin)
	assert.False(t, exists)

	// Arrange
	mockPlugin := &LoadedPlugin{
		Name: "antminer-plugin",
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Act
	plugin, exists = manager.GetPluginByDriverName("antminer")

	// Assert
	assert.Equal(t, mockPlugin, plugin)
	assert.True(t, exists)
}

func TestManager_HasPluginForDriverName(t *testing.T) {
	manager := NewManager(&Config{})

	// Assert
	assert.False(t, manager.HasPluginForDriverName("antminer"))

	// Arrange
	mockPlugin := &LoadedPlugin{
		Name: "antminer-plugin",
	}
	manager.pluginsByDriverName["antminer"] = mockPlugin

	// Assert
	assert.True(t, manager.HasPluginForDriverName("antminer"))
	assert.False(t, manager.HasPluginForDriverName("whatsminer"))
}

func TestManager_GetAllPlugins(t *testing.T) {
	manager := NewManager(&Config{})

	// Test empty manager
	plugins := manager.GetAllPlugins()
	assert.Empty(t, plugins)

	// Add mock plugins
	mockPlugin1 := &LoadedPlugin{Name: "plugin1"}
	mockPlugin2 := &LoadedPlugin{Name: "plugin2"}
	manager.plugins["plugin1"] = mockPlugin1
	manager.plugins["plugin2"] = mockPlugin2

	// Test getting all plugins
	plugins = manager.GetAllPlugins()
	assert.Len(t, plugins, 2)
	assert.Contains(t, plugins, "plugin1")
	assert.Contains(t, plugins, "plugin2")
	assert.Equal(t, mockPlugin1, plugins["plugin1"])
	assert.Equal(t, mockPlugin2, plugins["plugin2"])
}

func TestManager_Shutdown(t *testing.T) {
	manager := NewManager(&Config{})

	// Test shutdown with no plugins
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	err := manager.Shutdown(ctx)
	require.NoError(t, err)

	// Verify maps are cleared
	assert.Empty(t, manager.plugins)
	assert.Empty(t, manager.pluginsByDriverName)
}
