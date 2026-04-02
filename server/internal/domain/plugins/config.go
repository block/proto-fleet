package plugins

import (
	"path/filepath"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type Config struct {
	PluginsDir string `help:"Directory containing plugin binaries" default:"./plugins" env:"DIR"`

	Enabled bool `help:"Enable plugin system" default:"true" env:"ENABLED"`

	MaxStartupTimeSeconds int `help:"Maximum plugin startup time in seconds" default:"30" env:"MAX_STARTUP_TIME"`

	ShutdownTimeoutSeconds int `help:"Maximum plugin shutdown time in seconds" default:"10" env:"SHUTDOWN_TIMEOUT"`

	ShutdownGracePeriodSeconds int `help:"Grace period for each plugin to exit cleanly in seconds" default:"5" env:"SHUTDOWN_GRACE_PERIOD"`

	FailOnUnhealthy bool `help:"Fail startup if plugin health check fails" default:"false" env:"FAIL_ON_UNHEALTHY"`

	LogLevel string `help:"Plugin log level (debug, info, warn, error)" default:"info" env:"LOG_LEVEL"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.MaxStartupTimeSeconds <= 0 {
		return fleeterror.NewInvalidArgumentErrorf("MaxStartupTimeSeconds must be positive, got %d", c.MaxStartupTimeSeconds)
	}
	if c.ShutdownTimeoutSeconds <= 0 {
		return fleeterror.NewInvalidArgumentErrorf("ShutdownTimeoutSeconds must be positive, got %d", c.ShutdownTimeoutSeconds)
	}
	if c.ShutdownGracePeriodSeconds <= 0 {
		return fleeterror.NewInvalidArgumentErrorf("ShutdownGracePeriodSeconds must be positive, got %d", c.ShutdownGracePeriodSeconds)
	}
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.LogLevel] {
		return fleeterror.NewInvalidArgumentErrorf("invalid LogLevel %q, must be one of: debug, info, warn, error", c.LogLevel)
	}
	return nil
}

// GetPluginsDir returns the absolute path to the plugins directory
func (c *Config) GetPluginsDir() (string, error) {
	absPath, err := filepath.Abs(c.PluginsDir)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("failed to get absolute path for plugins directory: %v", err)
	}
	return absPath, nil
}
