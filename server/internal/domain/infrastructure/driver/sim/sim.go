// Package sim provides a development and test-only infrastructure driver that
// validates commands and logs the requested state without performing I/O.
//
// The adapter is intentionally not registered by the production infrastructure
// registry. Tests that need it must register DriverType explicitly.
package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
)

// DriverType is the registry key for the log-only simulator adapter.
const DriverType = "sim"

// Controller implements driver.Controller without performing device I/O.
type Controller struct {
	logger *slog.Logger
}

var _ driver.Controller = (*Controller)(nil)

// New returns a log-only simulator controller.
func New() driver.Controller {
	return &Controller{logger: slog.Default()}
}

// ValidateConfig accepts only an empty JSON object.
func (Controller) ValidateConfig(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("driver_config for sim must be an empty JSON object")
	}

	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return errors.New("driver_config for sim must be an empty JSON object")
	}
	if len(config) != 0 {
		return errors.New("driver_config for sim does not accept fields")
	}
	return nil
}

// SetState validates the simulator device and logs the requested on/off state.
func (c Controller) SetState(ctx context.Context, device driver.Device, state driver.DesiredState) error {
	if device.ID <= 0 {
		return errors.New("sim device ID must be positive")
	}
	if device.DriverType != DriverType {
		return errors.New("sim controller requires driver type sim")
	}
	if err := c.ValidateConfig(device.DriverConfig); err != nil {
		return fmt.Errorf("sim configuration invalid for device %d: %w", device.ID, err)
	}

	var power string
	switch state.Power {
	case driver.PowerOff:
		power = "off"
	case driver.PowerOn:
		power = "on"
	default:
		return fmt.Errorf("power mode is invalid for device %d", device.ID)
	}

	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(
		ctx,
		"sim infrastructure state set",
		slog.Int64("device_id", device.ID),
		slog.String("power", power),
	)
	return nil
}

// Capabilities reports support for on/off commands.
func (Controller) Capabilities() map[string]bool {
	return map[string]bool{"on_off": true}
}
