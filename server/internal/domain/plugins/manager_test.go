package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
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
		GRPCServer: func(options []grpc.ServerOption) *grpc.Server {
			if os.Getenv("FLEET_SHUTDOWN_TEST") == "stuck" {
				options = append(options, grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
					if strings.HasSuffix(info.FullMethod, "/Shutdown") {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return handler(ctx, req)
				}))
			}
			return grpc.NewServer(options...)
		},
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
	name := os.Getenv("FLEET_SHUTDOWN_TEST_NAME")
	if name == "" {
		name = "manager-capability-test"
	}
	return &pb.HandshakeResponse{DriverName: name, ApiVersion: "v1"}, nil
}

func (s *managerCapabilityServer) DescribeDriver(ctx context.Context, _ *emptypb.Empty) (*pb.DescribeDriverResponse, error) {
	flags := map[string]bool{}
	if s.supported {
		flags[sdk.CapabilityScopedDeviceReuse] = true
	}
	identifier, err := s.Handshake(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &pb.DescribeDriverResponse{
		DriverName: identifier.DriverName, ApiVersion: identifier.ApiVersion, Caps: &pb.Capabilities{Flags: flags},
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
				Enabled: true, PluginsDir: pluginsDir, MaxStartupTimeSeconds: 10, LogLevel: "error", ShutdownGracePeriodSeconds: 5,
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

func TestManager_ShutdownCleanPluginsWithoutExtraWait(t *testing.T) {
	manager := NewManager(&Config{ShutdownGracePeriodSeconds: 5, MaxStartupTimeSeconds: 10})
	for i := range 3 {
		loadShutdownTestPlugin(t, manager, fmt.Sprintf("clean-%d", i), "clean")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	err := manager.Shutdown(ctx)
	t.Logf("three-plugin shutdown took %s", time.Since(started))
	require.NoError(t, err)
	assert.Empty(t, manager.GetAllPlugins())
	assert.Empty(t, manager.pluginsByDriverName)
	require.NoError(t, manager.Shutdown(ctx))
}

func TestManager_ShutdownStuckPluginDoesNotDelayOtherPlugins(t *testing.T) {
	manager := NewManager(&Config{ShutdownGracePeriodSeconds: 2, MaxStartupTimeSeconds: 10})
	stuck := loadShutdownTestPlugin(t, manager, "stuck", "stuck")
	clean := loadShutdownTestPlugin(t, manager, "clean", "clean")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	started := time.Now()
	go func() { result <- manager.Shutdown(ctx) }()
	// The clean plugin must finish while the stuck plugin is still within
	// its grace period, regardless of map iteration order.
	require.Eventually(t, clean.Client.Exited, time.Second, 10*time.Millisecond)
	assert.False(t, stuck.Client.Exited())
	select {
	case err := <-result:
		require.ErrorContains(t, err, "plugin stuck: shutdown interrupted before process exit: context deadline exceeded")
		assert.NotContains(t, err.Error(), "plugin clean:")
	case <-ctx.Done():
		t.Fatal("shutdown did not finish within the shared budget")
	}
	assert.Less(t, time.Since(started), 4*time.Second)
	assert.True(t, stuck.Client.Exited())
	assert.Empty(t, manager.GetAllPlugins())
	require.NoError(t, manager.Shutdown(ctx))
	assertNoPluginKillGoroutines(t)
}

func TestManager_ShutdownCancelledContextReapsPlugins(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%t", expired), func(t *testing.T) {
			manager := NewManager(&Config{ShutdownGracePeriodSeconds: 5, MaxStartupTimeSeconds: 10})
			loaded := loadShutdownTestPlugin(t, manager, "stuck", "stuck")
			ctx, cancel := context.WithCancel(t.Context())
			wantError := context.Canceled
			if expired {
				cancel()
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				wantError = context.DeadlineExceeded
			}
			cancel()
			started := time.Now()
			err := manager.Shutdown(ctx)
			require.ErrorContains(t, err, wantError.Error())
			assert.Less(t, time.Since(started), time.Second)
			assert.True(t, loaded.Client.Exited())
			require.NoError(t, manager.Shutdown(ctx))
			assertNoPluginKillGoroutines(t)
		})
	}
}

func TestManager_ShutdownAlreadyExitedPluginWithCancelledContext(t *testing.T) {
	manager := NewManager(&Config{ShutdownGracePeriodSeconds: 5, MaxStartupTimeSeconds: 10})
	loaded := loadShutdownTestPlugin(t, manager, "exited", "clean")
	loaded.Client.Kill()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, manager.Shutdown(ctx))
	assert.Empty(t, manager.GetAllPlugins())
}

func assertNoPluginKillGoroutines(t *testing.T) {
	t.Helper()
	var stacks strings.Builder
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(&stacks, 2))
	assert.NotContains(t, stacks.String(), "go-plugin.(*Client).Kill(")
}

func loadShutdownTestPlugin(t *testing.T, manager *Manager, name, mode string) *LoadedPlugin {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), name)
	// Execute this test binary as a real go-plugin subprocess, including the
	// production loading and SDK handshake path.
	script := fmt.Sprintf("#!/bin/sh\nGORACE=atexit_sleep_ms=0 PROTO_FLEET_PLUGIN_MANAGER_TEST_HELPER=1 FLEET_SHUTDOWN_TEST=%s FLEET_SHUTDOWN_TEST_NAME=%s exec '%s' -test.run=^TestManagerPluginHelperProcess$ -- supported\n", mode, name, strings.ReplaceAll(binary, "'", "'\\''"))
	require.NoError(t, os.WriteFile(path, []byte(script), 0700))
	require.NoError(t, manager.loadPlugin(t.Context(), name, path))
	loaded, ok := manager.GetPlugin(name)
	require.True(t, ok)
	t.Cleanup(func() {
		// A regression in Shutdown must not strand the test in Client.Kill's
		// blocked RPC or leave a child behind after the test fails.
		if loaded.process != nil {
			_ = loaded.process.Kill()
		}
		loaded.Client.Kill()
	})
	return loaded
}
