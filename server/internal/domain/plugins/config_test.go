package plugins

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_GetPluginsDir(t *testing.T) {
	tests := []struct {
		name       string
		pluginsDir string
	}{
		{
			name:       "relative path",
			pluginsDir: "./plugins",
		},
		{
			name:       "absolute path",
			pluginsDir: "/tmp/plugins",
		},
		{
			name:       "current directory",
			pluginsDir: ".",
		},
		{
			name:       "empty path defaults to current dir",
			pluginsDir: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				PluginsDir: tt.pluginsDir,
			}

			absPath, err := config.GetPluginsDir()

			require.NoError(t, err)
			assert.True(t, filepath.IsAbs(absPath), "returned path should be absolute")
		})
	}
}

func TestConfig_GetPluginsDir_FailureScenarios(t *testing.T) {
	t.Run("unreadable current directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Skipping on Windows - different permission model")
		}

		// Create a temporary directory and make it unreadable
		tempDir := t.TempDir()

		// Change to the temp directory
		originalDir, err := os.Getwd()
		require.NoError(t, err)

		t.Chdir(tempDir)

		// Cleanup function to restore state
		t.Cleanup(func() {
			// Restore permissions first, then change directory
			_ = os.Chmod(tempDir, 0755) // nolint:gosec // G302: Restoring permissions
			t.Chdir(originalDir)
		})

		// Remove write and execute permissions from current directory to cause filepath.Abs to fail
		err = os.Chmod(tempDir, 0000) // nolint:gosec // G302: Intentional use of restrictive permissions for test failure scenario
		require.NoError(t, err)

		config := &Config{
			PluginsDir: "relative/path", // This requires resolving against current dir
		}

		absPath, err := config.GetPluginsDir()

		require.Error(t, err)
		assert.Empty(t, absPath)
		assert.Contains(t, err.Error(), "failed to get absolute path for plugins directory")
	})
}

func TestConfig_GetPluginsDir_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		pluginsDir string
		validate   func(t *testing.T, absPath string, err error)
	}{
		{
			name:       "path with spaces",
			pluginsDir: "./plugins with spaces",
			validate: func(t *testing.T, absPath string, err error) {
				require.NoError(t, err)
				assert.True(t, filepath.IsAbs(absPath))
				assert.Contains(t, absPath, "plugins with spaces")
			},
		},
		{
			name:       "path with unicode characters",
			pluginsDir: "./plugins-测试-🔌",
			validate: func(t *testing.T, absPath string, err error) {
				require.NoError(t, err)
				assert.True(t, filepath.IsAbs(absPath))
				assert.Contains(t, absPath, "plugins-测试-🔌")
			},
		},
		{
			name:       "nested relative path",
			pluginsDir: "./nested/deep/plugins/directory",
			validate: func(t *testing.T, absPath string, err error) {
				require.NoError(t, err)
				assert.True(t, filepath.IsAbs(absPath))
				assert.Contains(t, absPath, "nested/deep/plugins/directory")
			},
		},
		{
			name:       "path with dot segments",
			pluginsDir: "./plugins/../plugins/./subdir",
			validate: func(t *testing.T, absPath string, err error) {
				require.NoError(t, err)
				assert.True(t, filepath.IsAbs(absPath))
				// The path should be cleaned/normalized
				assert.Contains(t, absPath, "plugins/subdir")
				assert.NotContains(t, absPath, "../")
				assert.NotContains(t, absPath, "/./")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				PluginsDir: tt.pluginsDir,
			}

			absPath, err := config.GetPluginsDir()
			tt.validate(t, absPath, err)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid configuration",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 5,
				FailOnUnhealthy:            false,
				LogLevel:                   "info",
			},
			expectError: false,
		},
		{
			name: "invalid MaxStartupTimeSeconds - zero",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      0,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "info",
			},
			expectError: true,
			errorMsg:    "MaxStartupTimeSeconds must be positive",
		},
		{
			name: "invalid MaxStartupTimeSeconds - negative",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      -5,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "info",
			},
			expectError: true,
			errorMsg:    "MaxStartupTimeSeconds must be positive",
		},
		{
			name: "invalid ShutdownTimeoutSeconds - zero",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     0,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "info",
			},
			expectError: true,
			errorMsg:    "ShutdownTimeoutSeconds must be positive",
		},
		{
			name: "invalid ShutdownTimeoutSeconds - negative",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     -10,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "info",
			},
			expectError: true,
			errorMsg:    "ShutdownTimeoutSeconds must be positive",
		},
		{
			name: "invalid ShutdownGracePeriodSeconds - zero",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 0,
				LogLevel:                   "info",
			},
			expectError: true,
			errorMsg:    "ShutdownGracePeriodSeconds must be positive",
		},
		{
			name: "invalid ShutdownGracePeriodSeconds - negative",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: -5,
				LogLevel:                   "info",
			},
			expectError: true,
			errorMsg:    "ShutdownGracePeriodSeconds must be positive",
		},
		{
			name: "invalid LogLevel",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "invalid",
			},
			expectError: true,
			errorMsg:    "invalid LogLevel",
		},
		{
			name: "valid LogLevel - debug",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "debug",
			},
			expectError: false,
		},
		{
			name: "valid LogLevel - warn",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "warn",
			},
			expectError: false,
		},
		{
			name: "valid LogLevel - error",
			config: Config{
				PluginsDir:                 "./plugins",
				Enabled:                    true,
				MaxStartupTimeSeconds:      30,
				ShutdownTimeoutSeconds:     10,
				ShutdownGracePeriodSeconds: 5,
				LogLevel:                   "error",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
