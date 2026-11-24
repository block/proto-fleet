package sdk

import (
	"context"
	"fmt"
)

// CapabilityContainerControl denotes command transport support. Each operation
// additionally requires current support and known state from the controller.
const CapabilityContainerControl = "container_control"

// ContainerControlRequest always carries an explicit desired state. For the
// dedicated main circuit breaker, Enabled means closed (false means open).
type ContainerControlRequest struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled"`
}

func (r ContainerControlRequest) Validate() error {
	if r.Enabled == nil {
		return fmt.Errorf("equipment desired state is required")
	}
	switch r.Name {
	case "dry_cooler", "dry_cooler_auto", "tank_led_light", "coolant_pump", "air_conditioner_1", "air_conditioner_2", "main_circuit_breaker":
		return nil
	default:
		return fmt.Errorf("unsupported equipment control %q", r.Name)
	}
}

// DeviceContainerController is optional. Success acknowledges the write;
// callers must obtain a subsequent observation to confirm physical state.
type DeviceContainerController interface {
	SetContainerControl(ctx context.Context, req ContainerControlRequest) error
}
