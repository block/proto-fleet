package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
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

// The helper runs the test binary as a real go-plugin subprocess. Its wire
// responses emulate both an older SDK runtime and a current scoped runtime.
func TestManagerPluginHelperProcess(t *testing.T) {
	if os.Getenv("PROTO_FLEET_PLUGIN_MANAGER_TEST_HELPER") != "1" {
		return
	}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: sdk.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"driver": &managerCapabilityPlugin{supported: os.Args[len(os.Args)-1] == "supported"},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
	os.Exit(0)
}

type managerCapabilityPlugin struct {
	sdk.DriverPlugin
	supported bool
}

func (p *managerCapabilityPlugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	pb.RegisterDriverServer(server, &managerCapabilityServer{supported: p.supported})
	return nil
}

type managerCapabilityServer struct {
	pb.UnimplementedDriverServer
	supported bool
}

func (s *managerCapabilityServer) Handshake(context.Context, *emptypb.Empty) (*pb.HandshakeResponse, error) {
	return &pb.HandshakeResponse{DriverName: "manager-capability-test", ApiVersion: "v1"}, nil
}

func (s *managerCapabilityServer) DescribeDriver(context.Context, *emptypb.Empty) (*pb.DescribeDriverResponse, error) {
	flags := map[string]bool{}
	if s.supported {
		flags[sdk.CapabilityScopedDeviceReuse] = true
	}
	return &pb.DescribeDriverResponse{
		DriverName: "manager-capability-test", ApiVersion: "v1", Caps: &pb.Capabilities{Flags: flags},
	}, nil
}

func TestManagerLoadRequiresScopedRuntime(t *testing.T) {
	t.Setenv("PROTO_FLEET_PLUGIN_MANAGER_TEST_HELPER", "1")
	testBinary, err := os.Executable()
	require.NoError(t, err)
	for _, capability := range []string{"unsupported", "supported"} {
		t.Run(capability, func(t *testing.T) {
			pluginsDir := t.TempDir()
			pluginPath := filepath.Join(pluginsDir, "runtime")
			quotedBinary := "'" + strings.ReplaceAll(testBinary, "'", "'\"'\"'") + "'"
			script := "#!/bin/sh\nexec " + quotedBinary + " -test.run='^TestManagerPluginHelperProcess$' -- " + capability + "\n"
			require.NoError(t, os.WriteFile(pluginPath, []byte(script), 0700))
			manager := NewManager(&Config{
				Enabled: true, PluginsDir: pluginsDir, MaxStartupTimeSeconds: 10, LogLevel: "error",
			})
			t.Cleanup(func() { require.NoError(t, manager.Shutdown(context.Background())) })
			err := manager.LoadPlugins(t.Context())
			if capability == "unsupported" {
				require.ErrorContains(t, err, sdk.CapabilityScopedDeviceReuse)
				require.ErrorContains(t, err, "rebuild the plugin with the current Fleet SDK")
				require.Empty(t, manager.GetAllPlugins())
				require.False(t, manager.HasPluginForDriverName("manager-capability-test"))
				return
			}
			require.NoError(t, err)
			loaded, ok := manager.GetPluginByDriverName("manager-capability-test")
			require.True(t, ok)
			require.True(t, loaded.Caps[sdk.CapabilityScopedDeviceReuse])
			require.Len(t, manager.GetAllPlugins(), 1)
		})
	}
}
